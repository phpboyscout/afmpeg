# 0008 — performance strategy (spike)

Status: **SPIKE COMPLETE** (the investigation ran: `cmd/afmpeg-bench` *measured* the Wasm-encode
question. Finding — the penalty is an *encoder* penalty (decode+scale ≈ 2.4× native; single-threaded
H.264 encode 13–63×). The WASM levers are preset + encoder choice + the RuntimePool for throughput;
the native backend (0028) is the escape for genuinely encode-bound consumers. **Go/no-go: no dedicated
optimisation spec warranted** (R-AF-12 closes). Results in `docs/development/spikes/0008-perf/`.)
Date: 2026-06-29 (spike complete 2026-07-04)
Parent: [0001-afmpeg.md](0001-afmpeg.md) §9 (the performance risk)
Supersedes: [0006-hardening-roadmap.md](0006-hardening-roadmap.md) §2B (R-AF-12) and §2E (RuntimePool)
Owns: **R-AF-12** (performance)

## 1. Why this is its own spec

0006 parked performance as "wait for wasm-threads, then build a threaded `ffmpeg.wasm`." The
2026 landscape says that wait is effectively open-ended, so the deferral needs reframing as a
real investigation rather than a pending trigger. Two findings drive it:

- **Intra-encode threading is not coming for the foreseeable future.** wazero does not enable
  Wasm threads; `wasi-threads` is now a *legacy* proposal; the successor
  (shared-everything-threads, component-model) is unimplemented with no ship date. Planning
  around "threads land soon" is planning around a mirage.
- **Wasm SIMD standardised (Wasm 3.0, 2025) but FFmpeg can't use it as we build.** Our build is
  `--disable-asm`, and FFmpeg's SIMD is hand-written assembly, not portable intrinsics. The
  `-msimd128` feature buys nothing without bespoke Wasm-SIMD codepaths that do not exist
  upstream.

So the two threading/SIMD levers 0006 banked on are **unavailable**. The honest question is no
longer "when do threads land" but **"is single-threaded Wasm encode actually a problem, for
whom, and what non-threaded levers move the needle?"** We have never measured it.

## 2. The unknown

Everything we believe about afmpeg performance is *assumed*, not measured:

- single-threaded encode is "materially slower than native" — by **how much**, on what work?
- is that gap a real blocker for any consumer, or fine for the in-memory edge case it was
  always scoped to (0001 §9)?
- if it matters, which lever helps most — and is it worth the maintenance cost?

A spike answers these before any optimisation work is scoped.

## 3. The spike

### 3.1 Workloads (representative, not exhaustive)

- **Transcode** — `mkv → H.264/aac mp4` at a couple of resolutions/durations.
- **Reel-shaped** — stills → `xfade`-concat + audio-mix → H.264/aac mp4 (the keyrx-style job
  from 0005), the closest thing to a real consumer workload.
- **Scale / thumbnail** — cheap, to separate decode/filter cost from encode cost.

### 3.2 Axes to measure

| Axis | Variants |
|---|---|
| **Encoder** | openh264 (lgpl) vs libx264 (gpl) — speed *and* output size/quality |
| **Encoder effort** | preset / speed knobs (openh264 complexity, x264 preset) |
| **Module build** | the shipped `-Oz` vs a `-O2`/`-O3` build (size-over-the-wire vs runtime speed) |
| **Parallelism** | one `Runtime` vs a fleet (N instances across cores) on a batch |
| **Baseline** | native `ffmpeg` on the same host, same job, for the honest multiple |

### 3.3 Exit criteria

A short written report with: a perf table per workload/axis, the native multiple, and a
**go/no-go** — either "single-threaded is acceptable for the scoped use; close R-AF-12" or
"here is the one lever worth promoting to its own implementation spec, and why."

## 4. Candidate levers (to evaluate, not pre-commit)

Ordered by how plausibly they help *without* threads:

1. **Instance-level parallelism (the RuntimePool, ex-0006 §2E).** With intra-encode threads
   off the table, fanning *batches* across N module instances on N cores is the only real
   parallelism. Helps throughput, not single-job latency. The likeliest worthwhile outcome.
2. **Build-flag tuning** — `-O2`/`-O3` vs `-Oz`. Trades module download size for runtime
   speed; the spike quantifies both sides so the trade is informed, not guessed.
3. **Encoder/preset selection** — openh264 vs libx264 and their speed knobs, surfaced as
   guidance rather than a default change.
4. **Accept as-is** — a legitimate outcome. The in-memory path was always the edge case; if
   the multiple is tolerable, the right move is to document it and stop.

## 5. Non-goals

- **Intra-encode Wasm threads** — unavailable (see §1); explicitly out until the standards and
  wazero both move, at which point this spec is revisited.
- **A native/CGO backend** — dropped (0006 §2C); not a performance lever we will pursue.
- **Hand-written Wasm-SIMD FFmpeg codepaths** — far beyond afmpeg's maintenance appetite.

## 6. Open questions

- **What is "acceptable"?** Without a target (e.g. "a 60 s reel in under N s"), the spike can
  measure but not *judge*. Needs a number, ideally from a real consumer.
- **Is there a consumer with a genuine perf need?** keyrx keeps native FFmpeg as its local fast
  path (0001 §9); the Wasm path is the portable/sandboxed edge case. If no consumer is
  throughput-bound, even a confirmed gap may not be worth closing.
- **Does RuntimePool earn its keep before a batch consumer exists?** It is the most reusable
  lever, but building it speculatively risks API surface nobody uses.

## 7. Requirements

- **R-AF-12** *(reframed)* — Characterise Wasm-encode performance against native across the §3
  workloads, and *if* the spike justifies it, address it via **instance-level parallelism
  and/or build tuning** — never intra-encode threads (unavailable) and never a native backend
  (dropped). Any lever promoted to implementation gets its own spec.

## 8. Definition of done

The spike report (§3.3) lands, with a go/no-go and — if "go" — a named lever and a follow-on
implementation spec. If "no-go", R-AF-12 closes with the measurements recorded as the rationale.
