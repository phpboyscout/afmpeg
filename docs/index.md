---
title: afmpeg
description: Pure-Go FFmpeg on a virtual filesystem — no CGO, no host FFmpeg, no temp files.
date: 2026-06-26
tags: [overview, introduction]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# afmpeg

**A pure-Go FFmpeg binding that runs on a virtual / in-memory filesystem.** No CGO,
no host FFmpeg install, no temp files: FFmpeg is embedded as a WebAssembly module and
executed via [wazero](https://wazero.io/) (a zero-dependency, pure-Go WASM runtime),
with its I/O bridged to an [`afero.Fs`](https://github.com/spf13/afero) — so inputs and
outputs can live entirely in memory (or any afero backend), and the whole thing
cross-compiles to a single static binary.

!!! info "Status: design approved, implementation not started"
    This repo holds the **design + requirements**. The five gating decisions are
    resolved and the work is decomposed into component specs. Start with the
    source-of-truth thesis, [0001 — afmpeg](development/specs/0001-afmpeg.md), and the
    components (0002–0006) under [Development](development/index.md).

## Why it exists

It was extracted from a need in keryx: keryx renders short reels by **shelling out to
the ffmpeg binary**, which needs real files on disk — so it can't render an **in-memory
project** (a remote cloned into RAM, no local checkout). Every existing Go option was
rejected: purego bindings are immature and still need host libav; CGO bindings break a
clean static cross-compile; and the existing wazero/WASM binding lacks the filters and
codecs real workflows need. afmpeg is the **"wazero + WASM done right"** synthesis — a
maintained FFmpeg-WASM build with the codecs/filters we need, a first-class afero
virtual-filesystem I/O layer, and a clean Go API. Until afmpeg is usable, keryx renders
local-filesystem-only; afmpeg reaching usable status is what lifts that lock-out.

## How it works

Three layers — the middle one is the novel engineering:

1. **Embedded FFmpeg-WASM module** — FFmpeg + x264 compiled to `wasm32-wasi`, configured
   to only the codecs/filters needed; shipped as a separate artifact, not embedded.
2. **The afero ↔ wazero vfs bridge** (the heart) — routes the guest ffmpeg's WASI
   filesystem syscalls to a `sys.FS` backed by the caller's `afero.Fs`, so reads and
   writes hit an in-memory filesystem with no host disk touched.
3. **The Go API** — compile the module once into a reusable `Runtime`, then `Run` an
   ffmpeg invocation over a supplied `afero.Fs`; a general command builder layers on top.

See the [architecture explainer](explanation/concepts/architecture.md) for the full flow,
and the [roadmap](development/index.md) for how the specs decompose the build.

## Where to go next

<div class="grid cards" markdown>

- :material-school: **[Tutorials](tutorials/index.md)** — learn afmpeg by doing.
- :material-wrench: **[How-to guides](how-to/index.md)** — solve a specific task.
- :material-lightbulb: **[Explanation](explanation/index.md)** — the architecture and the *why*.
- :material-book-open-variant: **[Reference](reference/index.md)** — config, CLI, and the Go API.
- :material-file-document-multiple: **[Development](development/index.md)** — specs and contributor docs.

</div>

The Go API reference lives on [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg).
