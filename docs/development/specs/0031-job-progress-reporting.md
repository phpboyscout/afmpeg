# 0031 — job progress reporting

Status: **IMPLEMENTED (phase A)** — phase B deferred to its own downstream spec/PR (§5). Phase A
shipped as `afmpeg.WithProgress` + the observed-fs reporter (`pkg/afmpeg/progress.go`), with
unit + gated-integration coverage; the decision-bearing questions for A (D1 delivery, D7 native)
are resolved (§7).
Date: 2026-07-11
Parent: [0004](0004-runtime-and-api.md) (the `Run`/`RunJob`/`Frames` public surface this extends);
[0003](0003-vfs-bridge.md) (the vfs bridge — the observation point design A exploits);
[0007](0007-libav-direct-engine.md) (the job-spec/engine contract design B extends)
Owns: **R-PROGRESS** — live, in-flight progress reporting for a running job.

Validated by the spike in [`spikes/0031-progress-observed-fs/`](../spikes/0031-progress-observed-fs/)
(observed-fs progress against the real `ffmpeg-wasi-gpl` engine).

## 1. Why

A consumer (keyrx) wants a **progress indicator** for a running job — today afmpeg
returns nothing until the invocation completes and hands back a `Result`. There is no
in-flight signal.

The obvious source — the ffmpeg CLI's `frame=… time=… speed=…` stderr line — **does not
exist here**. The engine is a headless libav-direct driver (0007), not the CLI. It writes
to stderr **only on error paths** (`fprintf(stderr, …)`); the encode loop in
`ffmpeg-wasi/src/process.c` (`avcodec_receive_packet` → `av_interleaved_write_frame`)
emits nothing per frame. Even with perfect stderr streaming there is no signal to watch.
So progress has to come from somewhere else.

## 2. Scope

**In:**
- A public `Progress` value and a way to receive a stream of them during one invocation,
  covering `Run` / `RunJob` / `Frames`.
- **Design A** — *observed filesystem* progress: host-side, ships first, no engine change.
- **Design B** — *engine progress side-channel*: richer frame/time/speed, a later change to
  ffmpeg-wasi, delivered behind the **same** `Progress` stream.
- Back-pressure, monotonicity, and multi-input policy for the reported number.

**Out (referenced, not owned):**
- Wall-clock **ETA** as a guarantee — we report best-effort completion, not a promised
  time-remaining (a consumer may derive an ETA from the fraction + elapsed).
- Progress for `Probe` (sub-second; not worth instrumenting).
- Cancellation — already covered by `ctx` (0027); progress is orthogonal.
- Native backend (0028) parity for design A — see D7.

## 3. The two designs and the plan (A → B)

Both feed the same public `Progress` stream; they differ only in the *source* and the
*fidelity* of the number.

- **A — observed filesystem (host-side, zero engine change).** afmpeg already implements
  the filesystem the engine reads/writes through: `internal/vfs` calls the caller's
  `afero.Fs` `Read`/`Write`/`Seek` **incrementally** (`internal/vfs/file.go`). Wrapping that
  `afero.Fs` lets the host watch **input consumed** and **output produced** in real time.
  Progress fraction = `bytes_read(input) / input_size`. The spike proved this: 808 read
  events gave a smooth, monotonic 0→100 % across a 45 s real AAC encode, entirely through
  the public API. Granularity is **bytes**, not frames/time.

- **B — engine progress side-channel (a later ffmpeg-wasi change).** The driver emits
  structured progress — frames encoded, `out_time`, `speed` — which the host surfaces on
  the same stream. Fraction = `out_time / duration` (more accurate than byte-ratio, and it
  works for cases design A can't: generative/lavfi inputs, seek-heavy demuxers). This is an
  engine change and touches the 0007 contract.

**Decision (D0, resolved 2026-07-11 with the user): ship A first, then B.** A satisfies the
progress-bar need now with no engine work; B is a fidelity upgrade delivered behind an
unchanged public API. The `Progress` type is designed up front (§6) so B adds fields/sources
without a breaking change.

## 4. Design A — observed filesystem

### 4.1 Mechanism

A host-side `afero.Fs` decorator wraps the fs the caller passes to the invocation. Its files
count bytes crossing `Read`/`ReadAt` (input consumed) and `Write`/`WriteAt` (output
produced), tagged by path, and hand deltas to afmpeg's progress plumbing. Input totals come
from `Stat` at open time. **No change to `internal/vfs`, the engine, or the job spec** — the
decorator sits between afmpeg and the caller's fs, and afmpeg owns it (the caller does not
wrap anything).

### 4.2 The reported number

- **Primary signal: input read position.** `sum(bytes_read over input files) /
  sum(input_size)`. For linear demuxers (wav, mpegts, raw, most streams) this tracks
  completion closely and monotonically.
- **Output bytes** are reported too (`OutputBytes`) but are **coarse** — the muxer buffers
  and flushes in chunks (the spike saw 13 write events vs 808 reads). Useful for "bytes
  produced", not for a smooth bar.
- **Monotonic clamp (D3).** Some demuxers seek (mp4 with a trailing `moov`, index scans) or
  re-read an input, so raw `bytes_read` is not monotonic. afmpeg reports `Fraction =
  max(fraction seen so far)` clamped to `[0,1]` so the bar never goes backwards.
- **Multi-input (D4).** Fraction is size-weighted across all declared input files
  (`sum read / sum size`). Concat and multi-input jobs get a single blended fraction.

### 4.3 Limits (honest, and why B exists)

- Byte-progress, not time/ETA; not wall-clock.
- **Generative inputs** (lavfi sources with no input file) and **very short ops** produce
  little or nothing to observe → `Fraction == -1` (unknown). afmpeg still emits `Elapsed`
  and any `OutputBytes` so a consumer can show an indeterminate spinner.
- Seek-heavy inputs degrade the fraction's accuracy (clamped, so safe but lumpy).
- **Sequential multi-input (concat), found in impl:** the denominator is discovered lazily as
  inputs open, and the monotonic clamp (D3) interacts with size-weighting (D4) — finishing an
  early input can hold the fraction near a plateau until later, larger inputs are opened and
  read. Safe (never regresses), but lumpy; phase B's `out_time/duration` removes it.

These are precisely the cases design B fixes.

## 5. Design B — engine progress side-channel (later)

The driver emits structured progress during the encode loop. Candidate transports (a D for
B's own implementation, not settled here):

- **B-i — a progress file in the vfs.** The driver writes NDJSON progress records (`frame`,
  `out_time_us`, `total_size`, `speed`) to a well-known vfs path (e.g. `/dev/afmpeg-progress`
  served by the vfs, like the existing `/dev/null` and `/dev/urandom`). The host **is** the
  filesystem, so it sees each write instantly — same observation seam as design A, richer
  payload. **Preferred:** it reuses A's plumbing and needs no new host-import ABI.
- **B-ii — a host import function** in the `env` module the driver calls with progress
  numbers (synchronous upcall into Go). Most direct, but a new ABI surface beside setjmp.
- **B-iii — an `av_log` callback** in the driver formatting a stats line to stderr/a stream.
  Rejected in spirit — it recreates the CLI-scrape coupling this spec set out to avoid.

B's fraction (`out_time / duration`) supersedes A's byte-ratio **when present**; afmpeg
prefers the engine number and falls back to the observed-fs number. Because B changes what
the engine emits, it **may bump the job-spec vocabulary version** (0007 §4) if the driver
gates the side-channel behind a spec field; a purely additive `/dev/…` write that older hosts
ignore need not. Settle in B's implementation spec/PR.

## 6. Public API (the `Progress` type and delivery)

### 6.1 The value

```go
// Progress is one best-effort progress sample for an in-flight invocation.
type Progress struct {
    // Fraction is completion in [0,1], or -1 when it cannot be determined
    // (e.g. a generative input under design A).
    Fraction float64

    Elapsed     time.Duration // since the invocation began
    InputBytes  int64         // bytes read from inputs so far (design A)
    InputTotal  int64         // total input size, 0 if unknown
    OutputBytes int64         // bytes written to outputs so far

    // Populated only by design B (zero under A):
    Frame   int64         // frames processed
    OutTime time.Duration // media timestamp reached
    Speed   float64       // ×realtime encode speed
}
```

One type spans both designs: A fills the byte/`Fraction` fields; B adds `Frame`/`OutTime`/
`Speed` and a better `Fraction` — **no breaking change** when B lands (D0).

### 6.2 Delivery — how a caller receives the stream (D1, the main open decision)

Progress is **per-invocation** (the `Runtime` is shared and serialised on `Run`), so it
cannot hang off `New`. Three shapes considered:

- **D1-a — context-scoped channel (recommended).** `ctx = afmpeg.WithProgress(ctx, ch)`;
  `Run`/`RunJob`/`Frames` all honour it with **no signature change**. afmpeg wraps the fs,
  owns the drain goroutine, sends best-effort on `ch`, and **never closes it** (the caller
  owns the channel's lifecycle); it simply stops sending when the call returns. One seam
  covers every call variant. Matches the user's "goroutine + channel" intuition.
- **D1-b — a per-call option / new method** (`RunJob(ctx, fs, cmd, afmpeg.WithProgress(ch))`).
  Explicit, but `Run`'s variadic `...string` makes options awkward and it multiplies
  signatures across Run/RunJob/Frames.
- **D1-c — a callback** `func(Progress)`. Lowest consumer footgun (no goroutine to manage),
  but afmpeg then calls user code on the drain path; a channel is what was asked for.

**Recommendation: D1-a**, with the channel semantics in D2.

### 6.3 Back-pressure (D2)

The observation point runs on the engine's I/O syscall path; **it must never block the
engine.** afmpeg's internal sink does a **non-blocking send** (buffered channel, newest
sample wins / drop when full) onto a drain goroutine, which forwards to the caller's channel.
A slow consumer loses intermediate samples, never correctness, and never stalls the encode.
The spike dropped 0 events at an 8192 buffer, but that is a tuning knob, not a guarantee.

## 7. Decisions

- **D0 — Ship A, then B, behind one `Progress` stream.** *Resolved 2026-07-11 (user).*
- **D1 — Delivery shape = context-scoped channel** `afmpeg.WithProgress(ctx, ch)`, honoured
  by `Run`/`RunJob`/`Frames` with no signature change; afmpeg wraps the fs, owns the drain
  goroutine, sends best-effort, and never closes the caller's channel (D2). *Resolved
  2026-07-11 (user).*
- **D2 — Back-pressure = non-blocking, newest-wins, drop-on-full; afmpeg owns the goroutine;
  never blocks the engine; never closes the caller's channel.** *Proposed.*
- **D3 — Fraction is monotone-clamped** (`max` seen, `[0,1]`) so a seeking demuxer can't
  rewind the bar. *Proposed.*
- **D4 — Multi-input fraction is size-weighted** across declared inputs. *Proposed.*
- **D5 — Unknown progress is `Fraction == -1`**, with `Elapsed`/`OutputBytes` still emitted
  (indeterminate-spinner support). *Proposed.*
- **D6 — `Probe` is out of scope** (sub-second). *Proposed.*
- **D7 — Native backend (0028) support.** Design A observes the `afero.Fs`, but the native
  backend bridges I/O over IPC, not the vfs — so A's observation point differs there.
  **Design A is WASM-backend only; the native backend gets progress via design B's
  side-channel** (it already speaks a driver IPC framing). *Resolved 2026-07-11 (user).*

## 8. Open questions

- **B transport** — `/dev/afmpeg-progress` vfs file (B-i, preferred) vs host import (B-ii),
  and whether B bumps the vocab version (§5). Deferred to B's own implementation spec.
- **Buffer size / coalescing default** for D2 (8192 was fine in the spike; pick a shipping
  default at implementation time).

## 9. Requirements & acceptance

- **R-PROGRESS-1 (A)** — during a `Run`/`RunJob`/`Frames` on the WASM backend, a caller
  receiving the `Progress` stream observes ≥1 sample with rising `Fraction` **before** the
  call returns, for a job with a linear file input. (The spike already demonstrates this;
  the acceptance test asserts monotonic, in-flight delivery.)
- **R-PROGRESS-2 (A)** — the progress path never blocks or fails the invocation: a job run
  with a non-draining channel completes with the identical `Result` as one run without
  `WithProgress` (drop-on-full, D2).
- **R-PROGRESS-3 (A)** — `Fraction` is within `[0,1]` and non-decreasing across a run
  (D3); a generative-input job yields `Fraction == -1` without error (D5).
- **R-PROGRESS-4 (B, later)** — with an engine that emits the side-channel, `Progress`
  carries non-zero `Frame`/`OutTime`/`Speed` and `Fraction` derives from `out_time/duration`;
  the public API and the consumer code are unchanged from A.
- Test-first from these contracts, table-driven, `t.Parallel()`, ≥90 % on new `pkg/` code;
  a gated integration test (`AFMPEG_TEST_FFMPEG_WASI`) exercises R-PROGRESS-1/2/3 against the
  real engine (the spike is the seed).
