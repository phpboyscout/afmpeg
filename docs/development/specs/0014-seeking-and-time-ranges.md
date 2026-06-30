# 0014 — seeking & time ranges

Status: **DRAFT / SCOPING** (one of the 0013–0023 batch promoting [0012](0012-feature-parity-roadmap.md)
§7's Tier-1 gaps to specs. Design only — not yet built. Review before building.)
Date: 2026-06-30
Parent: [0012](0012-feature-parity-roadmap.md) §4F/§7; [0007](0007-libav-direct-engine.md) (the engine this extends)
Owns: **R-PARITY-SEEK** (input fast/accurate seek + output duration/end + PTS rebasing)

## 1. Why

Clip extraction — "give me 5 seconds from 0:12" — is one of the most-asked media operations, and
today the engine can only do it the expensive way: the `trim`/`atrim` filter, which **decodes from
the start of the file** and throws away everything before the window. For a 2-hour source that is
minutes of wasted decode to emit a 5-second clip. FFmpeg's `-ss` solves this by seeking the demuxer
to the nearest keyframe *before* decoding; `-t`/`-to` stop early. Both are cheap, single-threaded,
file-based — squarely inside the WASI envelope ([0012](0012-feature-parity-roadmap.md) §2) — and
pair with stream copy ([0013](0013-remux-and-stream-copy.md)) to make fast remux-trim possible. This
is the highest-leverage time-domain gap alongside 0013.

## 2. Scope

In: input-level **fast seek** (keyframe, no decode before the target) and **accurate seek**
(keyframe seek + decode-and-discard to the exact frame); output **duration** (`-t`) and **end**
(`-to`); **PTS rebasing** (zero-base the output vs preserve source timestamps, `-copyts`); the rule
for trimming a **copied** stream (0013). Out: the in-graph `trim`/`atrim` filter (stays available,
unchanged — §3); per-frame select/`thumbnail` extraction (→ [0021](0021-frame-extraction-op.md));
input loop/`-stream_loop`; reverse/`-itsoffset`. No new codec, lib, or build flag (§5).

## 3. Approach / design (libav mechanics)

**Fast seek (default).** Before the read loop, for each input that requests a start, call
`avformat_seek_file(fmt, -1, INT64_MIN, ts, ts, 0)` (seconds → `AV_TIME_BASE`), landing on the
keyframe at-or-before `ts`. Demuxing then begins there — the packets before it are never read or
decoded. This is the whole cost saving.

**Accurate seek.** Fast-seek as above, then in the feed path drop decoded frames whose
`best_effort_timestamp` (rescaled to a common base) is `< start_pts`, per graph input, until the
first frame ≥ `start_pts` — from there frames flow into the buffersrc normally. Cost = decoding the
keyframe-to-target gap (a fraction of a GOP), not the whole file.

**Duration / end.** Track an end cutoff per input: `end_pts = (mode==duration) ? start + duration
: end`. In the read loop, once an input's packet PTS reaches `end_pts`, mark that input EOF (flush
its decoders, close its buffersrc) — the existing multi-input EOF path in `process.c` already
drains and trailers cleanly. `duration` and `end` are mutually exclusive on one output.

**PTS rebasing.** Default **reset**: subtract `start_pts` from frame PTS as it enters the buffersrc
so the output starts at t=0 (what almost every consumer wants from a clip). `copy_ts: true`
preserves source timestamps (output begins at the seek point's PTS) — needed when timelines must
stay aligned across a multi-output split.

**Composition with the filtergraph.** Seek happens at the **demuxer**, before decode, so the
filtergraph sees an already-windowed stream — seek and filters compose without interaction. The
`trim`/`atrim` filter is therefore *not* replaced: it remains the only tool for trimming **one
stream among several to a different window**, or trimming **mid-graph** (after a concat/overlay). The
rule of thumb: input `seek`+`duration` for cheap whole-input clipping; `trim` for in-graph surgery.

**Composition with stream copy (0013).** A copied stream is muxed packet-for-packet with no
re-encode, so it **can only cut on keyframes**. Therefore: on a copied stream, `seek` always behaves
as **fast** (start snaps to the keyframe at-or-before the request) and an explicit `mode:"accurate"`
on a copied stream is a **hard error** (frame-accuracy requires the decode-discard the copy path
skips). `duration`/`end` on a copy likewise snap to the last keyframe at-or-before the cutoff. This
constraint is stated to the consumer, not silently fudged.

## 4. Job-spec & build impact

New vocabulary (owned here):

```jsonc
{
  "op": "process",
  "inputs":  [ { "path": "in.mp4",
                 "seek": { "start": 12.5, "mode": "fast" } } ],  // mode: fast (default) | accurate
  "outputs": [ { "path": "out.mp4", "video_codec": "libx264",
                 "duration": 5.0,        // -t  (seconds)  ─┐ mutually
                 "end": 17.5,            // -to (seconds)  ─┘ exclusive
                 "copy_ts": false } ]    // false (default) → zero-base; true → preserve source PTS
}
```

- `inputs[].seek` — `{start: seconds, mode: "fast"|"accurate"}`. `start` alone with no `mode`
  ⇒ fast. Per-input (each input seeks independently).
- `outputs[].duration` / `outputs[].end` — the output's time window; measured on the **output
  (rebased) timeline** unless `copy_ts` (then source timeline). Output-level (not global) so a
  multi-output split can window each file differently.
- `outputs[].copy_ts` — the PTS-rebasing directive (default `false` = reset to zero).
- **afmpeg Go model:** `Input` gains `Seek *Seek{Start float64; Mode string}`; `Output` gains
  `Duration`/`End float64` + `CopyTS bool`; `JobSpec()` emits the above. Mirrors the existing
  `command.go` shape (additive, no redesign).
- **Vocabulary version bump** ([0007](0007-libav-direct-engine.md) §4, 0012 §6): an older afmpeg
  must fail cleanly on these fields rather than silently ignore a requested window.
- **Build:** none. `avformat_seek_file` and packet-PTS handling are core libavformat — no
  `--enable-*`, no new dep.

## 5. Licensing & size impact

Negligible. Seeking, `-t`/`-to`, and timestamp handling are in `libavformat`/`libavutil` already
linked by every variant — **no new decoder, library, or build flag**, so the `.wasm` is unchanged
and the **LGPL default** is unaffected. This is pure driver + vocabulary work; it lands identically
in lean and full builds.

## 6. Decisions

- **D-0014-A — fast seek is the default; accurate is opt-in.** Keyframe seek is the cheap, common
  case; the decode-discard cost of frame-accuracy is paid only when `mode:"accurate"` is asked for.
- **D-0014-B — one `seek` object with a `mode` enum**, not parallel `fast_seek`/`accurate_seek`
  fields — keeps the surface small and the fast/accurate choice explicit and local to the input.
- **D-0014-C — `start` on inputs, `duration`/`end` on outputs.** Matches FFmpeg's mental model
  (input seek vs output window) and lets a multi-output split window each file independently.
- **D-0014-D — PTS reset to zero is the default.** A clip that starts at t=0 is what consumers
  expect; `copy_ts:true` is the opt-in for timeline preservation.
- **D-0014-E — `trim`/`atrim` is retained, not deprecated.** Demux-seek and in-graph trim are
  complementary (§3); seek is the cheap whole-input path, `trim` the mid-graph/per-stream scalpel.
- **D-0014-F — accurate seek on a copied stream (0013) is a hard error; copy trims snap to
  keyframes.** A copy cannot be frame-accurate without re-encoding; the contract states the
  constraint rather than silently producing an off-by-a-GOP cut.

**Open questions.**
- **Q1 — `end` timeline under `copy_ts`.** When timestamps are preserved, is `end` an absolute
  source PTS or still relative to the seek point? (Proposed: absolute source PTS — `-to` semantics.)
- **Q2 — combining input `start` with a graph `trim`.** Define the precedence/offset when both
  window the same stream (seek first, then `trim` on the rebased timeline) — pin in impl.
- **Q3 — streams without keyframes** (e.g. some PCM/`image2`): fast seek is exact there; confirm the
  `mode` distinction collapses harmlessly.
- **Q4 — per-input `duration`** (rare; cap one input of several). Out of scope for v1 unless a
  consumer needs it; output `duration` covers the common case.

## 7. Requirements

- **R-PARITY-SEEK** — The engine extracts a time window without decoding from the file start:
  input-level **fast seek** (keyframe, cheap) and opt-in **accurate seek** (frame-exact via
  decode-discard); output **duration**/**end** cutoffs; **PTS rebasing** (zero-base default,
  `copy_ts` opt-in). Composes with the filtergraph (seek precedes decode) and obeys the
  keyframe-only rule when trimming a copied stream (0013). Surfaced through the versioned job-spec
  and afmpeg's `Command` model.

## 8. Test surface

- **Unit (afmpeg):** `JobSpec()` emits `seek`/`mode`/`duration`/`end`/`copy_ts` correctly;
  `duration`+`end` together is a validation error; `mode:"accurate"` on a copy-mapped output errors.
- **Integration (real driver, `AFMPEG_TEST_FFMPEG_WASI`):** from a synthetic multi-keyframe fixture,
  (a) fast seek lands on the keyframe ≤ target and the output duration is correct; (b) accurate seek
  starts at the exact frame; (c) `duration` and `end` each cut to the expected length; (d) default
  output PTS starts at 0, `copy_ts:true` preserves source PTS (assert via `probe`); (e) accurate
  seek on a copied stream is rejected. Fixtures stay dependency-free (synthetic media, 0012 §6).

## 9. Dependencies & sequencing

- **Independent of 0013 for the re-encode path** — fast/accurate seek + duration/end work on the
  existing decode→filter→encode loop and can land first.
- **Shares the copy semantics with [0013](0013-remux-and-stream-copy.md)** — the keyframe-only trim
  rule (D-0014-F) is meaningful only once stream copy exists; sequence the copy-trim tests after
  0013, or land the rule as a validation guard that 0013 wires up.
- **Vocabulary-version coordination** with the rest of the 0013–0023 batch (one bump per landed
  field set, 0012 §6).

## 10. Definition of done

The job-spec gains `inputs[].seek{start,mode}`, `outputs[].duration|end`, and `outputs[].copy_ts`
(versioned); the engine seeks each input before its read loop, supports fast and accurate modes,
enforces the end cutoff, and rebases (or preserves) PTS per `copy_ts`; the copied-stream trim rule
(D-0014-F) is enforced with a clear error; afmpeg's `Command`/`Input`/`Output` model and `JobSpec()`
carry the fields; the relationship to `trim`/`atrim` and to 0013's copy path is documented; and the
§8 unit + integration tests (incl. the keyframe-snap and copy-reject cases) pass against the real
driver. The decisions (D-0014-A…F) are recorded; the open questions (Q1–Q4) are resolved in impl.
