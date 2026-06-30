# 0001 — afmpeg: pure-Go FFmpeg on a virtual filesystem

Status: **APPROVED / INTENT** (scaffold — the build has not started. This is the design
+ requirements. The §10 decisions were resolved on 2026-06-26 (see §10) and the work is
decomposed into component specs 0002–0006; those are implemented test-first, each citing
this spec. This remains the source-of-truth thesis.)
Date: 2026-06-26 (§10 resolved 2026-06-26)
Provenance: extracted from the keryx ffmpeg-render-binding spike
(`keryx/docs/development/spikes/ffmpeg-render-binding.md`), which found no existing
option delivered "in-memory + pure-Go + the filters/codecs we need".

## 1. What & why

**afmpeg is a pure-Go FFmpeg binding whose I/O runs on an `afero.Fs` — including a
fully in-memory filesystem — with no CGO, no host FFmpeg install, and no temp files.**

FFmpeg is embedded as a **WebAssembly (WASI) module** and executed via **wazero** (a
zero-dependency, pure-Go WASM runtime). Its filesystem syscalls are bridged to an
`afero.Fs`, so a caller can hand afmpeg in-memory inputs and read in-memory outputs —
the whole pipeline staying in RAM and cross-compiling to a single static binary.

The driving need (keryx): render short reels (xfade crossfade chain + audio mix →
H.264/AAC mp4) for **in-memory projects** (a git remote cloned into RAM, no local
checkout) — which today is impossible because keryx shells out to the ffmpeg binary,
which needs real files on disk. The spike rejected every existing path; afmpeg is the
"wazero + WASM done right" option that fills the gap **and** is generally reusable.

## 2. Goals / non-goals

**Goals**
- Pure Go: `CGO_ENABLED=0`, cross-compiles, single static binary, no host FFmpeg.
- I/O over `afero.Fs` — in-memory (`MemMapFs`), OS (`OsFs`), or any backend.
- **Customer-agnostic and general-purpose.** afmpeg is a general ffmpeg toolkit for
  *any* workflow (transcode, scale/crop/pad, overlay, concat, crossfade, thumbnail,
  audio extract/mix, mux, …). afmpeg defines the abstraction; consumers adapt to it —
  **keryx is the first reference customer, not the API author.** No consumer's
  opinionated structure (e.g. keryx's "reel") is baked into afmpeg's surface.
- A general baseline codec/filter/muxer set covering common workflows (§5 R-AF-3), of
  which keryx's reel render is one *validating example*, not the definition.
- A clean, idiomatic Go API: the universal `Run(fs, args…)` primitive plus a general,
  use-case-agnostic command builder (R-AF-7).
- A reproducible **FFmpeg-WASM build pipeline** (the hard, separable sub-project).

**Non-goals (initially)**
- Hardware acceleration (NVENC/VAAPI/VideoToolbox) — WASM is CPU-only.
- Real-time / streaming / live capture.
- A full typed libav* object API (à la go-astiav) — afmpeg is invocation-first; a
  typed API is a possible later layer.
- Windows is best-effort, not a launch target (Linux + macOS first).

## 3. Approach

Three layers; the middle one is the novel engineering.

1. **Embedded FFmpeg-WASM module** (`internal/wasm/ffmpeg.wasm`). FFmpeg + its deps
   (x264, etc.) compiled to `wasm32-wasi` via the wasi-sdk/clang toolchain, with a
   configure that includes only the needed codecs/filters/muxers (size + build time).
   Produced by the build pipeline (§6); embedded with `//go:embed` (or downloaded on
   first use — §9 open question). go-ffmpreg's `build.sh` is the reference start point.
2. **The afero ↔ wazero `sys.FS` bridge** (`internal/vfs`, the heart of afmpeg).
   ffmpeg-in-the-guest issues WASI filesystem syscalls (`path_open`, `fd_read`,
   `fd_write`, `fd_seek`, …); wazero routes them to a mounted `experimental/sys.FS`.
   afmpeg implements that `sys.FS` **backed by an `afero.Fs`**, so the guest's reads
   and writes hit the caller's afero (e.g. an in-memory `MemMapFs`) with no host disk.
   Also provides `/tmp` (writable memfs) and `/dev/null` the guest may need.
3. **The Go API** (`pkg/afmpeg`). Instantiate the runtime once (compile the module),
   then `Run(ctx, fs, args…)` mounts `fs`, runs the ffmpeg command, returns
   exit/stderr. Higher-level helpers (a timeline/render builder mirroring keryx's
   `provider.Timeline`) layer on top.

```
pkg/afmpeg/        public API (Runtime, Run, options, errors; later: render helpers)
internal/vfs/      afero.Fs  →  wazero experimental/sys.FS adapter (THE core)
internal/wasm/     embedded ffmpeg.wasm + the wazero module wiring
build/             the FFmpeg→WASI build pipeline (wasi-sdk, configure flags, Docker)
docs/development/specs/
```

## 4. The Go API (sketch — not final)

```go
package afmpeg

// Runtime holds the compiled wazero module + runtime; build once, reuse (compilation
// is the expensive step). Safe for sequential reuse; concurrency model is §10-D.
type Runtime struct { /* ... */ }

func New(ctx context.Context, opts ...Option) (*Runtime, error) // compiles the module
func (r *Runtime) Close(ctx context.Context) error

// Run executes one ffmpeg invocation with its filesystem bridged to fs (paths in
// args resolve against fs — e.g. "in/cover.png", "out/reel.mp4"). Returns the exit
// code + captured stderr; no host disk is touched.
func (r *Runtime) Run(ctx context.Context, fs afero.Fs, args ...string) (Result, error)

type Result struct { ExitCode int; Stderr string }

// Probe mirrors ffprobe (duration etc.) over the same fs bridge.
func (r *Runtime) Probe(ctx context.Context, fs afero.Fs, path string) (Probe, error)
```

Usage (keryx-shaped, fully in-memory):
```go
rt, _ := afmpeg.New(ctx)
defer rt.Close(ctx)
fs := afero.NewMemMapFs()              // or the caller's in-memory worktree
// ... write cover.png, cards/NN.png, vo/*.mp3, music.mp3 into fs ...
_, err := rt.Run(ctx, fs, "-i", "in.txt", /* xfade graph */, "out/reel.mp4")
mp4, _ := afero.ReadFile(fs, "out/reel.mp4")   // the result, in memory
```

## 5. Requirements

**Core (MUST)**
- `R-AF-1` Pure Go: builds with `CGO_ENABLED=0` and cross-compiles (linux/macOS,
  amd64/arm64) without a C toolchain or host FFmpeg.
- `R-AF-2` I/O is `afero.Fs`-backed: a `MemMapFs` runs end-to-end with **no host
  filesystem access** (verifiable — deny OS fs in tests).
- `R-AF-3` The WASM build carries a **general baseline** codec/filter/muxer set covering
  common workflows — not one customer's graph. Decode of common containers/codecs
  (mp4/mov, mkv/webm, mp3, image2) and encode of at least H.264 (libx264) + AAC, plus a
  general filter set (`scale`, `crop`, `pad`, `fps`, `format`, `setsar`, `overlay`,
  `concat`, `xfade`, and the audio filters `amix`/`adelay`/`volume`/`afade`/`alimiter`/
  `aresample`). The exact curated baseline (and a lean vs full variant, R-AF-9) is owned
  by spec 0002. keryx's reel set (`xfade` + libx264 + AAC + mp4 + the audio filters) is a
  **subset** of this baseline — one validating example, not the definition.
- `R-AF-4` `Run` returns the exit code + stderr; a non-zero exit surfaces ffmpeg's
  error tail (no silent failures).
- `R-AF-5` ffprobe-equivalent duration probing over the same fs bridge (keryx needs
  VO clip durations to drive card timing).
- `R-AF-6` Deterministic, **reproducible** WASM build (pinned ffmpeg + deps + toolchain
  versions; documented `build/`); the artifact's provenance is recorded.

**SHOULD**
- `R-AF-7` A general, use-case-agnostic **command builder** (typed inputs/filtergraph/
  outputs/options, with a raw escape hatch on every scope) so callers compose *any*
  ffmpeg invocation without hand-assembling arg slices. Not tied to any workflow; a
  consumer's reel/timeline is built *on* it, in the consumer's code (spec 0005).
- `R-AF-8` Context cancellation aborts a running invocation promptly.
- `R-AF-9` Pluggable codec/filter sets via alternate WASM builds (a lean build vs a
  full build) selected at construction.
- `R-AF-10` Licensing-clean option: an **LGPL** build variant (no GPL components like
  x264) alongside the default full/GPL build (§9).

**MAY (later)** — *dispatched 2026-06-29 (see [0006](0006-hardening-roadmap.md) §2):*
- `R-AF-11` A native (purego/CGO libav) backend for speed — **dropped**. The libav-direct
  engine removes the need, and CGO re-introduces the posture §2 avoids; revisit only if a
  concrete capability ffmpeg-wasi can't deliver forces a separate opt-in backend.
- `R-AF-12` Performance — **→ [0008](0008-performance-strategy.md)** (a measurement-first spike;
  wasm-threads/SIMD turned out to be unavailable, so the lever is instance-level parallelism /
  build tuning, not threads).
- `R-AF-13` A thin `cmd/afmpeg` CLI — **→ [0009](0009-afmpeg-cli.md)** (deferred, value-unproven;
  job-spec-native only — *not* the drop-in `ffmpeg` arg path, which v0.4.0 removed).

## 6. The FFmpeg→WASI build pipeline (the hard sub-project)

Compiling FFmpeg + x264 to `wasm32-wasi` is the bulk of the effort and is **separable**
from the Go binding. Approach:
- Toolchain: **wasi-sdk** (clang targeting `wasm32-wasi`); a pinned Docker build for
  reproducibility (R-AF-6).
- Build x264 (and any other deps) for wasm first, then FFmpeg `./configure` with
  `--target-os=none --arch=wasm32 --enable-cross-compile`, `--disable-everything` then
  `--enable-*` only the §5 R-AF-3 set (keeps the module small + the build tractable).
- Pin to an FFmpeg version that **doesn't require pthreads** unless/until wazero
  supports wasm-threads (go-ffmpreg pins n5.1.x for this reason — §9 perf).
- Output `ffmpeg.wasm`; record size + the exact configure line + versions.
- Reference: go-ffmpreg's `build.sh` (Codeberg) — adapt, extend the codec/filter set.

This sub-project can be spiked/built independently; the Go layers (§3.2/§3.3) can be
developed against a stand-in WASM module meanwhile.

## 7. keryx integration (the first consumer)

keryx is the **reference customer** — it validates afmpeg, it does not define it.
keryx renders behind a `provider.Renderer` seam (`internal/render/ffmpeg`, the default
shells out to the ffmpeg binary). afmpeg lands as an **alternate `Renderer`
implementation** selected by config (`providers.render: afmpeg`) — no call-site changes
(keryx's pluggable-provider pattern). **keryx adapts to afmpeg, not the reverse:** keryx
keeps owning its reel decisions (segments, crossfade, encode profile) and builds an
afmpeg command/arg slice for them **in keryx's repo** (on the R-AF-7 builder or raw
`Run`); afmpeg carries no reel/timeline types. When afmpeg is usable, keryx's in-memory
render lock-out (spec 0015 D1) lifts: the in-memory worktree's afero fs is handed
straight to afmpeg. Until then keryx stays local-only + native ffmpeg.

## 8. Phased roadmap

- **Phase 1 — the WASM build** (§6): produce a reproducible `ffmpeg.wasm` with the
  R-AF-3 set. De-risks everything; can run as its own spike.
- **Phase 2 — the vfs bridge + Run** (§3.2/§3.3): the afero↔sys.FS adapter, the wazero
  wiring, `Run`/`Probe`, R-AF-1/2/4/5. End-to-end in-memory transcode test.
- **Phase 3 — render helper + keryx backend** (R-AF-7, §7): the timeline helper; wire
  as a keryx `Renderer`; validate parity with native ffmpeg output.
- **Phase 4 — hardening**: LGPL build (R-AF-10), perf (R-AF-12), pluggable backends
  (R-AF-11), CLI (R-AF-13).

## 9. Risks & open questions

- **FFmpeg licensing (the big one).** FFmpeg + x264 is **GPL**; embedding a GPL
  `ffmpeg.wasm` makes the combined binary GPL. Fine for a private tool (keryx), a
  problem for a permissively-licensed library. Options: ship a GPL build *and* an
  LGPL build (drop x264; H.264 via openh264 or none), and/or distribute the wasm as a
  separate artifact rather than `//go:embed`. **Decide before public release.**
- **Performance.** WASM ffmpeg without wasm-threads is single-threaded → materially
  slower than native encode (the keryx reel is ~30–45 s of H.264; a multi-minute
  render could be several× slower). Acceptable for the in-memory *edge case*; keep
  native ffmpeg as keryx's fast path for local projects. Re-evaluate with R-AF-12.
- **Binary size.** The embedded wasm is sizeable (go-ffmpreg's ~7.5 MB gzip). Embed
  vs download-on-first-use vs separate artifact — R-AF-6/§9 decision.
- **wazero writable-fs maturity.** The `experimental/sys.FS` write path must support
  everything ffmpeg's muxer needs (seek-on-write for the mp4 moov atom, `/tmp`).
  Validate early in Phase 2.
- **Build maintenance.** Owning an FFmpeg-WASI build is ongoing work (version bumps,
  toolchain drift). Weigh vs the materialise↔readback bridge keryx can use meanwhile.

## 10. Decisions — RESOLVED 2026-06-26

All five gating decisions were walked with Matt and resolved before any component spec
was drafted. The resolutions below are binding on specs 0002–0006.

- **D-A — name. RESOLVED: keep `afmpeg`** (afero + ffmpeg). Confirmed unclaimed — no
  GitHub repo, no Go module of that name; the `afero` root is the distinguishing signal
  vs the existing Go ffmpeg bindings. `go.mod` (`gitlab.com/phpboyscout/afmpeg`) stands.
- **D-B — WASM source. RESOLVED: adapt go-ffmpreg's `build.sh`** as the proven wasi-sdk
  start point, extending its `./configure` to add the R-AF-3 set (xfade, AAC, the audio
  filters). Not from scratch; not interim-stock-only. Owned by **spec 0002**.
- **D-C — licensing posture. RESOLVED: permissive Go package + separable GPL wasm.** The
  Go code is permissively licensed (Apache-2.0/MIT — confirm in 0002); the GPL/x264
  `ffmpeg.wasm` ships as a **separate downloadable artifact, never `//go:embed`-ed**, so
  the copyleft obligation only attaches to a consumer who fetches+bundles it. keyrx uses
  the GPL/x264 build now (private; top quality). An **LGPL/openh264 variant is tracked**
  (R-AF-10) for public/permissive consumers. Rationale: x264 is the *single* GPL piece in
  keyrx's render — AAC (native LGPL encoder), xfade, scale/fps, amix/adelay/volume/afade/
  alimiter, mp4 mux are all already LGPL-clean — so the GPL surface is one separable
  encoder, not the whole pipeline. Owned by **spec 0002**.
- **D-D — concurrency model. RESOLVED (provisional): one invocation at a time per
  `Runtime`**, with a pooled/parallel mode as a documented follow-up. Final shape and the
  instance-pool design are owned by **spec 0004** (its local decision against the wazero
  module wiring).
- **D-E — scope of v1. RESOLVED: raw `Run(ctx, fs, args…)` + `Probe` first** (the novel
  bridge + invocation core — specs 0003/0004). The general command builder (R-AF-7)
  follows as **spec 0005**, once `Run` is proven end-to-end.
- **D-F — customer-agnostic surface. RESOLVED 2026-06-27 (Matt):** afmpeg is a
  general-purpose ffmpeg toolkit; no consumer's opinionated structure is baked into its
  API. The first reel-shaped render helper (a verbatim port of keryx's `buildArgs`) was
  **rejected and reverted before merge**; R-AF-7 is reframed as a general command builder
  and keryx's reel moves into keryx's repo, built on afmpeg. The §5 R-AF-3 codec set is a
  general baseline, not "keryx's set". keryx adapts to afmpeg (§7).

### Component spec map (the compartmentalised work)

| Spec | Phase (§8) | Scope | Requirements owned |
|---|---|---|---|
| **0002** wasm-build-pipeline | 1 | FFmpeg+x264 → `wasm32-wasi` (adapt go-ffmpreg); reproducible Docker build; licence variants | R-AF-3, R-AF-6, R-AF-10; D-B, D-C |
| **0003** vfs-bridge | 2 | afero.Fs → wazero `experimental/sys.FS` adapter (the core); `/tmp`, `/dev/null`; seek-on-write | R-AF-2 |
| **0004** runtime-and-api | 2 | `New`/`Run`/`Probe`/`Result`/`Close`; module wiring; stderr/exit; ctx-cancel | R-AF-1, R-AF-4, R-AF-5, R-AF-8; D-D, D-E |
| **0005** command-builder | 3 | R-AF-7 general, use-case-agnostic ffmpeg command builder (inputs/filtergraph/outputs/options + raw escape hatch); `RunCommand`. keyrx's reel is built on it, in keyrx's repo | R-AF-7 |
| **0006** hardening-roadmap | 4 | **dispatched** — LGPL build-out + download-cache done; perf → 0008; CLI → 0009; native backend dropped | R-AF-10 (done) |
| **0007** libav-direct-engine | — | the pivot: `ffmpeg-wasi` libav-direct engine + job-spec vocabulary (supersedes 0002) | R-AF-3, R-AF-6, R-AF-10 |
| **0008** performance-strategy | 4 | spike: measure Wasm-encode perf vs native; non-threaded levers (RuntimePool, build tuning) | R-AF-12 |
| **0009** afmpeg-cli | 4 | deferred (value-unproven): job-spec-native `cmd/afmpeg`, never `ffmpeg`-arg-compatible | R-AF-13 |
| **0010** signed-release-acquisition | 4 | certified `WithModuleRelease` (KMS-signed checksum + provenance); BYO `WithModuleURL` stays uncertified | R-AF-14 |
| **0011** wkd-attestation | 4 | WKD key distribution + embedded↔WKD cross-check + shared offline rotation key (via the `signing` module) | R-AF-15, R-AF-16 |
| **0012** feature-parity-roadmap | 5 | survey of missing FFmpeg features (codecs/formats/filters/operations) bucketed by the WASI envelope + licence; dispatches child specs 0013+ toward parity | — (roadmap) |

## 11. Alternatives considered

See the keryx spike (`keryx/docs/development/spikes/ffmpeg-render-binding.md`): ffgo
(purego — immature), go-astiav (CGO — posture-breaking), go-ffmpreg (wazero — missing
filters/AAC, not vfs-integrated), materialise↔readback bridge (keryx's interim, no new
dep). afmpeg is the maintained "wazero + WASM + afero-vfs" synthesis those lack.

## 12. Dev method / DoD

Mirror keryx: TDD (failing test → code → green CI), tests assert **no host-fs access**
for the in-memory path (the core guarantee), reproducible-build check, docs per
component. The WASM build pipeline gets its own CI (slow; gated). `CGO_ENABLED=0` is a
hard CI gate.
