---
title: Runtime options
description: Every option afmpeg.New accepts — what it does, what it defaults to, and what happens when it is wrong or omitted.
date: 2026-08-02
tags: [reference, runtime, options, defaults]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Runtime options

Every option `afmpeg.New` accepts, with its default and its failure mode. These are the
`afmpeg.Option` values passed to `New`, plus the two nested option sets — `FetchOption` for
`WithModuleURL` and `ReleaseOption` for `WithModuleRelease`.

```go
rt, err := afmpeg.New(ctx, opts...)
```

`New` is the only place these apply. Per-invocation behaviour (a deadline you bring yourself,
a progress channel) rides on the context you pass to `Run`/`RunJob`/`Probe`/`Frames` instead.

## Defaults at a glance

| Option | Default when omitted | Disable it with |
|---|---|---|
| `WithMemoryLimit` | **512 MB** (`512 << 20` bytes) of guest linear memory | `WithMemoryLimit(0)` |
| `WithTimeout` | **1 hour** per invocation | `WithTimeout(0)` |
| module source | none — `New` returns `ErrNoModule` | — (one is mandatory) |
| `WithBackend` | the sandboxed WASM backend (wazero) | — |
| `WithReleaseProfile` | `ProfileLean` | — |
| `WithReleaseWKDEmail` | `ffmpeg-wasi-release-v2@phpboyscout.uk` | `WithReleaseWKDEmail("")` |
| `WithSHA256` | none — a `WithModuleURL` download is **not** verified | — (always set it) |
| `WithCacheDir` / `WithReleaseCacheDir` | `os.UserCacheDir()/afmpeg` | — |
| `WithHTTPClient` / `WithReleaseHTTPClient` | `http.DefaultClient` (no client timeout) | — |

## Which module does afmpeg run?

The module is never embedded, so exactly one source is required. `New` returns
[`ErrNoModule`](../explanation/components/errors.md) when none is configured and no
`WithBackend` was given:

```
afmpeg: no wasm module configured (use WithModuleRelease, WithModuleURL,
WithModuleFile, WithModuleBytes, or WithModuleFS)
```

| Option | Source | Verified |
|---|---|---|
| `WithModuleRelease(tag, variant, opts...)` | a published ffmpeg-wasi release | **yes** — signature, checksum and provenance, always |
| `WithModuleURL(url, opts...)` | any URL, cached on disk | only if you pass `WithSHA256` |
| `WithModuleFile(path)` | a host filesystem path | no |
| `WithModuleBytes(b)` | bytes you already hold | no |
| `WithModuleFS(fs, path)` | any `afero.Fs` | no |

### What happens if you pass two module options

They do not merge and they do not error — one wins, and it may not be the last one you wrote:

- **Direct bytes beat a deferred fetch, whatever the order.** `WithModuleFile`,
  `WithModuleBytes` and `WithModuleFS` load bytes as the option is applied;
  `WithModuleURL` and `WithModuleRelease` only register a fetch, which `New` runs *if* no
  bytes are present. So `New(ctx, WithModuleRelease(…), WithModuleFile("ffmpeg.wasm"))`
  runs the file and never fetches or verifies anything.
- **Among options of the same kind, the last one wins** — two `WithModuleFile` calls leave
  the second file's bytes; two `WithModuleURL` calls leave the second URL's fetch.

Pass one. Passing two is a silent choice, not a rejected one.

### What `WithBackend` switches off

`WithBackend(b)` replaces the WASM backend with your own — in practice the
[native driver](../how-to/use-the-native-backend.md) from `pkg/afmpeg/native`. When it is set:

- **no module is resolved or compiled** — every `WithModule*` option becomes inert, including
  a `WithModuleRelease` fetch, which never runs;
- **`WithMemoryLimit` has no effect** — the cap is a wazero runtime setting, and there is no
  wazero runtime. A native driver is an ordinary subprocess bounded by the operating system;
- **`WithTimeout` still applies** — the deadline is imposed by `Run`, above the backend seam.

`New` still runs its vocabulary preflight against the backend, so an outdated native driver is
rejected exactly as an outdated module is.

## Memory: `WithMemoryLimit`

Caps the guest's WebAssembly linear memory. Past the cap the guest's `memory.grow` fails, so an
over-allocating decode returns `ENOMEM` and exits non-zero instead of taking the host process
down with it.

| | |
|---|---|
| **Default** | `512 << 20` bytes (512 MB) |
| **Units** | bytes, rounded **up** to whole 64 KiB WebAssembly pages |
| **Ceiling** | clamped to the wasm32 maximum of 65,536 pages (4 GiB) — a larger value is silently reduced rather than panicking |
| **Disable** | `WithMemoryLimit(0)`; any value ≤ 0 means "no cap" |
| **Applies to** | the WASM backend only |

Hitting the cap is **not** a Go error. The guest fails its allocation and exits, so you see a
non-zero `Result.ExitCode` with an allocation failure in `Result.Stderr`. If a legitimate job
needs more — large frames, many parallel filter buffers — raise it explicitly rather than
removing it.

## Time: `WithTimeout`

The default deadline `Run` imposes on a single invocation.

| | |
|---|---|
| **Default** | 1 hour |
| **Disable** | `WithTimeout(0)` |
| **Precedence** | **your context wins.** If the context passed to `Run`/`RunJob`/`Probe`/`Frames` already carries a deadline, afmpeg uses it unchanged and never extends it. The default applies only to a context with no deadline of its own |
| **When the clock starts** | after the invocation slot is acquired, so time spent queued behind another job does not eat the budget |
| **Applies to** | every backend |

On the WASM backend an invocation that overruns returns a wrapped context error —
`afmpeg: invocation aborted` — rather than a `Result`. On the native backend the driver process
is killed instead, which surfaces as a `Result` with a negative `ExitCode`. Either way the
`Runtime` stays usable afterwards.

A call that is still **queued** behind another invocation when its context is cancelled gives up
there and then, with `afmpeg: run: cancelled while queued`. It does not wait for the job in front
of it to finish first.

The deadline does **not** cover `New` itself. The vocabulary preflight runs the trusted, pinned
module and is bounded only by the context you pass to `New`, which is also what bounds a
`WithModuleURL` or `WithModuleRelease` download.

## Download options for `WithModuleURL`

`FetchOption` values, passed after the URL. They apply to `WithModuleURL` only.

| Option | Default | Effect |
|---|---|---|
| `WithSHA256(hex)` | none | Verifies the downloaded (and decompressed) bytes. Mismatch → `ErrChecksumMismatch`, wrapped with the digest actually seen; the bytes are never compiled. **Without it the module is executed unverified.** |
| `WithGunzip()` | off | Decompresses a gzip download (`ffmpeg-wasi-lgpl.wasm.gz`). The checksum is checked **after** decompression, so pass the SHA-256 of the `.wasm`, not of the `.gz`. |
| `WithCacheDir(dir)` | `os.UserCacheDir()/afmpeg` | Where the module is cached. |
| `WithHTTPClient(c)` | `http.DefaultClient` | The client used for the download. The default has **no timeout** — the fetch is bounded only by `New`'s context. |

Other things this path enforces, none of them configurable:

- **A 256 MiB ceiling** on the downloaded, decompressed module. Over it:
  `afmpeg: module exceeds the 268435456-byte limit`.
- **Any non-200 response fails**: `afmpeg: download module: unexpected status …`.
- **The cache is content-addressed when it can be.** With `WithSHA256` the filename is the
  digest, so two URLs serving identical bytes share one entry, and a corrupt entry fails its
  checksum and is re-fetched. Without a SHA the filename is a hash of the URL and a stale entry
  is reused indefinitely.
- **Writing the cache is best-effort.** A read-only or full cache directory does not fail the
  run; it just means the next run downloads again.

## Certified-release options for `WithModuleRelease`

`ReleaseOption` values. The same set applies to `afmpeg.FetchReleaseAsset` and to
`native.NewFromRelease`.

| Option | Default | Effect |
|---|---|---|
| `WithReleaseProfile(p)` | `ProfileLean` | Selects the capability profile. `WithModuleRelease` accepts `ProfileLean` and `ProfileIntermediate` only. |
| `WithReleaseProvenance(&p)` | not captured | Writes the verified `Provenance` into your variable, so you can log exactly what you loaded. |
| `WithReleaseBaseURL(url)` | `https://gitlab.com/api/v4/projects/83847809/packages/generic/ffmpeg-wasi` | Fetch from a mirror or internal store. The signature is over content, so the URL is untrusted input — verification is unchanged. |
| `WithReleaseBundleDir(dir)` | online | Verify a local directory of pre-fetched assets instead of downloading. Air-gapped. |
| `WithReleaseCacheDir(dir)` | `os.UserCacheDir()/afmpeg` | Where the verified module is cached. |
| `WithReleaseHTTPClient(c)` | `http.DefaultClient` | Client for the release assets *and* the WKD key fetch. |
| `WithReleaseWKDEmail(email)` | `ffmpeg-wasi-release-v2@phpboyscout.uk` | The Web Key Directory identity the embedded key is cross-checked against. `""` disables the cross-check. |

### Which arguments are rejected outright

`New` returns an error before any network access when:

- **the variant is neither `lgpl` nor `gpl`** — `afmpeg: unknown variant "…" (want "lgpl" or "gpl")`;
- **the profile is `full`** — there is no WASM full module, so `WithModuleRelease` refuses it and
  names the alternative: `profile "full" is native-only — load it with native.NewFromRelease`;
- **the profile is anything else unrecognised** — `afmpeg: unknown profile "…" (want "lean" or
  "intermediate")`.

The tag is **not** validated. A tag that does not exist surfaces later as a download failure on
`checksums.txt`, not as a "no such release" error.

### What an offline bundle must contain

`WithReleaseBundleDir(dir)` reads four files from `dir`, all required, all by exact name:

| File | What it is |
|---|---|
| `ffmpeg-wasi-<variant>.wasm` (or `ffmpeg-wasi-intermediate-<variant>.wasm`) | the module for the requested variant and profile |
| `checksums.txt` | the digest manifest |
| `checksums.txt.sig` | the armored OpenPGP detached signature over `checksums.txt` |
| `provenance.json` | the build manifest naming each variant's file |

A missing one fails with `afmpeg: read <name> from bundle`. Verification is identical to the
online path and still mandatory; the offline path simply never touches the network, which also
means **the WKD cross-check does not run** — the embedded key is the only anchor.

### What the WKD cross-check does and does not do

On an online fetch afmpeg resolves its trust set from two places: the OpenPGP keys embedded in
the binary, and the same identity published through the Web Key Directory on
`phpboyscout.uk`. Then:

- **they must agree by fingerprint.** A divergence is a hard failure — the release is not loaded;
- **an unreachable WKD is tolerated.** The cross-check is a second anchor, not the trust root, so
  an outage falls back to the embedded key rather than failing the build;
- **`WithReleaseWKDEmail("")` skips it entirely.** Verification still happens against the pinned
  embedded key.

Why it exists at all is covered in
[verifying a release](../explanation/concepts/release-verification.md).

## Errors `New` can return

| Error | Cause |
|---|---|
| `ErrNoModule` | no module source and no `WithBackend` |
| `ErrChecksumMismatch` | downloaded bytes disagree with `WithSHA256`, or with the signed `checksums.txt` |
| `ErrProvenanceMismatch` | the signed provenance does not name the requested variant/profile |
| `verify.ErrSignatureInvalid` | `checksums.txt.sig` does not verify against the trusted keys |
| `verify.ErrKeyResolverMismatch` | the embedded key and the WKD key disagree |
| `afmpeg: compile module` | wazero could not compile the bytes — usually not a WASI ffmpeg build, or one needing features this runtime does not enable |
| `afmpeg: ffmpeg-wasi engine vocabulary vN is older than this afmpeg requires (v9); upgrade the module` | the module is a gated ffmpeg-wasi engine too old for the job spec this afmpeg emits |

The vocabulary check only fires for a module that answers the engine's `version` op. A generic
WASI module that does not is run as-is — a `Runtime` can drive any wasm module, it just has no
vocabulary contract to check. See
[engine releases](release-artifacts.md#which-engine-versions-does-this-afmpeg-accept).

## See also

- [Why a Runtime is capped, deadlined and serialised](../explanation/concepts/safe-defaults.md) —
  the reasoning behind the two defaults above.
- [Reuse a Runtime across many invocations](../how-to/reuse-a-runtime.md) — the long-lived pattern.
- [Obtain a module](../how-to/obtain-a-module.md) — the task-shaped version of the module options.
- [Limitations](limitations.md) — what these options cannot do.
