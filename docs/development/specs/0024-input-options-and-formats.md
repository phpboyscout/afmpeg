# 0024 — input demuxer options, forced/raw formats & stream selection

Status: **PROPOSED** (from the external review — decision pending. A candidate for the 0012 parity
batch, not committed work. The input-side mirror of [0015](0015-container-coverage.md)'s output side.
Design only.)
Date: 2026-07-02
Parent: [0012](0012-feature-parity-roadmap.md) §4F (per-input decode options, input stream selection);
[0007](0007-libav-direct-engine.md) §4 (the job-spec contract)
Source: external review — `ffmpeg-wasi/feature_parity_review*.md`
Owns: **R-PARITY-INPUT-OPTIONS**

## 1. Why / the gap

[0015](0015-container-coverage.md) gave the **output** side a muxer name (`outputs[].format`) and a
muxer-options dict (`outputs[].format_options`). The **input** side has no symmetric handle: the
engine opens every input with a hard-coded auto-probe and no options, so a consumer cannot (a) pass
**demuxer options**, (b) **force a demuxer** (`-f`), or (c) read **headerless/raw** inputs (raw YUV,
raw PCM) that carry no probeable header — the demuxer guesses wrong or aborts. The job-spec *already
advertises* `inputs[].options` (0007 §4, the job-spec reference), so this is also a **documented-but-
dead** field, not just a new feature: the contract promises something the engine silently drops.

## 2. Validation (confirmed against the code — this is a real gap)

Read of `ffmpeg-wasi/src/process.c` (the input-open loop) + afmpeg `pkg/afmpeg/command.go`:

- **Options dropped.** `op_process` opens each input with
  `avformat_open_input(&c.in[c.n_in], p, NULL, NULL)` — **NULL forced format, NULL options dict**.
  Nothing a caller puts in `inputs[].options` ever reaches the demuxer. The review confirms it
  independently (`feature_parity_review.md` §5: "completely lacks the ability to pass options to the
  format demuxers").
- **Field advertised but unmodelled.** The job-spec reference + 0007 §4 show `inputs:[{path,options}]`,
  yet afmpeg's `Input` struct (and `jobInput`) carry **only `Path`** — the Go side never even
  serialises `options`. So the field is doubly inert: not emitted, and ignored if it were.
- **No forced format.** With NULL for the format arg there is no `-f`; headerless raw inputs fail
  `avformat`'s auto-probe and abort (`feature_parity_review_supplementary.md` §2).
- **Coarse stream selection.** `parse_input_pad` maps only `N:v` / `N:a` via `av_find_best_stream`;
  a specific stream (`0:v:1`, the second video stream) cannot be addressed as a graph input.

## 3. Scope

**In (the input-side vocabulary + the wiring):** activate `inputs[].options` (demuxer AVDictionary);
add `inputs[].format` (forced demuxer name); the raw-format option params (`video_size`,
`pixel_format`, `framerate`, `sample_rate`, `channel_layout`, `sample_format`) that ride the options
dict; and a decision on where a specific-stream selector (`0:v:1`) lives (§7 Q1).

**Out (referenced, not owned):** the `map` **copy**-stream specifier vocabulary (`0:v`, `0:a:0`) →
[0013](0013-remux-and-stream-copy.md); output-side `format`/`format_options` → [0015](0015-container-coverage.md);
fast input seek `-ss/-t` → [0014](0014-seeking-and-time-ranges.md); metadata carry-over → [0020](0020-metadata-and-chapters.md).
Per 0012 §2: no threads, no SIMD, no network.

## 4. Approach / design (libav mechanics)

- **Demuxer options → AVDictionary.** In the input loop, build an `AVDictionary` from
  `inputs[].options` (mirroring how `parse_output` builds `enc_opts`) and pass **`&opts`** as the
  4th arg to `avformat_open_input`. After open, check leftover keys (unconsumed = typed warning/error).
  Free the dict. One-for-one with 0015's `format_options` → `avformat_write_header`.
- **Forced demuxer → `av_find_input_format`.** When `inputs[].format` is set, resolve it with
  `av_find_input_format(name)` and pass the result as the **2nd arg** (`fmt`) to
  `avformat_open_input`; NULL (auto-probe) stays the default. Symmetric to 0015's `oformat` on
  `avformat_alloc_output_context2`.
- **Raw formats fall out of the two above.** A raw input is just a forced demuxer (`format:"rawvideo"`
  / `"s16le"`) plus demuxer options carrying the geometry — no new mechanism:
  `{"path":"frames.yuv","format":"rawvideo","options":{"video_size":"1280x720","pixel_format":"yuv420p","framerate":"25"}}`.
  libav reads `video_size`/`pixel_format`/`framerate` (rawvideo) and `sample_rate`/`channel_layout`
  (PCM demuxers) straight from the demuxer dict. Requires the raw demuxers in the build allowlist (§5).
- **Specific stream selection.** Extend `parse_input_pad` to accept an optional stream index
  (`N:v:K` / `N:a:K`) and, when present, resolve the K-th stream of that type instead of
  `av_find_best_stream(..., -1, ...)`. Small, contained change to one function. (See Q1 — this
  overlaps 0013's `map` grammar and may belong there.)

## 5. Job-spec & build impact

- **Vocabulary (mine).** Add **`inputs[].format`** (string, optional — forced demuxer) and **activate
  `inputs[].options`** (`map[string]string` — demuxer dict, already advertised). afmpeg `command.go`:
  `Input` gains `Format string` + `Options map[string]string` (both `omitempty`); today `Input` is
  `{Path}` only, so `jobInput` stops being a bare alias. This is the input mirror of 0015's
  `Output.Format` + `Output.FormatOptions`.
- **Version bump.** Additive job-spec increment — one per landed spec (0012 §6). An old engine that
  ignores `inputs[].format`/`options` must fail cleanly, not silently mis-open (the field is the gate).
- **Build.** The raw demuxers (`rawvideo`, the `pcm_*` family, `data`) join the `--enable-demuxer=`
  allowlist in `ffmpeg-wasi build/libav.sh` — native, no new lib. Coordinate the PCM set with
  [0016](0016-native-codec-batch.md) (it owns the PCM *codecs*; this spec wants their *demuxers*).

## 6. Licensing & size

**Nil.** Everything is native libavformat — no `--enable-gpl`, no external lib. Lands identically in
the LGPL default and the GPL variant (0012 §3). The raw demuxers are a few KB of `.wasm`; the wiring
is zero. Same "expands the default variant for free" story as 0013/0015.

## 7. Decisions & open questions

- **D-0024-A — two option maps stay type-scoped, matching 0015.** `inputs[].options` → the *demuxer*
  (`avformat_open_input`); `outputs[].options` → the *encoder*; `outputs[].format_options` → the
  *muxer*. Inputs need only one map (there is no input encoder), so no `input_options`/`format_options`
  split is warranted — a single `options` dict per input suffices.
- **D-0024-B — raw/headerless input = `format` + `options`, not a new op or concept.** A raw source is
  a forced demuxer plus geometry in the dict; no `raw:true` flag, no new field. Keeps the model flat.
- **D-0024-C — `format` overrides probe; probe (auto) is the default.** Mirror of D-0015-C on the
  output side; only forced/raw inputs set it.
- **Q1 — does specific-stream selection (`0:v:1`) belong here or with [0013](0013-remux-and-stream-copy.md)?**
  0013 owns the `map` input-stream-specifier grammar (`0:v`, `0:a:0`) for *copy*; extending
  `parse_input_pad` for *graph* inputs is the same indexing idea on a different surface. Lean: 0013
  owns the **grammar**; this spec only asserts the graph-input parser honours the indexed form.
  Resolve before either builds so the two don't diverge on `:K` semantics.
- **Q2 — leftover-key strictness.** After `avformat_open_input`, unconsumed dict keys mean a typo or
  a wrong-demuxer option. Hard error vs warn-and-continue? Lean: typed error (fail loud), matching the
  job-spec's "exactly as capable as it claims" posture.
- **Q3 — which raw demuxers ship in the lean build?** `rawvideo` + `pcm_s16le`/`f32le` are the
  common pair; the long PCM tail is full-build padding. Defer the partition to [0022](0022-build-size-matrix.md).

## 8. Requirements

- **R-PARITY-INPUT-OPTIONS** *(owned)* — the engine passes `inputs[].options` to the demuxer as an
  AVDictionary and honours `inputs[].format` as a forced demuxer, enabling raw/headerless inputs;
  afmpeg's `Input` carries both; the job-spec version is bumped and gates on an old engine.
- **R-0024-1** — a raw input (`rawvideo`/`pcm_*` + geometry options) decodes end-to-end through the
  existing graph → encode → mux path with no other spec change.
- **R-0024-2** — an indexed graph-input pad (`N:v:K`) selects the K-th stream of that type (grammar
  coordinated with 0013).
- **R-0024-3** — unconsumed demuxer options surface as a typed diagnostic (Q2), not a silent drop.
- **R-0024-4** — zero licence delta, both variants (native only).

## 9. Test surface

- **Demuxer option honoured:** open an input with a demuxer option and assert it took effect (e.g. a
  probe/analyse-limiting option, or a value that changes parsed stream info).
- **Forced format:** name a valid mp4 `clip.bin` and open it with `format:"mp4"`; probe confirms it
  parses where auto-probe-by-extension would not.
- **Raw round-trip:** emit a raw `.yuv` (rawvideo) + a raw `.pcm` (s16le) from the dep-free synthetic
  corpus, re-open each with `format` + `video_size`/`pixel_format`/`framerate` (resp. `sample_rate`/
  `channel_layout`), transcode to mp4/wav, and assert dimensions/rate survive. No licensed media.
- **Indexed stream selection:** a 2-video-stream input; `0:v:1` feeds the graph from the *second*
  stream (assert it differs from `0:v:0`).
- **Negatives (typed errors):** unknown `format` name; bogus/unconsumed option key (Q2); raw input
  with missing required geometry.

## 10. Dependencies & sequencing

- **Mirror of [0015](0015-container-coverage.md)** — same shape on the input side; build after or
  alongside 0015 so `format`/`format_options`/`options` read as one symmetric family in the reference.
- **Coordinates with [0013](0013-remux-and-stream-copy.md)** on the `:K` stream-index grammar (Q1) —
  settle the shared grammar before either lands. Otherwise independent; the demuxer-options + forced-
  format wiring is self-contained in `process.c`'s input loop.
- **Touches [0016](0016-native-codec-batch.md)** (PCM demuxers ride with its PCM codecs) and feeds
  [0022](0022-build-size-matrix.md) (which raw demuxers are lean vs full, Q3).

## 11. Definition of done

`inputs[].options` reaches the demuxer as an AVDictionary and `inputs[].format` forces the demuxer in
`process.c`'s input loop; raw/headerless inputs (rawvideo/PCM) open via `format` + geometry options and
transcode through the existing path; indexed graph-input selection (`N:v:K`) works with a grammar
agreed with 0013; afmpeg's `Input` carries `Format` + `Options` and the job-spec version is bumped;
the raw demuxers are in the build allowlist (both variants, no licence delta); and Q1–Q3 are resolved
in implementation. The advertised-but-dead `inputs[].options` field is now live, closing the
contract gap the external review surfaced.
