# afmpeg

**A pure-Go FFmpeg binding that runs on a virtual / in-memory filesystem.** No CGO,
no host FFmpeg install, no temp files: FFmpeg is supplied as a separate WebAssembly
module and executed via [wazero](https://wazero.io/) (a zero-dependency, pure-Go WASM
runtime), with its I/O bridged to an [`afero.Fs`](https://github.com/spf13/afero) — so
inputs and outputs can live entirely in memory (or any afero backend), and the whole
thing cross-compiles to a single static binary.

> **Status: released (v0.11.0).** The runtime (`New` / `Run` / `RunJob` / `Probe` /
> `Frames` / `Close`), the `Command` builder, and certified module acquisition
> (`WithModuleRelease`) are all shipped and stable. It drives the companion
> [ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) engine over the structured job spec:
> transcode, remux/stream-copy, seeking & clips, multi-input `filter_complex`,
> subtitles & burn-in, metadata/chapters, frame extraction, analysis measurements, and
> **live progress** (`WithProgress`). A **native backend** (`WithBackend` /
> `native.NewFromRelease`) drives ffmpeg-wasi's native driver for **48–58× faster**
> software encode plus HEVC/AV1 encode — still CGO-free. The design record lives
> in [`docs/development/specs/`](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0001-afmpeg); the current build
> order is the [implementation roadmap](docs/development/implementation-roadmap.md).

## Why this exists

It was extracted from a need in [keryx](https://gitlab.com/phpboyscout/keryx) (the
content-marketing tool): keryx renders short reels by **shelling out to the ffmpeg
binary**, which needs real files on disk — so it can't render an **in-memory project**
(a remote cloned into RAM, no local checkout). keryx's spike
(`keryx/docs/development/spikes/ffmpeg-render-binding.md`) evaluated the existing
options and found none viable:

- **purego/dlopen bindings** (e.g. `ffgo`) — immature, and still need host libav libs.
- **CGO libav bindings** (e.g. `go-astiav`) — mature and in-memory-capable, but **CGO**
  breaks a clean static cross-compile.
- **wazero + embedded ffmpeg.wasm** (e.g. `go-ffmpreg`) — the right *posture* (pure Go,
  no host deps, embeddable), but the stock builds lack the filters/codecs many
  workflows need (e.g. `xfade`, AAC) and aren't filesystem-virtualised.

`afmpeg` is the **"wazero + WASM done right"** synthesis: a maintained FFmpeg-WASM build
with the codecs/filters we need, a first-class **afero virtual-filesystem** I/O layer,
and a clean Go API — so a consumer (keryx, or anyone) can transcode / filter / mux
**entirely in memory, pure Go**.

afmpeg now supplies that binding, and keryx renders reels through it — **in-memory,
pure Go**, no local checkout required.

## How it works

Three layers — the middle one is the novel engineering:

1. **The FFmpeg-WASM module** — current FFmpeg compiled to `wasm32-wasi` (H.264 encode via
   openh264 on the LGPL default, or libx264 on the GPL variant), configured down to the
   codecs/filters real workflows need. Shipped as a **separate downloadable artifact, never
   `//go:embed`-ed** (see *Licensing* below).
2. **The afero ↔ wazero vfs bridge** (the heart) — the guest ffmpeg's WASI filesystem
   syscalls are routed to a mounted `experimental/sys.FS` that afmpeg implements **backed
   by an `afero.Fs`**. The guest's reads and writes hit the caller's filesystem (e.g. an
   in-memory `MemMapFs`) with no host disk touched.
3. **The Go API** — compile the module once into a reusable `Runtime`, then `Run` an
   ffmpeg invocation with its I/O bridged to a caller-supplied `afero.Fs`. A general,
   use-case-agnostic command builder layers on top (spec 0005).

A second **native backend** (spec 0028) swaps layer 1 for ffmpeg-wasi's native driver, run
out-of-process with the same `afero.Fs` served over a seekable AVIO-over-IPC socket — same API,
same no-host-disk guarantee, **48–58× faster** software encode (and HEVC/AV1). Select it with
`WithBackend` / `native.NewFromRelease`; WASM stays the default.

```go
rt, _ := afmpeg.New(ctx, afmpeg.WithModuleRelease("n8.1.2-11", afmpeg.VariantLGPL)) // compile once, reuse
defer rt.Close(ctx)

fs := afero.NewMemMapFs()            // or the caller's in-memory worktree
// ... write inputs into fs ...
cmd := afmpeg.NewCommand(
    afmpeg.WithInput("in/clip.mp4"),
    afmpeg.WithFilterComplex("[0:v]scale=1280:-2[v]"),
    afmpeg.WithOutput("out/reel.mp4", afmpeg.Map("[v]"), afmpeg.VideoCodec("libx264")),
)
res, _ := rt.RunJob(ctx, fs, cmd)
out, _ := afero.ReadFile(fs, "out/reel.mp4")   // the result, in memory
```

## Licensing

The Go package is **permissively licensed**. FFmpeg + x264 is GPL, so the full/GPL
`ffmpeg.wasm` is distributed as a **separate downloadable artifact** rather than embedded
— the copyleft obligation attaches only to a consumer who fetches and bundles it, not to
the library. An **LGPL/openh264 variant** is tracked for fully-permissive consumers. x264
is the single GPL component in the target render set; AAC, `xfade`, the mp4 muxer, and the
audio filters are all already LGPL-clean. See spec 0001 §10 (D-C).

## Roadmap

The foundations (specs 0001–0007) and the full feature-parity roadmap (0013–0021, 0024, 0027)
shipped; the strategic tier on top — signed releases (0010), the **native backend** (0028),
**HEVC/AV1 encode + AV1 decode** (0023), **analysis measurements**, and **live progress**
(0031/0032) — is shipped as of **v0.11.0** (job-spec vocab v9). The
**[implementation roadmap](docs/development/implementation-roadmap.md)** tracks per-spec status
and the current build order; the design records live in
[`docs/development/specs/`](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0001-afmpeg).

| Spec | Scope |
|------|-------|
| [0001](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0001-afmpeg) | The thesis: design, requirements, the resolved decision record (§10) |
| [0003](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0003-vfs-bridge) | The afero.Fs → wazero `sys.FS` adapter (the core) |
| [0004](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0004-runtime-and-api) | `New` / `Run` / `RunJob` / `Probe` / `Close` — the public API |
| [0007](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0007-libav-direct-engine) | The libav-direct engine + structured job spec (supersedes the CLI-string design) |
| [0010](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0010-signed-release-acquisition) | Signature-verified module acquisition (`WithModuleRelease`) |
| [0028](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0028-native-subprocess-backend) | The native subprocess backend — 48–58× faster software encode, HEVC/AV1, still CGO-free |
| [0031](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0031-job-progress-reporting) / [0032](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0032-engine-progress-side-channel) | Live job progress (`WithProgress`) — observed-fs (phase A) + engine side-channel (phase B) |
| [0034](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0034-fraction-source-precedence) | Which source `Progress.Fraction` derives from — engine time over input bytes, and `-1` rather than a false "done" |

What remains is a menu of **trigger-gated** work — a standalone [CLI](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0009-afmpeg-cli)
(0009), WASM threading (0030), native `arm64`/`darwin` (0022), HW-accel encoders, and measure-first
perf/AV-sync (0026/0025) — none of it on a critical path. See the roadmap's
[pick-up menu](docs/development/implementation-roadmap.md#pick-up-menu-for-a-future-session).

## Quick links

- Documentation: [`docs/`](docs/index.md) (Diátaxis — tutorials / how-to / reference / explanation)
- Design + decision record: [`0001-afmpeg`](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0001-afmpeg)
- API overview: [`pkg/afmpeg/doc.go`](pkg/afmpeg/doc.go) · published reference: [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg)
- Local dev: `just` (build) · `just test` · `just ci` · `just docs-serve`
