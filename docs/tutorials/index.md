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

!!! note "Coming with the implementation"
    afmpeg is at the scaffold/intent stage, so there are no tutorials yet. The first
    one — *transcode a file entirely in memory* — lands with the runtime + API
    (spec [0004](../development/specs/0004-runtime-and-api.md)).

Planned:

- **Your first in-memory transcode** — write inputs into an `afero.MemMapFs`, run a
  command, read the output back, all without touching disk.
- **Compose a command with the builder** — assemble inputs, a filtergraph, and outputs
  for any ffmpeg workflow with the command builder (spec
  [0005](../development/specs/0005-render-helper-and-keyrx-backend.md)).

If you just need to accomplish a specific task and already know the basics, see the
[how-to guides](../how-to/index.md) instead.
