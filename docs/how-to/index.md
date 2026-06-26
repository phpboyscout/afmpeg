---
title: How-to guides
description: Task-oriented guides for solving specific problems with afmpeg.
date: 2026-06-26
tags: [how-to]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# How-to guides

Goal-oriented, practical recipes for someone who already knows the basics. Each guide
solves one specific problem.

!!! note "Coming with the implementation"
    These guides land alongside the features they cover. afmpeg is currently at the
    scaffold/intent stage.

Planned:

- **Supply the `ffmpeg.wasm` module** — afmpeg does not embed the GPL build; how to point
  the runtime at the module artifact (`WithModuleFile` / `WithModuleBytes` / `WithModuleFS`)
  — spec [0004](../development/specs/0004-runtime-and-api.md), decision D-C.
- **Run against an in-memory filesystem** — wire an `afero.MemMapFs` and verify no host
  filesystem access (spec [0003](../development/specs/0003-vfs-bridge.md)).
- **Probe a media file's duration** over the fs bridge.
- **Use afmpeg as a keryx render backend** — select `providers.render: afmpeg`
  (spec [0005](../development/specs/0005-render-helper-and-keyrx-backend.md)).
- **Build the FFmpeg WASM module** — the reproducible pipeline
  (spec [0002](../development/specs/0002-wasm-build-pipeline.md)).
