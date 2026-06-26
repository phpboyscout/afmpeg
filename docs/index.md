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

!!! warning "Status: scaffold / intent — the build has not started"
    This repo currently holds the **design + requirements**. Start with the
    source-of-truth spec, [0001 — afmpeg](development/specs/0001-afmpeg.md), and the
    component specs (0002–0006) it decomposes into.

## Why it exists

It was extracted from a need in keryx: keryx renders short reels by **shelling out to
the ffmpeg binary**, which needs real files on disk — so it can't render an **in-memory
project** (a remote cloned into RAM, no local checkout). Every existing Go option was
rejected: purego bindings are immature and still need host libav; CGO bindings break a
clean static cross-compile; and the existing wazero/WASM binding lacks the filters and
codecs real workflows need. afmpeg is the **"wazero + WASM done right"** synthesis — a
maintained FFmpeg-WASM build with the codecs/filters we need, a first-class afero
virtual-filesystem I/O layer, and a clean Go API.

## Where to go next

<div class="grid cards" markdown>

- :material-school: **[Tutorials](tutorials/index.md)** — learn afmpeg by doing.
- :material-wrench: **[How-to guides](how-to/index.md)** — solve a specific task.
- :material-lightbulb: **[Explanation](explanation/index.md)** — the architecture and the *why*.
- :material-book-open-variant: **[Reference](reference/index.md)** — config, CLI, and the Go API.
- :material-file-document-multiple: **[Development](development/index.md)** — specs and contributor docs.

</div>

The Go API reference lives on [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg).
