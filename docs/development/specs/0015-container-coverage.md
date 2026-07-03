# 0015 — container coverage

Status: **IMPLEMENTED** (Phase 2 of the [implementation roadmap](../implementation-roadmap.md);
shipped at **vocabulary version 5** into the **intermediate** build profile ([0022](0022-build-size-matrix.md)).
The native (de)muxer batch — `mpegts`, `flv`, `avi`, `gif`, `hls`, `dash`, `segment`,
`ogg`/`adts`/`caf`/`aiff`/`au`, plus the fMP4 `movflags` on the lean `mp4` muxer — is enabled in
`INTERMEDIATE_ENABLE` (gif rides with its enc/dec codec per §9). The vocabulary adds
`outputs[].format` (forced muxer, D-0015-C) and `outputs[].format_options` (muxer dict →
write_header, D-0015-B), routed in `process.c`; a segmenting output is one `outputs[]` entry
(D-0015-A) that reports **`segmented: true`** (Q1: marker, not an enumerated child list — segments
are on the fs by pattern). Q2 confirmed: the re-encode path needs no BSF; a copy remux auto-inserts
`h264_mp4toannexb` (0013). Q3: segment naming is caller-supplied via `format_options`. Zero licence
delta — all native (R-0015-4). Verified: round-trips → mpegts/flv/avi/gif; **HLS segmenting**
(≥2 `.ts` + `.m3u8` on an in-memory fs, no network); **fragmented MP4** (moof); forced-muxer
selection; and **0013's deferred mp4→ts copy remux + full A/V ts concat-copy**, which MPEG-TS
switches on.)
Date: 2026-06-30
Parent: [0012](0012-feature-parity-roadmap.md) §4C/§5 (Tier 1 container batch); [0007](0007-libav-direct-engine.md) (the engine + the job-spec contract)
Owns: **R-PARITY-CONTAINERS**

## 1. Why

The engine transcodes into a handful of containers (`mp4/mov, matroska/webm, mp3, wav, image2`)
and reads a few more. A consumer asking "can it write an HLS ladder?" / "ingest an MPEG-TS
capture?" / "emit an animated GIF?" gets "no" today — yet **every one of these is a native,
in-tree libav (de)muxer**: no new library, no licence delta, only `.wasm` size and (for the
segmenting muxers) a small reconciliation with how one `outputs[]` entry maps to bytes on disk.
This is the cheapest large jump in reach in the whole 0012 batch, and the strategic ones —
**MPEG-TS + HLS/DASH** — are exactly the web-delivery formats the file-based WASI envelope can
still serve (segments-to-disk, no network). This spec owns the container allowlist and the
**vocabulary for segmenting output**.

## 2. Scope

**In — native demux/mux flags (one `--enable-(de)muxer` batch) + the muxer-option vocabulary:**

- **MPEG-TS** demux + mux — broadcast/capture ingest, HLS-TS segment payloads.
- **HLS** + **DASH** segmenting muxers — adaptive-streaming *output*, file-based (segment files
  + a playlist/manifest on the mounted fs). The marquee feature; the design wrinkle (§3).
- **Fragmented MP4 / CMAF** — the existing `mp4` muxer's fragmentation flags (`movflags`); the
  init+media segment shape DASH/low-latency want. No new muxer — flags only.
- **Then, breadth:** `flv`, `avi`; **`gif`** mux (animated — pairs with palettegen/gif encode in
  [0017](0017-native-filter-batch.md)/[0016](0016-native-codec-batch.md)); **`ogg`** mux (we have
  demux only — opus/vorbis output); **`adts`** (aac streaming); **`caf`/`aiff`/`au`** (audio).

**Out (referenced, not owned):**

- The **concat demuxer** and stream-copy remux/bitstream-filter vocabulary → **[0013](0013-remux-stream-copy.md)**.
  Container *convert* lands here as muxer reach; the `-c copy` path that makes it cheap is 0013's.
- New **codecs/encoders** (gif encode, opus/vorbis encode, ac3) → [0016](0016-native-codec-batch.md)/[0018](0018-lgpl-encoder-expansion.md).
  This spec enables the *containers*; the codecs that fill them are theirs.
- **Network HLS/DASH delivery** — non-goal by construction (0012 §2, `--disable-network`). We
  write segments + playlist as *files*; serving them is the consumer's job.

## 3. Approach / design

**The batch.** Extend the `--enable-demuxer=`/`--enable-muxer=` lists in `ffmpeg-wasi build/libav.sh`
(the `--disable-everything` allowlist). All native; identical for both LGPL and GPL variants
(no GPL trigger among them). **Dependency note:** the `hls` muxer muxes TS or fMP4 segments, so it
implies the `mpegts` and `mp4` muxers; the `dash` muxer implies fragmented `mp4`. Enable the
payload muxers alongside, or the segmenter fails at runtime.

**The wrinkle — one logical output, many files on disk.** `src/process.c` today treats each
`outputs[]` entry as one muxer writing one `avio` file (extension → muxer via
`avformat_alloc_output_context2`; `avio_open` unless `AVFMT_NOFILE`). The HLS/DASH/`segment`
muxers invert this: **one muxer opens *N* child files itself** (segments) plus a playlist/manifest.
The good news: those muxers already carry `AVFMT_NOFILE`, and the engine **already skips `avio_open`
for `NOFILE` muxers** and lets `avformat_write_header`/`av_write_trailer` drive the child I/O — so
the multi-file expansion is mostly *already handled* by the existing loop. What's missing is
(a) a way to **select** a muxer the extension doesn't imply (the `segment` muxer; forcing `hls`),
(b) a channel for **muxer-level options** (segment duration, naming pattern, fragmentation flags),
and (c) **result reporting** for an output that is now a set of files. See §4.

**Probe.** The new demuxers (`mpegts`, `flv`, `avi`, …) must surface in `op:"probe"` —
`format` + per-stream codec/type, same as the existing demuxers. Free once the demuxer is enabled
(libav's `avformat_open_input` autodetects); the spec just asserts probe reports them.

## 4. Job-spec & build impact

- **Build:** longer allowlist strings in `build/libav.sh`; provenance manifest (per-release) gains
  the new formats; the codec/filter/format matrix doc (0012 §6) regenerates.
- **Muxer selection (vocabulary, mine).** Add **`outputs[].format`** — an optional muxer name
  (e.g. `"hls"`, `"dash"`, `"segment"`, `"mpegts"`) that overrides the extension guess. Extension
  stays the default (`.m3u8`→hls, `.mpd`→dash work without it); `format` is needed for the
  `segment` muxer and any force. Engine: pass it as the `oformat` arg to
  `avformat_alloc_output_context2`.
- **Muxer options (vocabulary, mine).** Segmenting needs options the **muxer** reads, not the
  encoder — `hls_time`, `hls_segment_filename`, `hls_playlist_type`, `hls_flags`; `seg_duration`,
  `use_template`, `init_seg_name`, `media_seg_name`; `segment_time`, `segment_format`,
  `segment_list`; and the fMP4 `movflags` (`+frag_keyframe+empty_moov+default_base_moof`).
  Today `outputs[].options` flow **only to the encoder** (`avcodec_open2`); they never reach the
  muxer. Add a dedicated **`outputs[].format_options`** map routed to `avformat_write_header`'s
  options dict (and `faststart`/`movflags` migrate there from `options`). Keeping the two maps
  distinct avoids guessing which dict an option belongs to. (D-0015-B.)
- **Segmenting output, expressed (mine):** `outputs[].path` is the **playlist/manifest**
  (`stream.m3u8` / `manifest.mpd`); the **segment file pattern** is a `format_options` value
  (`hls_segment_filename: "seg_%03d.ts"`), resolved against the same mounted fs. Segment duration
  and flags are `format_options` too. No new top-level concept — a segmenting output is just an
  output with a `format` + `format_options`.
- **Result reporting:** the success JSON reports one entry per `outputs[]` (path = the playlist).
  For a `NOFILE`/segmenting output, add a `segmented: true` marker; whether to also enumerate the
  child segment files is an open question (Q1) — they are discoverable on the fs by pattern.

## 5. Licensing & size impact

- **Licence: none.** Every format here is in-tree libav, no `--enable-gpl`, no external lib —
  the batch lands **identically in the LGPL default and the GPL variant**. Pure 0012-§3 "expands
  the default variant" work.
- **Size:** modest. Each (de)muxer is a few KB of `.wasm`; `mpegts`+`hls`+`dash` are the largest
  (segment/timing machinery) but still small beside a codec lib. The set is a natural candidate for
  the lean/full axis ([0022](0022-lean-full-matrix.md)) — a **lean web build** wants
  `mp4/hls/dash/mpegts`; `avi/flv/au/aiff` are full-build padding. Defer the partition to 0022;
  enable the batch here.

## 6. Decisions & open questions

- **D-0015-A — segmenting is "an output that expands," not a new op.** HLS/DASH reuse the existing
  `outputs[]` machinery (and its `NOFILE` child-file handling from `n8.1.2-5`); the playlist is the
  output path. No `op:"segment"`. Keeps the multi-output model (one filter graph → mapped pads →
  outputs) intact; a segmenting output is one more muxer kind.
- **D-0015-B — two option maps: `options` (encoder) + `format_options` (muxer).** Resolves the
  ambiguity that today's single `options` map is encoder-only, so muxer flags (incl. the
  documented `movflags +faststart`) have nowhere correct to go. `format_options` → write_header.
- **D-0015-C — `format` overrides the extension; extension is the default.** `.m3u8`/`.mpd` need
  no `format`; the `segment` muxer and forced selections set it.
- **Q1 — enumerate produced segment files in the result?** Marker-only (`segmented:true`) vs a
  full child-file list. List is friendlier to a consumer cleaning up / uploading; cost is walking
  the muxer's output. Lean to marker + the pattern that was requested; decide in impl.
- **Q2 — bitstream filters for cross-container remux** (`h264_mp4toannexb` for mp4→TS) are owned by
  [0013](0013-remux-stream-copy.md); but TS *encode* output from a decoded graph (our normal path)
  needs none. Confirm the re-encode path needs no BSF before declaring TS mux done.
- **Q3 — DASH+CMAF init/media naming defaults** — do we hardcode sane `init_seg_name`/`media_seg_name`,
  or require them in `format_options`? Lean to documented defaults.

## 7. Requirements

- **R-PARITY-CONTAINERS** — the engine demuxes and muxes the §2 native container set, selected by
  extension or `outputs[].format`, with muxer behaviour controlled via `outputs[].format_options`,
  identically in both variants.
- **R-0015-1** — segmenting muxers (`hls`, `dash`, `segment`) write segment files + a
  playlist/manifest to the mounted fs from a single `outputs[]` entry (path = playlist), no network.
- **R-0015-2** — fragmented MP4 / CMAF output via `mp4` + `movflags` through `format_options`.
- **R-0015-3** — `op:"probe"` reports `format` + streams for every newly enabled demuxer.
- **R-0015-4** — the muxer batch adds **zero** licence obligations (no GPL trigger, no new lib).

## 8. Test surface

- **Round-trips (integration, over the real driver):** transcode → `mpegts`, → `flv`, → `avi`,
  → `gif` (animated), → `ogg`; then **probe** each back and assert `format` + stream codecs.
- **Segmenting:** a process job with `format:"hls"` + `hls_time`/`hls_segment_filename` produces
  *≥2 segment files + a `.m3u8`* on an in-memory afero fs; the playlist references the segments;
  same for `dash` (`.mpd` + init/media segments). Assert the file *set*, not just one path —
  the engine's multi-file claim is the thing under test.
- **Audio containers:** `adts`/`caf`/`aiff`/`au` write + probe round-trips.
- **Fixtures:** synthetic WAV/PNG (0007's dependency-free corpus) cover audio + image2/gif; TS/flv
  round-trips re-encode from those, so no licensed media corpus is needed for this spec.

## 9. Dependencies & sequencing

- **Independent of, but complementary to, [0013](0013-remux-stream-copy.md)** — container reach is
  most useful *with* `-c copy` (fast container convert), but each stands alone; build either first.
- **Pairs with [0016](0016-native-codec-batch.md)/[0017](0017-native-filter-batch.md)** for GIF
  (gif encoder + palettegen/paletteuse) and [0018](0018-lgpl-encoder-expansion.md) for `ogg`
  (opus/vorbis encode) — the container is inert without its codec, so land gif-mux and gif-encode
  together. MPEG-TS/HLS/DASH work today with the existing h264/aac encoders, so they need no codec
  spec — **do them first** (highest value, no cross-spec dependency).
- **Feeds [0022](0022-lean-full-matrix.md)** — this batch is the first real input to the
  lean/full format partition.

## 10. Definition of done

The native demux/mux batch (§2) is enabled in `build/libav.sh` for both variants; `outputs[].format`
+ `outputs[].format_options` are specified and the engine routes them (muxer selection + write_header
options); a single segmenting `outputs[]` entry produces a verified segment-set + playlist on the
mounted fs with no network; probe reports the new demuxers; the matrix doc + provenance regenerate;
and the open questions (Q1–Q3) are resolved in implementation. Decisions D-0015-A…C are recorded and
agreed. Codecs and the concat/stream-copy path remain with their owning specs.
