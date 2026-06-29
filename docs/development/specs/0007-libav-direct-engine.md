# 0007 — the libav-direct engine + the ffmpeg-wasi project

Status: **DRAFT / SCOPING** (the design for the WASM media engine afmpeg drives. Pivots
away from "compile the ffmpeg CLI to wasm" — see §1. **Supersedes spec 0002.** Review
before building.)
Date: 2026-06-28
Parent: [0001-afmpeg.md](0001-afmpeg.md); supersedes [0002-wasm-build-pipeline.md](0002-wasm-build-pipeline.md)
Owns: R-AF-3 (the codec/filter set, reframed), R-AF-6 (reproducible build), R-AF-10 (licence variants)

## 1. Why this supersedes 0002 (the pivot)

Spec 0002 assumed we'd compile **the ffmpeg CLI** to wasm (adapting go-ffmpreg). Research
(recorded in 0001 §9 and the session decision log) found that path is a dead end for a
security-first, current-FFmpeg, CGO-free tool:

- go-ffmpreg pins **FFmpeg n5.1**, which is **end-of-life** (the 5.1 branch ended at
  n5.1.10; current is n8.x). Shipping an EOL media decoder is a non-starter.
- **FFmpeg 7.0+ rewrote the `ffmpeg` CLI to be multithreaded** (`fftools/ffmpeg_sched.c`;
  `ffmpeg_deps="… threads"`). It cannot be built without threads, and **wazero implements
  the threads *instructions* (atomics) but not `wasi-threads` thread-*spawn*** — so the
  modern CLI can't run on wazero. Switching to a thread-capable runtime (Wasmtime, etc.)
  means **CGO**, which forfeits afmpeg's entire reason to exist (R-AF-1).

**The way under the wall:** the threading is only in `fftools` (the CLI). The **libav\*
*libraries* build to `wasm32-wasi` single-threaded with no trouble.** So instead of the
CLI, we link the libraries directly and drive them with our own thin C program. This was
**spike-proven (2026-06-28):** FFmpeg **n8.1.2** libav\* compiled to wasm, a C driver
linked them, and it ran under wazero (with afmpeg's existing setjmp/longjmp env module),
reporting `version_info: n8.1.2` and a working codec/muxer/filter API.

This gives us **current FFmpeg, on pure-Go wazero, CGO-free** — and makes us the reference
for server-side (WASI) FFmpeg, since the CLI wall stops everyone else.

## 2. Two repositories (the architecture)

```
┌─ ffmpeg-wasi (NEW repo) ───────────────────────────┐      ┌─ afmpeg (this repo, permissive) ──────┐
│  libav* (current FFmpeg) + codec deps               │      │  Runtime.Run(fs, args…) + vfs + env    │
│  + OUR C driver: a JSON job-spec → libav            │ ───► │  Command builder → driver job spec     │
│    (demux/decode/filter/encode/mux), single-thread  │ .wasm│  WithModuleURL(<artifact>, WithSHA256) │
│  → driver.wasm release artifact (LGPL + GPL)        │      │  (Run stays generic for any module)    │
└─────────────────────────────────────────────────────┘      └────────────────────────────────────────┘
```

The **interface between them is the job-spec vocabulary (§4)** — a versioned compatibility
surface, not "arbitrary ffmpeg CLI args."

## 3. The ffmpeg-wasi engine

- **Libraries:** current FFmpeg (target n8.x), `libavformat/libavcodec/libavfilter/
  libavutil/libswscale/libswresample`, compiled `wasm32-wasi`, **single-threaded**
  (`--disable-pthreads`, `--disable-asm`), via wasi-sdk/clang with the LLVM native
  setjmp/longjmp lowering (`-mllvm -wasm-enable-sjlj`, which emits the `__wasm_setjmp`/
  `__wasm_longjmp` imports afmpeg's runtime already provides — spec 0004 R-0004-9).
- **The driver (`driver.c`, our code, MIT):** a WASI command. It reads a **job spec**
  (§4), opens inputs/outputs against the mounted WASI fs, builds the filtergraph via
  `avfilter_graph_parse2`, configures decoders/encoders, runs the processing loop, and
  exits. No CLI, no threads. Errors → stderr + non-zero exit; probe results → stdout JSON.
- **The artifact:** `driver.wasm` (+ `.gz`), published per §7.

## 4. The vocabulary (the contract) — clean & custom (D-FW-B, RESOLVED)

Not an ffmpeg-CLI subset (that would be a leaky, partial-compat trap). A **structured job
spec** the driver reads — mirroring afmpeg's existing `Command` model, with the one place
reinvention is folly (the filter graph) delegated to libav's own parser:

```jsonc
// operation "process": transcode / filter / mux
{
  "op": "process",
  "inputs":  [ { "path": "in/clip.mp4", "options": { "…": "…" } } ],
  "filter":  "[0:v]scale=1280:-2[v]",          // ffmpeg filtergraph STRING (libavfilter parses it)
  "outputs": [ { "path": "out/x.mp4",
                 "map": ["[v]"],
                 "video_codec": "libx264", "audio_codec": "aac",
                 "options": { "crf": "23", "movflags": "+faststart" } } ]
}
// operation "probe": report stream info (duration, codecs, …) as JSON on stdout
{ "op": "probe", "inputs": [ { "path": "in/clip.mp4" } ] }
```

- **Structured** inputs/outputs/codecs/maps/options — typed, validated, *exactly* its
  capabilities (no "looks-like-ffmpeg-but-isn't"). We own and document this surface.
- **The `filter` string is ffmpeg's filtergraph syntax** — we delegate to
  `avfilter_graph_parse2`; reinventing that DSL would be folly, and users' filtergraph
  knowledge transfers there.
- **Transport (D to confirm in impl):** the spec is passed as a single argv argument, or
  written to a file in the vfs the driver reads. afmpeg's `Run`/vfs already carry it.
- This *is* afmpeg's `Command` (Inputs / FilterComplex string / Outputs) — so afmpeg's
  builder serialises to the job spec rather than to CLI args; no model redesign.

## 5. Licensing — MIT tooling, both LGPL+GPL artifacts shipped (D-FW-C, RESOLVED pending review)

Three licences, kept deliberately distinct:

- **The ffmpeg-wasi *repo source* — build tooling + `driver.c` — is MIT (ours).** Clean-room
  (the libav-direct pivot means we don't use go-ffmpreg's 40 KB GPL CLI patch); it vendors
  **no** FFmpeg/x264 source (cloned at build time); and — the fact that keeps it MIT — the
  **tooling never *links* libav/x264, it only orchestrates the build** (clone → configure →
  compile → package), so it is not a derivative of GPL code. This MIT pipeline is genuinely
  valuable, reusable IP: the reference "FFmpeg → WASI, libav-direct" build nobody else has.
- **The released artifacts carry the licence their contents demand** — libav\* is LGPL-2.1+:
  - **LGPL variant (default):** no `--enable-gpl`, no x264. H.264 encode via **openh264**
    (BSD; document the self-compiled AVC-patent caveat) or omitted. Proprietary-compatible.
  - **GPL/full variant (opt-in):** `--enable-gpl` + **libx264**, best-in-class H.264.
- **We ship BOTH variants in every release**, so a consumer who just wants a working module
  picks the licence that fits and skips building. This does **not** compromise us:
  distributing two separate, independent artifacts together is **mere aggregation**
  (GPLv3 §5) — the GPL artifact does not infect the LGPL artifact, the MIT tooling/driver
  source, or afmpeg.
- **Obligations we meet:** each asset is clearly licence-labelled; the provenance manifest
  records variant + licence; and we satisfy GPL/LGPL **corresponding-source** (and LGPL
  **relink**) via the public MIT repo (our scripts + `driver.c`) + the pinned upstream
  FFmpeg/x264 — anyone can rebuild/relink from public sources.
- **LGPL is the floor** (we cannot relicense libav\* below it); **MIT is what we own**
  (tooling + driver); **afmpeg stays permissive** (it downloads an artifact; the GPL/LGPL
  obligation attaches to the consumer who runs it, never to afmpeg's source). *(A real
  licence review precedes any release.)*

## 6. The codec/filter baseline (R-AF-3, reframed)

A general baseline (validated by the §9 workflow spread), not one consumer's set: decode of
common containers/codecs; encode of at least **H.264** (libx264 GPL / openh264 LGPL),
**AAC** (native), plus common image/audio encoders; the general filter set
(`scale`/`crop`/`pad`/`overlay`/`concat`/`xfade`/`format` + the audio filters). A **lean**
vs **full** build is selectable. Finalised in the ffmpeg-wasi repo's own build spec.

## 7. Versioning & release (D-FW: track upstream FFmpeg)

- **Tag = upstream FFmpeg version + build revision**, e.g. `n8.1.2-1` (the suffix bumps for
  toolchain/config rebuilds of the same FFmpeg). Releases are cut when a new FFmpeg version
  lands or a rebuild is needed — *not* conventional-commit/releaser-pleaser driven.
- **Custom release pipeline** (no goreleaser): tag → Docker build (both variants) →
  emit `.wasm`(+`.gz`) + a **provenance manifest** (FFmpeg/dep/toolchain versions, configure
  line, variant, licence) + **`checksums.txt`** → publish as **GitLab release assets**.
- afmpeg consumers pin `WithModuleURL(<release asset>, WithSHA256(<published sum>))`.

## 8. afmpeg-side integration

- **`Runtime.Run` stays generic** — it runs any wasm module with args over the vfs; the
  interim go-ffmpreg path (raw ffmpeg args) still works for anyone who wants it.
- **The `Command` builder targets the job-spec vocabulary** (§4) — its `Args()` (or a new
  `Job()`) serialises to the driver's spec. `Probe` becomes an `op:"probe"` job parsing the
  driver's JSON, replacing the `ffmpeg -i` stderr scrape (spec 0004 D-0004-A) once the
  driver is the module in use.
- **Pinning:** afmpeg documents/pins a known-good `ffmpeg-wasi` artifact + sha; the job-spec
  vocabulary version is the compatibility check between the repos.

### Status (2026-06-28) — validated end-to-end

The whole stack — **keyrx → afmpeg → ffmpeg-wasi** — was validated before the first
ffmpeg-wasi release: keyrx's `afmpeg` renderer drives the engine to produce a real reel
(PNG stills → xfade-concat + audio mix → h264/aac mp4) **entirely in memory, no system
ffmpeg**. Done on the afmpeg side:

- ✅ **`Result.Stdout`** — exposes the engine's structured (probe/process) JSON.
- ✅ **`Command.JobSpec()` + `Runtime.RunJob()`** — the generic emitter from the `Command`
  struct to the job spec; `Args()` stays for CLI ffmpeg. No consumer concepts leaked in.
- ✅ **`WithModuleURL` + `WithSHA256`** — pin a published `ffmpeg-wasi` artifact.
- ✅ Integration tests (`TestIntegration_FFmpegWasiDriver`, `TestIntegration_RunJob`,
  gated on `AFMPEG_TEST_FFMPEG_WASI`) prove the seam against the real driver.

### Remaining afmpeg work (next steps)

1. ✅ **`Probe` over the driver** *(done, v0.4.0)* — `Probe` drives the engine's `probe` op
   (`{"op":"probe"}`) and parses `Result.Stdout` into `Probe{Format, DurationSec, Streams}`.
   The CLI path was removed entirely (job-spec only). Unblocks keyrx swapping its `ffprobe`
   `ProbeDuration` to `afmpeg.Probe`.
2. ✅ **Runtime reuse guidance** *(done)* — `docs/how-to/reuse-a-runtime.md` covers
   compile-once / reuse-many (the long-lived shared `Runtime`) and the one-at-a-time
   serialisation (parallelise with a fleet; `RuntimePool` is 0006 §2E).
3. ✅ **A "consume ffmpeg-wasi" how-to** *(done)* — `docs/how-to/obtain-a-module.md` covers
   file/bytes/fs/URL acquisition with checksum-verified caching; refreshed to the `n8.1.2-2`
   release (lgpl now encodes H.264 via openh264).

## 9. Requirements

- `R-FW-1` Current FFmpeg libav\* builds to `wasm32-wasi`, single-threaded, CGO-free; loads
  and runs under wazero (composing afmpeg's env module + features). **Spike-proven.**
- `R-FW-2` The driver executes a job spec end-to-end over the WASI fs — a real in-memory
  transcode (decode→filter→encode→mux), no host fs. (Next validation beyond the spike.)
- `R-FW-3` The job-spec vocabulary (§4): structured I/O/codecs + the libav filtergraph
  string; `process` and `probe` operations; versioned.
- `R-FW-4` LGPL (default) + GPL (full) variants from clean-room, permissively-licensed
  tooling; MIT driver; per-artifact licence recorded.
- `R-FW-5` Reproducible build; tag tracks upstream FFmpeg; release pipeline publishes
  checksummed, provenance-stamped artifacts.
- `R-FW-6` Validated across the workflow spread: transcode, scale, overlay, concat,
  thumbnail, audio-extract, probe.
- `R-FW-7` afmpeg integration: `Command` → job spec; `Run` stays generic; pinned consumption.
- `R-FW-8` **Docs & marketing are a first-class, day-one deliverable** — not an afterthought.
  ffmpeg-wasi is a flagship "nobody else has done this" project, so the narrative is part of
  the product. It ships:
  - A full **Diátaxis** docs site: *tutorials* (your first in-memory transcode), *how-to*
    (each workflow + choosing a variant + verifying checksums), *reference* (the job-spec
    vocabulary, the artifacts/variants/provenance, the supported codec/filter matrix),
    *explanation* (the architecture, **why libav-direct beats the CLI/threads wall**, the
    licensing model).
  - A **marketing narrative** leaning into the genuine differentiators: **current,
    maintained FFmpeg** (not EOL) · **WASI-native / server-side** (not the browser one) ·
    **pure-Go-embeddable, CGO-free** (wazero) · **sandboxed** · *the* reference for FFmpeg on
    WASI — because everyone else hit the threading wall and we went under it.

## 10. Decisions

- **D-FW-A — name. RESOLVED 2026-06-28: `ffmpeg-wasi`.** The most truthful name: it is
  FFmpeg's libav\*, and `wasi` (not `wasm`) owns the uncontested server-side niche, distinct
  from the crowded browser `ffmpeg.wasm`. Keeps the searchable, honest `ffmpeg` keyword.
- **D-FW-B — vocabulary. RESOLVED 2026-06-28: clean custom** structured job spec (not an
  ffmpeg-CLI subset), with the filter graph delegated to libav's parser.
- **D-FW-C — licensing. RESOLVED 2026-06-28 (pending legal review):** the repo source (build
  tooling + `driver.c`) is **MIT — owned, reusable IP** (it orchestrates, never links GPL).
  **Both LGPL (default) and GPL/x264 (opt-in) artifacts ship in every release** for consumer
  convenience — mere aggregation (GPLv3 §5), no compromise. LGPL is the floor; afmpeg stays
  permissive; corresponding-source/relink met via the public repo + pinned upstream.
- **D-FW-D — separate repo. RESOLVED:** the GPL/LGPL engine lives in `ffmpeg-wasi`; afmpeg
  consumes the artifact and stays permissive.
- **D-FW-E — driver language. RESOLVED 2026-06-28: C** (not Rust). The driver is a thin shim
  over C libraries; Rust's safety would cover only the glue (the codecs stay C), the wasm
  sandbox already contains memory-bug blast radius to the guest, and Rust adds a second
  toolchain + FFI/allocator integration risk for marginal benefit.

## 11. Phased roadmap

- **Phase A — engine build** (R-FW-1/4/5): clean-room build of current libav\* + the LGPL/GPL
  variants; the release pipeline. (The spike de-risked the compile/link/run.)
- **Phase B — the driver** (R-FW-2/3/6): the job-spec parser + the processing loop; the
  workflow-spread validation (real in-memory transcode).
- **Phase C — afmpeg integration** (R-FW-7): `Command` → job spec; pinned consumption;
  reframe `Probe` onto the driver.
- **Phase D — hardening** (0006 carries over): perf, LGPL/openh264 hardening, the lean/full
  matrix.

## 12. Definition of done (this scoping spec)

- The pivot, the two-repo split, the vocabulary, the licensing posture, and the versioning
  are recorded and agreed. 0002 is marked superseded. The ffmpeg-wasi repo is created with
  its own build spec citing this one; afmpeg's specs are reframed accordingly.
