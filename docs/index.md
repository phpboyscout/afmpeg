---
title: afmpeg
description: Pure-Go FFmpeg on a virtual filesystem, with no CGO and no host FFmpeg install.
date: 2026-06-26
tags: [overview, introduction]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

<div class="hero" markdown>

![afmpeg logo](images/branding/logo_transparent.svg)

# afmpeg

<p class="hero-tagline">Pure-Go FFmpeg on a virtual filesystem. No CGO, no host FFmpeg install.</p>

</div>

**A pure-Go FFmpeg binding that runs on a virtual / in-memory filesystem.** No CGO,
no host FFmpeg install, no temp files: FFmpeg is supplied as a separate WebAssembly
module and executed via [wazero](https://wazero.io/) (a zero-dependency, pure-Go WASM
runtime), with its I/O bridged to an [`afero.Fs`](https://github.com/spf13/afero), so
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
    remux, clip, filter, burn in subtitles, edit metadata, extract frames, and read
    analysis-filter measurements (`ProcessResult.Analysis`), entirely in
    memory. A running job reports [live progress](how-to/watch-job-progress.md) on a channel
    (`WithProgress`): completion, frames, media time and encode speed. For encode- or
    throughput-bound work there is also an opt-in
    [native backend](how-to/use-the-native-backend.md), the same engine as a signed native
    subprocess, for native-speed encode and the full profile's HEVC/AV1. See the
    [latest afmpeg release](https://gitlab.com/phpboyscout/afmpeg/-/releases); design rationale
    is in the specs under [Development](development/index.md) (start with
    [0001](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0001-afmpeg)).

## Why it exists

It was extracted from a need in keryx: keryx renders short reels by **shelling out to
the ffmpeg binary**, which needs real files on disk, so it can't render an **in-memory
project** (a remote cloned into RAM, no local checkout). Every existing Go option was
rejected: purego bindings are immature and still need host libav; CGO bindings break a
clean static cross-compile; and the spike found the stock wazero/WASM builds short of the
filters and codecs keryx needed (it names `xfade` and AAC), with their I/O going to the
host filesystem rather than to one you supply. afmpeg closes both gaps: an FFmpeg-WASM
build we maintain and configure (`xfade` and `aac` are in the lean profile), and an I/O
layer that answers the guest's filesystem calls out of an `afero.Fs` you hand it. keryx
now renders its reels through afmpeg, in-memory and pure Go, with no local checkout.

## How it works

Three layers. The middle one is where the work is:

1. **The FFmpeg-WASM module**: current FFmpeg compiled to `wasm32-wasi`, configured to
   only the codecs/filters needed; shipped as a separate artifact, never embedded.
2. **The afero ↔ wazero vfs bridge** (the heart): routes the guest ffmpeg's WASI
   filesystem syscalls to a `sys.FS` backed by the caller's `afero.Fs`, so reads and
   writes hit an in-memory filesystem with no host disk touched.
3. **The Go API**: compile the module once into a reusable `Runtime`, then `Run` an
   ffmpeg invocation over a supplied `afero.Fs`; a general command builder layers on top.

The runtime sits behind a **backend seam** (spec 0028): the WASM module is the default, and
an opt-in [native backend](how-to/use-the-native-backend.md) runs the same engine as a native
subprocess for native-speed encode and HEVC/AV1. Same API, same afero I/O, no CGO.

See the [architecture explainer](explanation/concepts/architecture.md) for the full flow,
and the [roadmap](development/index.md) for how the specs decompose the build.

## Where to go next

<div class="grid cards" markdown>

- :material-school: **[Your first in-memory transcode](tutorials/first-in-memory-transcode.md)**:
  start here: a working program in about fifteen minutes.
- :material-wrench: **[How-to guides](how-to/index.md)**: solve a specific task.
- :material-lightbulb: **[Explanation](explanation/index.md)**: the architecture, and why it is that shape.
- :material-book-open-variant: **[Reference](reference/index.md)**: every option, field, default
  and limit.
- :material-file-document-multiple: **[Development](development/index.md)**: contributor docs.

</div>

The Go API reference lives on [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg).

## Further reading

Everything written about the estate, including the curated guides, is on
[the blog](https://phpboyscout.uk/topics/).

!!! tip "Ask phpbotscout"

    ![phpbotscout](https://phpboyscout.uk/images/projects/logo-phpbotscout.png){ width="84" align=left style="border-radius:10px;margin-right:1rem" }

    He answers questions about the projects over on the Discord, citing the docs
    where they already cover it, and offering to raise an issue where they don't.
    Bring a bug, an idea, or a questionable engineering decision.

    [Join the Discord](https://discord.gg/mQzGbmGyzZ){ .md-button .md-button--primary }
