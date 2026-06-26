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
   afero.Fs → wazero                module wiring                  (FFmpeg + x264,
   experimental/sys.FS              over wazero                     wasm32-wasi)
          ▲                                                              │
          └──────────────── WASI fs syscalls (path_open, fd_read, ──────┘
                            fd_write, fd_seek, …) routed to the afero.Fs
```

## 1. The embedded FFmpeg-WASM module

FFmpeg and its dependencies (x264, …) compiled to `wasm32-wasi`, configured down to only
the codecs/filters real workflows need. It is produced by a reproducible build pipeline
and — per the licensing decision — shipped as a **separate downloadable artifact, not
`//go:embed`-ed**, so the GPL obligation stays at arm's length from the permissively
licensed Go package. See spec
[0002](../../development/specs/0002-wasm-build-pipeline.md).

## 2. The afero ↔ wazero vfs bridge (the heart)

ffmpeg-in-the-guest issues WASI filesystem syscalls. wazero routes them to a mounted
`experimental/sys.FS`. afmpeg implements that `sys.FS` **backed by an `afero.Fs`** — so
the guest's reads and writes hit the caller's filesystem (e.g. an in-memory `MemMapFs`)
with no host disk touched. It also provides a writable `/tmp` and `/dev/null` the guest
needs, and must handle seek-on-write (the mp4 muxer rewrites the `moov` atom under
`+faststart`). This is what every other Go ffmpeg-wasm binding lacks. See spec
[0003](../../development/specs/0003-vfs-bridge.md).

## 3. The Go API

`New` compiles the module once (the expensive step) into a reusable `Runtime`. `Run`
mounts a caller-supplied `afero.Fs`, executes one ffmpeg invocation, and returns the exit
code + captured stderr; `Probe` returns a media file's duration over the same bridge. A
higher-level render helper (mirroring keryx's `provider.Timeline`) layers on top. See
specs [0004](../../development/specs/0004-runtime-and-api.md) and
[0005](../../development/specs/0005-render-helper-and-keyrx-backend.md).

## Why this shape

The alternatives — purego/dlopen bindings (immature, still need host libav), CGO libav
bindings (break a clean static cross-compile), and the stock wazero binding (missing
filters/AAC, not filesystem-virtualised) — each failed at least one of *pure-Go*,
*in-memory*, or *has-the-codecs-we-need*. afmpeg is the synthesis that holds all three.
The full reasoning is in spec [0001](../../development/specs/0001-afmpeg.md) §11.
