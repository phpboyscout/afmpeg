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
> specs (0013–0034) with dependencies and prerequisites, and its **Pick-up menu** of the remaining
> trigger-gated work. Phases 0–4 are shipped (through **vocab v9** — job progress), and Phase 5
> (the native backend + matrix, HEVC/AV1, perf) is **largely shipped** too. Current anchors:
> afmpeg **v0.14.0**, ffmpeg-wasi **n9.0.1-1**. What remains
> is all optional/trigger-gated — `0009` CLI, `0030` WASM threads, arm64/darwin native, HW-accel
> encode, `0025`/`0026`.

## Contributor docs

- [CI security scanning](ci-security-scanning.md) — how the MR security gate works
  (govulncheck / osv-scanner / trivy / gitleaks), and the recorded decisions behind the
  osv-scanner ignore + job overrides (incl. the unfixable, unreachable x/crypto advisory).

## Specs

The source of truth. They live in the [project wiki](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/home) — see the
[register](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/home) for all of them, in number order, with their current status.

Start with [`0001-afmpeg`](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0001-afmpeg), the thesis; it decomposes into the
component specs.

They moved out of `docs/` because a spec is a point-in-time decision record: read
for the conclusion it reached, not kept true as the code changes. `docs/` holds only
what does change with the code.

| Also | Scope |
|------|-------|
| [external review — validation & disposition](https://gitlab.com/phpboyscout/afmpeg/-/wikis/external-review/home) | The commissioned external review + our per-finding validation verdicts and spec mapping |

## Method

- **Spec first.** Get a spec to `APPROVED`, then implement against it test-first.
  New specs claim the next number from the [register](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/home) and are published
  to the wiki as `specs/NNNN-<slug>`, not committed to this repository.
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

The runtime has a gated integration suite that loads **real** ffmpeg-wasi modules and
transcodes in memory. It skips unless pointed at a module, and there is one variable per
capability profile (spec [0022](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0022-capability-profiles)):

| Variable | Module |
|---|---|
| `AFMPEG_TEST_FFMPEG_WASI` | a **lean** build (or richer) |
| `AFMPEG_TEST_FFMPEG_WASI_INTERMEDIATE` | an **intermediate** build |

Profiles are cumulative but not interchangeable. Roughly a third of the suite exercises
mpegts, HLS, libopus/libmp3lame/libvpx, yadif, loudnorm, libass burn-in or AV1 decode —
none of which a lean build carries — so those tests need the intermediate module and skip
without it, naming the variable to set. An intermediate build satisfies a lean test, so
setting only the intermediate variable runs everything too.

```sh
just test-integration /path/to/ffmpeg-wasi-lgpl.wasm /path/to/ffmpeg-wasi-intermediate-lgpl.wasm
```

or directly:

```sh
AFMPEG_TEST_FFMPEG_WASI=/path/to/ffmpeg-wasi-lgpl.wasm \
AFMPEG_TEST_FFMPEG_WASI_INTERMEDIATE=/path/to/ffmpeg-wasi-intermediate-lgpl.wasm \
  go test ./pkg/afmpeg/ -run Integration -v
```

Both are published on every [ffmpeg-wasi release](https://gitlab.com/phpboyscout/ffmpeg-wasi/-/releases).

#### Backend B (the native driver)

The `pkg/afmpeg/native` tests drive a native **driver binary** rather than a WASM module, and
a driver varies on two independent axes — the capability profile *and* the licence variant:

| Variable | Driver |
|---|---|
| `AFMPEG_TEST_NATIVE_DRIVER` | lean / lgpl |
| `AFMPEG_TEST_NATIVE_DRIVER_GPL` | lean / gpl |
| `AFMPEG_TEST_NATIVE_DRIVER_INTERMEDIATE` | intermediate / lgpl |
| `AFMPEG_TEST_NATIVE_DRIVER_INTERMEDIATE_GPL` | intermediate / gpl |
| `AFMPEG_TEST_NATIVE_DRIVER_FULL` | full / lgpl |
| `AFMPEG_TEST_NATIVE_DRIVER_FULL_GPL` | full / gpl |

Both axes are ordered, so a richer driver satisfies a poorer requirement and the resolver
picks the least-rich adequate one supplied. In practice **one intermediate/gpl driver runs
every native test**, because gpl is a superset of lgpl and intermediate of lean.

The variant axis is not cosmetic: `cropdetect` carries `cropdetect_filter_deps="gpl"`
upstream, so it is absent from every lgpl build no matter how rich the profile — as is the
`libx264` encoder. A test needing either skips on an lgpl driver rather than failing with a
message about a missing filter.

Put these in a `.env` (the justfile sets `dotenv-load`) and `just test-integration` picks
them up. Full is native-only, per spec
[0022](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0022-capability-profiles) §4 —
there is no WASM-full module.

The runtime provides the `env` setjmp/longjmp host module and the WebAssembly feature set a
real FFmpeg build needs (spec [0004](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0004-runtime-and-api) R-0004-9), so a released
[ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) engine (spec
[0007](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0007-libav-direct-engine)) loads and runs.
