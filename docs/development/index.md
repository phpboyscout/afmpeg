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
