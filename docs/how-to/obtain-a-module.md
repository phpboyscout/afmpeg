---
title: Obtain an ffmpeg.wasm module
description: How to supply afmpeg with its WebAssembly ffmpeg module — from a file, bytes, an afero fs, or a URL with caching.
date: 2026-06-27
tags: [how-to, module, wasm]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Obtain an ffmpeg.wasm module

afmpeg does **not** embed or bundle the ffmpeg WebAssembly module, and it never
downloads one behind your back. You supply it — deliberately, so the module's
licence (a full/GPL build links x264) never attaches to afmpeg's permissively
licensed Go package (spec [0001](../development/specs/0001-afmpeg.md) D-C). `New`
returns [`ErrNoModule`](../explanation/components/errors.md) if none is given.

There are four ways to provide it.

## From a file or bytes

If you already have the `.wasm` on disk or in memory:

```go
rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile("ffmpeg.wasm"))
// or afmpeg.WithModuleBytes(b)
// or afmpeg.WithModuleFS(fs, "ffmpeg.wasm")  // from any afero.Fs
```

## From a URL, with caching (no manual wrangling)

`WithModuleURL` downloads the module once and caches it under your OS cache dir,
so subsequent runs are offline. **You choose the URL and accept its licence.**
Because the module is executable code, pair it with `WithSHA256`:

```go
rt, err := afmpeg.New(ctx, afmpeg.WithModuleURL(
    "https://gitlab.com/api/v4/projects/83847809/packages/generic/ffmpeg-wasi/n8.1.2-2/ffmpeg-wasi-lgpl.wasm",
    afmpeg.WithSHA256("b2925737383f3c68c70e8f2df9e40c2339dd8ff03f0f20691b059e82b636d428"),
))
```

Options: `WithSHA256` (verify), `WithGunzip` (decompress a `.wasm.gz`),
`WithCacheDir` (override the cache location), `WithHTTPClient` (your own client,
e.g. for proxies or timeouts). A checksum mismatch returns
[`ErrChecksumMismatch`](../explanation/components/errors.md) and the bytes are
never executed.

## Where do I get a module?

- **[ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk)** *(the route)* — the companion
  libav-direct engine: **current FFmpeg**, published as **lgpl** (default) and **gpl** WASI
  modules, each with a checksum and provenance. **Both encode H.264** — the `lgpl` module via
  openh264, the `gpl` module via libx264. Pin a release asset + its SHA-256 (the example above
  is the `lgpl` module from
  [`n8.1.2-2`](https://gitlab.com/phpboyscout/ffmpeg-wasi/-/releases/n8.1.2-2)). It speaks the
  structured job spec — drive it with [`Command.JobSpec()` / `RunJob`](compose-a-command.md)
  and [`Probe`](run-in-memory.md).

  A GPL module makes the *combined running program* GPL; afmpeg keeps it at arm's length (a
  separate artifact you fetch), but your obligations follow the variant you choose. The `lgpl`
  module's self-compiled openh264 carries an [AVC patent caveat](https://ffmpeg-wasi.phpboyscout.uk/explanation/licensing/#h264-encode-and-the-avc-patent-pool).
- **Build your own** — any current FFmpeg compiled to `wasm32-wasi` with the feature set
  afmpeg's runtime enables (spec [0004](../development/specs/0004-runtime-and-api.md)
  R-0004-9). afmpeg runs it.
