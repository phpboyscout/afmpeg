# 0002 — the FFmpeg→WASI build pipeline

Status: **DRAFT** (component spec; not started. Implements spec 0001 Phase 1 / §6 and the
D-B / D-C resolutions. Review before building the pipeline.)
Date: 2026-06-26
Parent: [0001-afmpeg.md](0001-afmpeg.md) §6, §9, §10 (D-B, D-C)
Owns: **R-AF-3** (the codec/filter set), **R-AF-6** (reproducible build), **R-AF-10** (LGPL
variant)

## 1. Purpose

Produce a **reproducible `ffmpeg.wasm`** — FFmpeg + x264 compiled to `wasm32-wasi` — that
contains exactly the codecs/filters afmpeg's consumers need, and nothing else. This is the
hard, *separable* sub-project (0001 §6): it has no Go dependency and can be built and
validated entirely on its own. The Go layers (0003/0004) develop against its output (or a
stand-in) in parallel.

Per **D-B** the pipeline is **adapted from go-ffmpreg's `build.sh`** (Codeberg —
`https://codeberg.org/gruf/go-ffmpreg`), the proven wasi-sdk start point, **not** built from
scratch. We extend its `./configure` to add the filters/encoders go-ffmpreg's stock build
lacks (xfade, AAC), which is the precise gap the keyrx spike identified.

## 2. Scope

In scope:
- A pinned, containerised toolchain (wasi-sdk/clang) and a `build/` tree that produces
  `ffmpeg.wasm` deterministically from source.
- The `./configure` line enabling **only** the R-AF-3 set.
- Cross-compiling x264 (GPL build) to wasm as an FFmpeg dependency.
- Two build variants: **full/GPL** (default, with x264) and a tracked **LGPL** variant
  (openh264 or no H.264 encoder) — D-C / R-AF-10.
- A provenance manifest (versions, configure line, sha256, size) emitted alongside the wasm.
- A gated CI job (slow; not on every push).

Out of scope:
- The Go bridge, runtime, API (0003/0004) — they only *consume* this artifact.
- wasm-threads / SIMD builds (0006 / R-AF-12) — pin to a no-pthreads FFmpeg for now.
- Hardware accel (0001 non-goal).

## 3. The capability set — a general baseline (R-AF-3)

afmpeg is a **general-purpose** toolkit (spec 0001 D-F), so the build carries a curated
**general baseline** of codecs/filters/muxers covering common workflows — *not* one
customer's graph. The baseline is deliberately bounded (size + build time matter; this is
not "all of ffmpeg") and re-enabled explicitly from `--disable-everything`. A **lean**
variant (a smaller subset) and the **full** variant are selectable (R-AF-9); the table
below is the full baseline.

| Category | Baseline items | Licence |
|---|---|---|
| Demux | mp4/mov, matroska/webm, mp3, wav, image2, gif | LGPL |
| Video decode | h264, vp8/vp9, mjpeg, png, gif | LGPL |
| Audio decode | aac, mp3, opus, vorbis, pcm, flac | LGPL |
| Video filters | `scale`, `crop`, `pad`, `fps`, `format`, `setsar`, `transpose`, `overlay`, `concat`, `xfade` | LGPL |
| Audio filters | `amix`, `adelay`, `volume`, `afade`, `aresample`, `aformat`, `alimiter` | LGPL |
| Video encode | **`libx264`** (H.264, full/GPL variant); `mjpeg`, `png` (thumbnails) | x264 **GPL**; rest LGPL |
| Audio encode | **`aac`** (native), `libopus`/`opus`, `pcm`, `flac` | LGPL |
| Mux | mp4/mov, matroska/webm, mp3, wav, image2 | LGPL |
| Probe | `format=duration` (and stream info) across the demux set (R-AF-5) | LGPL |

The list is a starting point to refine during the build (entries may move between the
lean/full variants, or drop if they bloat the module disproportionately) — the
**principle** is a general baseline validated by several unrelated workflows, not a single
consumer's command. Record the final enabled set in the provenance manifest (§4).

**Validation — the proof-of-capability bar.** The artifact must run a spread of unrelated
invocations to a valid output inside the guest: e.g. a transcode (mkv→mp4 h264/aac), a
scale, an overlay (`-filter_complex`), a concat, a single-frame thumbnail, an audio
extract, **and** — as one example among them — keryx's crossfade reel (looped stills →
`xfade` chain + `amix`/`alimiter` → libx264/AAC mp4 `+faststart`). These become the
0004/0005 end-to-end tests over the vfs bridge. keryx's reel is a *subset* of the
baseline, not its definition.

## 4. Build approach (adapt go-ffmpreg)

1. **Toolchain, pinned.** wasi-sdk (clang → `wasm32-wasi`) at a fixed version, in a Docker
   image pinned by digest. The image is the reproducibility boundary (R-AF-6).
2. **x264 → wasm first.** Cross-compile x264 for `wasm32` as a static lib FFmpeg links.
   (Full/GPL variant only; the LGPL variant skips it — §6.)
3. **FFmpeg `./configure`**, adapted from go-ffmpreg's `build.sh`:
   `--target-os=none --arch=wasm32 --enable-cross-compile --disable-everything`, then
   `--enable-*` only the §3 set, `--enable-gpl --enable-libx264` (full variant).
4. **Pin FFmpeg to a no-pthreads-requiring release** (go-ffmpreg pins n5.1.x for exactly
   this — 0001 §6/§9): wazero has no wasm-threads yet, so the encode is single-threaded.
   The pinned version is recorded in the provenance manifest.
5. **Emit** `ffmpeg.wasm` + `ffmpeg.wasm.json` (provenance: ffmpeg/x264/wasi-sdk versions,
   full configure line, sha256, byte size, build date, variant).

`build/` layout (proposed):
```
build/
  Dockerfile            # pinned wasi-sdk toolchain image
  build.sh              # adapted from go-ffmpreg; drives configure + make
  configure.full.sh     # the --enable-* set incl. x264 (GPL)
  configure.lgpl.sh     # the LGPL variant (openh264 / no x264)
  versions.lock         # ffmpeg, x264, openh264, wasi-sdk pins
  README.md             # how to build, how to bump a pin, provenance format
```

## 5. Licensing & distribution (D-C / R-AF-10)

Per **D-C** the build outputs are governed as **separate artifacts**, decoupled from the Go
module's licence:

- The **default full/GPL `ffmpeg.wasm`** (with x264) is **published as a release/download
  artifact, never committed raw and never `//go:embed`-ed** into the Go package. `.gitignore`
  already excludes `internal/wasm/*.wasm` — keep it that way. 0004 fetches/loads it at
  runtime or build-time wiring (its decision), so the copyleft obligation attaches only to a
  consumer who bundles it, not to the afmpeg library source.
- The Go module itself is **permissively licensed** (Apache-2.0 or MIT — confirm in the
  scaffolding task; the repo currently carries an MIT-style `LICENSE`).
- An **LGPL variant** (`configure.lgpl.sh`: openh264 or no H.264 encoder) is produced by the
  same pipeline and published alongside, for permissive consumers (R-AF-10). Its H.264
  quality/patent caveats are documented (0001 §9, D-C rationale). It is **tracked, not gating
  v1** — keyrx ships on the GPL/x264 build.
- The provenance manifest records the variant and its licence so a consumer can choose
  knowingly.

## 6. Requirements

- `R-0002-1` The full/GPL build runs the §3 spread of unrelated workflows end-to-end to
  valid outputs (transcode, scale, overlay, concat, thumbnail, audio extract, and the
  keryx reel as one example) — proving the general baseline, not a single command.
- `R-0002-2` The build is reproducible: same pinned inputs → identical `ffmpeg.wasm` sha256
  (R-AF-6). The Docker image and all source versions are digest/tag-pinned in `versions.lock`.
- `R-0002-3` A provenance manifest (`ffmpeg.wasm.json`) is emitted with versions, the exact
  configure line, sha256, size, and variant.
- `R-0002-4` `CGO_ENABLED` is irrelevant here (no Go), but the artifact MUST load under a
  pure-Go wazero runtime (validated by 0004) — i.e. plain `wasm32-wasi`, no host imports
  beyond WASI.
- `R-0002-5` The GPL `ffmpeg.wasm` is **not** embedded in or committed to the Go module
  source (D-C); it is a published artifact.
- `R-0002-6` An LGPL variant builds from the same pipeline (R-AF-10); H.264 caveats documented.
- `R-0002-7` Module size is recorded; a target ceiling is noted (go-ffmpreg ≈ 7.5 MB gzip as
  the reference — 0001 §9).

## 7. Definition of done

- `just` (or `build/build.sh`) produces `ffmpeg.wasm` + manifest from a clean checkout.
- The §3 command runs inside a throwaway WASI host (e.g. `wasmtime` with a temp preopen) to
  prove capability **independently of the Go layers** — this is the Phase-1 gate.
- Two consecutive builds yield identical sha256 (R-0002-2).
- A gated, slow CI job builds + verifies the wasm; it does not run on every push.
- `build/README.md` documents the build, a pin bump, and the provenance format.

## 8. Risks (carried from 0001 §9)

- **Build maintenance burden** — owning an FFmpeg-WASI build is ongoing (version/toolchain
  drift). Mitigated by pinning + adapting (not forking-and-diverging) go-ffmpreg's build.
- **Single-threaded encode** — pinned no-pthreads FFmpeg is slow; acceptable for the
  in-memory edge case (0001 §9). Revisit under 0006 / R-AF-12.
- **wazero writable-fs needs (moov atom seek-on-write)** surface here as a *muxer* concern but
  are validated in 0003 — flagged so the configure keeps `+faststart` workable.

## 9. Sequencing

Independent — can start immediately, in isolation, before any Go code. 0003/0004 develop
against a stand-in module until this lands, then swap to the real artifact for the R-AF-3
end-to-end test. Reference: go-ffmpreg `build.sh`.
