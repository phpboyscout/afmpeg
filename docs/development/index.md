---
title: Development
description: Specs, decision records, and contributor docs for afmpeg.
date: 2026-06-26
tags: [development]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Development

afmpeg follows **spec-driven development**: no implementation change without a spec it
implements. The specs are the authoritative decision record; the code is downstream of
them.

> **Picking up implementation?** Start at the
> [implementation roadmap](implementation-roadmap.md) — the phased build order across all
> specs (0013–0028) with dependencies and prerequisites. Phases 0–4 are shipped
> (v0.4.0–v0.6.0); the remaining Phase 5 work is design-only and trigger-gated.

## Specs

The source of truth. Start with 0001, the thesis; it decomposes into the component specs.

| Spec | Scope |
|------|-------|
| [0001 — afmpeg](specs/0001-afmpeg.md) | The thesis: design, requirements, the decision record (§10) |
| [0002 — wasm-build-pipeline](specs/0002-wasm-build-pipeline.md) | ~~FFmpeg CLI → `wasm32-wasi`~~ **superseded by 0007** |
| [0003 — vfs-bridge](specs/0003-vfs-bridge.md) | The afero.Fs → wazero `sys.FS` adapter (the core) |
| [0004 — runtime-and-api](specs/0004-runtime-and-api.md) | `New`/`Run`/`Probe`/`Close`, the public surface |
| [0005 — command-builder](specs/0005-render-helper-and-keyrx-backend.md) | General ffmpeg command builder (use-case-agnostic; a consumer's reel is built on it) |
| [0006 — hardening-roadmap](specs/0006-hardening-roadmap.md) | **Dispatched**: LGPL build-out + download-cache done; perf → 0008; CLI → 0009; native backend dropped |
| [0007 — libav-direct-engine](specs/0007-libav-direct-engine.md) | The pivot: the `ffmpeg-wasi` libav-direct engine (current FFmpeg, CGO-free) + the job-spec vocabulary |
| [0008 — performance-strategy](specs/0008-performance-strategy.md) | Spike: measure Wasm-encode perf vs native; decide if/which non-threaded lever (RuntimePool, build tuning) is worth it |
| [0009 — afmpeg-cli](specs/0009-afmpeg-cli.md) | Deferred (value-unproven): a job-spec-native `cmd/afmpeg` CLI — never `ffmpeg`-arg-compatible |
| [0010 — signed-release-acquisition](specs/0010-signed-release-acquisition.md) | A certified `WithModuleRelease` path — KMS-signed checksum + provenance verification (BYO `WithModuleURL` stays uncertified) |
| [0011 — wkd-attestation](specs/0011-wkd-attestation.md) | WKD key distribution + the embedded↔WKD cross-check + shared offline rotation key, via `gitlab.com/phpboyscout/signing` |
| [0012 — feature-parity-roadmap](specs/0012-feature-parity-roadmap.md) | Survey of the FFmpeg features ffmpeg-wasi is missing, bucketed by the WASI envelope + licence and dispatched to child specs (toward parity for standalone consumers) |
| [0013 — remux & stream copy](specs/0013-remux-and-stream-copy.md) | `-c copy` packet passthrough + bitstream filters + concat demuxer (fast remux/trim, no re-encode) — child of 0012 |
| [0014 — seeking & time ranges](specs/0014-seeking-and-time-ranges.md) | `-ss/-t/-to` fast + accurate input seek and output cutoffs (clip extraction) — child of 0012 |
| [0015 — container coverage](specs/0015-container-coverage.md) | Native mpegts/hls/dash/flv/avi/gif/ogg/CMAF demuxers + muxers (incl. segmenting output) — child of 0012 |
| [0016 — native codec batch](specs/0016-native-codec-batch.md) | In-tree decoder/encoder allowlist expansion (ac3, pcm family, image, prores, …) — child of 0012 |
| [0017 — native filter batch](specs/0017-native-filter-batch.md) | In-tree filter expansion (loudnorm, select/thumbnail, eq/color, hstack, deinterlace, …) — child of 0012 |
| [0018 — LGPL encoder expansion](specs/0018-lgpl-encoder-expansion.md) | External LGPL/BSD encoder libs into the default variant (Opus, MP3, VP8/9, WebP, Vorbis) — child of 0012 |
| [0019 — text & subtitles](specs/0019-text-and-subtitles.md) | drawtext (freetype) + subtitle burn-in/streams (libass) + a subtitle stream type — child of 0012 |
| [0020 — metadata & chapters](specs/0020-metadata-and-chapters.md) | Probe-read + output-set tags, chapters, disposition, language, cover art — child of 0012 |
| [0021 — frame extraction op](specs/0021-frame-extraction-op.md) | A first-class `frames` op (thumbnails / frames at timestamps / scene-select) — child of 0012 |
| [0022 — build & distribution matrix](specs/0022-build-size-matrix.md) | The governing bundling policy: **lean/intermediate/full** profiles × runtime (WASM/Native) × LGPL/GPL × platform; the codec-set-per-profile every codec spec defers to (R-AF-3) |
| [0023 — HEVC & AV1](specs/0023-hevc-and-av1.md) | Tier-3 heavy codecs: x265 (GPL/full), dav1d AV1 decode (default); AV1 encode deferred — child of 0012 |
| [0024 — input options & formats](specs/0024-input-options-and-formats.md) | **PROPOSED** (external review): activate `inputs[].options`, forced/raw input format, input stream selection |
| [0025 — A/V sync & frame-rate](specs/0025-av-sync-and-framerate.md) | **PROPOSED** (external review, low severity): CFR / vsync policy vs the `fps`-filter workaround |
| [0026 — engine hot-path performance](specs/0026-engine-hot-path-performance.md) | **PROPOSED** (external review): reuse AVFrame, lowest-PTS demux, larger AVIO buffer — code-level companion to 0008 |
| [0027 — runtime security hardening](specs/0027-runtime-security-hardening.md) | **PROPOSED** (external review): wazero memory ceiling (OOM), hard timeout, cJSON guards — protects the untrusted-media thesis |
| [0028 — native backends](specs/0028-native-subprocess-backend.md) | **PROPOSED** (strategic): HW-accel escape hatch — **our native `driver.c` + seekable AVIO/IPC (MUST)** + a deferred local-ffmpeg-via-HTTP path (MAY); WASM stays default; CGO-free/MIT |
| [external review — validation & disposition](external-review/README.md) | The commissioned external review + our per-finding validation verdicts and spec mapping |

## Method

- **Spec first.** Get a spec to `approved`, then implement against it test-first.
- **Library before CLI.** Logic lives in `pkg/`; any command layer is a thin adapter.
- **Test-first from the spec's contracts**, table-driven with `t.Parallel()`; the
  per-package coverage bar is **≥90%** on new `pkg/` code.
- **Every package carries a `doc.go`.** The package-level documentation lives in a
  dedicated `doc.go` (not scattered above a random file's `package` clause), so the
  package's purpose is discoverable in one place and on `pkg.go.dev`.
- **Docs land with the code, not after.** A change that adds or reshapes a component
  ships its [Diátaxis](https://diataxis.fr/) documentation in the same MR — an
  explanation page for a new component, a how-to for a new task, reference for a new
  config/CLI surface. Docs are part of "done", never an afterthought.
- **Verify before PR:** `just ci` (tidy, generate, test, race, lint).

## Local workflow

```sh
just            # build (tidy + generate + CGO_ENABLED=0 build)
just test       # unit tests with coverage
just test-race  # race detector
just lint       # golangci-lint
just ci         # the full local gate
just docs-serve # preview this site
```

### Integration test (real ffmpeg)

The runtime has a gated integration test that loads a **real** ffmpeg-wasi module and
transcodes in memory. It skips unless pointed at a module:

```sh
AFMPEG_TEST_FFMPEG_WASI=/path/to/ffmpeg-wasi.wasm go test ./pkg/afmpeg/ -run Integration -v
```

The runtime provides the `env` setjmp/longjmp host module and the WebAssembly feature set a
real FFmpeg build needs (spec [0004](specs/0004-runtime-and-api.md) R-0004-9), so a released
[ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) engine (spec
[0007](specs/0007-libav-direct-engine.md)) loads and runs.
