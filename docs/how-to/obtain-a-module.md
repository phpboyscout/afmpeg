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
    "https://example.com/ffmpeg.wasm",
    afmpeg.WithSHA256("e3b0c44298fc1c14…"),  // verify the download
))
```

Options: `WithSHA256` (verify), `WithGunzip` (decompress a `.wasm.gz`),
`WithCacheDir` (override the cache location), `WithHTTPClient` (your own client,
e.g. for proxies or timeouts). A checksum mismatch returns
[`ErrChecksumMismatch`](../explanation/components/errors.md) and the bytes are
never executed.

## Where do I get a module?

- **The afmpeg build** — a general-baseline `ffmpeg.wasm` from the reproducible
  pipeline (spec [0002](../development/specs/0002-wasm-build-pipeline.md)),
  published as full/GPL and LGPL variants. *(Not yet released.)*
- **An interim community build** — e.g. go-ffmpreg's gzipped wasm. It is **GPL-3.0**
  and includes libx264, so it suits internal/GPL-compatible use:

  ```go
  rt, err := afmpeg.New(ctx, afmpeg.WithModuleURL(
      "https://codeberg.org/gruf/go-ffmpreg/raw/tag/v0.6.20/embed/ffmpreg.wasm.gz",
      afmpeg.WithGunzip(),
      afmpeg.WithSHA256("…"),
  ))
  ```

  Mind the licence: a GPL module makes the *combined running program* GPL. afmpeg
  keeps it at arm's length (a separate artifact you fetch), but your obligations
  follow the module you choose.
- **Build your own** — any FFmpeg compiled to `wasm32-wasi` with the feature set
  afmpeg's runtime enables (spec [0004](../development/specs/0004-runtime-and-api.md)
  R-0004-9). afmpeg runs it.
