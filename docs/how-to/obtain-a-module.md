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
licensed Go package (spec [0001](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0001-afmpeg) D-C). `New`
returns [`ErrNoModule`](../explanation/components/errors.md) if none is given.

There are several ways to provide it. For the project's **own** releases, prefer
the certified path; for your own builds, supply them directly.

## Certified release (recommended)

`WithModuleRelease` fetches a published [ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk)
release by `(tag, variant)` and **verifies it before it runs** — the release's
`checksums.txt` carries a detached **OpenPGP** signature made by a key held in AWS KMS
(signable only by ffmpeg-wasi's tag pipeline), and afmpeg checks that signature against a
**public key pinned inside afmpeg** (via `gitlab.com/phpboyscout/go/signing`), then the
module's and provenance's checksums, then that the provenance names the variant you asked
for:

```go
var prov afmpeg.Provenance
rt, err := afmpeg.New(ctx, afmpeg.WithModuleRelease(
    "n9.0.1-1", afmpeg.VariantLGPL,
    afmpeg.WithReleaseProvenance(&prov), // optional: what was loaded
))
// prov.FFmpegVersion, prov.Variants[...] — verified, not just downloaded.
```

Verification is **mandatory** (the key is embedded — there is no skip flag). On online
fetches afmpeg also **cross-checks the embedded key against the copy published via WKD**
on `openpgpkey.phpboyscout.uk` — a second anchor independent of GitLab; a mismatch fails,
a WKD outage falls back to the pinned key. Any tamper fails with a typed error:
`ErrSignatureInvalid`, `ErrChecksumMismatch`, or `ErrProvenanceMismatch`. The verified
module is cached, so later runs skip the download.

Options: `WithReleaseProvenance` (capture the verified provenance), `WithReleaseProfile`
(select the capability profile — see below), `WithReleaseBaseURL` (fetch from a mirror /
internal store — still verified against the pinned key), `WithReleaseBundleDir` (air-gapped:
verify a local directory of pre-downloaded assets; skips WKD), `WithReleaseWKDEmail`
(override the WKD identity, or `""` to disable the cross-check), `WithReleaseCacheDir`,
`WithReleaseHTTPClient`. See the trust model in
[verifying a release](../explanation/concepts/release-verification.md), or check a release
yourself with [verify a release by hand](verify-a-release-by-hand.md).

`VariantLGPL` is the default, proprietary-compatible build (H.264 via openh264);
`VariantGPL` adds libx264. This path is for **our** releases — we can only certify
what we publish; for your own builds use `WithModuleURL` or a file below.

### Profiles: lean (default), intermediate, or full

The build comes in three [capability profiles](https://ffmpeg-wasi.phpboyscout.uk/reference/variants/)
(ffmpeg-wasi spec 0022):

- **lean** — web-delivery essentials at the smallest size (the default).
- **intermediate** — lean plus every practical software codec, format, and filter (the LGPL
  encoders, the native codec/container batches, and text/subtitle burn-in).
- **full** — intermediate plus the heavy encoders **AV1** (SVT-AV1, both variants) and
  **HEVC/H.265** (x265, GPL variant only). These need threads and SIMD, so full is
  **native-only** — there is no WASM full module.

Over the WASM path, `WithReleaseProfile` selects `ProfileLean` (default) or `ProfileIntermediate`
— the intermediate build is a distinct, separately-signed asset
(`ffmpeg-wasi-intermediate-<variant>.wasm`), fetched and verified through the identical trust chain:

```go
rt, err := afmpeg.New(ctx, afmpeg.WithModuleRelease(
    "n9.0.1-1", afmpeg.VariantLGPL,
    afmpeg.WithReleaseProfile(afmpeg.ProfileIntermediate),
))
```

Omit the option (or pass `afmpeg.ProfileLean`) for the lean module. This is the certified
equivalent of reaching for the intermediate build via `WithModuleURL` — same bytes, but
verified against the pinned key rather than a checksum you supply.

`ProfileFull` is **native-only** — `WithModuleRelease` refuses it (there is no WASM full
module) and points you at the native driver. To run HEVC/AV1 encode or get native-speed
software encode, load the signed native driver instead with
[`native.NewFromRelease`](use-the-native-backend.md) — the native equivalent of this whole
page, verified through the same trust chain.

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
    "https://gitlab.com/api/v4/projects/83847809/packages/generic/ffmpeg-wasi/n9.0.1-1/ffmpeg-wasi-lgpl.wasm",
    afmpeg.WithSHA256("0c4bf74a01317f9c2aa8e76033b3a7f22f6ba7821adbe37fab031ba64873fa5a"),
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
  [`n9.0.1-1`](https://gitlab.com/phpboyscout/ffmpeg-wasi/-/releases/n9.0.1-1)). It speaks the
  structured job spec — drive it with [`Command.JobSpec()` / `RunJob`](compose-a-command.md)
  and [`Probe`](run-in-memory.md).

  A GPL module makes the *combined running program* GPL; afmpeg keeps it at arm's length (a
  separate artifact you fetch), but your obligations follow the variant you choose. The `lgpl`
  module's self-compiled openh264 carries an [AVC patent caveat](https://ffmpeg-wasi.phpboyscout.uk/explanation/licensing/#h264-encode-and-the-avc-patent-pool).
- **Build your own** — any current FFmpeg compiled to `wasm32-wasi` with the feature set
  afmpeg's runtime enables (the stable WebAssembly V2 set plus extended-const, tail-call and
  exception handling, the last of which carries the setjmp/longjmp lowering a real FFmpeg build
  needs). afmpeg runs it, and an engine that answers the vocabulary query must be recent enough —
  see [which engine versions this afmpeg accepts](../reference/release-artifacts.md#which-engine-versions-does-this-afmpeg-accept).

Exact asset filenames, provenance keys, pinned key fingerprints and cache locations are in
[engine releases](../reference/release-artifacts.md); the option defaults are in
[runtime options](../reference/runtime-options.md).
