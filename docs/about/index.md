---
title: About afmpeg
description: Why afmpeg exists, where it runs, and how a pure-Go library ends up running real FFmpeg without a disk, an install or a C toolchain.
tags: [about, overview]
---

# About afmpeg

afmpeg is FFmpeg for Go programs that cannot, or would rather not, shell out. It runs the
real thing, current FFmpeg, as a WebAssembly module inside your process, and answers every
file the engine opens out of a filesystem you hand it. This page is the short version of
why, where and how. The long versions are under [Explanation](../explanation/index.md).

## Why it exists

It was extracted from a need in [keryx](https://keryx.phpboyscout.uk). keryx renders short
reels by shelling out to the `ffmpeg` binary, which needs real files on disk, so it could
not render an in-memory project: a remote cloned into RAM with no local checkout. Every
existing Go route was rejected on the way here. Bindings over `purego` still need a host
`libav` and were immature; CGO bindings break the clean static cross-compile that is half
the point of writing the tool in Go; and the stock FFmpeg-in-WebAssembly builds turned out
short of the filters and codecs keryx needed, with their I/O aimed at the host filesystem
rather than one you supply.

afmpeg closes both gaps. The engine is a maintained, configured FFmpeg build, and the I/O
layer answers the guest's filesystem calls out of an `afero.Fs` of your choosing. keryx
now renders its reels through it, in memory and in pure Go.

## Where it runs

Anywhere a Go binary runs. There is no native dependency to install and no C toolchain at
build time, so the program cross-compiles to a single static binary exactly as it did
before FFmpeg was involved. The engine module is fetched at first use from a published
[ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) release and
[verified before it runs](../explanation/concepts/release-verification.md), or supplied
from a URL of your own.

The filesystem it works over is whatever you give it: an in-memory `MemMapFs` for a
pipeline that never touches disk, the host filesystem when that is what you want, or any
other afero backend. It is a server-side and command-line library, not a browser one.

## How it works

Three layers, and the middle one is where the work is. The
[FFmpeg module](https://ffmpeg-wasi.phpboyscout.uk) is current FFmpeg compiled to
`wasm32-wasi` by the sibling project and shipped as a separate artifact, never embedded,
so its licence stays at arm's length from yours. The
[vfs bridge](../explanation/components/vfs-bridge.md) routes the guest's WASI filesystem
calls to a `sys.FS` backed by your `afero.Fs`, which is what makes "no host disk" true
rather than aspirational. The Go API compiles the module once into a reusable `Runtime`
and runs jobs over it, with a typed command builder on top so a transcode is composed
from Go values rather than argument strings.

A Runtime is [capped, deadlined and serialised](../explanation/concepts/safe-defaults.md)
by default, because most of the media it will ever see came from someone else. And the
runtime sits behind a backend seam: the WebAssembly module is the default, and an opt-in
[native backend](../how-to/use-the-native-backend.md) runs the same engine as a signed
native subprocess, behind the same API and the same afero I/O, when native-speed encode or
the HEVC and AV1 encoders matter more than the sandbox.
[Architecture](../explanation/concepts/architecture.md) has the full flow.

## Who is behind it

afmpeg is built and maintained by Matt Cockayne as part of the
[phpboyscout](https://phpboyscout.uk) estate, alongside ffmpeg-wasi, which builds the
engine it runs. The library is published on
[pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg), its design lives in the
specs on the [project wiki](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/home), and
the [branding](branding.md) page has the mark and the palette if you need them.
