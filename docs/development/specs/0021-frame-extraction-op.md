# 0021 — frame extraction op

Status: **IMPLEMENTED** (Phase 3 of the [implementation roadmap](../implementation-roadmap.md).
The third first-class engine `op` after `probe`/`process`: `op_frames` in ffmpeg-wasi's new
`src/frames.c` (dispatched from `driver.c`), a seek-driven gather loop over the four selectors —
single `timestamp`, explicit `timestamps`, regular `interval`, and `scene` (`select='gt(scene,T)'`
or `thumbnail`) — each frame optionally scaled and encoded (png/mjpeg/webp) to an engine-owned
templated path, reported in result JSON. afmpeg models it as a typed `FrameJob`/`FrameSelect` with
a `Runtime.Frames` method (`pkg/afmpeg/frames.go`). Bumps the job-spec vocabulary to **v6**. Seek
selectors run in any profile; `scene` needs the intermediate `select`/`thumbnail` filters. Proven
by `frames.c` selectors + afmpeg's `TestIntegration_Frames` (all selectors, scale, count, mjpeg).
The `tile` contact-sheet stretch (§3/D-0021-E) is deferred as a follow-up.)
Date: 2026-06-30
Parent: [0012](0012-feature-parity-roadmap.md) §4F/§7; [0007](0007-libav-direct-engine.md) §4 (the `op` dispatch)
Owns: **R-PARITY-FRAMES** — the `frames` op + its job-spec vocabulary.

## 1. Why

Pulling still frames out of a video — a poster at 5s, a thumbnail strip every 10s, a contact sheet,
the "best" representative frame — is the single most-asked media chore, and today ffmpeg-wasi makes
it awkward. You express it as a `process` job whose `filter` is some `fps=1/10`/`select='eq(n,0)'`
gymnastics into an `image2` muxer, then reason about pad timing and frame counts by hand. The
filtergraph carries knowledge (which timestamps, how many, where the files land) that belongs in
**structured fields**, not a hand-tuned graph string. A dedicated `op` makes the bread-and-butter
case a few typed keys, leaves the graph for genuinely graph-shaped work, and gives afmpeg a clean
`Probe`-shaped surface to model. This is the libav-direct philosophy ([0007](0007-libav-direct-engine.md)
§4 / job-spec.md): a typed surface exactly as capable as the engine, no leaky partial-CLI illusion.

## 2. Scope

**In:** a new `op: "frames"` that pulls one or more stills from a video input to image files, by
four selectors — (a) a **single timestamp**, (b) a **list of explicit timestamps**, (c) a **regular
interval** (every N seconds), (d) **scene-change / best** frames (`select='gt(scene,…)'` or the
`thumbnail` filter). Each frame → an image (png/mjpeg, +webp under [0018](0018-lgpl-encoder-expansion.md)),
optional `scale`, written to a templated path with a frame index/timestamp. A count limit caps
output. The op owns its vocabulary (§4) and its decode→scale→encode loop (§3).

**Out (boundaries — depended on, not owned here):** fast input **seeking** is
[0014](0014-seeking-time-ranges.md) (we *call* it for timestamp accuracy); the `select` / `thumbnail`
/ `tile` **filters** are enabled by [0017](0017-native-filter-batch.md); the **image encoders**
(mjpeg/png native today, webp [0018](0018-lgpl-encoder-expansion.md)) already exist. The
**contact-sheet / sprite-sheet** variant (via `tile`) is a noted **stretch goal** (§3), not first
delivery. Audio, multi-input, and general transcode stay with `process`.

## 3. Approach / design

A new `op_frames(spec)` branch in `driver.c`'s dispatch (a third `strcmp(op, "frames")` beside
`probe`/`process`), in its own `frames.c`. It reuses `process.c`'s decode → `scale` → image-encode
machinery (the `buffer`/`scale`/encoder path), but as a **distinct loop**, not a thin `process`
wrapper — the access pattern is fundamentally seek-and-grab-one, not stream-everything:

- **Explicit / single / interval timestamps** → for each target time, **fast input seek**
  (`av_seek_frame`, [0014](0014-seeking-time-ranges.md)) to the nearest preceding keyframe, decode
  forward to the first frame ≥ target, run it through an optional `scale` filter, encode to the image
  codec, write file. Seek-per-frame is far cheaper than `process`'s decode-the-whole-stream-through-`trim`.
  An `interval` is expanded to a timestamp list from the probed duration (capped by `count`).
- **Scene / best** → no seek; stream-decode through the `select='gt(scene,T)'` or `thumbnail` filter
  ([0017](0017-native-filter-batch.md)) and emit each frame the filter passes (capped by `count`).
- **Output naming** → one file per frame from a `path` **template** (`frame_%03d.png` style; index
  and/or timestamp tokens). The engine writes N files and reports each (like `process`'s per-output
  JSON), rather than relying on an `image2` muxer's implicit numbering.

**Why distinct, not a `process` wrapper:** `process` is one streaming pass over a `filter_complex`
into muxers; `frames` is a seek-driven gather into a *templated set of single-image files* with
engine-owned timestamp→file logic. Sharing the decode/scale/encode helpers (refactor the reusable
bits out of `process.c`) keeps it DRY without contorting either path. **Stretch:** a `sheet`/`tile`
mode feeds the gathered frames through `tile` into one contact-sheet image — same gather, one
muxed output; defer until the per-frame op is solid.

## 4. Job-spec & build impact

The vocabulary the `frames` op adds (afmpeg's `Command` gains a parallel emitter; bumps the
versioned job-spec contract — [0012](0012-feature-parity-roadmap.md) §6):

```jsonc
{
  "op": "frames",
  "inputs": [ { "path": "in/clip.mp4" } ],   // exactly one video input
  "select": {                                 // exactly one selector
    "timestamp":  12.5,                        // (a) single, seconds
    "timestamps": [1.0, 5.0, 30.0],            // (b) explicit list
    "interval":   10.0,                        // (c) every N seconds
    "scene":      0.4                           // (d) scene-change threshold (or "thumbnail")
  },
  "path":  "out/frame_%03d.png",              // template (index/timestamp tokens)
  "codec": "png",                              // image encoder: png | mjpeg | webp(0018)
  "scale": "320:-2",                           // optional, ffmpeg scale args
  "count": 25                                  // optional cap on frames emitted
}
```

`select` is a one-of (validation rejects zero or multiple). **Difference from a `process` + image
muxer:** there the timestamps/count live inside a `fps`/`select` graph and the `image2` muxer owns
naming; here they are typed fields, the seek path replaces full-stream decode for accuracy and
speed, and naming is an engine-owned template. **Build impact:** none of its own — it links the
existing decoders + `scale` + png/mjpeg encoders; it *requires* [0014](0014-seeking-time-ranges.md)
(seek) and [0017](0017-native-filter-batch.md) (`select`/`thumbnail`; `tile` for the stretch) to be
in the build. No new external lib (webp is 0018's).

## 5. Licensing & size impact

**LGPL-clean, default variant.** Everything the op touches — the decoders, `scale`, the `select`/
`thumbnail`/`tile` filters, the png/mjpeg encoders — is native/LGPL; the op adds **driver code
only**, no new lib, negligible `.wasm` growth. webp output rides on [0018](0018-lgpl-encoder-expansion.md)
(libwebp, BSD — still LGPL-clean). No GPL surface, no full-variant gate. Within the §2 envelope:
single-threaded, no network, file-only.

## 6. Decisions & open questions

- **D-0021-A — a distinct `op`, not a `process` preset.** Frame extraction gets its own `op` and
  loop (§3); it shares decode/scale/encode helpers but owns timestamp→file logic. A `process` macro
  would re-leak the graph gymnastics this exists to remove.
- **D-0021-B — one of four selectors per job** (`timestamp` / `timestamps` / `interval` / `scene`),
  validated as mutually exclusive. Keeps the schema unambiguous; richer composition stays in `process`.
- **D-0021-C — timestamp accuracy via fast input seek** ([0014](0014-seeking-time-ranges.md)),
  decoding forward to the first frame ≥ target — not the `trim` filter (which decodes everything).
  Hard-depends on 0014; the op is not delivered before it.
- **D-0021-D — engine-owned templated naming**, one file per frame (`frame_%03d.png`), reported in
  the result JSON — not the `image2` muxer's implicit numbering.
- **D-0021-E — contact-sheet/`tile` is a stretch goal** (a later `sheet` mode), not first delivery.
- **Open:** template token vocabulary (index only, or `%t`/timestamp too?); seek-accuracy contract
  (nearest-keyframe vs exact-frame, the precise-vs-fast knob — coordinate with 0014); default `count`
  when `interval` over a long input has no cap; whether `scale` is a string (ffmpeg args) or typed.

## 7. Requirements

- **R-PARITY-FRAMES** — a first-class `frames` op extracting stills by single timestamp / explicit
  list / interval / scene-change, each to a scaled image (png/mjpeg/webp) via a templated path, with
  a count cap; LGPL-clean, default variant; its vocabulary versioned in the job-spec contract.
- **R-PARITY-FRAMES-A** — afmpeg models the op (a `Frames`/`FrameJob` emitter beside
  `Command.JobSpec()`), parsing the per-frame result JSON.

## 8. Test surface

- **Engine (ffmpeg-wasi):** per selector — single timestamp lands on the expected frame; a list
  yields N files at the right times; `interval` over a known-duration fixture emits the right count;
  `scene`/`thumbnail` emits ≥1 plausible frame; `scale` resizes; `count` caps; template numbering is
  correct; png and mjpeg both encode. Tiny synthetic clip fixtures (a few seconds, known frames).
- **afmpeg:** unit test the emitter (selector → JSON) + a gated integration test
  (`AFMPEG_TEST_FFMPEG_WASI`) extracting frames over the real driver, asserting file count + decoded
  dimensions. Stretch: a `tile` contact-sheet case once that mode lands.

## 9. Dependencies & sequencing

- **Hard-depends:** [0014](0014-seeking-time-ranges.md) (fast seek — timestamp accuracy) and
  [0017](0017-native-filter-batch.md) (`select`/`thumbnail`; `tile` for the stretch). Sequence
  **after** both. png/mjpeg encoders exist today; webp is [0018](0018-lgpl-encoder-expansion.md)
  (optional enhancement, not a blocker).
- **Spans both repos:** the `op` + loop in ffmpeg-wasi (`frames.c`, dispatch); the emitter + result
  parsing in afmpeg. Bumps the job-spec vocabulary version ([0012](0012-feature-parity-roadmap.md) §6).
- **Independent of** the other 0013–0023 siblings beyond 0014/0017/0018.

## 10. Definition of done

A consumer issues an `op: "frames"` job — by single timestamp, list, interval, or scene-change — and
the engine writes correctly-named, optionally-scaled png/mjpeg (or webp) stills to the mounted fs,
reporting each in result JSON, with timestamp accuracy from 0014's seek; afmpeg emits the op from a
typed model and parses the result; all four selectors + `scale`/`count`/template are covered by
engine and gated-integration tests; the vocabulary version is bumped and the job-spec reference page
documents the op. The `tile` contact-sheet stretch is noted as a follow-up. Licence-clean, default
variant, within the WASI envelope.
