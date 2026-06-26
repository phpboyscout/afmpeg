# afmpeg

> **Provisional working name** (*afero + ffmpeg*). Rename freely — it appears in
> `go.mod`, the import paths, and a couple of docs.

**A pure-Go FFmpeg binding that runs on a virtual / in-memory filesystem.** No CGO,
no host FFmpeg install, no temp files: FFmpeg is embedded as a WebAssembly module and
executed via [wazero](https://wazero.io/) (a zero-dependency, pure-Go WASM runtime),
with its I/O bridged to an [`afero.Fs`](https://github.com/spf13/afero) — so inputs
and outputs can live entirely in memory (or any afero backend), and the whole thing
cross-compiles to a single static binary.

> **Status: scaffold / intent — the build has not started.** This repo is the design
> + requirements for a future build. **Before any implementation, read
> [`docs/development/specs/0001-afmpeg.md`](docs/development/specs/0001-afmpeg.md)** —
> the full thesis, approach, requirements, API sketch, the FFmpeg-WASM build pipeline,
> and the open questions (licensing, wasm-threads/perf). It is the source of truth.

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

`afmpeg` is the **"wazero + WASM done right"** option: a maintained FFmpeg-WASM build
with the codecs/filters we need, a first-class **afero virtual-filesystem** I/O layer,
and a clean Go API — so a consumer (keryx, or anyone) can transcode / filter / mux
**entirely in memory, pure Go**.

Until `afmpeg` is feasible, keryx renders **local-filesystem-only** (in-memory render
locked out). `afmpeg` reaching usable status is what lifts that lock-out.

## Quick links

- Design + requirements: [`docs/development/specs/0001-afmpeg.md`](docs/development/specs/0001-afmpeg.md)
- Intended API: [`pkg/afmpeg/doc.go`](pkg/afmpeg/doc.go)
