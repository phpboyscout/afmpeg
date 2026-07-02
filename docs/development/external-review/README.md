# External review — validation & disposition

A commissioned external review of afmpeg + ffmpeg-wasi (AI-assisted, dated 2026-07-01). The raw
reports are preserved here verbatim; this index records **what we validated against the code** and
**where each finding went**. Every finding was checked against `ffmpeg-wasi/src/process.c` +
`afmpeg/pkg/afmpeg/runtime.go`.

## Reports

- `ARCHITECTURE_REVIEW.md` — the consolidated report (perf + security + parity + the dual-backend
  proposal).
- `feature_parity_review.md` / `_supplementary.md` — feature gaps vs native FFmpeg.
- `performance_review.md` / `_supplementary.md` — perf characteristics + code-level bottlenecks.
- `security_review.md` / `_supplementary.md` — sandbox posture + hardening gaps.

## Disposition

**Already covered by our own roadmap (validated as real, no new spec — the review corroborates
specs we'd already drafted):**

| Finding | Verdict | Spec |
|---|---|---|
| Stream copy (`-c copy`) missing — always re-encodes | ✅ confirmed | [0013](../specs/0013-remux-and-stream-copy.md) |
| Subtitles / data / attachments ignored (video+audio only) | ✅ confirmed | [0019](../specs/0019-text-and-subtitles.md) |
| Seeking / trimming (`-ss/-t/-to`) missing | ✅ confirmed | [0014](../specs/0014-seeking-and-time-ranges.md) |
| Metadata / chapters stripped | ✅ confirmed | [0020](../specs/0020-metadata-and-chapters.md) |
| Muxer options (`-movflags`) / forced output format (`-f`) | ✅ confirmed | [0015](../specs/0015-container-coverage.md) |
| Input `-map` stream selection (partial) | ✅ confirmed | [0013](../specs/0013-remux-and-stream-copy.md) + [0024](../specs/0024-input-options-and-formats.md) |
| Codec availability (small curated set) | ✅ confirmed | [0016](../specs/0016-native-codec-batch.md) / [0018](../specs/0018-lgpl-encoder-expansion.md) |
| No hardware acceleration (WASM) | ✅ confirmed — **non-goal** | [0012 §2](../specs/0012-feature-parity-roadmap.md); reconsidered by [0028](../specs/0028-native-subprocess-backend.md) |
| Single-thread / no SIMD | ✅ confirmed — **non-goal** | [0012 §2](../specs/0012-feature-parity-roadmap.md) / [0008](../specs/0008-performance-strategy.md) |

**New valid findings — a PROPOSED spec each (for the include/decline discussion):**

| Finding | Code evidence | Spec |
|---|---|---|
| `inputs[].options` inert + no forced/raw input format | `avformat_open_input(…, NULL, NULL)`; afmpeg `Input` has only `Path` | [0024](../specs/0024-input-options-and-formats.md) |
| No A/V-sync / frame-rate mode (VFR drift) — **low severity** (the `fps` filter mitigates) | `pull_sinks` forwards buffersink PTS as-is | [0025](../specs/0025-av-sync-and-framerate.md) |
| Hot-path inefficiency: per-packet `AVFrame` alloc, round-robin demux, 32 KB AVIO buffer | `pull_sinks` alloc/free per packet; round-robin read loop; default AVIO | [0026](../specs/0026-engine-hot-path-performance.md) |
| **Runtime hardening**: no wazero memory limit (OOM), no hard timeout, cJSON guards | `runtime.go` `New()` lacks `WithMemoryLimitPages`; `Run` holds `mu` sans deadline | [0027](../specs/0027-runtime-security-hardening.md) |
| **Dual-backend**: opt-in native FFmpeg subprocess via an HTTP bridge (HW-accel) | architectural proposal; feasibility sound, sidesteps the CGO objection | [0028](../specs/0028-native-subprocess-backend.md) |

**Notable corrections we made to the review:**
- The dual-backend proposal (0028) revives **R-AF-11**, which we *dropped* in
  [0006](../specs/0006-hardening-roadmap.md) §2C — but the review's subprocess+HTTP approach
  avoids the CGO objection that killed it, so it is genuinely re-openable. It is in real tension
  with afmpeg's sandbox thesis; 0028 lays that out.
- A/V-sync (0025) is **lower severity** than a naive read suggests — the enabled `fps` filter
  already lets a caller force CFR in-graph today.
- The memory-limit gap (0027) is the **highest-priority** new item: it directly undercuts the
  "safely process untrusted media" value proposition.

The five PROPOSED specs are design-only and await an include/decline decision per item.
