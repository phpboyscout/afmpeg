# 0025 — A/V sync & frame-rate control

Status: **PROPOSED** (external review — decision pending; LOW severity. Not yet dispatched by
[0012](0012-feature-parity-roadmap.md); a candidate for inclusion. Design only; no implementation.)
Date: 2026-07-02
Parent: [0012](0012-feature-parity-roadmap.md) (§2 envelope, §4D filters); [0007](0007-libav-direct-engine.md) (the engine + the PTS path)
Source: external commissioned review — `ffmpeg-wasi/feature_parity_review_supplementary.md` §5
Owns: **R-PARITY-AVSYNC** (proposed)

## 1. Why / the gap

Native FFmpeg's `ffmpeg` CLI carries explicit A/V-sync machinery — `-vsync`/`-fps_mode`, `-async`,
`-r` — that **drops or duplicates video frames** to hold a constant frame rate (CFR) and stretches
audio to hold sync. The libav-direct engine ([0007](0007-libav-direct-engine.md)) has **none of it**:
`src/process.c`'s `pull_sinks()` pulls each buffersink frame and hands its PTS **straight to the
encoder** (`drain_encoder`). There is no CFR enforcement, no frame drop/dup, no audio-stretch-for-sync
policy. For a **variable-frame-rate (VFR)** source, or one with corrupt/non-monotonic timestamps, the
output can drift — the review's concern (§5). This spec asks the narrow question: does that gap warrant
**engine work**, or is the existing `fps` filter ([0017](0017-native-filter-batch.md)) a sufficient
answer we need only **document**?

## 2. Validation (confirmed — but LOW severity)

The review's claim is **code-accurate**. In `pull_sinks()` the buffersink PTS is forwarded verbatim,
with a **deliberate comment** that it must *not* be overridden (doing so — e.g. with a decoder's
`best_effort_timestamp` — breaks filters like `xfade` that synthesise their own timestamps). So the
absence of a sync policy is by design, not an oversight; there is genuinely no `-vsync`/`-async`
equivalent. **Why it is nonetheless LOW severity:** the `fps` filter **is already enabled** (0012 §4D
"have today"; owned by [0017](0017-native-filter-batch.md)), so a caller can force CFR **in-graph
today** — `[0:v]fps=30[v]` drops/dups to a constant rate, and `aresample=async=1` corrects audio drift
— with **no engine change**. The gap is therefore *partly solved already*; it bites only the caller who
doesn't know to reach for those filters. That is a docs gap first, an engine gap a distant second.

## 3. Scope

**In:** deciding whether A/V-sync/frame-rate control needs engine work at all; if not, the recommended
**filter-based pattern** (`fps`, `aresample=async`) as documented guidance; if so, the smallest
driver/job-spec surface that would add it. A VFR test fixture either way.

**Out (non-goals):** no threads/SIMD/network ([0012](0012-feature-parity-roadmap.md) §2); no new
library (native only); **no change to the deliberate no-override PTS contract** in `pull_sinks()` — any
CFR logic sits *before* the sink (in-graph), never by rewriting the sink's PTS. Reproducing the full
`ffmpeg` CLI sync matrix (`vsync` modes `passthrough`/`cfr`/`vfr`/`drop`, `-async` sample-rate
compensation) is explicitly **not** a goal; the envelope target is "caller can get CFR when they need
it," not CLI bit-parity.

## 4. Approach / design (options)

- **(a) Document the filter workaround — RECOMMENDED default.** Treat this as a *documentation*
  deliverable: a how-to and a reference note that the CFR/anti-drift tools are `fps=<rate>` (video) and
  `aresample=async=1` / `aresample=async=1000` (audio), composed in the `filter` string. Zero engine
  change, zero vocabulary bump, ships now, and stays honest about the single-threaded cost of dup/drop.
- **(b) Driver-level frame-rate mode — only if (a) proves insufficient.** An optional per-output
  frame-rate target the driver honours by **auto-injecting `fps` into the graph** ahead of that output's
  buffersink (drop/dup to the target). This is sugar over (a): it keeps the no-override PTS contract
  (the `fps` filter does the work in-graph), but bumps the job-spec vocabulary and adds driver code and
  a version increment ([0012](0012-feature-parity-roadmap.md) §6). Justified only by a real consumer who
  cannot express `fps` themselves.
- **(c) Audio async resampling for drift.** The `aresample=async` counterpart to (b) — either as
  caller-written filter (part of (a)) or, under (b), auto-injected. Same reasoning: prefer the filter
  string; promote to a field only on demand.

The recommendation is **(a) now, (b)/(c) deferred behind a trigger** — the filter set already covers the
capability, so engine work buys ergonomics, not reach.

## 5. Job-spec impact

- **Under (a): none.** No new field, op, or version bump — the `filter` string already carries `fps` and
  `aresample=async`. This mirrors [0017](0017-native-filter-batch.md)'s load-bearing point: the
  capability lives *inside* the graph vocabulary, not the schema.
- **Under (b)/(c): a small additive bump.** An optional `outputs[].fps` (number/rational) and/or an
  `outputs[].vsync_mode` enum (`passthrough` | `cfr`), desugaring to an injected `fps`/`aresample`. One
  version increment per [0012](0012-feature-parity-roadmap.md) §6, gated so an old afmpeg fails cleanly.
  Not built unless the trigger fires.

## 6. Decisions & open questions

- **D-0025-A — the PTS no-override contract is inviolable.** Whatever lands, CFR/sync is achieved
  *in-graph* (before the buffersink), never by rewriting the sink's PTS — that comment in `pull_sinks()`
  guards `xfade` and must stand.
- **D-0025-B — filter-first.** The capability is expressed through the `filter` string (`fps`,
  `aresample=async`); a job-spec field is only ever **sugar** over that, never a second mechanism.
- **D-0025-C — recommendation (provisional): document, defer engine work.** Ship (a); hold (b)/(c)
  behind a real consumer need. Honest framing: this may be *all* that's warranted.

**Open questions:**

- **Q-0025-1 (CENTRAL) — is this worth engine work at all?** Or does documenting the `fps`-filter
  pattern fully discharge the review finding? This is the decision the user is being asked to make.
- **Q-0025-2 — trigger for (b)/(c).** What signal promotes the auto-inject field? (A consumer with VFR
  sources who cannot author the filter string themselves — otherwise (a) suffices.)
- **Q-0025-3 — inclusion.** Should this spec join the [0012](0012-feature-parity-roadmap.md) §7 dispatch
  table at all, or remain an un-dispatched PROPOSED note pending a consumer report of actual drift?

## 7. Requirements

- **R-PARITY-AVSYNC (proposed)** — A caller can obtain constant-frame-rate, drift-free output from a
  VFR or timestamp-noisy source. **Satisfied today** by the enabled `fps` + `aresample=async` filters
  composed in the `filter` string (option (a)); *if* a trigger promotes it, additionally by an optional
  `outputs[].fps`/`vsync_mode` field that desugars to an in-graph `fps`/`aresample` (option (b)/(c)),
  without ever violating the PTS no-override contract (D-0025-A). No engine work is committed by this
  spec.

## 8. Test surface

- **A VFR fixture → assert CFR output.** A synthetic clip with a deliberately variable/irregular frame
  cadence (or gappy PTS); run `[0:v]fps=30[v]` and assert the output is constant 30 fps (uniform PTS
  deltas / expected frame count) — proving the workaround holds. Gated on `AFMPEG_TEST_FFMPEG_WASI`
  over the real driver, like other engine tests.
- **An audio-drift fixture** — mismatched/irregular audio timing through `aresample=async=1`, asserting
  bounded A/V offset.
- Under (b)/(c) only: an equivalence test that `outputs[].fps=30` produces byte-comparable output to the
  hand-written `fps=30` graph (proving the field is pure sugar).

## 9. Dependencies & sequencing

- **Depends on [0017](0017-native-filter-batch.md)'s `fps` filter** — already enabled ([0012](0012-feature-parity-roadmap.md)
  §4D "have today"), which is *why* option (a) needs no new work. `aresample` is likewise already in the
  filter set.
- **Independent** of the remux/seek ([0013](0013-remux-and-stream-copy.md)/[0014](0014-seeking-and-time-ranges.md))
  work; touches neither. If (b)/(c) is ever adopted it's an additive vocabulary spec, sequenced after a
  trigger — not before.

## 10. Definition of done

The finding is validated (§2, confirmed + LOW), the central question (Q-0025-1) is put to the user, and
a recommendation is on record: **document the `fps`/`aresample=async` filter pattern** (a how-to + the
supported-filter reference note) as the CFR/anti-drift path, with the driver-level `outputs[].fps`
option (b)/(c) **specified but deferred** behind a consumer trigger. The PTS no-override contract
(D-0025-A) is preserved in every variant. A VFR fixture proving the workaround exists. Nothing in this
spec commits engine work; its output is a decision, not code.
