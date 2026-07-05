# 0023 — HEVC & AV1 (heavy codecs)

Status: **NATIVE ENCODE IMPLEMENTED (2026-07-05)** — the WASM deferrals below stand (no-threads/
no-asm/size make HEVC & AV1 encode impractical in `.wasm`), but the [0028](0028-native-subprocess-backend.md)
native driver **inverts every blocker**: threads + SIMD are exactly what x265 and SVT-AV1 want. So
HEVC encode (`libx265`, GPL/full-gpl only) and AV1 encode (`libsvtav1`, both variants) now ship in the
**native `full` profile** (0022) — built by `ffmpeg-wasi build/deps.sh` (cmake) + `build/libav.sh`,
published + signed as `ffmpeg-wasi-driver-linux-amd64-full-<variant>`, selectable via
`native.NewFromRelease(…, WithReleaseProfile(ProfileFull))`. Verified end-to-end (HEVC→mp4, AV1→mkv
over the afero/IPC bridge). D-0023-B/D **flip to done for native** (the wasm verdicts are unchanged;
AV1 decode via dav1d + HW-accel remain follow-ups). Originally one of the 0013–0023 batch mapping the
[0012](0012-feature-parity-roadmap.md) roadmap; the §7 "HEVC / AV1" Tier-3 row.
Date: 2026-06-30 (native encode implemented 2026-07-05)
Parent: [0012](0012-feature-parity-roadmap.md) §7 (the dispatch row), §4A/§4B (the AV1/HEVC
entries), §2 (the envelope), §3 (the licensing lens), §5 (Tier 3); [0007](0007-libav-direct-engine.md)
§5 (D-FW-C, the LGPL/GPL split)
Owns: **R-PARITY-HEAVY-CODECS**

## 1. Why

HEVC and AV1 are the next-generation video codecs a "does it do X?" consumer eventually asks for —
better compression than H.264/VP9, and increasingly the delivery target for 4K/HDR. But they are
the **worst fit for our envelope** (0012 §2): the encoders are computationally enormous, lean
hardest on threads and asm — both of which we forbid — and they drag the largest external libs into
the `.wasm`. HEVC additionally carries an **active patent pool** (unlike the sunsetting AVC one,
licensing.md). So this spec is deliberately a *deferral with conditions*, not a build plan: it
records the per-codec recommendation (do / defer) and the gates (a real consumer need · acceptable
size · a measured [0008](0008-performance-strategy.md) perf number) each must clear to graduate.

## 2. Scope

Four distinct codec directions, each judged separately — **decode and encode do not share a verdict**:

- **HEVC/H.265 decode** — already shipped (the `hevc` native decoder is in the `build/libav.sh`
  allowlist today). In scope only to *record* it as done and note no lib is needed.
- **HEVC/H.265 encode** — `libx265` (GPL) → full variant only; large; slow single-threaded; HEVC
  patent-pool exposure.
- **AV1 decode** — `libdav1d` (BSD) → LGPL default variant; the practical decoder (the native
  FFmpeg AV1 decoder is too slow). Royalty-free.
- **AV1 encode** — `libaom` / `SVT-AV1` / `rav1e` → heavy, slow, and of uncertain cross-compile
  feasibility; likely **impractical** in this envelope pending measurement.

**Out of scope** (other specs own these): LGPL encoders Opus/MP3/VP8-9/WebP → [0018](0018-lgpl-encoder-expansion.md);
the native codec batch → [0016](0016-native-codec-batch.md); the lean/full size bucketing →
0022; perf measurement methodology → [0008](0008-performance-strategy.md). No new job-spec
vocabulary — these are `video_codec` string values (`libx265`, `libaom-av1`, `libsvtav1`) plus the
existing `av1`/`hevc` decoder names; the [0007](0007-libav-direct-engine.md) §4 contract is unchanged.

## 3. Approach / design

The build mechanics are the **established cross-compile pattern** from `ffmpeg-wasi build/deps.sh`
(zlib/x264/openh264) + `build/libav.sh`: a `build_<lib>` function emits a static `wasm32-wasi`
archive into `$PREFIX`; `libav.sh` adds `--enable-lib<x>` + the explicit `--enable-encoder=`/
`--enable-decoder=` (under `--disable-everything`). The GPL gate already exists — `libx265` joins
`libx264` behind `[ "$VARIANT" = gpl ] && GPL_FLAGS="--enable-gpl …"`. Each lib must build with
`--disable-asm`-equivalents and no threads, mirroring how x264 took `--host=x86-gnu --disable-asm`
(the portable C path) and openh264 took `ARCH=generic USE_ASM=No` + a single-thread shim.

- **libx265** (HEVC encode) — cmake-based; needs a wasi toolchain file, asm off (`ENABLE_ASSEMBLY=OFF`),
  threads off, CLI off, 8-bit. The cmake build is more involved than x264's autoconf but tractable.
- **libdav1d** (AV1 decode) — **meson/ninja** build; needs a meson cross file for wasm32-wasi, asm
  off (`enable_asm=false`). dav1d is designed with a clean C fallback, so the no-asm path is
  first-class — the lowest-risk new lib here.
- **AV1 encode** — *feasibility is itself the open question* (§6): `libaom` (cmake, very large,
  very slow), `SVT-AV1` (cmake, architected around a thread pool — single-thread strips its whole
  point), `rav1e` (**Rust** — conflicts with the C-only wasi-sdk/clang toolchain, D-FW-E; out).

No design beyond "wire the lib the same way the existing ones are wired." The hard part is not
plumbing — it is whether the result is *usable* (§5).

## 4. Build & job-spec impact

- **`build/deps.sh`:** new `build_x265` (gpl branch only, beside `build_x264`); new `build_dav1d`
  (both variants, beside zlib/openh264 — it is LGPL-clean); AV1-encode functions **not** added until
  a verdict flips (§6). Each carries the patent/licence caveat comment x264/openh264 carry.
- **`build/libav.sh`:** `--enable-libx265 --enable-encoder=libx265` into `GPL_FLAGS`;
  `--enable-libdav1d --enable-decoder=libdav1d` into the default `ENABLE` set; AV1 encoder enables
  deferred. `hevc` decode is already enabled — no change.
- **job-spec / afmpeg:** none. `video_codec: "libx265"` (full variant), `"libdav1d"`/decode-side
  `av1`, and (if ever) `"libaom-av1"`/`"libsvtav1"` are plain codec-name strings the driver passes
  to libav. Capability is variant- and build-gated, surfaced through the codec/filter matrix doc
  (0012 §6), not new vocabulary.

## 5. Licensing, PATENTS & size impact (central)

This is the section that justifies Tier 3. Three independent axes:

- **Copyright licence.** `libx265` is **GPL-2.0-or-later** → it can only enter the **full/GPL
  variant** (0007 §5, the `--enable-gpl` gate), never the LGPL default. `libdav1d` is **BSD** and
  `libaom`/`SVT-AV1` are **BSD/permissive** → AV1 is LGPL-clean and rides the **default variant**,
  exactly like the dav1d entry in 0012 §3.
- **Patents.** **HEVC has an *active, fragmented* patent landscape** — multiple pools (Via LA,
  Access Advance, plus unaffiliated holders) with no sunset in sight, unlike the AVC pool that
  expires 2027-11-29 (licensing.md). We **mirror the openh264/AVC posture** for `libx265`:
  self-compiled from source → **outside any binary patent grant**; ship on a deliberate, bounded
  basis — low expected distribution volume, **comply-on-demand** (pull promptly from the full
  variant if any HEVC rights-holder asks), and state plainly that GPL is a *copyright* licence that
  conveys **no** patent rights. The caveat is **harder than AVC's** (active pools, no expiry) — so
  HEVC encode is the most licence-exposed thing in the catalogue and should ship only on real
  demand. **AV1 is royalty-free by design** (AOMedia's patent commitment) → no patent caveat; this
  is the decisive advantage that makes AV1 *decode* the recommended graduation.
- **Size.** These are the **largest** libs in the survey. `libx265` and `libaom` each add roughly
  a megaclass of static code; `libdav1d` is more modest but still sizeable. Against `--enable-small`
  this directly threatens the lean build (0022) — so any graduation lands in **full** (or a
  dedicated heavy bucket), never lean. Exact numbers are a 0022/0008 measurement input.

## 6. Decisions & open questions

- **D-0023-A — HEVC decode: KEEP (done).** Native, already enabled, no lib, both variants. Recorded
  as shipped; nothing to build.
- **D-0023-B — HEVC encode (`libx265`): DEFER to full variant, gate on demand.** GPL + active
  patent pools + size + single-thread slowness make it the highest-cost encoder. Promote only when
  a real consumer needs HEVC *output* and accepts the full-variant + patent posture. Build path
  (cmake, asm/threads off) is feasible — the blocker is *should we*, not *can we*.
- **D-0023-C — AV1 decode (`libdav1d`): RECOMMEND for the default variant.** The strongest case in
  this spec: BSD (LGPL-clean), royalty-free, the *correct* decoder, lowest-risk no-asm build (clean
  C fallback). The one Tier-3 item that plausibly graduates first — gated only on size budget (0022)
  and a decode-throughput sanity check (0008).
- **D-0023-D — AV1 encode: DEFER as experimental, pending a 0008 measurement.** Likely
  **impractical** single-threaded: `SVT-AV1` is thread-architected (loses its purpose at 1 thread),
  `libaom` is famously slow even threaded, `rav1e` is Rust (toolchain conflict, D-FW-E). Verdict
  flips only if a 0008 spike shows a *tolerable* single-thread `libaom`/`SVT-AV1` encode at low
  preset on representative clips.
- **Open — cross-compile feasibility per encoder.** dav1d (meson) and x265 (cmake) are expected to
  build; **AV1-encode buildability is unverified** — cmake/meson cross files for wasm32-wasi, asm
  fully disablable, no thread-spawn assumptions. Each is a spike before any promotion.
- **Open — does "AV1 decode, no AV1 encode" satisfy consumers?** Decode-only is the common need
  (play/transcode-*from* AV1); confirm before investing in encode.

## 7. Requirements

- **R-PARITY-HEAVY-CODECS** (this spec owns it): the heavy-codec gap is *documented and gated* —
  each of the four directions has an explicit do/defer verdict, a licence/patent posture, and the
  conditions (consumer need · size budget 0022 · measured perf 0008) for graduation. No code ships
  from this spec; it is the decision record the build specs cite when a verdict flips.

## 8. Test surface

When (if) a codec graduates, it needs the standard 0012 §6 treatment: a licence-clean fixture
(a short AV1/HEVC clip — the synthetic in-memory WAV/PNG fixtures cannot cover these), a round-trip
integration test over the real driver (decode AV1 → frames; full-variant HEVC encode → mux → probe),
and a **perf assertion is the gating test, not a pass/fail correctness one** — the 0008 harness
records single-thread frames/sec so the defer/promote line is data-backed. Until graduation: no
tests; the codec names are simply absent from the build allowlist.

## 9. Dependencies & sequencing

- **Depends on 0022** (lean/full matrix) — these libs *must* land in full/heavy, never lean; the
  bucketing decision precedes any enable.
- **Depends on 0008** (perf) — the encode verdicts (D-0023-B, D-0023-D) are unresolvable without a
  single-thread measurement; 0008 is the gate.
- **Independent of** 0016/0017/0018 — no shared libs; this is purely the heavy/gated tail.
- **Sequencing:** last of the parity batch. Likely order *if* pursued: AV1 decode (dav1d, D-0023-C)
  first and standalone → HEVC encode (D-0023-B) on demand → AV1 encode (D-0023-D) only after a
  positive 0008 spike, if ever.

## 10. Definition of done (this scoping spec)

The four heavy-codec directions are catalogued with per-direction verdicts (D-0023-A keep,
D-0023-B/D defer, D-0023-C recommend), the GPL+HEVC-patent posture is stated by mirroring the
openh264/AVC model, the size threat to the lean build is flagged, and the graduation gates (0022
size · 0008 perf · consumer demand) plus the open cross-compile-feasibility questions are recorded.
Implementation is explicitly **out of scope** — this spec exists so the build specs have a
defensible answer to "why isn't HEVC/AV1 in the box yet?"
