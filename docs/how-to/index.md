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

- **[Obtain an ffmpeg.wasm module](obtain-a-module.md)** — supply the module from a file,
  bytes, an afero fs, or a URL with checksum-verified caching (`WithModuleURL`).
- **[Run ffmpeg over an in-memory filesystem](run-in-memory.md)** — build a `Runtime`,
  supply the `ffmpeg.wasm` module (`WithModuleFile` / `WithModuleBytes` / `WithModuleFS`),
  transcode against an `afero.MemMapFs`, probe a duration, and cancel a render.
- **[Compose a command with the builder](compose-a-command.md)** — assemble any
  invocation (inputs / filtergraph / outputs) as typed data and run it with `RunJob`
  (`JobSpec()`).

The WASM engine itself — current FFmpeg compiled to `wasm32-wasi`, with its `process`/`probe`
job spec — is the companion [**ffmpeg-wasi**](https://ffmpeg-wasi.phpboyscout.uk) project;
grab a released module via [obtain a module](obtain-a-module.md).
