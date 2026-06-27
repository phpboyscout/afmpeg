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
- **[Compose an ffmpeg command with the builder](compose-a-command.md)** — assemble any
  invocation (inputs / filtergraph / outputs) as typed data, struct or `NewCommand`.

Planned (land with their features):

- **Build the FFmpeg WASM module** — the reproducible pipeline
  (spec [0002](../development/specs/0002-wasm-build-pipeline.md)).
