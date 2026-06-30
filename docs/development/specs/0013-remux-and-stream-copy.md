# 0013 — remux & stream copy

Status: **DRAFT / SCOPING** (the Tier-1, highest value-to-cost parity item from [0012](0012-feature-parity-roadmap.md)
§7. Owns the `copy` codec, the bitstream-filter selector, and the concat-demuxer input mode in the
job-spec vocabulary. Review before building.)
Date: 2026-06-30
Parent: [0012](0012-feature-parity-roadmap.md) §4F/§5 (Tier 1); [0007](0007-libav-direct-engine.md) §4 (the vocabulary)
Owns: **R-PARITY-REMUX** (stream copy / `-c copy`, bitstream filters, concat demuxer)

## 1. Why

The engine today can only *transcode*: every stream is decoded → filtered → re-encoded ([0007](0007-libav-direct-engine.md),
`process.c`). That is the expensive path, and for the most-asked operations — **change the container,
not the bytes** (mp4 → mkv, mp4 → ts), **cut a clip on a keyframe**, **join like-codec segments** —
it is the *wrong* path: it burns CPU, costs quality, and needs an encoder for a codec we may not even
ship. Stream copy is the packet-passthrough path FFmpeg calls `-c copy`: read demuxed packets →
(optional bitstream filter) → mux, **no decode, no encode**. Cheap, fast, lossless, and — the
headline (D-0013-F) — it needs **no codec at all**, so it works for *any* codec in *either* licence
variant, including streams we cannot decode (HEVC, AC-3, DTS …). It is the single biggest reach-per-
byte win on the roadmap.

What a consumer gets: fast container conversion, keyframe-accurate copy-trim (paired with 0014),
stream-copy concatenation, and mixed jobs (copy the video, re-encode only the audio) — all in one
job-spec shape.

## 2. Scope

**In:** a per-output-stream `copy` directive; the demuxed-packet passthrough path in `process.c`
(bypassing graph/decoder/encoder); bitstream filtering (auto + explicit override) for correct
cross-container remux; the **concat demuxer** input mode (stream-copy join of like-codec files);
mixing copy and transcode streams in one job; the vocabulary additions + build flags + version bump.

**Out (other specs / non-goals):** fast input seek `-ss/-t/-to` and *where* a keyframe cut lands →
**0014** (this spec just passes seeked packets through); the
re-encode `concat` *filter* (already shipped, §0012 §4D — distinct from the demuxer here); MPEG-TS /
HLS mux+demux, which the marquee ts/segment remux demos depend on → **0015**
(copy itself lands independently on mp4/mkv/webm); metadata/disposition carry-over on copy →
**0020**. Per §2 of the parent: no threads, no SIMD, no network.

## 3. Approach / design (concrete libav mechanics)

A copy stream **bypasses the filtergraph entirely**. Today every mapped output stream is a graph
output pad → `buffersink` → encoder. A copy stream is instead a direct input-stream → output-stream
wire, carried as a new descriptor alongside the existing `GIn`/`GOut`:

- **Setup (before `avformat_write_header`).** For each copied `(input, stream)` → output: create the
  output stream with `avformat_new_stream`, `avcodec_parameters_copy(ost->codecpar, ist->codecpar)`,
  clear `codec_tag` if the muxer needs it, and set `ost->time_base = ist->time_base`. If a bitstream
  filter applies (§3.1), alloc it (`av_bsf_alloc`), feed `ist->codecpar` into `bsf->par_in`,
  `av_bsf_init`, then take `bsf->par_out` into the output stream's `codecpar`. Copy streams and
  encoder streams are both added to each muxer *before* its `write_header`.
- **Read loop.** Today `feed()` routes a packet only to decoders. We add a parallel route: for a
  packet whose `(input, stream)` is copy-mapped, **do not decode** — push it through the BSF
  (`av_bsf_send_packet` / `av_bsf_receive_packet`, a drain loop) or, with no BSF, take it as-is, then
  `av_packet_rescale_ts(pkt, ist->time_base, ost->time_base)`, `pkt->stream_index = ost->index`,
  `av_interleaved_write_frame(ofmt, pkt)`. A stream can be both copied *and* decoded (tee) — the two
  routes are independent. Encoded pads and copied packets interleave into the same muxer naturally
  (`av_interleaved_write_frame` orders by dts).
- **Timestamps.** A copied output starts at the first packet's dts (nonzero after a seek). Apply the
  muxer's `avoid_negative_ts make_zero` (and `+genpts` where needed) so a trimmed copy starts at
  zero; precise `-ss` semantics are **0014**.

### 3.1 Bitstream filters (auto-first)

Cross-container remux often needs a BSF: H.264/HEVC carry length-prefixed NAL units in mp4 but
Annex-B start codes in MPEG-TS (`h264_mp4toannexb` / `hevc_mp4toannexb`); AAC needs `aac_adtstoasc`
going ADTS → MP4. **Modern lavf muxers auto-insert the required BSF** in the write path when the BSF
is *compiled in* — so the default is **auto**: enable the BSFs in the build (§4) and let the muxer
choose. The engine keeps a tiny fallback table keyed on `(codec, output container)` for cases the
muxer does not auto-detect, and the spec exposes an **explicit override** per copied stream, plus
`"none"` to force-disable.

### 3.2 Concat demuxer (stream-copy join)

Distinct from the `concat` *filter* (which decodes + re-encodes). The concat **demuxer** presents a
playlist of like-codec files as one continuous input. The engine materialises a concat list file in
the vfs (`file 'seg0.ts'\nfile 'seg1.ts'` …), opens it with `av_find_input_format("concat")` (`safe`
set for the sandboxed paths), and the resulting single input is then **stream-copied** (or filtered)
like any other. The like-codec/like-timebase requirement is the demuxer's; a mismatch surfaces as a
typed error.

## 4. Job-spec & build impact

**Vocabulary additions** (this spec owns them; bumps the job-spec version, [0007](0007-libav-direct-engine.md)
§4 / `command.go`):

- **`video_codec` / `audio_codec` accept the sentinel `"copy"`** (mirrors `-c:v copy`). The mapped
  stream is passed through, not encoded. No struct change in `command.go` — both are already `string`.
- **`outputs[].map` entries gain a second kind: input stream specifiers** — `"0:v"`, `"0:a:0"`,
  `"0:0"` — alongside the existing bracketed graph-pad labels (`"[vout]"`). Disambiguation: bracketed
  `[…]` = graph pad (encoded); unbracketed `in:type[:idx]` = input stream (copied). `Output.Map` is
  already `[]string` — no struct change.
- **`outputs[].bitstream_filters`** — an object mapping a copied map-key to a BSF name or chain
  (`{"0:v": "h264_mp4toannexb"}`); absent → auto; `"none"` → force-off. New `Output.BitstreamFilters
  map[string]string` (`omitempty`).
- **`inputs[].concat`** — a `[]string` of like-codec paths opened via the concat demuxer (distinct
  from the `concat` filter). New `Input.Concat []string` (`omitempty`); `Input` gains its first field
  beyond `Path`.

```jsonc
// remux mp4 → mkv, no re-encode (both streams copied)
{"op":"process","inputs":[{"path":"in.mp4"}],
 "outputs":[{"path":"out.mkv","map":["0:v","0:a"],"video_codec":"copy","audio_codec":"copy"}]}

// mixed: copy the video, re-encode only the audio
{"op":"process","inputs":[{"path":"in.mp4"}],"filter":"[0:a]loudnorm[aout]",
 "outputs":[{"path":"out.mp4","map":["0:v","[aout]"],"video_codec":"copy","audio_codec":"aac"}]}

// stream-copy concat of like-codec segments
{"op":"process","inputs":[{"concat":["a.ts","b.ts","c.ts"]}],
 "outputs":[{"path":"joined.ts","map":["0:v","0:a"],"video_codec":"copy","audio_codec":"copy"}]}
```

**Build flags** (`ffmpeg-wasi build/libav.sh`, under `--disable-everything`): add
`--enable-bsf=h264_mp4toannexb,hevc_mp4toannexb,aac_adtstoasc,extract_extradata` and
`--enable-demuxer=concat`. **No new external lib, no new codec.** (MPEG-TS mux/demux for the ts demos
comes from 0015.)

## 5. Licensing & size impact

The strongest licensing story on the roadmap: **copy touches no codec library**, so it lands wholly
in the **LGPL default** and even *extends reach beyond our codec set* — you can remux an HEVC or DTS
stream we cannot decode or encode. The BSFs and the concat demuxer are **native** (in-tree libav\*,
LGPL) — flag-only, no GPL trigger, no patent-encumbered component, negligible `.wasm` growth. No
size-matrix (0022) interaction.

## 6. Decisions

- **D-0013-A — `"copy"` is a codec sentinel; a copied stream is a mapped *input stream specifier*,
  not a graph pad.** `map` becomes two-kinded (bracketed pad vs `in:type` spec). Reuses existing
  fields, mirrors `-map 0:v -c:v copy`, no model redesign. An input-spec map entry implies copy (to
  *transcode* an input stream, route it through the graph as today).
- **D-0013-B — copy bypasses graph/decoder/encoder.** `av_read_frame → [bsf] →
  av_interleaved_write_frame`, rescaled. A single job freely mixes copy and transcode streams and
  outputs; a stream may be both copied and filtered.
- **D-0013-C — bitstream filtering is auto-first.** Rely on lavf muxer auto-insertion (BSFs compiled
  in per §4) + a small engine fallback table on `(codec, container)`; expose an explicit per-stream
  override and `"none"`. Don't make the caller reason about NAL formats for the common case.
- **D-0013-D — demuxer-concat is an input mode (`inputs[].concat`), distinct from the `concat`
  filter.** Engine materialises a list file and opens the concat demuxer; the joined input is then
  copied/filtered like any input. Like-codec requirement is the demuxer's; mismatch → typed error.
- **D-0013-E — keyframe-accurate copy-trim defers seek to 0014.** A
  copy can only cut on keyframes; this spec passes seeked packets through and zeroes start timestamps
  (`avoid_negative_ts make_zero`). *Where* a cut lands vs a requested time is 0014's vocabulary.
- **D-0013-F — copy needs no codec → it lands in the LGPL default and reaches codecs we don't
  ship.** The licensing headline (§5).

**Open questions.** (a) Type-wide `audio_codec:"copy"` cannot express *copy one audio, re-encode
another* in the same output — phase 1 is the type-wide sentinel; a per-stream `streams[]` fine-grain
form is a later extension if demanded. (b) Exact `(codec, container)` fallback-table coverage —
pin in impl. (c) Concat: list-file materialisation vs the `concat:` protocol, and `safe`/path
handling in the sandbox. (d) Whether to surface `-copyts` semantics now or defer entirely to 0014.

## 7. Requirements

- **R-PARITY-REMUX** *(owned)* — the engine stream-copies mapped input streams end-to-end (read →
  optional BSF → interleaved mux, no decode/encode); supports per-output `copy` codecs, auto +
  explicit bitstream filtering, the concat-demuxer input mode, and mixing copy with transcode in one
  job. The vocabulary additions (§4) are versioned and gate cleanly on an old engine.
- **R-0013-1** — `process.c` carries copy-stream descriptors parallel to `GIn`/`GOut`; copy streams
  are created + (BSF-)configured before `write_header` and interleave correctly with encoded streams.
- **R-0013-2** — bitstream filters compiled in (§4); auto-insertion verified for mp4↔ts H.264/AAC.
- **R-0013-3** — the concat demuxer joins like-codec inputs into one continuous copyable input.
- **R-0013-4** — afmpeg `command.go` carries `"copy"`, input-spec `map` entries,
  `Output.BitstreamFilters`, and `Input.Concat`; the job-spec version is bumped.

## 8. Test surface

- **Container convert, no re-encode:** mp4 (h264/aac) → mkv copy; assert *no decoder opened* and
  video packets are byte-identical to the source.
- **Cross-container BSF (auto):** mp4 → ts copy (needs `h264_mp4toannexb` + `aac_adtstoasc`) and the
  inverse ts → mp4; verify playable output. *(ts paths gated on 0015.)*
- **Mixed job:** copy video + re-encode audio to a different codec in one output.
- **Concat demuxer:** join two like-codec segments → one continuous copy output.
- **BSF override + `"none"`** honoured.
- **Negatives (typed errors):** copy into an incompatible container; concat of unlike codecs.
- **Fixtures:** the dep-free synthetic WAV/PNG corpus can't produce H.264 — **self-bootstrap**:
  encode a tiny h264/aac mp4 with openh264 (already shipped), then remux *that* in the copy tests.
  Confirms the parent's "licence-clean corpus" concern (§0012 §6) is sidestepped for this spec.

## 9. Dependencies & sequencing

- **Engine first:** build flags (`--enable-bsf=…`, `--enable-demuxer=concat`) + the copy/BSF/concat
  code in `process.c` + the job-spec version bump. Self-contained; lands on mp4/mkv/webm immediately.
- **afmpeg follows:** `Input.Concat`, `Output.BitstreamFilters`; `"copy"` and input-spec `map`
  entries pass through unchanged. Pin the new engine + vocabulary version.
- **Sequencing:** independent of other Tier-1 specs, but its highest-value demos want **0015**
  (MPEG-TS/HLS mux+demux) for ts/segment remux, and copy-trim wants **0014** (fast seek). **0020**
  (metadata) later carries stream disposition/language across a copy. Build copy now on mp4/mkv/webm;
  the ts/segment story switches on when 0015 lands.

## 10. Definition of done

The engine stream-copies mapped input streams with no decode/encode, auto-applies the right
bitstream filter (with an explicit override) for cross-container remux, joins like-codec files via
the concat demuxer, and mixes copy with transcode in a single job — all expressed through the
versioned `copy` / input-spec-`map` / `bitstream_filters` / `concat` vocabulary, with `command.go`
updated and the version bumped. Proven by the §8 tests (self-bootstrapped fixtures, byte-identical
copy assertion, mp4↔mkv now and mp4↔ts once 0015 lands) and the
ffmpeg-wasi job-spec reference + a remux how-to shipped in the same MRs. Keyframe/seek precision is
explicitly deferred to 0014.
