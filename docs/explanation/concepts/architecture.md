---
title: Architecture
description: The three layers of afmpeg and how a call flows from Go to the WASM guest and back.
date: 2026-06-26
tags: [explanation, architecture]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Architecture

afmpeg is three layers. The middle one is the novel engineering — everything else is
wiring around it.

```
   caller's afero.Fs                 pkg/afmpeg (the Go API)
   (MemMapFs / OsFs / …)        New → compile module once; Run/Probe per call
          │                                   │
          ▼                                   ▼
   internal/vfs  ──────────────►  internal/wasm  ──────────────►  ffmpeg.wasm
   afero.Fs → wazero                module wiring                  (FFmpeg + openh264
   experimental/sys.FS              over wazero                     /x264, wasm32-wasi)
          ▲                                                              │
          └──────────────── WASI fs syscalls (path_open, fd_read, ──────┘
                            fd_write, fd_seek, …) routed to the afero.Fs
```

## 1. The FFmpeg-WASM module

FFmpeg and its dependencies (openh264/x264, …) compiled to `wasm32-wasi`, configured down to
only the codecs/filters real workflows need. It is produced by a reproducible build pipeline
and — per the licensing decision — shipped as a **separate downloadable artifact, not
`//go:embed`-ed**, so the GPL obligation stays at arm's length from the permissively
licensed Go package. See spec
[0002](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0002-wasm-build-pipeline).

## 2. The afero ↔ wazero vfs bridge (the heart)

ffmpeg-in-the-guest issues WASI filesystem syscalls. wazero routes them to a mounted
`experimental/sys.FS`. afmpeg implements that `sys.FS` **backed by an `afero.Fs`** — so
the guest's reads and writes hit the caller's filesystem (e.g. an in-memory `MemMapFs`)
with no host disk touched. It also provides a writable `/tmp` and `/dev/null` the guest
needs, and must handle seek-on-write (the mp4 muxer rewrites the `moov` atom under
`+faststart`). This is what every other Go ffmpeg-wasm binding lacks. See spec
[0003](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0003-vfs-bridge).

## 3. The Go API

`New` compiles the module once (the expensive step) into a reusable `Runtime`. `Run`
mounts a caller-supplied `afero.Fs`, runs the module with the given args, and returns the
exit code + captured stdout/stderr; `RunJob`/`Command.JobSpec` render a job for the
[ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) engine, and `Probe` reports a file's
container, duration, and streams over the same bridge. The use-case-agnostic command
builder layers on top (a consumer's reel/timeline is built on it, in the consumer's code).
See specs [0004](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0004-runtime-and-api) and
[0005](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0005-render-helper-and-keyrx-backend).

## 4. The backend seam (WASM default · native opt-in)

`Run`/`Probe`/`Frames` sit behind a small **backend** interface (spec
[0028](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0028-native-subprocess-backend)). The default backend is the
WASM path above (wazero + the vfs bridge). An opt-in **native backend** (`pkg/afmpeg/native`,
wired with `WithBackend`) satisfies the same seam differently: it spawns the *same* libav-direct
engine compiled to a **native ELF** as a subprocess and serves the caller's `afero.Fs` to it over
a Unix-socket IPC bridge — a framed read/write/**seek** protocol that carries even the muxer's
backward seeks, so I/O still never touches host disk. It is **CGO-free** (a subprocess, not a
linked library, so the licensing arm's-length holds) and gives threads + SIMD: native-speed
software encode, and the full profile's HEVC/AV1 encoders that are impractical in WASM. The Go
API, the job spec, and the results are identical — only the runtime underneath changes. The
signed driver is acquired and verified with `native.NewFromRelease`, exactly as `WithModuleRelease`
verifies a `.wasm`. See the [native backend how-to](../../how-to/use-the-native-backend.md).

## Why this shape

The alternatives — purego/dlopen bindings (immature, still need host libav), CGO libav
bindings (break a clean static cross-compile), and the stock wazero binding (missing
filters/AAC, not filesystem-virtualised) — each failed at least one of *pure-Go*,
*in-memory*, or *has-the-codecs-we-need*. afmpeg is the synthesis that holds all three.
When native speed or HW-class codecs (HEVC/AV1) *are* required, the **native subprocess
backend** (§4) is the sanctioned escape hatch — the same engine, out-of-process and signed,
rather than reaching for the CGO libav binding this design set out to avoid. The full reasoning
is in spec [0001](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0001-afmpeg) §11.
