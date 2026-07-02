# 0012 — feature-parity roadmap (toward a complete standalone offering)

Status: **ROADMAP / SURVEY** (not buildable work — a gap analysis that catalogs what ffmpeg-wasi
is missing relative to FFmpeg, so each coherent gap can be promoted to its own spec. The
disposition table in §7 is the dispatch list, mirroring how [0006](0006-hardening-roadmap.md)
worked.)
Date: 2026-06-30
Parent: [0007](0007-libav-direct-engine.md) (the engine this extends); [0001](0001-afmpeg.md)
Spans: ffmpeg-wasi (the engine: codecs/formats/filters/build) + afmpeg (the job-spec vocabulary)

## 1. Why

ffmpeg-wasi is now a standalone, signed, feature-functional component (current FFmpeg, libav-direct,
CGO-free; multi-input `filter_complex` + multi-output muxing as of `n8.1.2-5`). To be *the*
reference server-side FFmpeg-on-WASI for **other** consumers — not just keyrx — it needs breadth:
a consumer evaluating it asks "does it do X?" and today the honest answer for most of FFmpeg's
surface is "not yet." This document is the map from "it transcodes" to "feature parity within the
WASI envelope," organised so each area becomes a spec.

## 2. The envelope — what "parity" can and cannot mean

wasm32-wasi, single-threaded, sandboxed, size-conscious. Four FFmpeg capabilities are **out of
scope by construction** — they are non-goals, not gaps, and bound the definition of parity:

| Constraint | Build flag | Consequence | Why it's fixed |
|---|---|---|---|
| **No threads** | `--disable-pthreads` | single-threaded encode/decode/filter | wazero has no `wasi-threads` thread-spawn; threading would forfeit CGO-free (R-AF-1). Bounds *throughput*, not *capability*. → [0008](0008-performance-strategy.md) |
| **No SIMD/asm** | `--disable-asm` | correctness fine, slower hot paths | FFmpeg's asm is x86; wasm SIMD128 would need different intrinsics FFmpeg doesn't emit. → 0008 |
| **No network** | `--disable-network` | no `http/rtmp/rtsp/hls`-over-network I/O | sandbox posture + a consumer fetches bytes itself (afmpeg's job). file/pipe only. |
| **Size budget** | `--enable-small` | every codec lib grows the `.wasm` | the **lean vs full** split (R-AF-3) governs which libs ship; some gaps are "yes, but in the full build." |

Everything below is achievable *within* this envelope. "Parity" = **everything FFmpeg does that a
single-threaded, sandboxed, file-based media processor can do.**

## 3. The licensing lens (which variant a gap lands in)

phpboyscout ships an **LGPL default** + a **GPL/full opt-in** variant. The good news for parity:
**most high-value codec gaps are LGPL-clean external libs** — they enrich the *default* variant:

- **LGPL/BSD-clean (default variant):** libvpx (VP8/9, BSD), libopus (BSD), libmp3lame (LGPL),
  libvorbis (BSD), libwebp (BSD), dav1d (AV1 decode, BSD), libass (subtitles, ISC), freetype
  (drawtext). Plus the many **native** (in-tree) decoders/muxers/filters — zero new lib, size only.
- **GPL-only (full variant):** libx265 (HEVC encode), a handful of GPL filters. Already the home of
  libx264.

So the bulk of parity work expands the **LGPL default** — strengthening the proprietary-compatible
offering, not just the GPL one.

## 4. Gap domains

Have-today is the `--disable-everything` allowlist in `ffmpeg-wasi build/libav.sh` +
`build/deps.sh` (external libs: zlib, x264, openh264). "Cost" = `.wasm` size + new lib + driver
work; "native" means in-tree (flag only, no lib).

### 4A. Decoders (input coverage)

Have: `h264, hevc, vp8, vp9, mjpeg, png, aac, mp3, opus, vorbis, flac, pcm_s16le`.

| Missing | Kind | Value | Notes |
|---|---|---|---|
| **AC-3 / E-AC-3** | native | high | broadcast/film audio; cheap |
| **PCM family** (`s24le, f32le, mulaw, alaw, …`) | native | high | pro/telephony audio; cheap |
| **Image: webp, gif, bmp, tiff** | native (webp needs libwebp for some) | high | thumbnail/image pipelines |
| **ProRes, DNxHD, DV** | native | med | editing/broadcast intermediates |
| **MPEG-2 / MPEG-4 / VC-1 / Theora** | native | med | legacy + broadcast |
| **ALAC, WMA, DTS, AMR, TrueHD** | native | med | format coverage |
| **AV1** | libdav1d (BSD) ext | med-high | growing; dav1d is sizeable but the right decoder |
| **Subtitle decoders** (srt/ass/webvtt/mov_text) | native | high | prerequisite for subtitle ops (4E) |

Most are **native** → a batch of `--enable-decoder=…` flags + size. AV1 is the one real new lib.

### 4B. Encoders (output coverage)

Have: `mjpeg, png, aac, flac, pcm_s16le` + `libopenh264` (LGPL), `libx264` (GPL).

| Missing | Lib / licence | Value | Notes |
|---|---|---|---|
| **Opus** | libopus (BSD) | **very high** | modern low-latency audio, WebRTC; LGPL-clean, modest size |
| **MP3** | libmp3lame (LGPL) | **very high** | ubiquitous; LGPL-clean |
| **VP8 / VP9** | libvpx (BSD) | high | WebM video; LGPL-clean |
| **WebP** | libwebp (BSD) | high | modern images/animation; LGPL-clean |
| **Vorbis** | libvorbis (BSD) | med | OGG audio |
| **GIF (animated)** | native | high | previews/thumbnails; cheap; pairs with palettegen (4D) |
| **AC-3** | native | med | broadcast audio |
| **PCM family, ALAC** | native | med | audio coverage |
| **HEVC / H.265** | libx265 (**GPL**) | high | big ask; full variant only; large+slow single-thread |
| **AV1** | aom / SVT-AV1 / rav1e | med | future-forward; heavy + slow single-thread |

The **LGPL-clean quartet — Opus, MP3, VP8/9, WebP — is the headline encoder win** (default variant).

### 4C. Containers (demux / mux)

Have demux: `mov/mp4, matroska/webm, mp3, wav, ogg, aac, flac, image2`.
Have mux: `mp4, mov, matroska, webm, mp3, wav, image2`.

| Missing | Kind | Value | Notes |
|---|---|---|---|
| **MPEG-TS** (demux+mux) | native | **very high** | broadcast, HLS segments, streaming source/target |
| **HLS / DASH** muxers (segmenting) | native | **very high** | adaptive-streaming *output*, file-based — works in the sandbox |
| **fragmented MP4 / CMAF** | native (mp4 flags) | high | streaming/low-latency delivery |
| **FLV** | native | med | legacy streaming/RTMP recordings |
| **AVI** | native | med | legacy ubiquity |
| **GIF mux** | native | high | animated output (with 4D palette filters) |
| **Ogg mux** | native | med | opus/vorbis output (have demux only) |
| **ADTS (aac), CAF, AIFF, AU** | native | low-med | audio container coverage |
| **concat demuxer** | native | high | stream-copy concatenation of like files (4F) |
| **Subtitle muxers** (srt/webvtt/ass) | native | high | sidecar subtitle output (4E) |

All **native** — a `--enable-demuxer/--enable-muxer` batch + size. MPEG-TS + HLS/DASH are the
strategic ones (web delivery).

### 4D. Filters (the largest surface — ~30 of FFmpeg's 400+)

Have: `null, anull, split, asplit, scale, crop, pad, format, fps, settb/asettb, setsar,
setpts/asetpts, trim/atrim, loop, transpose, overlay, concat, xfade, amix, adelay, volume,
afade, aresample, aformat, alimiter`.

High-value missing, by group (most **native** → flag + size; libs noted):

| Group | Filters | Lib | Value |
|---|---|---|---|
| **Video fade/transition** | `fade` (plain video fade) | native | high (have xfade/afade, not plain fade) |
| **Color/levels** | `eq, hue, colorbalance, curves, colorchannelmixer, lut/lut3d` | native | high (correction/grading) |
| **Sharpen/blur** | `unsharp, gblur, boxblur` | native | med |
| **Compose** | `hstack, vstack, xstack, blend, tile` | native | high (grids, side-by-side, contact sheets) |
| **Thumbnailing** | `select, thumbnail, framestep` | native | **very high** (smart frame extraction) |
| **GIF quality** | `palettegen, paletteuse` | native | high (pairs with GIF mux) |
| **Deinterlace** | `yadif, bwdif` | native | high (broadcast sources) |
| **Keying** | `chromakey, colorkey, despill` | native | med |
| **Speed** | `setpts`(have)/`atempo`, `reverse` | native | med-high (`atempo` audio speed) |
| **Text** | `drawtext, drawbox, drawgrid` | **freetype** ext | **very high** (watermarks, timestamps, captions) |
| **Subtitle burn-in** | `subtitles, ass` | **libass** ext | **very high** (4E) |
| **Audio loudness** | `loudnorm` (EBU R128), `dynaudnorm, acompressor, compand` | native | **very high** (podcast/broadcast normalisation) |
| **Audio EQ/dynamics** | `highpass, lowpass, equalizer, aecho, silenceremove, afftdn` | native | med-high |
| **Channels** | `pan, channelsplit, channelmap, join` | native | med (surround/routing) |
| **Analysis** | `cropdetect, blackdetect, silencedetect, signalstats, astats, ebur128` | native | high (automated decisions) |

The native filter batch is cheap (size only); **drawtext (freetype)** and **subtitles (libass)** are
the two that pull external libs — both very high value and LGPL-compatible.

### 4E. Subtitles (cross-cutting — a domain of its own)

FFmpeg handles subtitles as decode → filter/convert → encode/mux/burn-in. We have **none**. A
complete story needs: subtitle decoders (srt/ass/webvtt/mov_text, 4A), subtitle muxers (4C), the
`subtitles`/`ass` burn-in filters (libass, 4D), and **driver/vocabulary** support for a subtitle
*stream type* (the job-spec only models video/audio pads today). High value, spans all four layers.

### 4F. Operations & the job-spec vocabulary (driver work, not just flags)

The deepest gaps — these need engine code + a versioned vocabulary bump, not build flags. The
job-spec today: `op` (`probe`/`process`), `inputs[{path,options}]`, `filter`, `outputs[{path,map,
video_codec,audio_codec,options}]`.

| Capability | What's missing | Value | Driver work |
|---|---|---|---|
| **Stream copy (`-c copy`)** | remux without re-encode (container convert, keyframe trim) | **very high** | a passthrough path: read packets → (bitstream filter) → mux; no decode/encode. Cheap CPU, huge utility |
| **Bitstream filters** | `h264_mp4toannexb, aac_adtstoasc, …` | high | needed for correct remux between containers; pairs with stream copy |
| **Seeking / time ranges** | `-ss / -t / -to` fast input seek | **very high** | `av_seek_frame` before decode (vs the `trim` filter, which decodes all). Clip extraction |
| **Metadata & chapters** | read (probe extends) + set/passthrough on output | high | tags, chapters, per-stream language/disposition, cover art |
| **Input stream selection** | pick specific streams (2nd audio, a subtitle) | high | beyond `av_find_best_stream`; explicit input `map` |
| **Per-input decode options** | force `-r/-f/-pix_fmt`, framerate, start offset | med | wire `inputs[].options` through (partially modelled) |
| **Thumbnail / frame-extract op** | a first-class op: frames at timestamps → images | high | cleaner than fps+select+image2; a new `op` |
| **Stream-copy concat** | concat like-codec files fast | high | the concat *demuxer* path (vs the re-encode filter) |
| **Two-pass encoding** | VBR quality (x264/x265/libvpx) | med | driver orchestrates pass 1 → pass 2 |
| **Loudness 2-pass** | measure (ebur128) then normalise | med-high | analysis pass feeding `loudnorm` |
| **Progress reporting** | emit frames/time during long jobs | med (UX) | periodic JSON progress on a side channel |
| **Per-output container flags** | faststart (have), fragmentation, segment time | med | streaming output tuning |

**Stream copy + seeking are the two highest-leverage operation gaps** — both are cheap CPU,
universally expected, and unlock fast container-convert / clip-extract / remux workflows the
re-encode-only engine can't do well today.

### 4G. Performance — see 0008

Restated only: single-thread + no-SIMD is the throughput ceiling; the lever is instance-level
parallelism (`RuntimePool`) + build tuning. Owned by [0008](0008-performance-strategy.md); a real
consumer perf target is its trigger.

## 5. Prioritisation framework

Rank each gap by **consumer value ÷ cost** (cost = `.wasm` size + new lib + licence risk + driver
complexity):

- **Tier 1 — cheap, high value (mostly native flags + driver):** stream copy + bitstream filters;
  seeking (`-ss/-t`); native decoder batch (ac3, pcm family, image: webp/gif/bmp/tiff, prores);
  native muxer/demuxer batch (**mpegts, hls/dash**, flv, avi, gif, ogg, concat); native filter
  batch (fade, eq/color, **select/thumbnail**, hstack/vstack, deinterlace, **loudnorm**,
  palettegen); GIF + AC-3 encode; metadata.
- **Tier 2 — modest LGPL-clean libs, high value:** **Opus, MP3, VP8/9, WebP** encode; **drawtext**
  (freetype); **subtitles** burn-in (libass); thumbnail-extract op; two-pass.
- **Tier 3 — heavy / licence-gated (full variant / later):** **HEVC** encode (x265, GPL); **AV1**
  decode (dav1d) + encode (aom/SVT); DTS/TrueHD.
- **Non-goals (§2):** threads, SIMD-asm, network protocols, hardware accel.

A guiding bias: **Tier 1 + the LGPL-clean Tier 2 maximise the *default* variant's reach** — the
proprietary-compatible build becomes broadly useful before the GPL-only heavy hitters.

## 6. Cross-cutting concerns

- **The lean/full size matrix (R-AF-3).** As libs/components accrete, "ship everything" bloats the
  `.wasm`. Decide: one comprehensive build, or a **lean** (web-delivery essentials) + **full**
  (kitchen-sink) axis *orthogonal* to LGPL/GPL — i.e. up to four artifacts. Needs its own decision.
- **Vocabulary versioning.** Every 4F capability bumps the job-spec version (the afmpeg↔ffmpeg-wasi
  contract, 0007 §4). The version must gate features so an old afmpeg fails cleanly on a new op.
- **The codec/filter matrix is a doc deliverable.** A reference page enumerating exactly what each
  build supports (consumers ask "does it do X?") — keep it generated/checked against the build.
- **Test surface.** Each new codec/format/filter needs a fixture + an integration test; the
  dependency-free in-memory fixtures (synthetic WAV/PNG) won't cover everything — a small,
  licence-clean media corpus may be needed.

## 7. Disposition — the spec dispatch list

Child specs — **all DRAFT/SCOPING as of 2026-06-30** (the whole shape drafted together to expose
dependencies before any implementation commits):

| Area | Scope | Tier | Spec | Owns |
|---|---|---|---|---|
| **Remux & stream copy** | `-c copy`, bitstream filters, concat demuxer | 1 | [0013](0013-remux-and-stream-copy.md) | R-PARITY-REMUX |
| **Seeking & time ranges** | `-ss/-t/-to` fast input seek; clip extraction | 1 | [0014](0014-seeking-and-time-ranges.md) | R-PARITY-SEEK |
| **Container coverage** | mpegts, hls/dash, flv, avi, gif, ogg, fragmented-mp4 (native) | 1 | [0015](0015-container-coverage.md) | R-PARITY-CONTAINERS |
| **Native codec batch** | ac3, pcm family, image decoders, gif/ac3 encode (native) | 1 | [0016](0016-native-codec-batch.md) | R-PARITY-NATIVE-CODECS |
| **Native filter batch** | fade, eq/color, select/thumbnail, hstack, deinterlace, loudnorm, palette | 1 | [0017](0017-native-filter-batch.md) | R-PARITY-NATIVE-FILTERS |
| **LGPL encoder expansion** | Opus, MP3, VP8/9, WebP, Vorbis (libopus/lame/libvpx/libwebp/libvorbis) | 2 | [0018](0018-lgpl-encoder-expansion.md) | R-PARITY-LGPL-ENCODERS |
| **Text & subtitles** | drawtext (freetype) + subtitles burn-in/decode/mux (libass) + a subtitle stream type | 2 | [0019](0019-text-and-subtitles.md) | R-PARITY-SUBTITLES |
| **Metadata & chapters** | read/set tags, chapters, disposition, cover art | 1-2 | [0020](0020-metadata-and-chapters.md) | R-PARITY-METADATA |
| **Frame extraction op** | a first-class `frames` op (thumbnails/frames-at-timestamps) | 2 | [0021](0021-frame-extraction-op.md) | R-PARITY-FRAMES |
| **Build & distribution matrix** | lean/intermediate/full × WASM/Native × LGPL/GPL × platform; codec-set-per-profile (R-AF-3) | — | [0022](0022-build-size-matrix.md) | R-AF-3 (size) |
| **HEVC / AV1** | x265 (GPL, full) + dav1d (default); AV1 encode deferred | 3 | [0023](0023-hevc-and-av1.md) | R-PARITY-HEAVY-CODECS |
| Performance | instance pool + build tuning | — | [0008](0008-performance-strategy.md) | R-AF-12 |

### Sequencing (from the children's stated dependencies)

The drafts reveal the real graph — not a flat list:

- **Keystone decision first: [0022](0022-build-size-matrix.md)** (the lean/full × licence matrix).
  0016/0017/0018/0023 each *defer their bucket placement* to it, so settling it unblocks the rest.
- **Foundational ops (Tier 1, highest leverage): [0013](0013-remux-and-stream-copy.md) (stream
  copy) + [0014](0014-seeking-and-time-ranges.md) (seeking).** 0013 underpins 0020 (cover-art
  passthrough) and 0019 (subtitle copy); 0014 underpins 0021 (frame timestamps) and the
  keyframe-accurate copy-trim in 0013. These two reshape what the component *is*.
- **Cheap native breadth (parallelisable, no libs): [0015](0015-container-coverage.md),
  [0016](0016-native-codec-batch.md), [0017](0017-native-filter-batch.md)** — flag-and-size batches.
- **Build on the above: [0020](0020-metadata-and-chapters.md) (needs 0013),
  [0021](0021-frame-extraction-op.md) (needs 0014 + 0017's select/thumbnail).**
- **Default-variant codec reach: [0018](0018-lgpl-encoder-expansion.md)** (the LGPL encoder libs).
- **Cross-cutting, heaviest: [0019](0019-text-and-subtitles.md)** (a new subtitle stream type +
  two libs) and **[0023](0023-hevc-and-av1.md)** (gated on [0008](0008-performance-strategy.md)
  perf + a real need).

### Recommended implementation order

```
0022  decide the build matrix          ← KEYSTONE: a decision, not code; unblocks all bucketing
  │
  ├─ 0013 stream copy ───┐             ← Tier-1 foundational — reshape the component
  ├─ 0014 seeking ───────┤               (re-encode-only → fast remux / clip / convert)
  │                      │
  ├─ 0015 containers ────┤             native batches — parallelisable, no new libs
  ├─ 0016 native codecs ─┤
  ├─ 0017 native filters ┘
  │
  ├─ 0020 metadata        (needs 0013)
  ├─ 0021 frames op       (needs 0014 + 0017)
  ├─ 0018 LGPL encoders   (default-variant codec reach: Opus/MP3/VP8-9/WebP)
  │
  └─ 0019 subtitles · 0023 HEVC/AV1    ← heaviest / cross-cutting, last (0023 also gated on 0008)
```

**Start here next session:** resolve **0022**'s open questions (the lean/full × licence axis — a
discussion, not implementation), then build **0013 + 0014** as the first feature work. Everything
below 0013/0014 is parallelisable once the matrix is settled. No implementation is committed yet —
this is the deliberate commit point.

**Vocabulary versioning** (§6): 0013/0014/0015/0019/0020/0021 each add to the job-spec, and each
bumps the version additively — **one increment per *landed* spec**, in merge order, not per draft.

## 8. Definition of done (this survey)

The gap surface is catalogued, bucketed by the WASI envelope (§2) and licence (§3), prioritised
(§5), and dispatched to a **fully-drafted** child-spec set (§7) whose dependency graph is now
explicit. This is a **living document** — refine the tiers as consumer signal arrives (a real
consumer asking for X is the strongest input), and flip each child's Status from DRAFT to
IMPLEMENTED as it ships.
