# 0032 — engine progress side-channel (0031 phase B)

Status: **APPROVED** — decision-bearing questions resolved with the user 2026-07-12 (D-B1 device,
D-B3 vocab-v9 gating); cleared to implement across ffmpeg-wasi then afmpeg.
Date: 2026-07-12
Parent: [0031](0031-job-progress-reporting.md) (implements its phase B / decision D0);
[0003](0003-vfs-bridge.md) (the vfs device seam this rides); [0007](0007-libav-direct-engine.md)
(the job-spec vocabulary this bumps)
Owns: **R-PROGRESS-B** — engine-emitted frame/time/speed progress, behind the [0031](0031-job-progress-reporting.md) `Progress` stream.

## 1. Why

[0031](0031-job-progress-reporting.md) phase A shipped host-side progress by watching bytes cross
the `afero.Fs` bridge (`Fraction = bytes_read / input_size`). It works and needs no engine change,
but it is **byte progress, not time** (0031 §4.3): no `frame`/`out_time`/`speed`, no accurate ETA,
and `Fraction == -1` for generative/lavfi inputs and short ops. keyrx wants the richer readout.

The engine already produces exactly these numbers internally — it just never surfaces them (the
driver is headless; the encode loop in `ffmpeg-wasi/src/process.c` emits nothing per frame). Phase B
gives the driver a **cheap, one-way side-channel** to report them, and afmpeg surfaces them on the
**same** `WithProgress` channel — no public API change (0031 §6.1 reserved `Frame`/`OutTime`/`Speed`
for exactly this).

## 2. Scope

**In:**
- **ffmpeg-wasi:** the `process` op emits periodic progress records from its encode/mux loop.
- **afmpeg:** a `/dev/afmpeg-progress` vfs device that parses those records and feeds the existing
  `progressReporter`; `Progress.Frame/OutTime/Speed` populated; `Fraction` derived from
  `out_time / duration` when available.
- The job-spec vocabulary bump that gates engine emission on host opt-in.

**Out (referenced, not owned):**
- The public `Progress` type / `WithProgress` delivery / back-pressure — unchanged from
  [0031](0031-job-progress-reporting.md) (this only adds a richer *source*).
- The `frames` and `probe` ops (short; no progress — 0031 D6).
- Native backend parity — the native driver reports over its own IPC framing (0031 D7); this spec
  is the **WASM** side. The record schema is shared so native can adopt it later.

## 3. Design — the `/dev/afmpeg-progress` device (transport B-i)

The vfs already serves synthetic devices (`/dev/null` discards writes, `/dev/urandom` serves reads;
`internal/vfs/null.go`, `random.go`). Phase B adds a **write-only** device by the same pattern:

- **Engine:** when the job asks for progress (D-B3), the driver opens `/dev/afmpeg-progress` and,
  from the encode loop, writes one **NDJSON** record per flush interval:
  `{"frame":N,"out_time_us":T,"total_size":B}` (`\n`-terminated; `speed` is host-derived, see D-B5).
  Best-effort: if the open fails, the driver proceeds silently (it never blocks the encode on
  progress).
- **afmpeg:** `progressFile` (a new `experimentalsys.File`) whose `Write` parses complete NDJSON
  lines and calls a per-invocation callback; partial lines buffer until their newline. The callback
  updates the same `progressReporter` (0031) that already owns the drain goroutine, non-blocking
  send, and monotonic clamp.
- **Precedence (D-B4):** when engine records arrive, they supersede the observed-fs byte-fraction —
  `Fraction = out_time / duration` (duration from the input probe), and `Frame/OutTime/Speed` are
  filled. The observed-fs path (phase A) remains as the fallback when no engine record has arrived
  yet (early startup) or for a runtime/engine too old to emit.

This reuses the entire phase-A host pipeline; the only genuinely new host code is the device + line
parser.

## 4. Decisions

- **D-B1 — Transport = a `/dev/afmpeg-progress` vfs write-device** (0031 §5 B-i), not a host-import
  function (B-ii) or an `av_log` stderr scrape (B-iii). It reuses the proven device seam, needs no
  new host-ABI beside setjmp, and keeps the engine one-way and non-blocking. *Resolved 2026-07-12
  (user).*
- **D-B2 — Emission cadence.** The driver emits at most once per **~100 ms of media time** (and one
  final record at EOF), not per frame — enough for a smooth bar, cheap enough to ignore. afmpeg
  coalesces further (0031 D2 newest-wins). *Proposed (final value picked during the engine change).*
- **D-B3 — Gating + vocab bump to v9.** The driver emits **only** when the job spec sets
  `progress: true` (a new top-level field). afmpeg sets it **only** when `WithProgress` is active
  *and* the engine's vocab ≥ v9 (the existing preflight handshake knows the engine's version), and
  only then serves the device. This avoids an older host having `/dev/afmpeg-progress` routed to its
  real backing fs (a spurious file), and an older engine rejecting a too-new spec. Additive,
  version-gated, one vocab increment. *Resolved 2026-07-12 (user).*
- **D-B4 — Engine numbers win when present**; observed-fs byte-fraction is the fallback (§3).
  *Proposed.*
- **D-B5 — Record schema:** `frame` (int), `out_time_us` (int µs), `total_size` (int bytes, → a
  refined `OutputBytes`). **`speed` is derived host-side**, not emitted: the WASI engine has no
  advancing wall clock (the same reason `av_get_random_seed`'s `clock()`-jitter loop hangs — see
  `internal/vfs/random.go`), so it cannot measure ×realtime. afmpeg computes
  `Speed = OutTime / Elapsed` from the engine's `out_time` and its own wall clock (the reporter
  already tracks `Elapsed`). Schema shared with the future native side (D7); unknown/absent fields
  tolerated. *Refined during impl 2026-07-12: speed moved host-side (engine is clockless).*

## 5. Open questions

- **D-B1 confirm** — `/dev/afmpeg-progress` device vs host-import. Recommend the device.
- **`out_time` origin for multi-output jobs** — which output's timeline drives `Fraction` when a
  job writes several? (First mapped output, or max?) Settle in impl.
- **Cadence value** — 100 ms media-time vs a frame-count stride; pick during the engine change.
- **Does `total_size` deprecate phase A's observed output bytes?** Likely yes for `OutputBytes`
  when present; keep observed-fs for inputs.

## 6. Requirements & acceptance

- **R-PROGRESS-B1** — for a multi-second WASM `process` job with `WithProgress` active against a
  v9+ engine, samples carry monotonically rising `OutTime` and non-zero `Frame`/`Speed`, and
  `Fraction` derives from `out_time/duration` (verified within tolerance against the known input
  duration).
- **R-PROGRESS-B2** — a generative/lavfi input (no readable input file, `Fraction == -1` under
  phase A) now reports a real `Fraction` from `out_time/duration`.
- **R-PROGRESS-B3** — compatibility: an engine below v9 (or `WithProgress` inactive) behaves exactly
  as today — no `/dev/afmpeg-progress` served, no `progress` field sent, phase-A fallback intact,
  identical `Result`.
- **R-PROGRESS-B4** — the side-channel never blocks or fails the job: a malformed/partial record is
  dropped, and progress I/O is best-effort on both sides (0031 D2).
- Cross-repo: the engine change lands in **ffmpeg-wasi** (new engine build, vocab v9) and is
  consumed by afmpeg via `WithModuleRelease`; afmpeg tests gate on `AFMPEG_TEST_FFMPEG_WASI`
  pointing at a v9 build. Test-first from these contracts; ≥90 % on new `pkg/` code.
