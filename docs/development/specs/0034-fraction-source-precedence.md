# 0034 — Fraction source precedence (progress correctness)

Status: **APPROVED** — decisions D1–D4 resolved with the user 2026-07-31; safe to implement.
Date: 2026-07-31
Parent: [0031](0031-job-progress-reporting.md) (owns the `Progress` stream and the D3 monotonicity
contract this amends); [0032](0032-engine-progress-side-channel.md) (the engine-derived source whose
precedence this fixes)
Owns: **R-PROGRESS-FRAC** — which source `Progress.Fraction` derives from, and when it must decline
to answer.

## 1. Why

Reported from keryx adopting `WithProgress`
([afmpeg#2](https://gitlab.com/phpboyscout/afmpeg/-/work_items/2)): for a render whose inputs are
tiny relative to its output — a handful of small PNG cards plus a WAV bed, crossfaded into a 30–45s
H.264 reel — `Fraction` is **1.000 on the first sample and never moves**, for the whole render.

That is worse than the `-1` the contract reserves for "cannot be determined": a caller cannot
distinguish it from genuine completion, so it renders a bar that reads *done* for the entire job,
and a naive "did we receive samples?" test passes. keryx now ignores `Fraction` entirely and
re-derives its own from `OutTime`.

Two independent defects produce it.

**(a) The byte-observed ratio saturates.** `snapshot()` only counts an input's size into the
denominator once that input has been read from:

```go
if c.read > 0 && c.total > 0 { read += c.read; total += c.total }
```

so the denominator is discovered lazily as inputs open and the ratio is `bytes read ÷ bytes known so
far` ≈ 1 throughout. keryx measured exactly this: `in=367/367 → 734/734 → 32778/32778`.
[0031](0031-job-progress-reporting.md) §4.3 anticipated it ("the denominator is discovered lazily as
inputs open … safe, but lumpy").

**(b) The engine-derived fraction cannot correct it.** 0031 §4.3 asserts "phase B's
`out_time/duration` removes it." **It does not.** `fractionLocked` feeds both sources through the
*same* monotone clamp:

```go
if p.haveEng && p.eng.durationUs > 0 { frac = p.clampMaxFrac(outTimeUs / durationUs) }
if total > 0                         { frac = p.clampMaxFrac(read / total) }
```

`clampMaxFrac` is a shared `max`, so once the byte ratio reaches 1.0 the ceiling is pinned there
permanently and the engine's honest value can never pull it down. Confirmed by reproduction: with an
engine reporting `duration_us` and a 30s output, `Fraction` reads **1.0000 at `out_time` = 1s, 5s and
15s** (expected 0.033 / 0.167 / 0.500).

Consequence worth stating plainly: **upgrading the engine does not fix this.** keryx tested
`n8.1.2-9`, which omits `duration_us` for their shape, and reasonably concluded the engine was the
gap. On `n8.1.2-10` the engine supplies the right number and the host discards it.

The suite misses it because the phase-B test uses a purely generative input where `total == 0`, so
the byte branch never fires and the two sources never collide.

## 2. Scope

**In:**
- Source precedence and per-source monotonicity in `progressReporter` (`pkg/afmpeg/progress.go`).
- A fixed byte denominator taken from the job spec's declared inputs, replacing lazy discovery.
- A saturation guard for the byte-only path.
- One additive public field, `Progress.Source`.

**Out (referenced, not owned):**
- The engine record schema, the `/dev/afmpeg-progress` device, and `WithProgress` delivery /
  back-pressure semantics — unchanged from [0032](0032-engine-progress-side-channel.md) / 0031.
- The native backend, which is phase-A only until [0033](0033-native-progress-side-channel.md) is
  revived. It inherits D2/D3 (the byte-path fixes) and reports `SourceBytes` or `SourceUnknown`.
- `Frame` / `OutTime` / `Speed` — already engine-sourced and correct.

## 3. Decisions

- **D1 — engine-derived wins outright; hold at `-1` until it speaks.** When afmpeg requested engine
  progress (a `process` job, `progress:true`, per 0032 D-B3), `Fraction` reports **`-1` until the
  first engine record carrying `duration_us` arrives**, and is engine-derived from then on. The
  byte-observed value is not reported for such a job while an engine duration is still possible.
  - *Rationale:* it never regresses and never asserts a number it cannot stand behind. The cost is a
    brief `-1` window at startup, which is exactly what `-1` is for.
  - *Fallback:* if engine records arrive but none carries a `duration_us` (a pre-`n8.1.2-10` engine,
    or a shape it cannot measure), afmpeg reverts to the byte-observed source under D2/D3 rather
    than reporting `-1` forever. The switch is one-way and upward-clamped, so it cannot regress.
  - *Rejected — allow a one-time downward correction:* emitting the byte value early and letting the
    engine supersede it even when lower would amend 0031 D3 to permit a source-switch regression.
    Cheaper, but it ships a visibly jerking bar and trades a documented guarantee for a startup
    convenience.
  - *Rejected — floor the engine value at the last byte value:* preserves D3 strictly, but keeps
    `Fraction` pinned near 1.0 for exactly the workload this spec exists to fix.

- **D2 — the byte denominator is fixed upfront, not discovered lazily.** When progress is active,
  afmpeg stats **every input declared in the job spec** at invocation start and uses their summed
  size as a constant denominator, instead of accumulating it as inputs open. An input that cannot be
  statted (missing, generative/lavfi, a `concat` list) contributes 0 and is excluded from both
  numerator and denominator.
  - *Rationale:* removes defect (a) at its source. keryx's first sample becomes `367/32778 = 0.011`
    rather than `1.000`, and concat/multi-input jobs stop plateauing.

- **D3 — a saturated byte fraction reports `-1`.** On the byte-observed path, when the ratio reaches
  1.0 (all declared input bytes consumed) but the job is still running, `Fraction` reports **`-1`**,
  not `1.0`. The genuine end-of-job value is emitted by the final snapshot at `stop()`.
  - *Rationale:* keryx's suggestion #2. For encode-bound work the inputs are exhausted long before
    the job is; "cannot say" is honest where "100%" is a lie. D2 fixes the *rate*, but the byte
    signal still saturates early whenever output dominates input — this covers that residue.

- **D4 — `Fraction`'s provenance is public.** Add `Progress.Source` (`SourceUnknown` /
  `SourceBytes` / `SourceEngine`), additive and non-breaking.
  - *Rationale:* keryx's suggestion #3. A caller can decide whether to trust the number, and the
    field makes the two-source split self-documenting rather than folklore. Chosen over a doc-only
    note because inferring provenance from `OutTime != 0` is indirect and would silently mislead
    once native phase B lands.

- **D6 — an overrun engine duration reports `-1`.** When `out_time` passes the engine's reported
  `duration` by more than a small tolerance (2%, absorbing the last frame's timestamp rounding past
  the end), the duration is treated as **wrong** rather than reached, and `Fraction` reports `-1`
  until the job ends. *Found empirically during implementation (2026-07-31)*, running the keryx
  shape against a real `n8.1.2-10`: a looped image input produced an output far longer than the
  duration derived from the audio bed, and `Fraction` sat at a confident `1.000` while the job ran
  on — the exact failure this spec exists to remove, arriving via the engine source instead of the
  byte one.
  - *Rationale:* D3's principle applied to the other source. A correct duration is met, not
    exceeded; once exceeded, the engine's fraction has stopped measuring the job.
  - *Not* a guard on reaching exactly 1.0: a job whose `out_time` meets its duration genuinely is
    at its end, and flipping to `-1` there would make every normal job's bar flicker at 100%.

- **D5 — 0031 D3 (monotone, never-decreasing) is preserved, per-source.** Each source carries its
  own clamp; they are no longer max-ed together. D1's `-1` window and D3's saturation guard mean a
  transition never needs to move the number downward, so the public guarantee is unchanged. A
  transition from `-1` to a real value is not a decrease.

## 4. Consequences

- keryx can drop its `OutTime`-derived workaround and consume `Fraction` directly — on an engine
  that reports `duration_us`. On an older engine it gets `-1` for the encode-bound tail, which is
  correctly "unknown" rather than falsely "done".
- Callers that today treat `Fraction == 1.0` as a completion signal must use the invocation's return
  instead. This is a behaviour change, but the previous value was wrong for this shape; documented
  in the changelog and in the `Progress` doc comment.
- A short `-1` window now appears at the start of every engine-progress job. Consumers already have
  to handle `-1` (0031 D5), so this is within the existing contract.
- 0031 §4.3's claim that phase B removes the lazy-denominator plateau gets a dated correction — it
  was true in design and false in implementation.

## 5. Acceptance

- The keryx shape — small inputs fully consumed, engine reporting `duration_us`, 30s output —
  yields `Fraction` tracking `out_time/duration` (≈0.033 / 0.167 / 0.500), never a premature 1.0.
  This is the regression test the suite lacked.
- A job with **both** sources live asserts engine precedence (the gap the existing phase-B test
  misses by using a generative input with `total == 0`).
- Byte-only path: multi-input job reports a fraction against the **full** declared denominator from
  the first sample (D2), and reports `-1` once inputs are exhausted mid-job (D3).
- `Fraction` is non-decreasing across every sample of an invocation, ignoring `-1` (D5).
- `Source` matches the value's actual derivation in each case (D4).
- Existing 0031/0032 tests pass unchanged except where they assert the superseded saturation.

## 6. Open questions

None. D1–D5 resolved with the user 2026-07-31.
