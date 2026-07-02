# 0022 — the build & distribution matrix

Status: **DRAFT / SCOPING** (refined 2026-07-02 — the lean/intermediate/full profile model, the
runtime×profile mapping, and the native platform set are RESOLVED with the user; the concrete
codec-set-per-profile in §6 is the proposed starting policy. This spec **governs the
codec-bundling decision every other spec defers to**.)
Date: 2026-06-30 (refined 2026-07-02)
Parent: [0012](0012-feature-parity-roadmap.md), [0007](0007-libav-direct-engine.md)
Governs the bucketing for: [0015](0015-container-coverage.md) [0016](0016-native-codec-batch.md)
[0017](0017-native-filter-batch.md) [0018](0018-lgpl-encoder-expansion.md)
[0019](0019-text-and-subtitles.md) [0023](0023-hevc-and-av1.md); the native runtime is
[0028](0028-native-subprocess-backend.md).
Owns: **R-AF-3** (the build-variant / size-matrix portion)

## 1. Why

As the parity specs add codecs/formats/filters and 0028 adds a native runtime, "**what goes in
which distributable?**" needs one governing policy rather than each spec guessing. This is it: the
axes, the profiles, the artifact matrix, and the release/signing/pinning implications. The codec
specs (0015–0019, 0023) each say *what a codec is*; this spec says *which artifact it ships in*.

## 2. The axes

| Axis | Values | Notes |
|---|---|---|
| **Runtime** | WASM · Native | WASM: sandboxed, portable, **arch-independent**, single-thread. Native ([0028](0028-native-subprocess-backend.md)): threads + SIMD + HW-accel, **per-platform**. |
| **Profile** | lean · intermediate · full | Capability *classes* (§3), not arbitrary size cuts. |
| **Licence** | LGPL · GPL | LGPL is the default; GPL adds x264 / x265. |
| **Platform** (native only) | linux/amd64 · linux/arm64 · darwin/arm64 | Windows **deferred**, no date (D-0022-F). WASM has no platform axis. |

## 3. The profiles — capability classes

- **Lean** — web-delivery essentials: the codecs/containers/filters that cover the great majority
  of real jobs at the smallest size. Roughly what ffmpeg-wasi ships today. **WASM only.**
- **Intermediate** — lean **+ every practical *software* codec** (no hardware, no heavy
  thread-hungry encoders): the native-decoder batch, the LGPL encoder libs, the full filter set,
  all software containers, subtitles, AV1-*decode*. **Both runtimes, and byte-for-byte the *same
  codec set* in each** (D-0022-B — clean partitions; intermediate never flexes or muddies between
  WASM and Native).
- **Full** — intermediate **+ the heavy hitters**: heavy *software* encoders (x265/HEVC, software
  AV1 encode) **and** hardware-accelerated codecs (NVENC/VAAPI/VideoToolbox/QSV). Threads + SIMD +
  hardware make these viable, so **Native only**.

The load-bearing property: **intermediate is identical in WASM and Native.** A consumer develops
against WASM-intermediate (sandboxed, portable, slower) and drops to Native-intermediate (identical
capabilities, fast, unsandboxed) with **zero code or codec-support change** — only the
performance/security posture shifts.

## 4. Runtime × profile mapping

- **WASM → { lean, intermediate }.** No WASM-full: HW-accel and the heavy encoders need native.
- **Native → { intermediate, full }.** No Native-lean: native's whole point is capability, so
  intermediate is its floor.

## 5. The artifact matrix

Per release:

| Runtime | Profile | Licences | Platforms | Count |
|---|---|---|---|---|
| WASM | lean | lgpl, gpl | (arch-independent) | 2 |
| WASM | intermediate | lgpl, gpl | (arch-independent) | 2 |
| Native | intermediate | lgpl, gpl | linux/amd64, linux/arm64, darwin/arm64 | 6 |
| Native | full | lgpl, gpl | linux/amd64, linux/arm64, darwin/arm64 | 6 |

**= 16 signed artifacts per release** (Windows, when un-deferred, adds intermediate+full × lgpl+gpl
= 4 → 20). We do not shy from this number — §7 shows it costs one signature, plus a build matrix.

## 6. Codec-set-per-profile — the proposed policy (what the other specs defer to)

Additive: each profile is the previous plus its column. Codec specs are cited for the source of each set.

| | **Lean** (WASM) | **Intermediate** (+ over lean) | **Full** (+ over intermediate, Native) |
|---|---|---|---|
| **Decode** | h264, hevc, vp8/9, aac, mp3, opus, vorbis, flac, png, mjpeg | ac3/eac3, pcm family, prores, dnxhd, mpeg2/4, vc1, theora, alac, dts, dv, gif/bmp/tiff/webp, **AV1 (dav1d)** [0016/0023] | — |
| **Encode** | h264 (openh264; +**libx264** in gpl), aac, mjpeg, png | **Opus, MP3, VP8/9, WebP, Vorbis** [0018]; gif, ac3, pcm, alac [0016] | **x265/HEVC** (gpl only), **software AV1** (aom/SVT) [0023]; **HW: h264/hevc/av1 _nvenc/_vaapi/_videotoolbox/_qsv** |
| **Containers** | mp4, mov, mkv, webm, mp3, wav, ogg, image2 | mpegts, flv, avi, gif, hls, dash, cmaf, adts, caf, aiff [0015] | — |
| **Filters** | the curated set (scale/crop/pad/overlay/concat/xfade/format/fps + core audio) | the full native batch [0017] + drawtext/subtitles (freetype/libass) [0019] | — |

**Licence placement:** openh264 → all (LGPL); **libx264** → any *gpl* variant (incl. lean-gpl);
**x265** → *full-gpl only* (GPL + heavy); HW-accel encoders → both licences (the hardware encodes,
the wrappers are LGPL-compatible); the LGPL encoder quartet (Opus/MP3/VP8-9/WebP) → both.

## 7. Release, signing & pinning

- **Signing scales for free.** One `checksums.txt` + one OpenPGP signature **per release** covers
  *all 16 artifacts* (the manifest just lists more files) — the [0010](0010-signed-release-acquisition.md)/[0011](0011-wkd-attestation.md)
  model is unchanged; WKD/provenance don't grow. `provenance.json` enumerates every
  (runtime, profile, licence, platform).
- **Consumer selection.** afmpeg's `variant` generalises to **(profile, licence)**;
  `WithModuleRelease` picks the WASM artifact, `WithNativeRelease` adds the **platform**
  auto-detected from `runtime.GOOS`/`GOARCH`. A clear error when a requested (profile, platform)
  isn't published.
- **The real cost is the native build matrix, not the count.** Cross-compiling libav\* + the driver
  for `darwin/arm64` and `linux/arm64` from the CI host needs per-target toolchains/sysroots
  (goreleaser-style runners or cross-SDKs). 16 CI build jobs. This — not signing — is the genuine
  new engineering, and it lands with 0028's native tier, not before.

## 8. Decisions

- **D-0022-A — three profiles** (lean/intermediate/full) defined by capability class, not size.
- **D-0022-B — intermediate is identical across WASM and Native. RESOLVED (2026-07-02):** clean
  partitions; no flexing or muddied middle.
- **D-0022-C — heavy *software* encode (x265, software AV1) lands in FULL, not intermediate.
  RESOLVED:** keeps intermediate clean + practical single-threaded.
- **D-0022-D — mapping:** WASM {lean, intermediate}; Native {intermediate, full}.
- **D-0022-E — lean ships BOTH LGPL and GPL. RESOLVED:** x264 offers quality gains; let consumers
  choose.
- **D-0022-F — native platforms: linux/{amd64,arm64} + darwin/arm64. RESOLVED;** Windows deferred,
  no date.
- **D-0022-G — one signature per release covers every artifact** (no per-artifact signing).
- **Open:** the exact lean vs intermediate line for a few borderline decoders; whether WASM-full is
  ever warranted (currently no); the `provenance`/naming scheme for 16+ assets.

## 9. Requirements

- **R-AF-3 (size/variant portion)** — releases publish the profile × licence × runtime × platform
  matrix (§5); each artifact's codec set follows the §6 policy; intermediate is identical across
  runtimes; all artifacts are covered by a single per-release signature; afmpeg selects by
  (profile, licence, platform). A per-artifact **size budget** + a CI size-assertion gate.

## 10. Sequencing & dependencies

This spec **gates the bucket placement** the codec specs (0016/0017/0018/0019/0023) defer to — so
it should be settled before those build. The WASM profiles (lean/intermediate) can ship first (no
new runtime); the Native profiles (intermediate/full) + the platform matrix depend on
[0028](0028-native-subprocess-backend.md) and its build toolchain. Supersedes the current
lgpl/gpl-only variant model ([0007](0007-libav-direct-engine.md) §5, ffmpeg-wasi `variants.md`),
which becomes the WASM-lean row of this matrix.

## 11. Definition of done

The axes, the three capability-class profiles, the runtime×profile×licence×platform matrix (16
artifacts), the codec-set-per-profile policy, and the release/signing/pinning implications are
recorded and agreed. The codec specs cite §6 for placement; 0028 cites §5/§7 for the native matrix;
the size-budget gate is specified. Implementation (the build matrix, the native toolchains, the
afmpeg selection API) is downstream and uncommitted here.
