---
title: Use the native backend (native-speed encode, HEVC/AV1)
description: Drive afmpeg with the signed native driver instead of the WASM module — for native-speed software encode and the full profile's HEVC/AV1 encoders.
date: 2026-07-05
tags: [how-to, native, backend, hevc, av1]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Use the native backend

By default afmpeg runs the [ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) engine as a
sandboxed WebAssembly module — portable, arch-independent, single-threaded. For consumers who
are encode- or throughput-bound, afmpeg also has an **opt-in native backend** (spec
[0028](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0028-native-subprocess-backend), "Backend B"): the *same*
libav-direct engine compiled to a native ELF, driven over an in-memory IPC bridge. It is
**CGO-free** — a subprocess, not a linked library — so afmpeg's Go package stays permissively
licensed, and all I/O still crosses your `afero.Fs`, never the host disk.

Reach for it when you want:

- **Native-speed software encode** — threads + SIMD make H.264/VP9/etc. **48–58× faster** than
  the single-threaded WASM build.
- **HEVC or AV1 encode** — the heavy `libx265` / `libsvtav1` encoders are impractical in WASM,
  so they ship **only** in the native driver's **full** profile.

!!! note "Platform"
    The native driver is currently published for **linux/amd64** only. `NewFromRelease` resolves
    the driver for the host `GOOS`/`GOARCH`; on other platforms use the WASM module (or build the
    driver yourself). Hardware-accelerated encoders (NVENC/VAAPI/…) are a future addition to the
    full profile, gated on device availability.

## Certified native driver (recommended)

`native.NewFromRelease` is the native equivalent of
[`WithModuleRelease`](obtain-a-module.md): it fetches the signed native driver for `(tag,
variant)` from a published ffmpeg-wasi release, **verifies it through the same trust chain**
(the KMS/OpenPGP signature over `checksums.txt` against afmpeg's pinned key, the asset checksum,
the provenance match) **before the binary is ever written or executed**, caches it as a
content-addressed executable, and returns a `Backend`. Wire that into `afmpeg.New` with
`WithBackend`:

```go
import (
    "gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
    "gitlab.com/phpboyscout/afmpeg/pkg/afmpeg/native"
)

backend, err := native.NewFromRelease(ctx, "n8.1.2-11", afmpeg.VariantLGPL)
if err != nil { /* ... */ }

rt, err := afmpeg.New(ctx, afmpeg.WithBackend(backend))
// rt.RunJob / rt.Probe / rt.Frames — the API is identical to the WASM path.
```

A consumer **never executes an unverified binary**: fetch-and-verify happens up front, exactly
as for a `.wasm`. All the [`WithModuleRelease` options](obtain-a-module.md) apply
(`WithReleaseBaseURL` for a mirror, `WithReleaseBundleDir` for air-gapped verification,
`WithReleaseCacheDir`, `WithReleaseHTTPClient`, `WithReleaseWKDEmail`).

### Profiles: lean, intermediate, or full

The native driver ships in all three [capability
profiles](https://ffmpeg-wasi.phpboyscout.uk/reference/variants/); select one with
`WithReleaseProfile` (default `ProfileLean`):

| Profile | Adds over the previous | Asset |
|---|---|---|
| `ProfileLean` | H.264 encode (openh264; libx264 in gpl) at native speed | `ffmpeg-wasi-driver-linux-amd64-<variant>` |
| `ProfileIntermediate` | the full software batch — Opus/MP3/Vorbis/WebP/VP8-9 + subtitles, and **AV1 *decode*** (`libdav1d`) | `…-driver-linux-amd64-intermediate-<variant>` |
| `ProfileFull` | AV1 *encode* (`libsvtav1`, both variants), **HEVC** encode (`libx265`, **gpl only**) | `…-driver-linux-amd64-full-<variant>` |

AV1 **decode** (via `libdav1d`) is in the intermediate profile on **both** runtimes — the WASM
module decodes AV1 too (a single-threaded dav1d build; correct but slower than the threaded native
driver). AV1 **encode** needs the full profile (native only).

`ProfileFull` is the only way to reach HEVC/AV1 encode. HEVC (`libx265`) is GPL, so it is present
**only in the `gpl` variant** — the `lgpl` full driver encodes AV1 but rejects `libx265`. See the
[HEVC/AV1 licence &amp; patent posture](https://ffmpeg-wasi.phpboyscout.uk/explanation/licensing/#hevc--av1-encode--the-full-native-profile-only).

```go
// The full/gpl driver: HEVC (x265) + AV1 (SVT-AV1) at native speed.
backend, err := native.NewFromRelease(ctx, "n8.1.2-11", afmpeg.VariantGPL,
    afmpeg.WithReleaseProfile(afmpeg.ProfileFull))

rt, err := afmpeg.New(ctx, afmpeg.WithBackend(backend))

cmd := afmpeg.Command{
    Inputs:        []afmpeg.Input{{Path: "in.mp4"}},
    FilterComplex: "[0:v]format=yuv420p[v]",
    Outputs: []afmpeg.Output{{
        Path: "out.mp4", Map: []string{"[v]"},
        VideoCodec: "libx265",
        Options:    map[string]string{"preset": "medium"},
    }},
}
res, err := rt.RunJob(ctx, fs, cmd) // encodes HEVC over the IPC bridge, into fs
```

## From a local driver binary

If you have already built or downloaded the driver (e.g. via
`ffmpeg-wasi`'s [`build/Dockerfile.native`](https://ffmpeg-wasi.phpboyscout.uk/how-to/build-from-source/)),
point the backend straight at it — no fetch, no verification (you supply the bytes, you accept
them):

```go
backend := native.New(native.WithNativeBinary("/path/to/driver"))
rt, err := afmpeg.New(ctx, afmpeg.WithBackend(backend))
```

This is the bring-your-own path — the analogue of `WithModuleFile`. For the project's own
releases prefer `NewFromRelease`, which verifies the binary before it runs.

## How it works

`WithBackend` swaps afmpeg's execution seam: instead of instantiating the WASM module in
wazero, the native backend spawns the driver as a subprocess and serves your `afero.Fs` to it
over a Unix socket (a framed read/write/**seek** protocol — so even a muxer's backward seeks,
like an MP4 `moov` patch, round-trip through the filesystem you passed, never host disk). The
job spec, the `Command`/`Probe`/`Frames` API, and the results are **identical** to the WASM
path — only the runtime underneath changes. See the
[architecture overview](../explanation/concepts/architecture.md) and spec
[0028](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0028-native-subprocess-backend) for the full design.

!!! warning "Trust boundary"
    The native driver is a **native subprocess**, not a WASM sandbox — it runs with your
    process's privileges. Load it only from a source you trust: `NewFromRelease` gives you the
    signature-verified project artifact; `WithNativeBinary` trusts whatever path you supply.
