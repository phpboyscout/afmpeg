# 0018 — LGPL encoder expansion

Status: **IMPLEMENTED** (Phase 3 of the [implementation roadmap](../implementation-roadmap.md).
Five external LGPL/BSD-clean encoder libs cross-compiled into the **intermediate** build profile
([0022](0022-build-size-matrix.md)) via `ffmpeg-wasi build/deps.sh` — **libopus** (Opus),
**libmp3lame** (MP3), **libvorbis**+**libogg** (Vorbis), **libwebp** (WebP) and **libvpx**
(VP8/VP9) — each with its `--enable-lib*`/`--enable-encoder` pair in `libav.sh`. No vocabulary
change: the encoders are new `video_codec`/`audio_codec` name strings. libvpx's encoder uses
setjmp/longjmp → wasm exception-handling (libsetjmp.a), so the afmpeg runtime enables the
exnref EH feature. Round-trips proven in afmpeg's `TestIntegration_LGPLEncoders`.)
Date: 2026-06-30
Parent: [0012](0012-feature-parity-roadmap.md) §4B/§5 (Tier 2 encoder quartet); [0007](0007-libav-direct-engine.md) §3/§5 (the engine + the licence variants)
Owns: **R-PARITY-LGPL-ENCODERS**

## 1. Why

The engine encodes a thin set today — `mjpeg, png, aac, flac, pcm_s16le` natively, plus
`libopenh264` (BSD) and `libx264` (GPL, full variant only). For *output* coverage that is narrow:
a consumer asking "can it write Opus? MP3? WebM? WebP?" hears "not yet." Yet the highest-value
encoders missing are all **LGPL/BSD-clean external libs** — so they land in the **LGPL default**
variant, not the GPL one. They strengthen the proprietary-compatible build, which is the one most
consumers will reach for. This spec is the build/deps mechanics for that quartet (plus Vorbis):
each a new `build_<lib>()` in `ffmpeg-wasi build/deps.sh` + an `--enable-lib*`/`--enable-encoder`
pair in `build/libav.sh`, cross-compiled to wasm32-wasi, static, asm-free.

A bonus theme: several of these formats **decode natively today but cannot be encoded** — Opus and
Vorbis are in the §4A decoder allowlist (`--enable-decoder=…,opus,vorbis,…`). Adding their encoders
makes those formats **round-trippable** (decode *and* encode) rather than read-only.

## 2. Scope

**In scope** — five external encoder libraries, all into the **default (LGPL)** variant:

| Encoder | Lib | Licence | Value | Round-trips a native decoder? |
|---|---|---|---|---|
| **Opus** | libopus | BSD | **very high** (low-latency, WebRTC) | yes — `opus` decode is native today |
| **MP3** | libmp3lame | LGPL | **very high** (ubiquitous) | yes — `mp3` decode is native today |
| **VP8 / VP9** | libvpx | BSD | high (WebM video) | yes — `vp8`/`vp9` decode is native today |
| **WebP** | libwebp | BSD | high (modern still/animated images) | partial — webp *decode* is 0016's native batch |
| **Vorbis** | libvorbis | BSD | med | yes — `vorbis` decode is native today |

**Out of scope** (boundaries with sibling specs):

- **Native (in-tree) encoders** — GIF, AC-3, PCM family, ALAC — are flag-only, no new lib → **0016**.
- **HEVC (libx265, GPL)** and **AV1** (aom/SVT/rav1e) → **0023**. These are GPL-or-heavy; do **not**
  confuse this spec's BSD/LGPL quartet with the GPL x264/x265 path.
- **Text/subtitle libs** (freetype/libass) → **0019**.
- **The lean/full size-bucket decision** (R-AF-3) → **0022**; cross-referenced in §5 because these
  libs are the dominant size drivers, but the bucketing rule is decided there, not here.

## 3. Approach / design — the per-lib cross-compile pattern

Each lib follows the established `build/deps.sh` shape (the `build_zlib`/`build_x264`/`build_openh264`
trio): **git-clone a pinned tag → configure for `wasm32-wasi` → make a static archive into
`$PREFIX`**, with toolchain (`CC/CXX/AR/CFLAGS`) from `toolchain.sh`. The libav build then names
each encoder explicitly under `--disable-everything` (per `libav.sh` — the lib flag alone is not
enough; the encoder must be enabled by name). The hard constraints (§0007/§0012 envelope) apply to
*these* libs too: **single-threaded** (no `wasi-threads`), **asm-free** (`--disable-asm` or the
equivalent), **static** (`--enable-static --disable-shared`), **no network**, **size-conscious**.

The two design risks per lib are (a) the **asm-disable** path (each lib's asm is x86; the portable C
fallback must exist and be selectable) and (b) the **cross-compile** invocation (each uses a
different build system). Per lib:

- **libopus** — Autotools. `./configure --host=… --enable-static --disable-shared --disable-asm
  --disable-rtcd --disable-intrinsics --disable-doc --disable-extra-programs`. Closest to a clean
  cross-compile; modest source. *Risk: low* — well-behaved autotools, mirror the x264 host-triple +
  `LD="$CC"` link-probe trick.
- **libmp3lame** — Autotools. `./configure --host=… --enable-static --disable-shared --disable-gtktest
  --disable-frontend`. LGPL (the one true-LGPL lib here; the rest are BSD). *Risk: low–med* — old
  autotools; may need the `wasi-compat.h` force-include + a config.h `HAVE_*` sed pass like x264.
- **libvpx** — its own `configure` (not autotools). Needs an explicit
  `--target=generic-gnu` to force the portable C path, `--disable-runtime-cpu-detect`,
  `--disable-unit-tests --disable-examples --disable-tools --disable-docs`, threads off. *Risk:
  med–high* — largest of the five (`.wasm` cost), VP9 encode is **notably slow single-threaded** (no
  threads, no SIMD — flag this perf cost loudly), and `generic-gnu` asm-free correctness needs
  validation. This is the lib most likely to argue for the **lean/full** split (→ 0022).
- **libwebp** — CMake (or autotools). Cross-file/toolchain pointing at wasi-sdk clang, asm/SSE off
  (`-DWEBP_ENABLE_SIMD=OFF`), threads off, build `libwebp` + `libwebpmux`/`libwebpdemux` as needed
  for animation. *Risk: med* — CMake toolchain-file plumbing for wasm32-wasi is the unknown; the
  encode itself is light.
- **libvorbis** — Autotools, depends on **libogg** (also BSD) — so this is **two** `build_<lib>()`
  (`build_libogg` before `build_libvorbis`), both static into `$PREFIX`. *Risk: low–med* — the extra
  libogg dependency is the only wrinkle.

All five are added to the **LGPL** arm of `deps.sh`'s `case "$VARIANT"` (or unconditionally if we
also want them in GPL — they are licence-compatible with both), and gated in the size bucket per 0022.

## 4. Job-spec & build impact

- **Build side (ffmpeg-wasi).** New `build_libopus`/`build_libmp3lame`/`build_libvpx`/`build_libwebp`/
  `build_libogg`+`build_libvorbis` functions in `deps.sh`, each invoked for the default variant.
  In `libav.sh`, extend the `--enable-encoder=…` list and add the paired lib flags:
  `--enable-libopus --enable-encoder=libopus`, `--enable-libmp3lame --enable-encoder=libmp3lame`,
  `--enable-libvpx --enable-encoder=libvpx_vp8,libvpx_vp9`, `--enable-libwebp
  --enable-encoder=libwebp`, `--enable-libvorbis --enable-encoder=libvorbis`. (Matching demuxers/
  muxers — ogg mux for opus/vorbis output — are **0015**'s container batch, not here.)
- **Job-spec side (afmpeg).** **No vocabulary version bump and no new field.** These are new *values*
  for the existing `outputs[].video_codec` / `outputs[].audio_codec` strings (0007 §4): `libopus`,
  `libmp3lame`, `libvpx` / `libvpx-vp9`, `libwebp`, `libvorbis`. The driver already passes the codec
  name through to `avcodec_find_encoder_by_name`; these resolve once the lib is linked. afmpeg's
  `Command` builder needs no change — it already carries arbitrary codec-name strings.
- **Provenance.** Each lib + its pinned version + licence lands in `provenance.json` (per 0007 §7),
  so a consumer can read exactly which encoders a given `.wasm` carries.

## 5. Licensing & size impact (central here)

**Licensing — the headline.** Every lib in this spec is **BSD or LGPL**, all compatible with the
LGPL floor (0007 §5). None requires `--enable-gpl` or `--enable-nonfree`. So the whole quartet (plus
Vorbis) **enriches the default LGPL variant** — exactly the §0012 §3 thesis that "most high-value
codec gaps are LGPL-clean." This is the proprietary-compatible-build win, distinct from the GPL
x264/x265 path (0023). MP3 is the only *copyleft* lib (LGPL); the rest are permissive BSD — all
clear of the GPL boundary, all satisfiable by the existing corresponding-source/relink posture (the
public MIT build repo + pinned upstream).

| Lib | Licence | Approx. `.wasm` cost | Notes |
|---|---|---|---|
| libopus | BSD | small–modest | encoder + decoder; cheap for the value |
| libmp3lame | LGPL | small | mature, compact |
| libvpx | BSD | **large** (the dominant driver) | VP8+VP9 encode; the main argument for lean/full |
| libwebp | BSD | small–modest | +libwebpmux for animation |
| libogg+libvorbis | BSD | small | two archives, both tiny |

**Size — the cross-ref.** libvpx is the heavyweight; the others are individually cheap. Whether all
five ship in one comprehensive `.wasm` or split across a **lean** (web essentials: opus/mp3/webp)
vs **full** (adds libvpx/vorbis) axis is **0022's decision** — this spec only supplies the per-lib
cost inputs. Note `--enable-small` (already in `libav.sh`) trims libav glue but not the external
archives themselves.

**Perf.** Single-thread + no-SIMD makes **VP9 encode slow** (and VP8 less so); this is the §0012 §2
throughput ceiling, owned by 0008. Document it on the codec so a consumer picks VP8/VP9 knowing the
cost; the lever is instance-level parallelism (`RuntimePool`), not in-encode threads.

## 6. Decisions & open questions

- **D-0018-A — all five land in the LGPL default variant.** They are BSD/LGPL, compatible with the
  LGPL floor; gating them GPL-only would waste the licensing win. (Whether GPL *also* carries them is
  moot — it inherits everything the LGPL build has.)
- **D-0018-B — no job-spec vocabulary bump.** New codec *names* reuse the existing
  `video_codec`/`audio_codec` strings; the contract version is untouched (0007 §4). Per 0012 §6,
  only 4F *operations* bump the version — adding encoder names does not.
- **D-0018-C — Vorbis is in-scope despite "med" value, because libogg/libvorbis are tiny and pair
  with the (native) Ogg muxer to round-trip a format we already decode.** Low marginal cost.
- **D-0018-D — pin each lib to a release tag (not a moving branch),** mirroring `ZLIB_VERSION`/
  `OPENH264_VERSION` env pins; libvpx (no clean tag cadence) may pin a commit like x264's
  `X264_BRANCH`. Recorded in provenance.
- **Open — VP9 vs the lean bucket.** If 0022 makes libvpx full-only, the default variant loses WebM
  *video* encode (keeping opus/webp/mp3). Is web-delivery viable without VP9 in the lean build, or
  does that force AV1/H.264-only WebM? Defer to 0022, but flag the dependency.
- **Open — libwebp build system.** CMake-toolchain vs autotools for the cleanest asm-free wasm32-wasi
  cross-compile — pick at spike time.
- **Open — animated WebP / Ogg-Opus muxing seams.** Animation needs `libwebpmux`; Opus/Vorbis output
  needs the Ogg *muxer* (0015). Confirm the container side lands in lockstep so the encoders are
  actually reachable end-to-end.

## 7. Requirements

- **R-PARITY-LGPL-ENCODERS** — The default (LGPL) ffmpeg-wasi variant gains working encoders for
  **Opus (libopus), MP3 (libmp3lame), VP8/VP9 (libvpx), WebP (libwebp), and Vorbis (libvorbis)**,
  each cross-compiled to `wasm32-wasi`, **static**, **asm-free**, **single-threaded**, built by a
  `build_<lib>()` in `deps.sh` and enabled by an explicit `--enable-lib*` + `--enable-encoder=…`
  pair in `libav.sh`. Each lib + version + licence is recorded in `provenance.json`. afmpeg drives
  every new encoder by codec name through the existing job-spec, with **no vocabulary-version bump**.

## 8. Test surface

- **Build/CI (ffmpeg-wasi).** Each `build_<lib>()` cross-compiles cleanly and produces a static
  archive; the linked engine exposes each encoder (an `avcodec_find_encoder_by_name` smoke check, or
  a tiny driver probe). Asm-free + thread-free is asserted by the build flags, not runtime.
- **Round-trip (afmpeg integration, gated on `AFMPEG_TEST_FFMPEG_WASI`).** For each format,
  **decode-then-encode** the format back to itself (opus→opus, mp3→mp3, vp8→webm, vp9→webm,
  vorbis→ogg, image→webp), proving the new encode side and the existing native decode side meet.
  Mirrors `TestIntegration_RunJob*`.
- **Fixtures.** The in-memory synthetic WAV/PNG fixtures cover audio (opus/mp3/vorbis from synthetic
  WAV) and WebP (from synthetic PNG); VP8/VP9 need a tiny synthetic raw-frame or short clip fixture —
  a small licence-clean media seed (0012 §6) may be required for the video pair.
- **Provenance assertion.** A test reads `provenance.json` and asserts each enabled lib is listed for
  the LGPL variant.
- **Perf note (not a gate).** A coarse VP9-encode timing on a short clip, recorded to document the
  single-thread cost — informational, not a pass/fail.

## 9. Dependencies & sequencing

- **Depends on:** 0007 (the engine, the variant split, the deps.sh pattern) — **landed**. The
  `build_openh264`/`build_x264` precedents are the template.
- **Pairs with:** **0015** (containers) for Ogg mux (opus/vorbis output) and WebP/WebM muxing — the
  encoders are only reachable end-to-end once their muxers exist; sequence or co-land. **0016** owns
  the native decode side for any format whose decoder isn't already in the allowlist (e.g. webp
  decode).
- **Feeds:** **0022** (lean/full size matrix) — this spec is the **primary size-driver input** (libvpx
  especially). 0022 decides the bucketing; until then, default to "all five in the comprehensive
  default build."
- **Independent of:** 0019 (text/subtitles), 0023 (HEVC/AV1) — no shared libs.
- **Suggested order:** libopus + libmp3lame first (cheapest, highest value, cleanest cross-compile),
  then libwebp, libvorbis(+libogg), then libvpx (largest risk + the lean/full trigger).

## 10. Definition of done (this scoping spec)

The five LGPL/BSD encoder libs are scoped as `deps.sh` `build_<lib>()` + `libav.sh`
`--enable-lib*`/`--enable-encoder` additions to the **default variant**; the per-lib cross-compile
approach, asm-disable risk, and `.wasm` size cost are recorded (§3/§5); the decisions (D-0018-A…D)
and open questions (lean bucketing, libwebp build system, muxer seams) are captured; the boundaries
with 0015/0016/0019/0022/0023 are drawn; and the requirement (R-PARITY-LGPL-ENCODERS) plus the
round-trip test surface are stated. Implementation is **explicitly out of scope** — this spec is the
agreed plan, promoted from 0012 §7's disposition row, ready to build when picked up.
