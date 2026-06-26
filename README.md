# afmpeg

**A pure-Go FFmpeg binding that runs on a virtual / in-memory filesystem.** No CGO,
no host FFmpeg install, no temp files: FFmpeg is embedded as a WebAssembly module and
executed via [wazero](https://wazero.io/) (a zero-dependency, pure-Go WASM runtime),
with its I/O bridged to an [`afero.Fs`](https://github.com/spf13/afero) — so inputs
and outputs can live entirely in memory (or any afero backend), and the whole thing
cross-compiles to a single static binary.

> **Status: design approved, implementation not started.** This repo holds the
> design + requirements. The thesis is
> [`docs/development/specs/0001-afmpeg.md`](docs/development/specs/0001-afmpeg.md) (the
> source of truth); the five gating decisions in its §10 are resolved, and the work is
> decomposed into component specs **0002–0006**. Read those before implementing.

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

Until `afmpeg` is usable, keryx renders **local-filesystem-only** (in-memory render
locked out). `afmpeg` reaching usable status is what lifts that lock-out.

## How it works

Three layers — the middle one is the novel engineering:

1. **Embedded FFmpeg-WASM module** — FFmpeg + x264 compiled to `wasm32-wasi`, configured
   down to only the codecs/filters real workflows need. Shipped as a **separate
   downloadable artifact, never `//go:embed`-ed** (see *Licensing* below).
2. **The afero ↔ wazero vfs bridge** (the heart) — the guest ffmpeg's WASI filesystem
   syscalls are routed to a mounted `experimental/sys.FS` that afmpeg implements **backed
   by an `afero.Fs`**. The guest's reads and writes hit the caller's filesystem (e.g. an
   in-memory `MemMapFs`) with no host disk touched.
3. **The Go API** — compile the module once into a reusable `Runtime`, then `Run` an
   ffmpeg invocation with its I/O bridged to a caller-supplied `afero.Fs`. A higher-level
   render helper (mirroring keryx's timeline) layers on top.

```go
rt, _ := afmpeg.New(ctx, afmpeg.WithModuleFile("ffmpeg.wasm")) // compile once, reuse
defer rt.Close(ctx)

fs := afero.NewMemMapFs()            // or the caller's in-memory worktree
// ... write inputs into fs ...
res, _ := rt.Run(ctx, fs, "-i", "in/clip.mp4", /* … */, "out/reel.mp4")
out, _ := afero.ReadFile(fs, "out/reel.mp4")   // the result, in memory
```

> The signatures above are the intended shape (spec 0004), not yet implemented.

## Licensing

The Go package is **permissively licensed**. FFmpeg + x264 is GPL, so the full/GPL
`ffmpeg.wasm` is distributed as a **separate downloadable artifact** rather than embedded
— the copyleft obligation attaches only to a consumer who fetches and bundles it, not to
the library. An **LGPL/openh264 variant** is tracked for fully-permissive consumers. x264
is the single GPL component in the target render set; AAC, `xfade`, the mp4 muxer, and the
audio filters are all already LGPL-clean. See spec 0001 §10 (D-C).

## Roadmap

| Spec | Scope |
|------|-------|
| [0001](docs/development/specs/0001-afmpeg.md) | The thesis: design, requirements, the resolved decision record (§10) |
| [0002](docs/development/specs/0002-wasm-build-pipeline.md) | The reproducible FFmpeg → `wasm32-wasi` build pipeline + licence variants |
| [0003](docs/development/specs/0003-vfs-bridge.md) | The afero.Fs → wazero `sys.FS` adapter (the core) |
| [0004](docs/development/specs/0004-runtime-and-api.md) | `New` / `Run` / `Probe` / `Close` — the public API |
| [0005](docs/development/specs/0005-render-helper-and-keyrx-backend.md) | Timeline render helper + keyrx `Renderer` backend |
| [0006](docs/development/specs/0006-hardening-roadmap.md) | Deferred: LGPL build-out, perf (wasm-threads), native backend, CLI |

## Quick links

- Documentation: [`docs/`](docs/index.md) (Diátaxis — tutorials / how-to / reference / explanation)
- Design + decision record: [`docs/development/specs/0001-afmpeg.md`](docs/development/specs/0001-afmpeg.md)
- Intended API: [`pkg/afmpeg/doc.go`](pkg/afmpeg/doc.go) · published reference: [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg)
- Local dev: `just` (build) · `just test` · `just ci` · `just docs-serve`
