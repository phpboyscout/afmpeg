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

Available:

- **[Run ffmpeg over an in-memory filesystem](run-in-memory.md)** — build a `Runtime`,
  supply the `ffmpeg.wasm` module (`WithModuleFile` / `WithModuleBytes` / `WithModuleFS`),
  transcode against an `afero.MemMapFs`, probe a duration, and cancel a render.

Planned (land with their features):

- **Compose any ffmpeg command with the builder** — typed inputs/filtergraph/outputs
  (spec [0005](../development/specs/0005-render-helper-and-keyrx-backend.md)).
- **Build the FFmpeg WASM module** — the reproducible pipeline
  (spec [0002](../development/specs/0002-wasm-build-pipeline.md)).
