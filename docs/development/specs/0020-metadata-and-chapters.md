# 0020 — metadata & chapters

Status: **IMPLEMENTED** (Phase 3 of the [implementation roadmap](../implementation-roadmap.md).
Read side: `probe` (ffmpeg-wasi `driver.c`) now reports container `tags`/`chapters` and per-stream
`tags`/`disposition`/`language`, additively. Write side: `process` (`process.c`) accepts
`outputs[].metadata` (container tags), `outputs[].chapters` (`"copy"`/index passthrough deep-copy),
and `outputs[].stream_metadata` (per-map `language`/`disposition`/`tags`), threaded into the muxer
before `write_header`; the copy path now carries source disposition + tags, so `attached_pic` cover
art survives (D-0020-C). Shared glue in a new `src/meta.c` (disposition table, tag/disposition ↔
JSON) used by both ops. afmpeg's `Probe`/`ProbeStream` gain `Tags`/`Chapters`/`Disposition`/
`Language`; `Output` gains `Metadata`/`Chapters`/`StreamMetadata` (+ the `StreamMeta` type). Bumps
vocab to **v7**. Proven by `TestIntegration_Metadata` (container tags + per-stream language/
disposition write→read) + emitter unit tests. **Chapter authoring** (Q2) stays deferred, and the
chapter-copy round-trip is emission-tested only — no self-bootstrappable chaptered fixture, so its
end-to-end verification is corpus-gated like the 0016 decode-only codecs.)
Date: 2026-06-30
Parent: [0012](0012-feature-parity-roadmap.md) §4F (the metadata row) / §5 (Tier 1–2); [0007](0007-libav-direct-engine.md) (the engine + the job-spec contract)
Owns: **R-PARITY-METADATA**

## 1. Why

The engine moves *pixels and samples* but drops everything *about* them. `probe` reports codecs
and duration but not the title/artist/album tags, the chapter list, per-stream language, or the
disposition flags (`default`, `forced`, `attached_pic`) a consumer needs to make decisions; and
`process` writes output with no way to set tags, carry chapters across a transcode, or preserve a
stream's language/role. A podcast loses its chapters, an MP3 loses its cover art, a multi-language
file loses which audio track is the default. Every piece of this is a **native libav metadata API**
(`av_dict_*`, `AVChapter`, `AVStream->disposition`) — zero new library, near-zero `.wasm` cost —
so it is the cheapest "feels complete" jump in the batch. This spec owns the **read** extension to
`probe` and the **write** vocabulary on `process` for tags, chapters, disposition, language, and
cover art.

## 2. Scope

**In — read side (extend `op:"probe"`):**

- **Container + per-stream metadata tags** — `AVFormatContext->metadata` and each
  `AVStream->metadata` (`av_dict`), surfaced as a JSON tag map.
- **Chapters** — `AVFormatContext->chapters[]`: `start`/`end` (in seconds, via `time_base`) + the
  chapter's `title` tag.
- **Per-stream disposition** — the `AV_DISPOSITION_*` bitfield decoded to named booleans
  (`default`, `forced`, `attached_pic`, …) and **language** (the `language` tag), on each stream.

**In — write side (`op:"process"`):**

- Set the **output container tags** and **per-stream tags** from the job-spec before
  `avformat_write_header`.
- **Copy chapters through** a transcode (a passthrough directive — read input chapters, write them
  on the output `AVFormatContext`).
- **Preserve / override disposition + language** on output streams.
- **Attached pictures / cover art** — an `attached_pic` image stream (album art in mp3/mp4) carried
  through as a copied image stream, *not* run through the filtergraph.

**Out (referenced, not owned):**

- **Stream-copy mechanics** (`-c copy`, packet passthrough, bitstream filters) → **[0013](0013-remux-stream-copy.md)**.
  Cover-art passthrough and chapter carry *rely on* 0013's copy path; we own the metadata intent,
  0013 owns the byte-copy engine.
- **Subtitle-stream specifics** (subtitle codecs/muxers, language on subtitle tracks) → **0019**.
  We define the generic per-stream `language`/`disposition` fields; subtitle plumbing is theirs.
- **Redesigning `probe`** — the op already exists ([job-spec ref](../../../../ffmpeg-wasi/docs/reference/job-spec.md));
  we *extend* `describe_stream`/`probe_input`, not rebuild them.

## 3. Approach / design

**Read.** `probe_input` already walks `AVFormatContext`; extend it. Add a `tags` object built by
iterating `av_dict_get(fmt->metadata, "", t, AV_DICT_IGNORE_SUFFIX)`; add a `chapters` array over
`fmt->chapters[]` (rescale `start`/`end` by `ch->time_base`, read the `title` tag). In
`describe_stream`, add the stream's own `tags`, a decoded `disposition` object (named flags from
`st->disposition`), and `language` (the `language` tag, hoisted for convenience). All read-only,
all native — the demuxer already populated these dicts during `avformat_find_stream_info`.

**Write.** `process.c` builds each output `ofmt` and its streams, then calls
`avformat_write_header`. Thread metadata in *before* that call: `av_dict_set` the container tags
onto `ofmt->metadata`, and per-stream tags / `language` / `disposition` onto each `ost`. **Chapters**
copy by allocating `ofmt->chapters` from the chosen input's `AVChapter` array (deep-copy
`start`/`end`/`time_base` + the title dict). **Cover art** is the wrinkle: an `attached_pic` input
stream is a single packet, not a continuous track — it must bypass the decode→filter→encode loop
and be **stream-copied** (0013) onto an output stream that keeps the `AV_DISPOSITION_ATTACHED_PIC`
flag, or the muxer rejects it / re-encodes a still as video. So cover art = "copy this image stream
+ set its disposition," sitting squarely on 0013's passthrough path.

## 4. Job-spec & build impact

- **Build:** none — `av_dict_*`, `AVChapter`, disposition flags are core `libavformat`/`libavutil`,
  always linked. No allowlist change, no flag, no lib.
- **`outputs[].metadata` (vocabulary, mine).** A `{key: value}` tag map → `ofmt->metadata`
  (`{"title":"…","artist":"…","comment":"…"}`). `av_dict_set` each before `write_header`.
- **`outputs[].chapters` (vocabulary, mine).** A passthrough directive — `"copy"` (or an input
  index, e.g. `"0"`) to carry that input's chapters onto the output; absent/`"none"` → drop. Keep
  it a directive, not an inline chapter-authoring DSL (authoring is an open question, Q2).
- **Per-stream `metadata` / `language` / `disposition` (vocabulary, mine).** On the output-pad /
  stream-routing level: a tag map, a `language` string, and a `disposition` set (e.g.
  `["default"]`, `["attached_pic"]`) applied to the corresponding `ost`. These attach to whichever
  `outputs[].map` pad they belong to; shape pins in impl (a parallel array vs a per-map object).
- **Probe-result extension (mine):** `inputs[].tags`, `inputs[].chapters[]`
  (`start`/`end`/`title`), and per-stream `tags` / `disposition` / `language`. Purely additive —
  existing fields unchanged, so an old afmpeg ignores them.
- **afmpeg side:** the `Probe`/`ProbeStream` structs (`pkg/afmpeg/runtime.go`) gain `Tags`,
  `Chapters`, `Disposition`, `Language`; `Command`'s `Output` (`pkg/afmpeg/command.go`) gains
  `Metadata`, `Chapters`, and per-stream metadata fields, serialised in `jobSpec`.

## 5. Licensing & size impact

- **Licence: none.** All-native, both variants identically. Pure 0012-§3 "expands the default
  variant" work — proprietary-compatible.
- **Size:** negligible. The dict/chapter/disposition code is already in the linked `libavformat`;
  this is driver glue (a few KB of `driver.c`/`process.c`), not new object code. Not a lean/full
  axis concern — it ships in every build.

## 6. Decisions & open questions

- **D-0020-A — metadata is fields on existing ops, not a new op.** Read extends `probe`; write
  extends `process` `outputs[]`. No `op:"metadata"`. Keeps the two-op contract (0007 §4) intact.
- **D-0020-B — chapters are passthrough-only in v1.** `outputs[].chapters: "copy"` carries input
  chapters; *authoring* chapters from the spec is deferred (Q2). Covers the common case (transcode
  keeps chapters) cheaply.
- **D-0020-C — cover art rides 0013's stream-copy path, it is not filtered.** An `attached_pic`
  stream is copied + flagged, never decoded into the graph. This spec owns the *intent*
  (preserve cover art + its disposition); 0013 owns the copy mechanism. Hard dependency on 0013.
- **D-0020-D — `language`/`disposition` are generic per-stream fields.** Defined here for all
  stream types; 0019 consumes them for subtitle tracks without redefining them.
- **Q1 — per-stream metadata shape:** a `disposition`/`language`/`tags` block keyed by `map` pad
  vs a parallel `streams[]` array on the output. Pin in impl.
- **Q2 — chapter authoring** (define chapters inline in the spec, not just copy) — deferred;
  promote if a consumer needs it.
- **Q3 — tag key normalisation:** libav normalises some keys per-muxer (e.g. `title` vs
  `TITLE` in matroska). Pass keys through verbatim and let the muxer map them, or normalise? Lean
  to verbatim (libav already maps on write).

## 7. Requirements

- **R-PARITY-METADATA** — `probe` surfaces container + per-stream tags, chapters, disposition, and
  language; `process` sets output tags (container + per-stream), copies chapters through, preserves
  or overrides disposition/language, and carries `attached_pic` cover art — all native, both
  variants.
- **R-0020-1** — probe-result JSON gains `tags` / `chapters` / per-stream `tags`/`disposition`/
  `language`, purely additively (old consumers unaffected).
- **R-0020-2** — `outputs[].metadata` + `outputs[].chapters` + per-stream metadata are routed into
  `ofmt->metadata` / `ofmt->chapters` / `ost` before `avformat_write_header`.
- **R-0020-3** — cover art (`attached_pic`) survives a transcode as a copied, correctly-disposed
  image stream (via [0013](0013-remux-stream-copy.md)).
- **R-0020-4** — afmpeg's `Probe`/`Command` models expose the new fields.

## 8. Test surface

- **Read (integration, real driver):** probe a tagged file (title/artist), a chaptered file, and a
  multi-stream file; assert `tags`, `chapters` (start/end/title), per-stream `disposition` +
  `language`. Probe an MP3 with embedded art → an `attached_pic` stream is reported.
- **Write round-trips:** `process` with `outputs[].metadata` → probe back, assert the tags landed;
  transcode a chaptered input with `chapters:"copy"` → probe back, assert chapter count + titles;
  set a stream `language`/`disposition:["default"]` → probe back, assert it.
- **Cover art:** transcode an audio file carrying cover art → the output retains the `attached_pic`
  image stream (pairs with the 0013 copy tests).
- **Fixtures:** the 0007 dependency-free corpus (synthetic WAV/PNG) plus a small **tags/chapters**
  fixture authored *by the engine itself* (write tags → read them) — no licensed media needed for
  the tag/chapter path; cover-art tests reuse a generated PNG as the attached picture.

## 9. Dependencies & sequencing

- **Hard dependency on [0013](0013-remux-stream-copy.md)** for the cover-art / `attached_pic`
  passthrough (and any chapter-only remux). The *read* side and *tag/chapter write on a re-encode*
  are independent of 0013 — **build read + tag-write first**, fold in cover art when 0013 lands.
- **Feeds [0019](0019-text-and-subtitles.md)** — it reuses the generic `language`/`disposition`
  per-stream fields defined here for subtitle tracks.
- **Independent of the codec/container/filter batches** (0015/0016/0017) — metadata is orthogonal
  to which codecs ship; build in any order.

## 10. Definition of done

`probe` reports container + per-stream tags, chapters, disposition, and language (additively);
`process` accepts `outputs[].metadata`, `outputs[].chapters: "copy"`, and per-stream
`language`/`disposition`/tags, routing them into `ofmt`/`ost` before `write_header`; cover art
survives a transcode as a copied `attached_pic` stream over 0013's path; afmpeg's `Probe` and
`Command` models expose the new fields; tests cover read + write round-trips on the dependency-free
corpus. Decisions D-0020-A…D are recorded; the open questions (Q1–Q3) are resolved in
implementation. Stream-copy mechanics and subtitle-stream specifics remain with 0013 and 0019.
