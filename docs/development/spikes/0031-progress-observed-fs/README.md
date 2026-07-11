# Spike 0031 — observed-filesystem progress

**Question.** Can afmpeg surface *live* progress from a running job today, and if
so, by what mechanism? (Prompted by keyrx wanting a progress indicator.)

**Non-starter: stderr.** The engine is a headless libav-direct driver, not the
ffmpeg CLI. It writes to stderr **only on error paths** (`fprintf(stderr, …)`);
the encode loop (`ffmpeg-wasi/src/process.c` `avcodec_receive_packet` →
`av_interleaved_write_frame`) emits **nothing** per frame. There is no `frame=…
time=…` stats line to scrape — that is a CLI feature this engine does not have.
So even with perfect stderr streaming there is no signal to watch.

## The mechanism that works: watch the filesystem

afmpeg *is* the filesystem the engine uses. Every guest read/write syscall is a
Go method call on the caller-supplied `afero.Fs` (via `internal/vfs`, which calls
`af.Read`/`af.Write`/`af.Seek` **incrementally** — see `internal/vfs/file.go`).

So a host-side `afero.Fs` decorator can observe bytes flowing — **input
consumed** and **output produced** — in real time and push them on a channel.
A background goroutine drains that channel while `Runtime.Run` blocks. This is
the goroutine + channel shape, and it needs **no engine change and no afmpeg
change** — it runs entirely through the public API.

- `progressfs.go` — the observing `afero.Fs`/`afero.File` decorator.
- `main.go` — harness: big synthetic WAV → real AAC transcode on the real engine,
  progress drained live on a channel.

```
AFMPEG_TEST_FFMPEG_WASI=/…/ffmpeg-wasi-gpl.wasm \
  go run -tags spike ./docs/development/spikes/0031-progress-observed-fs
```

## Result (300 s WAV → AAC/mp4, real ffmpeg-wasi-gpl engine)

```
  t=  … in [##########..............]  42.3%  out 1.00 MiB
  …smooth, monotonic climb over the whole 45 s run…
  t=45681ms  in [########################] 100.0%  out 2.25 MiB

observed 821 I/O events (808 read, 13 write); first at 0ms, last at 45737ms
engine exit=0  wall=45.737s  output=2.53 MiB  dropped-events=0
VERDICT: live progress is observable at the afero.Fs boundary, today,
         through the public API — no engine or afmpeg change required.
```

## Findings (for the spec)

1. **Input-read position is the good signal.** 808 read events gave a smooth,
   monotonic 0→100% (demuxer consuming the source linearly). For most container
   formats `bytes_read / input_size` ≈ % complete. This is the primary progress
   proxy and it is free.
2. **Output-write is coarse.** 13 chunky events (muxer buffers, then flushes) —
   fine for "bytes produced", useless for smooth percentage.
3. **The sink is on the engine's I/O syscall path** and must never block it —
   buffered channel + non-blocking send (drop/coalesce when full). Back-pressure
   policy is a real API decision (0 dropped here at an 8192 buffer, but that is a
   knob, not a guarantee).
4. **Zero-change baseline.** No modification to afmpeg or ffmpeg-wasi; just wrap
   the `afero.Fs` you already pass to `Run`.

## Limitations → shape the spec, don't block it

- Read-% is **byte progress, not time/ETA**, and not wall-clock. Good enough for a
  progress bar; not a frame/time readout.
- **Seek-heavy demuxers** (mp4 with a trailing `moov`, index scans) make input
  offset non-monotonic; the input file may also be read more than once. WAV/TS/
  raw are clean; needs a `max(seen%)` clamp and per-format caveats.
- **No input file, no signal**: purely generative jobs (lavfi sources) and very
  short ops (probe, `frames`) have little/nothing to observe.
- **Multi-input / concat**: needs summing across inputs weighted by size.

## Two candidate designs for the spec

- **A — observed-fs (this spike).** Host-side, zero-change, ships now. `bytes`
  granularity. Likely enough for keyrx's progress bar. Recommended as the first
  cut. Public shape: `WithProgress(chan Progress)` (or a callback) on `Run`, where
  afmpeg wraps the fs internally and owns the drain goroutine + drop policy.
- **B — engine progress side-channel (future).** Driver emits structured progress
  (frame/time/speed) via an `av_log` callback or explicit writes to a known vfs
  path / host import; the host, being the filesystem, sees each write instantly.
  True frame/time granularity and works for generative inputs — but it is an
  engine (ffmpeg-wasi) change and a job-spec vocabulary concern.

A and B compose: ship A now behind a `Progress` type, add B later as a richer
source behind the same channel.
