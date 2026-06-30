# 0017 — native filter batch

Status: **DRAFT / SCOPING** (one of the [0012](0012-feature-parity-roadmap.md) §7 child specs —
the Tier-1 in-tree filter expansion. Design only; no implementation. Review before building.)
Date: 2026-06-30
Parent: [0012](0012-feature-parity-roadmap.md) (§4D filter table, §5 Tier 1); [0007](0007-libav-direct-engine.md) (the engine + the `filter` field)
Owns: **R-PARITY-NATIVE-FILTERS**

## 1. Why

The engine ships ~30 filters today (the [`build/libav.sh`](https://gitlab.com/phpboyscout/ffmpeg-wasi)
`--enable-filter` allowlist). FFmpeg has 400+. The `filter` job-spec field is already an arbitrary
ffmpeg filtergraph string parsed by `avfilter_graph_parse2` ([0007](0007-libav-direct-engine.md) §4,
`src/process.c`), so a consumer *can* write `loudnorm` or `eq` today — but configure's
`--disable-everything` means the filter is **not linked**, and the graph parse fails at runtime. This
spec closes the gap the cheapest way parity ever gets: in-tree filters are **flags + bytes only** —
no new library, no licence change, no driver code, no vocabulary change. Pure reach for the **LGPL
default** variant. The headline wins — `loudnorm`, `select`/`thumbnail`, `eq`/color, `fade`,
`hstack`, `yadif`, `atempo`, `palettegen` — are the filters consumers ask for first.

## 2. Scope

**In:** add native (in-tree) filters to the `--enable-filter` allowlist, grouped (§3); the
supported-filter **reference matrix** doc; the lean/full bucketing input ([0022](0022-lean-full-matrix.md));
fixtures + integration tests over the real driver; the analysis-filter-output **open question** (§6).

**Out (non-goals / hard boundaries):**

- **`drawtext` (libfreetype) and `subtitles`/`ass` (libass)** — they pull **external libraries**, not
  flags. They belong to **[0019](0019-text-and-subtitles.md)**. Excluded here entirely; cross-ref only.
- **No SIMD / no threads / no network** ([0012](0012-feature-parity-roadmap.md) §2). Filters run on the
  single-threaded path; `--disable-asm` means the C fallbacks (correct, slower). Some filters
  (`gblur`, `nlmeans`, `loudnorm` 2-pass) are heavier single-threaded — a doc note, not a blocker.
- **No new job-spec vocabulary.** This is the load-bearing point: these extend the *available
  vocabulary inside the `filter` string*; the spec schema, `outputs[].map` routing, and the driver
  are untouched. (Contrast [0013](0013-remux-stream-copy.md)/[0014](0014-seeking-time-ranges.md), which
  *do* bump the vocabulary.)

## 3. Approach / design

One change in substance: extend the `ENABLE` block's `--enable-filter=…` list in `build/libav.sh`.
Group the additions so the allowlist (and the doc matrix) stay legible, and so [0022](0022-lean-full-matrix.md)
can split them lean vs full:

| Group | Filters to add | Headline |
|---|---|---|
| **Video fade** | `fade` (plain dissolve; have `xfade`/`afade`, not plain `fade`) | ★ |
| **Color / levels** | `eq, hue, colorbalance, curves, colorchannelmixer, lut, lut3d` | ★ |
| **Sharpen / blur** | `unsharp, gblur, boxblur` | |
| **Compose / grid** | `hstack, vstack, xstack, blend, tile` | ★ (`hstack`) |
| **Frame select** | `select, thumbnail, framestep` | ★★ (smart extraction) |
| **GIF quality** | `palettegen, paletteuse` | ★ (pairs w/ GIF encode [0016](0016-native-codec-batch.md) + mux [0015](0015-container-coverage.md)) |
| **Deinterlace** | `yadif, bwdif` | ★ (broadcast sources) |
| **Keying** | `chromakey, colorkey` | |
| **Geometry** | `rotate, hflip, vflip, reverse` | |
| **Draw (native)** | `drawbox, drawgrid, vignette` | (note: `drawtext` is **0019**) |
| **Audio loudness** | `loudnorm` (EBU R128), `dynaudnorm, acompressor, compand` | ★★ (podcast/broadcast) |
| **Audio EQ / dynamics** | `highpass, lowpass, equalizer, atempo, aecho, silenceremove, afftdn` | ★ (`atempo` speed) |
| **Channels** | `pan, channelsplit, channelmap, join` | |
| **Analysis (metadata)** | `cropdetect, blackdetect, signalstats, silencedetect, ebur128, astats` | ★ (automated decisions) |

Everything above is a `libavfilter` in-tree filter — no `--enable-libX`. The driver wires
buffersrc→graph→buffersink exactly as today; a longer filtergraph string is the only difference at
runtime. `palettegen`/`paletteuse` are the one composition wrinkle: the classic two-pass
(`palettegen` → file → `paletteuse`) reduces to a single-pass split graph
(`split[a][b];[a]palettegen[p];[b][p]paletteuse`) the existing one-graph engine already supports.

## 4. Job-spec & build impact

- **Job-spec:** none. No new field, op, or version bump — the only spec touched is `build/libav.sh`'s
  `--enable-filter` list. (If the analysis-output open question (§6) lands as a driver change, *that*
  would bump the vocabulary; out of scope for this batch unless adopted.)
- **Build:** one allowlist edit. The supported-filter **reference matrix**
  ([0012](0012-feature-parity-roadmap.md) §6 "doc deliverable") gains every filter, generated/checked
  against the build so "does it do X?" has a sourced answer. Lean vs full assignment per filter feeds
  [0022](0022-lean-full-matrix.md).

## 5. Licensing & size impact

- **Licence:** all in-tree, all LGPL-clean — **zero `--enable-gpl` triggers** in this batch (the only
  GPL filters, e.g. some `frei0r`/postproc bindings, are deliberately excluded). Pure **LGPL-default**
  enrichment; the GPL variant inherits them too.
- **Size:** each filter adds object code only — individually small (KB-scale), collectively a measure
  worth tracking under `--enable-small`. The mitigation is the **lean/full split** ([0022](0022-lean-full-matrix.md)):
  the headline set (`fade, eq, hue, select, thumbnail, hstack, yadif, loudnorm, atempo, palettegen`)
  is a strong **lean** candidate; the long tail (`afftdn, nlmeans-class, signalstats, channel*`) can be
  **full**-only. Decision deferred to 0022; this spec supplies the per-group inventory it buckets.

## 6. Decisions & open questions

- **D-0017-A — native-only batch.** This spec owns *only* in-tree filters (flags + size). External-lib
  filters (`drawtext`/freetype, `subtitles`/`ass`/libass) are **0019**. Clean cut on the lib boundary.
- **D-0017-B — no vocabulary change.** Filters extend the `filter` string's reach, not the schema. No
  job-spec version bump; the engine artifact's filter set is the implicit capability surface.
- **D-0017-C — single-pass palette.** Ship `palettegen`+`paletteuse` as a one-graph split, not a
  driver-orchestrated two-pass — the existing engine handles it, and it pairs with GIF encode/mux.
- **D-0017-D — group the allowlist.** Keep `--enable-filter` grouped + commented (by §3 table) so the
  list, the doc matrix, and the 0022 lean/full split stay in lockstep.

**Open questions:**

- **Q-0017-1 — surface analysis-filter metadata on stdout?** `cropdetect`, `blackdetect`,
  `silencedetect`, `ebur128`, `signalstats`, `astats` compute their results and **emit them only to the
  log** (the `metadata` print path / `av_log`), which the engine currently routes to stderr. To make
  these *useful* for automated decisions (the whole point — "find the crop", "is this silent"), a
  consumer needs the values structured on **stdout**, like the `probe` op. This is a possible small
  **driver enhancement** (a metadata-capture path, or an `op:"analyse"`) — flagged, **not owned here**;
  if adopted it bumps the vocabulary and likely becomes its own spec. Without it the filters still
  *run*, they're just observe-only.
- **Q-0017-2 — heavy-filter guidance.** Single-threaded + no-SIMD makes `gblur`/`loudnorm`/`afftdn`
  noticeably slower; document expectations (→ [0008](0008-performance-strategy.md)) rather than gate.
- **Q-0017-3 — lean vs full line.** The exact per-filter bucketing is 0022's call; §5 proposes a seed.

## 7. Requirements

- **R-PARITY-NATIVE-FILTERS** — The engine's `--enable-filter` allowlist is expanded with the §3
  native (in-tree, LGPL-clean) filter groups, so a consumer's filtergraph string may use them and the
  graph parses + runs; the supported-filter reference matrix documents the full set against the build;
  no job-spec vocabulary change; external-lib filters (`drawtext`, `subtitles`) remain out (→ 0019).

## 8. Test surface

- **Per group, one integration test** over the real driver (gated on `AFMPEG_TEST_FFMPEG_WASI`, as
  existing engine tests), asserting the graph parses + produces output: e.g. `eq=contrast=1.2` on a
  synthetic PNG/video; `loudnorm` on a synthetic WAV; `select`/`thumbnail` extracting frames;
  `hstack` of two inputs; `yadif` on an interlaced fixture; `palettegen|paletteuse` → GIF (joins
  [0016](0016-native-codec-batch.md)/[0015](0015-container-coverage.md) once those land).
- **Build-matrix check** — a test asserting every filter named in the reference matrix is actually
  present in the built artifact (no doc/build drift).
- Fixtures lean on the existing **dependency-free synthetic** corpus where possible (synthetic
  WAV/PNG); the deinterlace + palette cases may need a small licence-clean media fixture
  ([0012](0012-feature-parity-roadmap.md) §6 "test surface").

## 9. Dependencies & sequencing

- **Independent / parallelisable** — no dependency on the remux ([0013](0013-remux-stream-copy.md)) or
  seeking ([0014](0014-seeking-time-ranges.md)) work; it's an isolated build-allowlist change.
- **Pairs with** [0016](0016-native-codec-batch.md) (GIF encode) + [0015](0015-container-coverage.md)
  (GIF mux) to complete the `palettegen`→GIF story; ship those together for a coherent GIF feature.
- **Feeds** [0022](0022-lean-full-matrix.md) (per-filter lean/full bucketing) and the
  [0012](0012-feature-parity-roadmap.md) §6 reference-matrix doc.
- **Excludes / hands off to** [0019](0019-text-and-subtitles.md) (the external-lib filters).
- **Q-0017-1**, if pursued, spawns a small driver/vocabulary spec (analysis-output) — sequenced after.

## 10. Definition of done

The native-filter groups (§3) are enabled in `build/libav.sh`, the released artifact's filtergraph
parser accepts them, the supported-filter reference matrix is generated/checked against the build, and
per-group integration tests pass over the real driver — all with **no job-spec change**, **no external
library**, and **no `--enable-gpl` trigger**. The analysis-output question (Q-0017-1) is recorded and
dispatched (not resolved here), and the per-filter lean/full inventory is handed to 0022. `drawtext`
and `subtitles` are explicitly deferred to 0019.
