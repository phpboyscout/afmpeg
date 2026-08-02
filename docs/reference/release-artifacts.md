---
title: Engine releases — variants, profiles, asset names and caches
description: The ffmpeg-wasi artifacts afmpeg loads — exact filenames, provenance keys, trust keys, cache locations, and the vocabulary version each afmpeg requires.
date: 2026-08-02
tags: [reference, releases, artifacts, variants, profiles]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Engine releases — variants, profiles, asset names and caches

afmpeg ships no engine. It loads one from a published
[ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) release, and this page is the naming and
compatibility contract between the two: what the assets are called, what the provenance keys
are, where verified bytes are cached, and which engine versions a given afmpeg will accept.

## Variants

A variant is a licence build. `afmpeg.Variant`, and the second argument to
`WithModuleRelease` / `native.NewFromRelease`:

| Constant | Value | H.264 encoder | Licence |
|---|---|---|---|
| `VariantLGPL` | `lgpl` | openh264 | LGPL-2.1-or-later |
| `VariantGPL` | `gpl` | libx264 | GPL-2.0-or-later |

Both encode H.264. Anything other than these two values is rejected before any network access:
`afmpeg: unknown variant "…" (want "lgpl" or "gpl")`.

A GPL module makes the *combined running program* GPL. afmpeg's own Go package stays permissive
because the engine is a separate artifact you fetch, but the obligation follows the variant you
choose.

## Profiles

A profile is a capability class. `afmpeg.Profile`, selected with `WithReleaseProfile`:

| Constant | Value | Adds over the previous | Available as |
|---|---|---|---|
| `ProfileLean` | `lean` | web-delivery essentials, smallest build (the default) | WASM **and** native |
| `ProfileIntermediate` | `intermediate` | every practical software codec, format and filter — including AV1 *decode* | WASM **and** native |
| `ProfileFull` | `full` | AV1 *encode* (SVT-AV1, both variants) and HEVC encode (x265, `gpl` only) | **native only** |

There is no WASM `full` module and there is not going to be one — those encoders need threads
and SIMD. `WithModuleRelease(…, WithReleaseProfile(ProfileFull))` fails immediately and names the
alternative:

```
afmpeg: profile "full" is native-only — load it with native.NewFromRelease,
not WithModuleRelease (there is no WASM full module)
```

## Asset names

Exact filenames, because these are what appear in `checksums.txt` and what you would fetch by
hand. `<variant>` is `lgpl` or `gpl`.

### WASM modules

| Profile | Asset |
|---|---|
| lean | `ffmpeg-wasi-<variant>.wasm` |
| intermediate | `ffmpeg-wasi-intermediate-<variant>.wasm` |

Each is also published gzip-compressed with a `.gz` suffix, for use with
`WithModuleURL(…, WithGunzip())`. The lean `lgpl` module of `n8.1.2-11` is about 5.5 MB
uncompressed.

Lean carries no profile segment in its name — that is deliberate backward compatibility with
releases that predate profiles, not an oversight.

### Native drivers

| Profile | Asset |
|---|---|
| lean | `ffmpeg-wasi-driver-<goos>-<goarch>-<variant>` |
| intermediate | `ffmpeg-wasi-driver-<goos>-<goarch>-intermediate-<variant>` |
| full | `ffmpeg-wasi-driver-<goos>-<goarch>-full-<variant>` |

`native.NewFromRelease` builds the name from the host's `runtime.GOOS` and `runtime.GOARCH`.
Drivers are currently published for **linux/amd64 only**, so on any other platform the fetch
fails on a missing asset rather than falling back to WASM.

### Provenance keys

`provenance.json` records each asset under a key, and afmpeg checks that the key it asked for
names the file it fetched. The keys differ from the filenames:

| Asset | Provenance key |
|---|---|
| `ffmpeg-wasi-<variant>.wasm` | `<variant>` |
| `ffmpeg-wasi-intermediate-<variant>.wasm` | `intermediate-<variant>` |
| `ffmpeg-wasi-driver-<goos>-<goarch>-<variant>` | `driver-<goos>-<goarch>-<variant>` |
| `ffmpeg-wasi-driver-<goos>-<goarch>-<profile>-<variant>` | `driver-<goos>-<goarch>-<profile>-<variant>` |

A key that is absent, or that names a different file, fails with `ErrProvenanceMismatch` — the
signature and checksums were fine, but the release does not corroborate what you asked for.

### The rest of a release

| File | Role |
|---|---|
| `checksums.txt` | `sha256sum`-style `<hex>  <name>` lines covering every asset |
| `checksums.txt.sig` | armored OpenPGP detached signature over `checksums.txt` |
| `provenance.json` | build manifest: FFmpeg version, build tag, commit, and the variants map |

The default base URL is
`https://gitlab.com/api/v4/projects/83847809/packages/generic/ffmpeg-wasi`, and an asset's URL
is `<base>/<tag>/<name>`.

## Trust keys

afmpeg embeds the release-signing public keys, so verification cannot be skipped or redirected.
As of this writing two are embedded, and a release is signed by both — a rotation in progress
rather than a choice:

| Fingerprint | Created |
|---|---|
| `710881C1DDAEABD138E53004A2166E59EB6060E1` | 2026-06-30 |
| `4C96ECB35C7446619FF78EB1ED1344E576B7BBBF` | 2026-07-24 |

Both carry the user ID `ffmpeg-wasi Release Signing <ffmpeg-wasi-release-v2@phpboyscout.uk>`,
which is also the Web Key Directory identity afmpeg cross-checks against by default. To fetch
them yourself:

```sh
gpg --locate-external-keys ffmpeg-wasi-release-v2@phpboyscout.uk
```

The older `ffmpeg-wasi-release@phpboyscout.uk` address still resolves through WKD, but it serves
only the 2026-06-30 key. Use the `-v2` address — it serves both, which is what verifying a
current release needs.

## Where verified bytes are cached

| What | Path | Filename |
|---|---|---|
| any downloaded asset — a `WithModuleURL` or `WithModuleRelease` module, and a native driver on its way in | `os.UserCacheDir()/afmpeg` (override: `WithCacheDir` / `WithReleaseCacheDir`) | the asset's SHA-256 when the digest is known, otherwise a hash of the URL, always with a `.wasm` suffix |
| a native driver, as an executable | `os.UserCacheDir()/afmpeg/native-driver` — **not** affected by `WithReleaseCacheDir` | the asset name plus the first 8 bytes of its SHA-256, mode `0755` |

A native driver therefore lands twice: the verified bytes go through the download cache above on
their way in, and the executable copy is written under `native-driver`.

On Linux `os.UserCacheDir()` is `~/.cache`, so a module lands at
`~/.cache/afmpeg/<sha256>.wasm`.

Both caches are content-addressed, which is what makes them safe: a cache entry is re-checked
against its expected digest on read, so a corrupted or tampered file is discarded and re-fetched
rather than executed. Cache **writes** are best-effort — a read-only cache directory costs you a
download next time, it does not fail the run.

If `os.UserCacheDir()` fails, module caching fails with `afmpeg: resolve cache dir`, while the
native driver falls back to `os.TempDir()`.

## Which engine versions does this afmpeg accept?

afmpeg stamps every process and probe job with the **job-spec vocabulary version** it emits, and
`New` checks the engine can handle it before returning. This afmpeg emits **v9**.

| Version | Introduced |
|---|---|
| 1 | baseline plus the version gate itself |
| 2 | stream copy and bitstream filters — `CodecCopy`, `in:type[:idx]` map specifiers, `BitstreamFilters` |
| 3 | seeking and time ranges — `Input.Seek`, `Output.Duration`/`End`, `CopyTS` |
| 4 | input options and formats — `Input.Format`, `Input.Options`, `N:v:K` graph selection |
| 5 | container coverage — `Output.Format`, `Output.FormatOptions` |
| 6 | frame extraction — the `frames` op |
| 7 | metadata and chapters — `Output.Metadata`/`Chapters`/`StreamMetadata`, and the read side on `Probe` |
| 8 | subtitle streams — `Output.SubtitleCodec` and `N:s` map specifiers |
| 9 | the job progress side-channel — `progress: true` and `/dev/afmpeg-progress` |

Three outcomes at `New`:

- **The module answers the `version` op with v9 or higher** — accepted.
- **It answers with less than 9** — rejected, loudly:
  `afmpeg: ffmpeg-wasi engine vocabulary vN is older than this afmpeg requires (v9); upgrade the module`.
  Failing here is the point: an older engine would otherwise drop a field it does not know about
  and produce quietly wrong output at the first job.
- **It does not answer at all** — tolerated. A pre-gate engine or a generic WASI module has no
  vocabulary contract to check, and a `Runtime` stays able to run any wasm module.

The check costs one invocation of the module at `New`, against an empty in-memory filesystem.

Engine-side features can still be newer than the vocabulary: `duration_us` in progress records
arrived in **n8.1.2-10** without a vocabulary bump, because an engine that omits it simply falls
back to byte-observed progress.

## See also

- [Obtain a module](../how-to/obtain-a-module.md) — how to pass any of this to `New`
- [Verify a release by hand](../how-to/verify-a-release-by-hand.md) — the same chain with `gpg` and `sha256sum`
- [Verifying a release](../explanation/concepts/release-verification.md) — what each layer defends against
- [Runtime options](runtime-options.md) — the option names and their defaults
