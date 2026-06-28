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
    "https://gitlab.com/api/v4/projects/83847809/packages/generic/ffmpeg-wasi/n8.1.2-1/ffmpeg-wasi-lgpl.wasm",
    afmpeg.WithSHA256("0f338dac4ed1be3819aaf26f1cdeef119e817b43103f1460ca19354ea56bacc9"),
))
```

Options: `WithSHA256` (verify), `WithGunzip` (decompress a `.wasm.gz`),
`WithCacheDir` (override the cache location), `WithHTTPClient` (your own client,
e.g. for proxies or timeouts). A checksum mismatch returns
[`ErrChecksumMismatch`](../explanation/components/errors.md) and the bytes are
never executed.

## Where do I get a module?

- **[ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk)** *(recommended)* — the companion
  libav-direct engine: **current FFmpeg**, published as **lgpl** (default) and **gpl**
  (libx264) WASI modules, each with a checksum and provenance. Pin a release asset + its
  SHA-256 (the example above is the `lgpl` module from
  [`n8.1.2-1`](https://gitlab.com/phpboyscout/ffmpeg-wasi/-/releases/n8.1.2-1)). It speaks the
  structured job spec — drive it with [`Command.JobSpec()` / `RunJob`](compose-a-command.md).
- **An interim community build** — e.g. go-ffmpreg's gzipped wasm (a CLI-style module run via
  `Args()`/`Run`). It is **GPL-3.0** and includes libx264, so it suits internal/GPL-compatible
  use:

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
