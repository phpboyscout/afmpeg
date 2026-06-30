# 0019 — text & subtitles

Status: **DRAFT / SCOPING** (a child of the [0012](0012-feature-parity-roadmap.md) parity roadmap,
§7 row "Text & subtitles", Tier 2. Spans ffmpeg-wasi — two external libs, the subtitle codec
pipeline, the burn-in filters — *and* afmpeg — a third stream type the job-spec doesn't model yet.
Design only; do not implement before review.)
Date: 2026-06-30
Parent: [0012](0012-feature-parity-roadmap.md) (§4E / §4D rows); [0007](0007-libav-direct-engine.md) (the engine + vocabulary this extends)
Owns: **R-PARITY-SUBTITLES**

## 1. Why

Text overlay and subtitles are table-stakes: watermarks, burnt-in timestamps, captions, and
sidecar/extract/convert of subtitle tracks are among the first things a consumer asks ffmpeg-wasi
for ("does it do `drawtext`? can it burn `.srt`? can it extract a subtitle track?"). Today the
answer is *no* on every count — we ship no text lib, no subtitle codec, and the job-spec
([0007](0007-libav-direct-engine.md) §4) models only **video and audio** graph pads. This is the
single biggest cross-cutting parity item: it pulls two libs, a codec family, two filters, **and** a
new stream type into the vocabulary at once.

## 2. Scope

Owned here (and *only* here):

- **libfreetype** → the `drawtext` filter (text/watermark/timestamp overlay onto a video pad).
- **libass** → the `subtitles` / `ass` filters (render `.srt`/`.ass`/`.vtt` *into* video — burn-in).
- **Subtitle codecs:** decoders (`subrip`/`srt`, `ass`, `webvtt`, `mov_text`) + muxers/encoders
  (`srt`, `webvtt`, `ass`, `mov_text`) — for extract / convert / sidecar / embedded-track output.
- **The subtitle *stream type*** in the job-spec: `outputs[].subtitle_codec` (incl. `copy`),
  subtitle stream mapping, burn-in-vs-stream expression, and **font-file provisioning**.

Explicitly **not** here (scope boundaries): native non-subtitle filters → [0017](0012-feature-parity-roadmap.md);
native codecs → [0016](0012-feature-parity-roadmap.md); generic `-c copy` stream copy → [0013](0012-feature-parity-roadmap.md)
(a subtitle stream *reuses* its passthrough path); metadata / per-stream language & disposition
tags → [0020](0012-feature-parity-roadmap.md). Bitmap subtitles (`dvdsub`/`pgs`/`dvb`) are an
open question (§6), not in the first cut.

## 3. Approach / design

Two mechanisms, deliberately distinct, because subtitles are *not* uniformly a filtergraph
citizen:

**(a) Burn-in (the filter path — contained).** `subtitles=sub.srt` / `ass=sub.ass` and `drawtext`
are ordinary lavfi filters on a **video** pad: `[0:v]subtitles=sub.srt[vout]`. The filter reads the
subtitle/font file *itself* from the mounted in-memory fs and rasterises (libass → freetype) onto
frames. **No new pad type** — it slots into the existing `filter` string and the existing
video-pad machinery in `src/process.c` untouched. The only new surface is the libs + the
font-provisioning convention (§4).

**(b) Subtitle streams (the stream path — the hard part).** A subtitle *track* (decode → re-encode
or copy → mux; or extract to a sidecar) does **not** traverse the lavfi graph — libavfilter has no
general subtitle buffersrc/buffersink. So `src/process.c` grows a **parallel pipeline** beside the
video/audio filtergraph: for each mapped subtitle stream, read its packets → either **copy** the
packet straight to the muxer (cheap, reuses 0013's passthrough) or **decode→encode** between
subtitle codecs (`AVSubtitle`, `avcodec_decode_subtitle2` / `avcodec_encode_subtitle`) → mux into
the output. This extends the engine's "pads → encoders → muxer" model with a third
`AVMEDIA_TYPE_SUBTITLE` lane that bypasses `avfilter_graph_*`.

Because subtitle streams skip the graph, they cannot be addressed by a *graph pad label* in `map`.
They are mapped by **input stream specifier** (`0:s:0`) instead — a second, smaller routing rule
the driver's output resolver learns (graph-pad labels → graph outputs; `N:s` specifiers → the
subtitle lane). This asymmetry is the core vocabulary decision (D-0019-B).

## 4. Job-spec & build impact

**Vocabulary (afmpeg `Command`/`Output` + the engine spec) — owned here:**

- `outputs[].subtitle_codec` — new string field: an encoder name (`srt`, `webvtt`, `mov_text`,
  `ass`) **or `"copy"`** (remux the track unchanged). Mirrors `video_codec`/`audio_codec`. An
  output may carry video + audio + subtitle codecs together (embedded track) or *only*
  `subtitle_codec` (sidecar `.srt`/`.vtt`).
- `outputs[].map` gains the ability to name **input subtitle specifiers** (`"0:s:0"`) alongside
  graph pad labels — the driver routes `[label]`→graph lane, `N:s`→subtitle lane.
- **Burn-in needs no new field** — it is expressed entirely in the `filter` string.
- **Font provisioning:** the consumer mounts a font file on the in-memory fs and names it by path
  in the filter (`drawtext=fontfile=/fonts/Sans.ttf:...`). **No `fontconfig`** (no system fonts, no
  network → fontconfig is useless and a size cost). `subtitles`/`ass` likewise take an explicit
  `fontsdir=`/embedded-`ass`-fonts path. Optionally add `inputs[].font` sugar later (D-0019-C / Q).

**Build (ffmpeg-wasi) — owned here:**

- `build/deps.sh`: add `build_freetype` + `build_libass`, cross-compiled `wasm32-wasi` static,
  following the openh264/x264 external-lib pattern (clang as linker, `wasi-compat.h`,
  `-D_WASI_EMULATED_*`, a hand-written `.pc` if upstream's is unusable). libass is configured
  `--disable-fontconfig --disable-require-system-font-provider` (and likely
  `--disable-harfbuzz`/`--disable-libunibreak` for the lean cut — see §6).
- `build/libav.sh` `ENABLE` set gains: `--enable-libfreetype --enable-libass`;
  `--enable-filter=drawtext,subtitles,ass`; `--enable-decoder=subrip,ass,webvtt,mov_text`;
  `--enable-encoder=srt,ass,webvtt,mov_text`; `--enable-muxer=srt,webvtt,ass`;
  `--enable-demuxer=srt,ass,webvtt` (`mov_text` rides the existing mov/mp4 (de)muxers).

## 5. Licensing & size impact

- **freetype** (FTL or GPLv2-or-later — we take FTL) and **libass** (ISC) are both
  **LGPL-compatible** → they land in the **default (LGPL) variant**, not the GPL-only build
  ([0012](0012-feature-parity-roadmap.md) §3). No new GPL trigger.
- **Size** is the real cost: freetype is modest; libass adds a font rasteriser + ASS layout engine.
  Trimming the optional shaping/breaking deps (harfbuzz, fribidi, libunibreak) keeps it leaner but
  weakens complex-script / bidi rendering — an explicit lean/full lever (→ [0022](0012-feature-parity-roadmap.md)).
  The subtitle codecs/muxers themselves are native and cheap.

## 6. Decisions + open questions

- **D-0019-A — burn-in and subtitle-streams are two separate mechanisms.** Burn-in = a video-pad
  filter (no vocabulary change beyond fonts); a subtitle *track* = a new non-graph stream lane.
  Don't force subtitles through the filtergraph (libavfilter can't carry them generally).
- **D-0019-B — subtitle streams are mapped by input stream specifier (`N:s`), not graph pad
  label.** They bypass `avfilter_graph_*`, so the output resolver routes `[label]`→graph,
  `N:s`→the subtitle lane; `subtitle_codec` (incl. `copy`) names the encoder/passthrough.
- **D-0019-C — fonts come from the mounted fs by explicit path; no fontconfig.** The sandbox has no
  system fonts and no network, so `fontconfig` is dead weight. The consumer mounts the font and
  names it (`fontfile=`/`fontsdir=`). *(Open: do we also add an `inputs[].font` convenience that
  the driver wires to the filter, or keep it path-only in the filter string?)*
- **D-0019-D — both libs ride the LGPL default variant** (freetype FTL + libass ISC are
  LGPL-compatible). No GPL gate.
- **Open questions:** (Q1) **bitmap subtitles** (`dvdsub`/`pgs`/`dvb`) burn via `overlay`, not
  libass — defer to a follow-up? (Q2) **harfbuzz/fribidi** in or out of the default build — the
  complex-script vs size trade (→ 0022). (Q3) does the `subtitles` filter's self-read of its file
  need the file *declared* as an `inputs[]` entry, or is an undeclared mounted path acceptable
  (consistency vs convenience)? (Q4) **vocabulary version bump** — `subtitle_codec` + `N:s` maps
  are a compatibility break the version gate ([0007](0007-libav-direct-engine.md) §4) must catch.

## 7. Requirements

- **R-PARITY-SUBTITLES** (owned) — ffmpeg-wasi can (a) **burn** `.srt`/`.ass`/`.vtt` and draw
  `drawtext` text onto video, fonts supplied from the mounted fs; and (b) carry subtitle **streams**
  end-to-end — decode, **copy**, transcode between `srt`/`webvtt`/`ass`/`mov_text`, and mux them
  (embedded or sidecar) — driven by `outputs[].subtitle_codec` + `N:s` maps, all LGPL-clean, within
  the single-thread / sandboxed / no-network envelope.

## 8. Test surface

- Burn-in: a fixture `.srt` + a mounted `.ttf` → `subtitles=` over a synthetic video → assert frames
  changed / output muxes. `drawtext` with a mounted font → overlaid text.
- Stream copy: an mp4 with a `mov_text` track → extract to sidecar `.srt` (probe the result);
  `.srt` → `.vtt` convert; `.srt` → embedded `mov_text` in mp4.
- Negative: a `drawtext`/`subtitles` filter with **no font on the fs** fails cleanly (not a crash);
  unknown `subtitle_codec` → typed error.
- afmpeg integration (gated on `AFMPEG_TEST_FFMPEG_WASI`): a `Command` with `subtitle_codec`/`copy`
  over the real driver. Needs a tiny licence-clean subtitle+font corpus ([0012](0012-feature-parity-roadmap.md) §6).

## 9. Dependencies & sequencing

- **Reuses [0013](0012-feature-parity-roadmap.md) (stream copy)** for the `subtitle_codec:"copy"`
  passthrough — sequence after, or co-design the shared packet-passthrough path.
- **Pairs with [0020](0012-feature-parity-roadmap.md) (metadata)** for subtitle language /
  disposition / default-track tags (a subtitle track is far less useful unlabelled).
- **Build-variant axis → [0022](0012-feature-parity-roadmap.md)** decides where the libass size +
  optional shaping deps land (lean vs full).
- Independent of the LGPL encoder expansion ([0018](0012-feature-parity-roadmap.md)) and native
  filter batch ([0017](0012-feature-parity-roadmap.md)).

## 10. Definition of done (this scoping spec)

The two mechanisms (filter burn-in vs subtitle stream lane), the vocabulary extension
(`subtitle_codec` incl. `copy` + `N:s` mapping), the font-provisioning convention (mounted fs, no
fontconfig), the build deltas (freetype + libass + the subtitle codec/format flags), and the
licensing/size posture are recorded and agreed; the decisions (D-0019-A…D) are settled and the open
questions (Q1–Q4) are dispatched; the engine-side extension to `src/process.c`'s pad→encoder→muxer
model (a third `AVMEDIA_TYPE_SUBTITLE` lane) is specified for implementation. No code yet.
