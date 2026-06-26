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
| [0002 — wasm-build-pipeline](specs/0002-wasm-build-pipeline.md) | FFmpeg+x264 → `wasm32-wasi`, reproducible build, licence variants |
| [0003 — vfs-bridge](specs/0003-vfs-bridge.md) | The afero.Fs → wazero `sys.FS` adapter (the core) |
| [0004 — runtime-and-api](specs/0004-runtime-and-api.md) | `New`/`Run`/`Probe`/`Close`, the public surface |
| [0005 — render-helper + keyrx-backend](specs/0005-render-helper-and-keyrx-backend.md) | Timeline helper + keyrx `Renderer` adapter |
| [0006 — hardening-roadmap](specs/0006-hardening-roadmap.md) | Deferred: LGPL build-out, perf, native backend, CLI |

## Method

- **Spec first.** Get a spec to `approved`, then implement against it test-first.
- **Library before CLI.** Logic lives in `pkg/`; any command layer is a thin adapter.
- **Test-first from the spec's contracts**, table-driven with `t.Parallel()`; the
  per-package coverage bar is **≥90%** on new `pkg/` code.
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
