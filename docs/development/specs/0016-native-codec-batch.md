# 0016 — native codec batch

Status: **IMPLEMENTED** (Phase 2 of the [implementation roadmap](../implementation-roadmap.md);
into the **intermediate** build profile ([0022](0022-build-size-matrix.md)), **flag-only, no
vocabulary change** as designed — more valid `video_codec`/`audio_codec` strings and demuxable
inputs. The §2 native decoders (ac3/eac3/dca, alac/wmav2, the PCM tail, bmp/tiff, prores/dnxhd/dv,
mpeg2video/mpeg4/vc1/wmv3/theora) and encoders (ac3, alac, the PCM tail, gif — with bmp/tiff added
so images round-trip) are in `INTERMEDIATE_ENABLE`; the supported-codec matrix is published
(ffmpeg-wasi `docs/reference/codecs.md`). All LGPL-clean, no `--enable-gpl`/`--enable-nonfree`
(R-PARITY-NATIVE-CODECS). Q1 resolved: both `vc1` and `wmv3`. Q2: the common PCM subset (§2), not
the full grid. Q3: gif encode acceptance rode with 0015/0017. D-0016-B: DTS decode-only (`dca`).
Verified by encode→decode round-trips for every codec with an encoder (ac3, alac, pcm_s24le/f32le,
bmp, tiff — gif via 0015/0017); the **decode-only** codecs are enabled but await the shared
licence-clean media corpus (§8) to exercise. §5 lean/full bucketing handed to 0022.)
Date: 2026-06-30
Parent: [0012](0012-feature-parity-roadmap.md) §4A/§4B/§7; [0007](0007-libav-direct-engine.md) §6 (the codec baseline)
Owns: **R-PARITY-NATIVE-CODECS** (the in-tree decoder/encoder allowlist)

## 1. Why

The engine's `--disable-everything` allowlist (`ffmpeg-wasi build/libav.sh`) names a deliberately
lean codec set — enough for the keyrx workflow and the §9 spread, not for "does it do X?" parity.
The cheapest breadth FFmpeg offers is its **in-tree native codecs**: decoders and encoders that
are part of libavcodec itself, enabled by a single `--enable-decoder=`/`--enable-encoder=` flag —
**no new external library, no new licence surface, no driver code.** The only cost is `.wasm`
size. This spec catalogs which native codecs to add and why, so the build allowlist and the
supported-codec matrix doc grow in one reviewed step. It is the highest value-to-cost row in the
roadmap: pure flags.

## 2. Scope

**Have today** — decode: `h264, hevc, vp8, vp9, mjpeg, png, aac, mp3, opus, vorbis, flac,
pcm_s16le`; encode: `mjpeg, png, aac, flac, pcm_s16le` (+ the **external** `libopenh264`/`libx264`
H.264 encoders, owned by the build, not this spec).

**Add — native decoders:**

| Group | Decoders | Value |
|---|---|---|
| Broadcast/film audio | `ac3, eac3` | high — DVD/Blu-ray/ATSC, TS streams |
| PCM family | `pcm_s24le, pcm_s32le, pcm_f32le, pcm_f64le, pcm_u8, pcm_s16be/s24be, pcm_mulaw, pcm_alaw` | high — pro/telephony audio |
| Image | `gif, bmp, tiff` | high — thumbnail/image pipelines |
| Editing intermediates | `prores, dnxhd, dvvideo` | med — broadcast/NLE sources |
| Legacy/broadcast video | `mpeg2video, mpeg4, vc1, theora` | med — DVD, MPEG-4 ASP, WMV3/VC-1, Ogg |
| Lossless/format audio | `alac, wmav2, dca` (DTS) | med — coverage; DTS is `dca` in libavcodec |

**Add — native encoders:**

| Group | Encoders | Value |
|---|---|---|
| Animated image | `gif` | high — previews; pairs with `palettegen`/`paletteuse` ([0017](0017-native-filter-batch.md)) + the GIF muxer ([0015](0015-container-coverage.md)) |
| Broadcast audio | `ac3` | med — AC-3 output for TS/broadcast targets |
| PCM family | `pcm_s24le, pcm_s32le, pcm_f32le, pcm_mulaw, pcm_alaw` | med — audio coverage |
| Lossless audio | `alac` | med — Apple-lossless output |

**Scope boundary — explicitly NOT this spec:**

- **External-lib codecs** — Opus/MP3/VP8-9/WebP/Vorbis **encode** via libopus/lame/libvpx/libwebp/
  libvorbis → **[0018](0018-lgpl-encoder-expansion.md)** (new libs, `build/deps.sh` work).
- **Subtitle codecs** (srt/ass/webvtt/mov_text) → **[0019](0019-text-and-subtitles.md)**.
- **HEVC/AV1** encode/decode (x265 GPL, dav1d/aom) → **[0023](0023-hevc-av1.md)**.
- **The lean/full size-matrix decision** (which bucket each codec lands in) → **[0022](0022-lean-full-size-matrix.md)**;
  this spec records a *proposed* bucketing per §5, but 0022 owns the axis.

## 3. Approach / design

Append codec names to the existing `ENABLE` allowlist in `build/libav.sh` — `--enable-decoder=…`
and `--enable-encoder=…` gain the §2 names. That is the whole engine change. No `driver.c`/
`process.c` change: the driver already resolves encoders by name from `outputs[].video_codec`/
`audio_codec` and decoders via `av_find_best_stream`/`avcodec_find_decoder`, so an enabled codec is
immediately reachable through the unchanged job-spec. Each name is justified in the build script's
comment and mirrored in the supported-codec matrix doc.

Some natives pull a tiny parser/bitstream dependency already in-tree (e.g. `ac3` needs the AC-3
parser, `prores`/`dnxhd` their own) — these are *internal* to libavcodec and enable transitively;
they are **not** external libs and add no `deps.sh` work. The only thing that grows is the static
archive, hence the size lens (§5).

## 4. Job-spec & build impact

**Job-spec impact: essentially nil.** These codecs add no new vocabulary — they are just more valid
strings in the existing `video_codec`/`audio_codec` fields (encode) and more demuxable inputs
(decode). No new `op`, no field, no version bump of the afmpeg↔ffmpeg-wasi contract. A consumer
that asks for `"audio_codec":"ac3"` on a build where it is enabled simply works; on a build where
it is not, the engine errors as it does for any unknown encoder today.

**Build impact:** `build/libav.sh` `ENABLE` string only. A codec that has no corresponding
container to carry it is useless in isolation — several pair with [0015](0015-container-coverage.md)
(e.g. `ac3`/`dca` in MPEG-TS, `gif` encode with the GIF muxer, `dvvideo` in AVI). Sequencing in §9.

## 5. Licensing & size impact

**Licensing — clean.** Every codec here is **in-tree libavcodec**, covered by libav\*'s LGPL-2.1+
([0007](0007-libav-direct-engine.md) §5). None triggers `--enable-gpl`; none is `--enable-nonfree`.
So the entire batch lands in the **LGPL default variant** — it strengthens the proprietary-compatible
build, not just the GPL one. (Patent posture — AC-3, DTS, MPEG-2/4, VC-1 carry codec patents, same
class as the H.264/HEVC the engine already decodes; the [licensing explanation](../explanation/licensing.md)
codec-patent note covers them, no new licence stance.)

**Size is the only real cost.** Each enabled codec grows the `.wasm`. Rough proposed bucketing for
the [0022](0022-lean-full-size-matrix.md) matrix (size order, not authoritative — 0022 decides):

- **lean (web-delivery essentials):** the PCM family (decode+encode, near-zero), `gif` (decode+encode),
  `bmp`, `tiff`, `ac3`/`eac3` decode. Small, ubiquitous, web-adjacent.
- **full (kitchen-sink coverage):** `prores`, `dnxhd`, `dvvideo`, `mpeg2video`, `mpeg4`, `vc1`,
  `theora`, `alac`, `wmav2`, `dca`, `ac3`/`alac` encode. Larger, niche, editing/broadcast/legacy.

The matrix decision is **deferred to 0022**; this spec only proposes the split so 0022 has a
starting partition. Until 0022 lands, the default is "enable all in both variants" and measure.

## 6. Decisions

- **D-0016-A — native-only; external encoders are 0018.** This spec owns *only* in-tree codecs
  (flag, no lib). The LGPL-clean external quartet (Opus/MP3/VP8-9/WebP) and Vorbis encode are
  0018's, because they add `build/deps.sh` libraries — a different cost class (size + lib + relink
  obligation) warranting its own review.
- **D-0016-B — DTS is decode-only (`dca`).** Enable the `dca` *decoder* for coverage; do **not**
  enable the `dca` encoder (experimental/low-quality in FFmpeg, `-strict experimental`). TrueHD/AMR
  are dropped from this batch (low value vs size) — promote later if a consumer asks.
- **D-0016-C — size bucketing is proposed, not decided here (defer to 0022).** §5 records a
  starting lean/full partition; 0022 owns the axis and the measured numbers. This spec must not
  encode a final per-variant gate.
- **D-0016-D — the supported-codec matrix is a doc deliverable.** Every codec added here lands in
  the ffmpeg-wasi `reference/variants.md` (or a generated codec-matrix page) in the same MR,
  checked against the build's actual `--enable-*` set — so "does it do X?" has an authoritative
  answer ([0012](0012-feature-parity-roadmap.md) §6).

**Open questions:**

- **Q1 — `vc1` vs `wmv3`.** VC-1 and WMV3 are near-siblings (`vc1`/`wmv3` decoders); enable both, or
  just `vc1`? (Lean call: both — they share most code.)
- **Q2 — which PCM tags exactly.** The PCM family is large (`s8/s16/s24/s32` × `le/be` × signed/
  unsigned + float + companded). Enable the common subset (§2) or the full grid? Near-zero size
  each argues for the full grid; pin in implementation.
- **Q3 — does `gif` encode want the muxer first?** GIF encode is only useful with the GIF muxer
  (0015) and `palettegen`/`paletteuse` (0017). Land the encoder flag here but gate the *acceptance*
  test on 0015/0017 (§9).

## 7. Requirements

- **R-PARITY-NATIVE-CODECS** — the engine's allowlist is extended with the §2 in-tree native
  decoders and encoders, enabled in the LGPL (default) variant, with **no** new external library,
  **no** job-spec vocabulary change, and **no** `--enable-gpl`/`--enable-nonfree` trigger. Each
  added codec is justified in the build script and recorded in the supported-codec matrix doc,
  checked against the build. The lean/full per-variant gate is deferred to [0022](0022-lean-full-size-matrix.md).

## 8. Test surface

- **Decode** — a licence-clean fixture per added decoder (or family): a short `.ac3`, a `.gif`/
  `.bmp`/`.tiff`, a ProRes/DNxHD/DV `.mov`, an MPEG-2/MPEG-4/VC-1/Theora sample, an `.alac`/`.wma`/
  `.dts` clip → `probe` reports the expected codec + a `process` decode-to-PCM/PNG succeeds.
- **Encode** — `process` to each new encoder produces a file that re-probes to the right codec:
  PCM/ALAC round-trips (encode→decode→compare), `ac3` encode, `gif` encode (gated on the GIF muxer,
  0015, + palette filters, 0017 — see Q3).
- **Corpus** — the dependency-free in-memory fixtures (synthetic WAV/PNG) cannot cover ProRes/DV/
  AC-3 etc.; a small **licence-clean media corpus** is needed (the [0012](0012-feature-parity-roadmap.md)
  §6 test-surface concern — coordinate so the corpus is shared across 0013–0023, not per-spec).
- **Matrix check** — a test asserting the documented codec matrix matches the build's `--enable-*`
  set, so the doc cannot drift (D-0016-D).

## 9. Dependencies & sequencing

- **Independent of** the driver/vocabulary specs (0013 remux, 0014 seeking) — pure build flags, no
  `process.c` change, so it can land any time.
- **Pairs with [0015](0015-container-coverage.md)** — several decoders/encoders need a container to
  be useful (AC-3/DTS in MPEG-TS, `gif` encode with the GIF muxer, `dvvideo` in AVI). Order 0015
  before/with the *acceptance* tests for those; the flags themselves can precede it.
- **Pairs with [0017](0017-native-filter-batch.md)** for GIF (palettegen/paletteuse) — Q3.
- **Feeds [0022](0022-lean-full-size-matrix.md)** — this batch is a major input to the size-matrix
  decision; 0022 measures the `.wasm` deltas this spec introduces.
- **No dependency on** 0018/0019/0023 (the external-lib / subtitle / HEVC-AV1 work) — disjoint by
  the §2 boundary.

## 10. Definition of done

The §2 native decoders and encoders are enabled in `build/libav.sh`'s allowlist (LGPL variant, no
new lib, no GPL trigger), each justified in-script and reflected in the ffmpeg-wasi supported-codec
matrix doc with a matrix-vs-build check; a licence-clean fixture proves decode and (where
applicable) encode for each, over the real driver; the job-spec is unchanged (no version bump); and
the proposed lean/full bucketing (§5) is handed to [0022](0022-lean-full-size-matrix.md) as its
starting partition. GIF encode acceptance is gated on 0015 + 0017 (Q3).
