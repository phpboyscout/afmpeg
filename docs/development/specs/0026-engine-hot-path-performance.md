# 0026 — engine hot-path performance

Status: **PROPOSED** (external review — decision pending. A commissioned code-level review of
`ffmpeg-wasi/src/process.c` surfaced three concrete within-thread inefficiencies; this spec
frames them as candidate work the user decides whether to adopt. **Design only — nothing is
committed, nothing is implemented.**)
Date: 2026-07-02
Parent: [0008](0008-performance-strategy.md) (the strategy-level perf spike this is the
code-level companion to); [0007](0007-libav-direct-engine.md) (the engine these paths live in)
Source: external commissioned review — `ffmpeg-wasi/performance_review.md` +
`performance_review_supplementary.md`
Owns: **R-ENGINE-PERF** (driver hot-path micro-efficiency, single-thread)

## 1. Why this is its own spec

[0008](0008-performance-strategy.md) owns the **strategy** question — is single-threaded Wasm
encode a problem, and if so which *lever* (instance parallelism, build flags) moves the needle.
It deliberately does **not** look inside `process.c`. The external review did, and found three
inefficiencies that are orthogonal to parallelism: they make the **single thread itself** do
less wasted work, on every job, single- or multi-input. That is a different axis from 0008's,
so it gets its own spec rather than bloating the spike. This spec **cross-references** 0008 for
the measurement framework and 0007 for the engine internals; it does **not** restate the
threads/SIMD ceiling ([0012](0012-feature-parity-roadmap.md) §2 / 0008 §1 own that) or the
stream-copy gap the review also raised ([0013](0013-remux-and-stream-copy.md) owns that).

## 2. Validation — the three findings, confirmed against `src/process.c`

All three were read back against the shipped driver (`ffmpeg-wasi/src/process.c`, engine
`n8.1.2-5`) and are real, not speculative-in-existence (their *payoff* is what needs measuring
— §5):

- **F1 — per-packet `AVFrame` alloc/free in the drain path.** `pull_sinks()` opens with
  `AVFrame *f = av_frame_alloc();` (line 286) and closes with `av_frame_free(&f);` (line 303).
  `feed()` calls `pull_sinks()` for **every packet** it processes (line 323), and the main loop
  calls `feed()` for **every packet read** (line 484) — plus `pull_sinks()` again on each input
  EOF (line 481) and the final drain (line 489). So a full `AVFrame` struct is allocated and
  freed once per demuxed packet: thousands of times a second on real work. Note the main loop
  **already** hoists its `pkt`/`frame` once (lines 461–462) and `feed()` reuses that passed-in
  frame — so `pull_sinks()` is the lone straggler that didn't get the same treatment.
- **F2 — naive round-robin demux.** The main loop (lines 464–487) does
  `for (int i = 0; i < c.n_in; i++) av_read_frame(c.in[i], pkt)` — exactly one packet from each
  input per turn, blind to timestamps. When multi-input jobs have inputs whose PTS/DTS diverge,
  libavfilter must buffer large volumes of *uncompressed* frames from the ahead input while it
  waits for the lagging one. Single-input jobs are structurally unaffected (one input, nothing
  to interleave).
- **F3 — default AVIO buffer size.** Inputs open via `avformat_open_input` (line 398) and
  outputs via `avio_open` (line 451), both using libavformat's default AVIO buffer (~32 KB).
  Every refill/flush is a WASI `fd_read`/`fd_write` that crosses the WASM↔Go boundary through
  afmpeg's afero VFS bridge ([0003](0003-vfs-bridge.md)) — an expensive context switch. A
  high-bitrate stream generates many such crossings.

## 3. Scope

**In:** the three micro-optimisations above — F1 frame reuse, F2 lowest-DTS demux, F3 larger
AVIO buffers. All are **within-single-thread efficiency**: they reduce wasted allocation, wasted
filter-buffer memory, and wasted boundary crossings *on one core*.

**Explicitly NOT in scope:**
- **Parallelism** (instance pool, batching) — that is 0008's lever, a different axis.
- **Intra-encode threads / SIMD / network** — the fixed ceiling (0012 §2); untouched here.
- **Stream copy / `-c copy`** — the review's "forced decode/encode" point is a *capability*
  gap owned by [0013](0013-remux-and-stream-copy.md), not a hot-path tweak.
- Any change to job-spec vocabulary or output correctness.

Licensing impact: **nil** — all three are internal to the MIT `driver.c`; no new lib, no
FFmpeg build-flag change, no variant impact.

## 4. Approach — per-fix mechanics (design, not code)

- **F1 — hoist the drain frame.** Add one reusable `AVFrame *frame` to `Ctx` (allocated once at
  setup, freed once at teardown alongside the existing cleanup at lines 531–534). `pull_sinks()`
  uses it and calls `av_frame_unref()` between sinks/iterations — which it *already does* at line
  298, so the change is purely swapping the per-call alloc/free for a shared, unref'd handle.
  Lowest-risk of the three; a mechanical, local edit.
- **F2 — lowest-DTS demux.** Replace the flat round-robin with: peek the next packet of each
  non-EOF input and read from the input whose next packet has the lowest DTS (falling back to
  PTS), mirroring native ffmpeg's `ffmpeg_demux` behaviour. Requires a one-packet lookahead per
  input (buffer the peeked packet, or use the demuxer's queued-packet position). Bounded
  complexity, but the most **logic** of the three — needs care around EOF/flush ordering (the
  existing per-input flush at lines 471–480 must still fire exactly once per input).
- **F3 — larger AVIO buffer.** Allocate a custom buffer (candidate 256 KB–1 MB) and install it:
  for inputs, open with a pre-sized `AVIOContext` (or set the format's `avio_buffer_size` before
  open); for outputs, size the `pb` buffer at `avio_open` time. This **interacts with the VFS
  bridge** — it trades fewer, larger `fd_read`/`fd_write` crossings for more guest memory held
  per stream. The most **speculative** of the three: its win is entirely a function of how
  expensive one afero-bridge crossing actually is, which we have never measured.

## 5. Measurement — a fix without a measured win is not worth its complexity

This is the load-bearing section. These are cheap and low-risk, but "plausible" is not
"measured." Each fix must show a real, repeatable improvement on a representative workload
before it lands — otherwise it is complexity for a rounding error.

- **Pair with 0008's spike harness.** 0008 (§3) stands up the perf-measurement rig (workloads,
  native baseline, per-axis table). This spec adds a **micro-benchmark** dimension to that rig:
  the same job run with each fix on/off, on the same host, reporting wall-clock + peak guest
  memory (F1/F3) and peak filter-buffer occupancy (F2).
- **Targeted workloads per fix:** F1 — a long, packet-dense job (audio-heavy or long duration)
  where per-packet alloc count is highest. F2 — a multi-input job with **deliberately divergent
  PTS** (e.g. a long video + a short offset audio), measuring peak memory, since F2's payoff is
  memory-not-time. F3 — a high-bitrate single-input transcode, sweeping buffer size
  (32 KB → 256 KB → 1 MB) to find the knee, since F3's payoff *is* the bridge-crossing cost.
- **Bar:** a fix ships only if it shows a measured, repeatable win that justifies its diff.
  F1 is expected to pay its (tiny) way easily; F3 is the one most likely to measure as noise —
  and if it does, it is dropped, not shipped on faith.

## 6. Decisions & open questions

- **D-0026-A — three independent fixes, adopt à la carte.** They share no code; each stands or
  falls on its own measurement. No all-or-nothing bundle.
- **D-0026-B — F1 is the anchor.** Lowest risk, clearest mechanism, mechanical diff — the one to
  do first (or alone) if only one is adopted.
- **D-0026-C — F3 gated hardest on measurement.** It is the most speculative and the only one
  that trades memory for speed and touches the VFS-bridge seam; it does not land without a clear
  measured knee.
- **D-0026-D — this work lives in `ffmpeg-wasi` (`driver.c`), released as a build-revision bump**
  (e.g. `n8.1.2-N`, per 0007 §7); afmpeg only re-pins. Design owned here for continuity with the
  0008 perf narrative.
- **OQ-1** Does F2 matter for any *real* consumer job, or only pathological PTS divergence? keyrx
  reels are multi-input — worth checking their actual PTS spread before investing in F2.
- **OQ-2** What buffer size is the F3 sweet spot, and does it regress the small-file / many-file
  case (where 1 MB per stream is pure overhead)? Possibly size-adaptive.
- **OQ-3** Is there value in a peek-buffer abstraction for F2 that a later stream-copy path
  ([0013](0013-remux-and-stream-copy.md)) could reuse?

## 7. Requirements

- **R-ENGINE-PERF** — Reduce wasted per-packet allocation (F1), uncompressed-frame buffering
  under divergent multi-input timestamps (F2), and WASM↔Go I/O boundary crossings (F3) in the
  `process.c` hot path — **each only if measured (§5) to earn its complexity**, correctness and
  job-spec vocabulary unchanged. Within-single-thread only; parallelism stays R-AF-12 (0008).

## 8. Test surface

- **Correctness is the invariant.** The existing integration suite (single-/multi-input,
  multi-output — e.g. `TestIntegration_RunJob`, `TestIntegration_RunJob_MultiOutput`) must pass
  **byte-comparable or quality-equivalent** before and after each fix; these are efficiency
  changes, output must not move. F2 especially needs the multi-input + EOF-flush paths re-proven.
- **A perf micro-benchmark harness** (§5), built as an extension of 0008's rig: per-fix on/off,
  reporting wall-clock, peak guest memory, and (F2) peak filter-buffer occupancy, over the
  targeted workloads. The harness output *is* the adoption evidence for §6.

## 9. Dependencies

- **[0008](0008-performance-strategy.md)** — provides the measurement framework (workloads,
  native baseline, methodology) this spec's micro-benchmarks plug into. This spec should not run
  ahead of the 0008 spike standing that rig up; the two are measured together.
- **[0007](0007-libav-direct-engine.md)** — the engine/driver these paths live in.
- **[0003](0003-vfs-bridge.md)** — the afero↔WASI bridge whose per-crossing cost determines F3's
  entire payoff.

## 10. Definition of done

Each of F1/F2/F3 has a **measured** verdict from the 0008-paired harness (§5): adopted (with the
number that justified it) or rejected (with the number that killed it), recorded here. Adopted
fixes ship in an `ffmpeg-wasi` build-revision release with the correctness suite green and
afmpeg re-pinned; rejected fixes are documented as "measured, not worth it" so the question is
closed, not merely deferred. If **none** clear the bar, that is a legitimate outcome and
R-ENGINE-PERF closes with the measurements as its rationale.
