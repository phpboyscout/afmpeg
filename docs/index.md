---
title: afmpeg
description: Pure-Go FFmpeg on a virtual filesystem — no CGO, no host FFmpeg, no temp files.
date: 2026-06-26
tags: [overview, introduction]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# afmpeg

**A pure-Go FFmpeg binding that runs on a virtual / in-memory filesystem.** No CGO,
no host FFmpeg install, no temp files: FFmpeg is supplied as a separate WebAssembly
module and executed via [wazero](https://wazero.io/) (a zero-dependency, pure-Go WASM
runtime), with its I/O bridged to an [`afero.Fs`](https://github.com/spf13/afero) — so
inputs and outputs can live entirely in memory (or any afero backend), and the whole
thing cross-compiles to a single static binary.

!!! success "Status: released"
    afmpeg runs real FFmpeg over a virtual filesystem today: the
    [vfs bridge](explanation/components/vfs-bridge.md), the runtime
    (`New`/`Run`/`RunJob`/`Probe`/`Close`), the `Command` builder (`JobSpec()`/`RunJob` for
    the [ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) engine), and both
    signature-verified [`WithModuleRelease`](how-to/obtain-a-module.md) and bring-your-own
    `WithModuleURL` module acquisition. Pair it with a released
    [ffmpeg-wasi](https://gitlab.com/phpboyscout/ffmpeg-wasi/-/releases) module to transcode,
    remux, clip, filter, burn in subtitles, edit metadata, and extract frames — entirely in
    memory. For encode- or throughput-bound work there is also an opt-in
    [native backend](how-to/use-the-native-backend.md) — the same engine as a signed native
    subprocess, for native-speed encode and the full profile's HEVC/AV1. See the
    [latest afmpeg release](https://gitlab.com/phpboyscout/afmpeg/-/releases); design rationale
    is in the specs under [Development](development/index.md) (start with
    [0001](development/specs/0001-afmpeg.md)).

## Why it exists

It was extracted from a need in keryx: keryx renders short reels by **shelling out to
the ffmpeg binary**, which needs real files on disk — so it can't render an **in-memory
project** (a remote cloned into RAM, no local checkout). Every existing Go option was
rejected: purego bindings are immature and still need host libav; CGO bindings break a
clean static cross-compile; and the existing wazero/WASM binding lacks the filters and
codecs real workflows need. afmpeg is the **"wazero + WASM done right"** synthesis — a
maintained FFmpeg-WASM build with the codecs/filters we need, a first-class afero
virtual-filesystem I/O layer, and a clean Go API. keryx now renders its reels through
afmpeg — in-memory, pure Go, with no local checkout.

## How it works

Three layers — the middle one is the novel engineering:

1. **The FFmpeg-WASM module** — current FFmpeg compiled to `wasm32-wasi`, configured to
   only the codecs/filters needed; shipped as a separate artifact, never embedded.
2. **The afero ↔ wazero vfs bridge** (the heart) — routes the guest ffmpeg's WASI
   filesystem syscalls to a `sys.FS` backed by the caller's `afero.Fs`, so reads and
   writes hit an in-memory filesystem with no host disk touched.
3. **The Go API** — compile the module once into a reusable `Runtime`, then `Run` an
   ffmpeg invocation over a supplied `afero.Fs`; a general command builder layers on top.

The runtime sits behind a **backend seam** (spec 0028): the WASM module is the default, and
an opt-in [native backend](how-to/use-the-native-backend.md) runs the same engine as a native
subprocess for native-speed encode and HEVC/AV1 — same API, same afero I/O, no CGO.

See the [architecture explainer](explanation/concepts/architecture.md) for the full flow,
and the [roadmap](development/index.md) for how the specs decompose the build.

## Where to go next

<div class="grid cards" markdown>

- :material-school: **[Tutorials](tutorials/index.md)** — learn afmpeg by doing.
- :material-wrench: **[How-to guides](how-to/index.md)** — solve a specific task.
- :material-lightbulb: **[Explanation](explanation/index.md)** — the architecture and the *why*.
- :material-book-open-variant: **[Reference](reference/index.md)** — the Go API and engine artifacts.
- :material-file-document-multiple: **[Development](development/index.md)** — specs and contributor docs.

</div>

The Go API reference lives on [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg).
