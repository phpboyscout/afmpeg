---
title: Tutorials
description: Learning-oriented walkthroughs for getting started with afmpeg.
date: 2026-06-26
tags: [tutorials]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Tutorials

Learning-oriented, step-by-step walkthroughs that teach afmpeg by doing — each
guaranteed to work from a clean start.

afmpeg is released (v0.3.0). Until a dedicated start-to-finish tutorial lands, the
**[how-to guides](../how-to/index.md)** are the practical, working walkthroughs:

- **[Obtain a module](../how-to/obtain-a-module.md)** — get a released
  [ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) engine (URL + SHA-256).
- **[Run over an in-memory filesystem](../how-to/run-in-memory.md)** — inputs into an
  `afero.MemMapFs`, transcode, read the output back, all without touching disk.
- **[Compose a command](../how-to/compose-a-command.md)** — assemble inputs / filtergraph /
  outputs and run it as a job over the ffmpeg-wasi engine.

If you just need to accomplish a specific task and already know the basics, see the
[how-to guides](../how-to/index.md) instead.
