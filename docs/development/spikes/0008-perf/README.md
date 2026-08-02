# Spike — spec 0008 performance measurement rig

Answers the [0008](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0008-performance-strategy) question — *"is single-threaded
WASM encode actually a problem, and by how much versus native?"* — with **measured numbers**
instead of assumptions.

The rig is [`cmd/afmpeg-bench`](../../../../cmd/afmpeg-bench). It runs a handful of
representative workloads through afmpeg's WASM runtime and against the host's native `ffmpeg`,
on the same host and the same jobs, and emits [`REPORT.md`](REPORT.md).

## What it measures

- **Per-workload timings** (transcode, reel-shaped xfade+amix, thumbnail) across the encoder
  axis — WASM openh264, WASM libx264, native libx264 — with the **native multiple** (the honest
  WASM-vs-native ratio on the *same* encoder).
- **x264 preset sweep** (ultrafast/veryfast/medium) — WASM vs native.
- **Fleet throughput** — N jobs serial on one `Runtime` vs across a pool of `Runtime`s
  (instance-level parallelism, the only real parallelism lever absent WASM threads — 0008 §4.1).
- **Thumbnail** (the `frames` op) — decode + scale with minimal encode, to separate decode/filter
  cost from encode cost.

## Run it

Needs a built [ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) module (or two, for the encoder
axis) and a native `ffmpeg` on `PATH`:

```sh
go run ./cmd/afmpeg-bench \
  -lgpl ../ffmpeg-wasi/dist/ffmpeg-wasi-lgpl.wasm \
  -gpl  ../ffmpeg-wasi/dist/ffmpeg-wasi-gpl.wasm \
  -runs 3 -batch 16 \
  -out docs/development/spikes/0008-perf/REPORT.md
```

Flags: `-lgpl`/`-gpl` (module paths — at least one required), `-native` (ffmpeg binary, default
`ffmpeg`), `-runs` (timed repetitions, median reported), `-batch` (fleet job count), `-out`
(report path; stdout always). The fixture is synthesised with native ffmpeg (`testsrc2` + `sine`),
so no media assets are checked in.

## Reading it

The `REPORT.md` in this directory is a captured run — reproduce it on your own host for numbers
that reflect your CPU. The **native multiple** is the number that matters: it is an *upper bound*
on the WASM penalty (native uses all cores + SIMD; WASM is single-threaded + `--disable-asm`), so
a workload whose multiple is small is comfortably fine in WASM, and the fleet speedup shows how
much of a throughput gap instance-level parallelism recovers.
