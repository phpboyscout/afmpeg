# Spike — Backend B custom-AVIO/IPC bridge (spec 0028 §5.2)

Proves the [0028](../../specs/0028-native-subprocess-backend.md) **Backend B** I/O model: a native
`libav*`-linked program whose *entire* media I/O is `avio_alloc_context` read/write/**seek**
callbacks over a **Unix socket** to a Go host serving an **in-memory store** (the afero stand-in) —
remuxing to a **non-fragmented MP4**, the case the HTTP-`PUT` bridge (Backend C) cannot do.

## Run

```sh
docker build -t aviospike . && docker run --rm aviospike
```

## Result (2026-07-02, Debian ffmpeg 5.1 / libavformat 59)

```
fixture in.mp4: 222952 bytes ... ftyp@4 mdat@44 moov@220393 -> non-fragmented (moov after mdat)
remux OK: 2 streams copied
output landed ENTIRELY in the in-memory store: 222952 bytes ... -> non-fragmented (moov after mdat)
ffprobe(out.mp4): format_name=mov,mp4,... duration=3.000000 nb_streams=2
IPC calls: reads=6 writes=179 seeks=4  BACKWARD-seeks-on-output(moov patch)=1
✅ seekable custom-AVIO wrote a NON-FRAGMENTED MP4 via backward seeks — the case HTTP PUT cannot do
```

## What it establishes

- Custom AVIO **read + seek** (input) and **write + seek** (output) both work over IPC.
- The `seek_cb` services the `av_write_trailer` **backward seek** (moov / `mdat`-size patch) — so
  **non-fragmented MP4 output works**, removing Backend C's one limitation.
- All media I/O round-trips through the in-memory store — **no host file** for in/out (the `/tmp`
  files here are only fixture generation + `ffprobe` verification).
- A native `libav*`-linked program compiles + runs — confirming the `driver.c`-compiled-native path.

`avio_spike.c` is a minimal stand-in for a native build of `ffmpeg-wasi`'s `driver.c`; `host.go`
is a minimal stand-in for afmpeg's afero-backed bridge. Not production code — a de-risking spike.
