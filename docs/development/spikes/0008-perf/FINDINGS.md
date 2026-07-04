# Spec 0008 — findings & go/no-go

The interpretation of [`REPORT.md`](REPORT.md) (a captured run; reproduce it on your own host
with `cmd/afmpeg-bench`). Numbers below are from that run — linux/amd64, 8 CPUs, native
ffmpeg 6.1.1, current `n8.1.2` intermediate modules.

## What the numbers say

1. **The WASM penalty is an *encoder* penalty, not a general one.**
   - Thumbnail (decode → scale → 1 frame) is only **2.4× native** — decode and filtering in WASM
     are near-native.
   - Transcode (encode-dominated) is **13–63× native**, and the filter-heavy reel up to **150×**.
   - So the whole gap lives in the H.264 encoder — single-threaded, `--disable-asm` (no SIMD) —
     exactly the two levers that are structurally unavailable in WASM ([0008](../../specs/0008-performance-strategy.md) §1).

2. **Preset choice is the single biggest free lever.** On transcode, x264 `ultrafast` is
   **13.1×** native vs `medium` at **54.9×** — a ~4× swing in WASM wall-clock (3.1 s vs 10.8 s)
   from one option, no code.

3. **openh264 is ~4× faster than libx264 in WASM** (2.35 s vs 9.75 s on transcode) — so the LGPL
   default is also the faster encode path, at a quality/size cost.

4. **Instance-level parallelism works.** A pool of 8 Runtimes clears an 8-job batch **5.0×**
   faster than one Runtime — the only real parallelism absent WASM threads (0008 §4.1), and it
   delivers close to core-count scaling for batch throughput.

5. **Absolute times are small for small media.** A 160p transcode is ~3 s at `ultrafast`; the
   in-memory/sandboxed path afmpeg was scoped to ([0001](../../specs/0001-afmpeg.md) §9) is rarely
   the hot path (keyrx keeps native ffmpeg as its local fast path).

## Go/no-go (R-AF-12)

**Do not promote a dedicated perf-optimisation implementation spec now.** The levers that matter
are either free guidance or already expressible:

| Lever (0008 §4) | Verdict |
|---|---|
| **Encoder/preset guidance** | **Adopt now (docs).** openh264 for the LGPL path; faster x264 presets. Biggest win per effort, zero code. |
| **Instance-level parallelism (RuntimePool)** | **Ready lever, gated on a batch consumer.** 5× on 8 cores is real, but no consumer is throughput-bound today; a `Runtime` pool is already expressible by the caller. Promote to a first-class helper only when a batch/throughput consumer appears. |
| **Build tuning (`-Oz` → `-O2/-O3`)** | **Not measured** (needs a rebuilt module). A follow-up *only* if guidance + fleet prove insufficient. |
| **Native backend ([0028](../../specs/0028-native-subprocess-backend.md))** | **The real answer for the genuinely encode/HW-bound consumer.** A 50–150× encode gap is decisive when it matters; the CGO-free native driver (threads + SIMD + HW-accel) is the escape hatch — and is the next track already in flight. |

**Disposition:** single-threaded WASM encode is **13–63× native (150× filter-heavy), but decode/
filter is ≈ native (2.4×)**. This is acceptable for afmpeg's scoped in-memory/sandboxed use with
**preset + encoder guidance**; instance-level parallelism is the ready throughput lever (gated on a
batch consumer); and the **native backend (0028)** is the escape for anyone genuinely perf- or
HW-bound. R-AF-12 closes with these measurements as the rationale — no speculative optimisation
spec is warranted.

## Side observation (not a blocker)

In one early run at 640×480, the reel job's mixed-audio path (`amix` → aac) exited non-zero with
`aac … 2 frames left in the queue on closing` (the audio encoder not fully drained at EOF). It did
**not** reproduce at 320×240 (the reel succeeded on both encoders in the captured run), so it is an
intermittent edge, not a confirmed defect — but worth an engine glance under
[0026](../../specs/0026-engine-hot-path-performance.md) (encoder-flush on filtered-audio EOF).
