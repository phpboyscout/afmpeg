# 0022 — build & size matrix (lean/full)

Status: **DRAFT / SCOPING**
Date: 2026-06-30
Parent: [0012](0012-feature-parity-roadmap.md), [0007](0007-libav-direct-engine.md)
Owns: R-AF-3 (size-matrix portion) — the build-profile axis, the release-artifact matrix, and the
extension of afmpeg's `variant` to `(licence × profile)`.

## 1. Why

Today's `.wasm` is ~5.4 MB at the [0007 §6](0007-libav-direct-engine.md) baseline. As
[0015](0015-container-coverage.md)–[0019](0019-text-and-subtitles.md) + [0023](0023-hevc-and-av1.md)
land demuxers/muxers/codecs/filters/libs, "enable everything" bloats it without bound — every
consumer pays for libass + x265 even to thumbnail a PNG. FFmpeg's `--disable-everything` + allowlist
already gives byte-level control ([0012 §2](0012-feature-parity-roadmap.md) size-budget row), so the
question is **not "can we trim"** but **"what cuts do we ship, sign, and pin."** This spec is the
cross-cutting decision the parity batch defers to: each of 0015–0019/0023 asks "which bucket?" — the
answer lives here.

## 2. Scope

In: the build-**profile** axis (its existence, shape, and naming); the lean allowlist vs the full
superset; the release-artifact/provenance/signing matrix; how afmpeg's `WithModuleRelease` extends;
per-artifact size budgets + a CI size-assertion gate. Out: the *contents* of each parity batch (owned
by 0015–0019/0023 — they only consult §6's bucket rule); the licence axis itself (0007 §5 / 0010 —
this spec is orthogonal to it); heavy-codec placement mechanics (0023).

## 3. Approach / design

**Two-point capability axis — `lean` (default) + `full` (opt-in) — orthogonal to LGPL/GPL.** A
`PROFILE` build-arg sits beside the existing `VARIANT`; the allowlist in `build/libav.sh` splits into
`ENABLE_LEAN` (today's baseline + the web quartet) and `ENABLE_FULL_EXTRA` (the 0015–0019 batches),
with `full = lean ∪ extra`. The published matrix is the product **licence × profile**:

| | **lean** (web-delivery essentials) | **full** (kitchen sink) |
|---|---|---|
| **lgpl** (default) | `ffmpeg-wasi-lgpl-lean.wasm` ← the default-default | `ffmpeg-wasi-lgpl-full.wasm` |
| **gpl** | `ffmpeg-wasi-gpl-lean.wasm` | `ffmpeg-wasi-gpl-full.wasm` |

**lean** = H.264 (dec+enc) · AAC · MP3 · Opus · VP8/VP9 (→ the [0018](0018-lgpl-encoder-expansion.md)
quartet: opus/lame/libvpx/libwebp) · WebP · the mp4/mov + matroska/webm + mp3/wav/ogg
demux+mux set · the core filter set (`scale crop pad overlay concat xfade fps format` + the audio
core `amix volume afade aresample aformat`). This is "deliver media to a browser" and nothing else.

**full** = lean **+** the native decoder/muxer/filter batches (ac3, pcm family, image decoders,
prores; mpegts, hls/dash, flv, avi, gif; the eq/color/compose/thumbnail/deinterlace/loudnorm filter
batch) **+** the external-lib heavies (freetype `drawtext`, libass `subtitles`, and — gpl-full only —
x265). The native batch is nearly free (size only); freetype/libass/x265 are why it is a *separate*
artifact, not the default.

**Rejected: named profiles** (`web`/`broadcast`/`audio`). They are *soft* segmentation we cannot
predict before consumer signal, and each new name multiplies the matrix. A consumer needing a bespoke
cut already has the mechanism — the `--disable-everything` + `--build-arg` allowlist — to roll their
own; we do not chase that surface speculatively. Two honest points (essentials vs everything) is the
floor; **this spec caps the dimensionality at `licence × profile` — no third axis.**

## 4. Build & release impact

- **`build/libav.sh` / `build/Dockerfile`** — add `PROFILE` (`lean`|`full`) beside `VARIANT`; the
  `ENABLE` string becomes `ENABLE_LEAN` + a `[ "$PROFILE" = full ] && ENABLE="$ENABLE $ENABLE_FULL_EXTRA"`
  append. Heavy-lib `--build-arg`s in `build/deps.sh` gate on **both** axes (x265 ⇒ gpl-full only;
  freetype/libass ⇒ any `*-full`).
- **`.gitlab-ci.yml`** — the 2 build jobs (`build:lgpl|gpl`) become **4** (`build:{lgpl,gpl}-{lean,full}`).
  Roughly **2× the CI compile time/cost** — the explicit proliferation tax to flag.
- **`build/sign-release.sh`** — `provenance.json`'s `variants` map grows from 2 entries to **4**
  (keyed `lgpl-lean`…`gpl-full`), each recording licence, profile, h264 encoder, **and built size**
  (§6). `checksums.txt` covers **4 `.wasm` + 4 `.gz`** (8 modules) + provenance; one signature still
  certifies the lot (0010 unchanged in mechanism — only wider).
- **`release` job** — publishes 8 module assets (+ `.gz`) instead of 4; the asset-link list and the
  generic-package upload loop scale accordingly. Each artifact is independently pinnable.

## 5. Licensing interaction

The profile axis introduces **no new licence risk** — it is a clean product with the 0007 §5 / 0010
licence axis. GPL-only components (x264, x265, GPL filters) live **only** in `gpl-*` regardless of
profile; freetype (FTL/GPL-compatible) and libass (ISC) are LGPL-clean, so `lgpl-full` stays a true
LGPL artifact — the "strengthen the *default* variant" thesis of [0012 §3](0012-feature-parity-roadmap.md)
holds at both profiles. full pulls more external libs ⇒ more corresponding-source/relink surface, but
all of it is already met by the public MIT repo + pinned upstream (0007 §5). The openh264 AVC-patent
caveat is unchanged and present in every `lgpl-*`. WKD attestation ([0011](0011-wkd-attestation.md))
covers each of the 4 artifacts via the single signed `checksums.txt` — no per-artifact key work.

## 6. Decisions + open questions

- **D-0022-A — axis = `lean` (default) + `full` (opt-in), orthogonal to licence. Named profiles
  rejected.** Two points only; the dimensionality ceiling is `licence × profile`.
- **D-0022-B — lean = the web-delivery allowlist of §3** (today's baseline + the 0018 quartet); full =
  lean ∪ the 0015–0019 batches + (gpl-full) x265. The lean allowlist is the authoritative list, lives
  in `build/libav.sh`, and is the contract the size budget guards.
- **D-0022-C — ship all 4 artifacts (full symmetric matrix).** Symmetry avoids special-case
  resolver/provenance/CI logic and preserves today's `gpl` pin as `gpl-lean` (no capability
  regression). `gpl-lean` is the lowest-value cell (x264 quality at minimal size is a narrow want) and
  is the first candidate to *stop publishing* if asset upkeep bites — the build still supports it.
- **D-0022-D — afmpeg surface: `Variant` stays the licence enum; add `type Profile string`
  (`ProfileLean`/`ProfileFull`, default `ProfileLean`).** `WithModuleRelease(tag, variant, opts…)`
  gains `WithProfile(Profile)`; the resolver builds `ffmpeg-wasi-<licence>-<profile>.wasm` and the
  provenance cross-check (0010 D-0010-H) asserts **both** axes. The 2-arg call still compiles
  (defaults to lean), so existing consumers move artifact with one option, not a rewrite.
- **D-0022-E — enforcement is a per-artifact size budget + a CI gate**, not a guideline. Budgets
  (`--enable-small`, raw `.wasm`): **lgpl-lean ≤ 8 MB · gpl-lean ≤ 10 MB · lgpl-full ≤ 16 MB ·
  gpl-full ≤ 22 MB** (starting numbers; x265 dominates gpl-full). A `build/size-budgets.txt` ceiling
  is asserted in CI (`stat -c%s` vs ceiling) — a regression **fails the pipeline**; actual sizes land
  in `provenance.json` so drift is auditable per release.
- **D-0022-F — the bucket rule the siblings defer to:** a new component lands in **lean** iff it is a
  web-delivery essential (the §3 set); otherwise **full**. 0015/0016/0017/0018/0023 each cite this
  rule rather than re-deciding.
- **Open:** (Q1) publish `gpl-lean` day-one or lazily on first pin? (Q2) artifact **rename** migration
  — pre-profile tags (`n8.1.2-*`) used `ffmpeg-wasi-lgpl.wasm`; does the resolver alias `lgpl ⇒
  lgpl-lean` for old tags, or do profiles only apply from the introducing tag onward? (Q3) gzip-size
  budget too, or raw only? (Q4) is libwebp lean or full? (leaning lean — pairs with the quartet).

## 7. Requirements

- **R-0022-1** A `PROFILE` build axis (`lean`|`full`) orthogonal to `VARIANT`, driving the
  `build/libav.sh` allowlist split; `full ⊇ lean` by construction.
- **R-0022-2** The lean allowlist is defined, web-delivery-scoped, and is a strict subset of full.
- **R-0022-3** Each published artifact has a committed size budget enforced by a CI gate that fails on
  regression; built sizes recorded in `provenance.json`.
- **R-0022-4** `provenance.json` records `(licence, profile, size)` per artifact; `checksums.txt`
  covers all 4 `.wasm` (+ `.gz`); the single 0010 signature certifies the set.
- **R-0022-5** afmpeg's release acquisition resolves/verifies `(licence × profile)`; default profile =
  lean; provenance cross-check asserts both axes with a typed mismatch error.
- **R-0022-6** 0015–0019/0023 each tag their additions `lean` or `full` per D-0022-F.

## 8. Test surface

- **CI size gate** — the budget assertion is itself the headline test (regression = red pipeline).
- **Build-matrix smoke** — each of the 4 artifacts loads under wazero + answers an `op:"probe"`.
- **Capability negative test** — a lean module is asserted to **lack** a full-only codec/filter
  (e.g. `subtitles`/x265), so the cut is real, not nominal.
- **afmpeg resolver/provenance tests** — `(licence, profile)` URL resolution; provenance asserts both
  axes; `WithProfile` default = lean; variant/profile-mismatch ⇒ typed error (extends 0010 §10).
- **Doc deliverable** — the [0012 §6](0012-feature-parity-roadmap.md) codec/filter matrix page gains a
  per-artifact column, generated/checked against the 4 builds.

## 9. Dependencies & sequencing

- **Gates** 0015/0016/0017/0018/0019/0023 — each defers its lean/full placement to D-0022-F, so the
  **axis + naming decision (D-0022-A/D) must land before the batches start enabling components**; the
  full plumbing can follow as the first full-only component arrives.
- **Depends on** [0007](0007-libav-direct-engine.md) (the build), [0010](0010-signed-release-acquisition.md)
  (the `Variant` enum, provenance cross-check, signing — widened, not rebuilt),
  [0011](0011-wkd-attestation.md) (attestation spans all 4 via one signed manifest).
- **Sequencing:** decide the axis here (this spec) → implement `PROFILE` plumbing + the 4-way CI/
  provenance/afmpeg surface as 0015/0016 lands its first full-only component → siblings slot into
  buckets thereafter.

## 10. Definition of done (this scoping spec)

The `lean`/`full` axis, its orthogonality to licence, the 4-artifact published matrix, the lean
allowlist boundary, the afmpeg `(licence × profile)` surface, the per-artifact size budgets + CI gate,
and the bucket rule the siblings consult are all decided and recorded — with the proliferation cost
(2× CI, 8 module assets, a wider provenance/pin surface) named, not hidden. The open questions (Q1–Q4)
are flagged for the implementing MR. 0015–0019/0023 can cite D-0022-F instead of re-litigating "which
bucket?".
