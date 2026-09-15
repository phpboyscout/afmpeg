---
title: afmpeg
description: Pure-Go FFmpeg on a virtual filesystem, with no CGO and no host FFmpeg install.
tags: [overview, introduction]
hide:
  - navigation
---

<div class="hero">
  <div class="hero-mark">
    <img src="images/branding/logo_transparent.svg" alt="">
  </div>
  <div class="hero-body">
    <p class="hero-eyebrow">pure go · in memory · no cgo</p>
    <h1 class="hero-title">afmpeg</h1>
    <p class="hero-tagline">FFmpeg from Go, and the disk never finds out.</p>
    <p class="hero-description">
      A pure-Go FFmpeg library that runs on a virtual filesystem. FFmpeg arrives
      as a separate WebAssembly module, runs under <a href="https://wazero.io/">wazero</a>,
      and every file it opens is answered out of an <code>afero.Fs</code> you hand it.
      So inputs and outputs live in memory (or any afero backend), nothing is
      installed on the host, no temp files are written, and the whole program
      cross-compiles to one static binary.
    </p>
    <div class="install-box">
      <span class="install-command">go get gitlab.com/phpboyscout/afmpeg</span>
      <button class="install-copy" type="button" title="Copy to clipboard">copy</button>
    </div>
    <div class="hero-buttons">
      <a href="tutorials/first-in-memory-transcode/" class="btn btn-primary">Get started</a>
      <a href="https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg" class="btn btn-secondary">API reference</a>
    </div>
  </div>
</div>

<div class="cap-grid">
  <div class="cap">
    <h3>Runs where the files are not</h3>
    <p>The vfs bridge routes the guest's filesystem calls to a <code>sys.FS</code> backed by your <code>afero.Fs</code>. An in-memory project, a remote cloned into RAM, an S3 bucket: FFmpeg reads and writes it without a byte touching host disk.</p>
  </div>
  <div class="cap">
    <h3>No CGO, no install</h3>
    <p>The engine is a WebAssembly module fetched at first use and verified against a signed release. Your binary stays statically linked and cross-compiles as it always did.</p>
  </div>
  <div class="cap">
    <h3>A typed command builder</h3>
    <p>Compose inputs, filters, outputs, seeks and metadata as Go values with <code>Command</code>, and hand the resulting job spec to the engine. No argument strings to get subtly wrong.</p>
  </div>
  <div class="cap">
    <h3>Progress on a channel</h3>
    <p>A running job reports completion, frames, media time and encode speed as it goes, so a long transcode can drive a progress bar or a cancellation instead of going quiet.</p>
  </div>
  <div class="cap">
    <h3>Capped, deadlined, serialised</h3>
    <p>A Runtime is bounded by default: memory capped, calls deadlined, invocations serialised. Media from strangers runs inside a sandbox with the limits already set.</p>
  </div>
  <div class="cap">
    <h3>A native backend when speed wins</h3>
    <p>The same engine as a signed native subprocess, behind the same API and the same afero I/O, for native-speed encode and the full profile's HEVC and AV1. Opt in per Runtime.</p>
  </div>
</div>

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
   only the codecs/filters needed; shipped as a separate artifact by
   [ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk), never embedded.
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

- :material-school: **[Tutorials](tutorials/index.md)**: a working program in about fifteen minutes.
- :material-wrench: **[How-to guides](how-to/index.md)**: solve a specific task.
- :material-lightbulb: **[Explanation](explanation/index.md)**: the architecture, and why it is that shape.
- :material-book-open-variant: **[Reference](reference/index.md)**: every option, field, default and limit.
- :material-file-document-multiple: **[Development](development/index.md)**: contributor docs.

</div>

## Status

**Released.** afmpeg runs real FFmpeg over a virtual filesystem today: the
[vfs bridge](explanation/components/vfs-bridge.md), the runtime
(`New`/`Run`/`RunJob`/`Probe`/`Close`), the `Command` builder (`JobSpec()`/`RunJob` for
the [ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) engine), and both
signature-verified [`WithModuleRelease`](how-to/obtain-a-module.md) and bring-your-own
`WithModuleURL` module acquisition. Pair it with a released
[ffmpeg-wasi](https://gitlab.com/phpboyscout/ffmpeg-wasi/-/releases) module to transcode,
remux, clip, filter, burn in subtitles, edit metadata, extract frames, and read
analysis-filter measurements (`ProcessResult.Analysis`), entirely in memory. The current
version is whatever is at the top of
[the releases page](https://gitlab.com/phpboyscout/afmpeg/-/releases); design rationale
is in the specs under [Development](development/index.md), starting with
[0001](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0001-afmpeg).

## Further reading

Everything written about the estate, including the curated guides, is on
[the blog](https://phpboyscout.uk/topics/).

!!! tip "Ask phpbotscout"

    ![phpbotscout](https://phpboyscout.uk/images/projects/logo-phpbotscout.png){ width="84" align=left style="border-radius:10px;margin-right:1rem" }

    He answers questions about the projects over on the Discord, citing the docs
    where they already cover it, and offering to raise an issue where they don't.
    Bring a bug, an idea, or a questionable engineering decision.

    [Join the Discord](https://discord.gg/mQzGbmGyzZ){ .md-button .md-button--primary }
