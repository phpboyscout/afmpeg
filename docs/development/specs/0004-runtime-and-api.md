# 0004 — the runtime & public API (`Run` / `Probe`)

Status: **DRAFT** (component spec; not started. Implements spec 0001 §3.3, §4 and the D-D /
D-E resolutions. Review before building.)
Date: 2026-06-26
Parent: [0001-afmpeg.md](0001-afmpeg.md) §3.3, §4, §10 (D-D, D-E)
Owns: **R-AF-1**, **R-AF-4**, **R-AF-5**, **R-AF-8**; resolves **D-D**, scopes **D-E**

## 1. Purpose

The public `pkg/afmpeg` surface: compile the WASM module once into a reusable `Runtime`, then
`Run` an ffmpeg invocation with its filesystem bridged (via 0003) to a caller-supplied
`afero.Fs`, returning the exit code + captured stderr; plus `Probe` (ffprobe-equivalent
duration). Per **D-E** this is **v1** — raw `Run(ctx, fs, args…)` over a vfs; the general
command builder is 0005.

## 2. Scope

In scope (package `pkg/afmpeg`):
- `New(ctx, opts…) (*Runtime, error)` — compiles the module (the expensive step), held for
  reuse; `Close(ctx)`.
- `Run(ctx, fs, args…) (Result, error)` — mounts `fs` via 0003, runs ffmpeg, captures
  exit/stderr.
- `Probe(ctx, fs, path) (Probe, error)` — duration over the same bridge (R-AF-5).
- Module loading wiring that honours **D-C**: the GPL wasm is **not** `//go:embed`-ed; it is
  supplied as an external artifact (option below).
- Context cancellation that aborts a running invocation (R-AF-8).
- Concurrency model per **D-D**.

Out of scope:
- The vfs adapter internals (0003) — consumed here.
- The general command builder (0005).
- The wasm build (0002).

## 3. API (from 0001 §4, made concrete)

```go
package afmpeg

type Runtime struct { /* compiled wazero module + runtime */ }

type Option func(*config)

// WASM source (D-C: not embedded). Exactly one is required.
func WithModuleFile(path string) Option            // load ffmpeg.wasm from disk (afero or os)
func WithModuleBytes(b []byte) Option              // caller supplies the module bytes
func WithModuleFS(fs afero.Fs, path string) Option // load from an afero fs

func New(ctx context.Context, opts ...Option) (*Runtime, error) // compiles once, reuse
func (r *Runtime) Close(ctx context.Context) error

// Run executes one ffmpeg invocation; paths in args resolve against fs.
func (r *Runtime) Run(ctx context.Context, fs afero.Fs, args ...string) (Result, error)

type Result struct {
    ExitCode int
    Stderr   string // captured guest stderr (the error tail on failure)
}

// Probe mirrors `ffprobe -show_entries format=duration` over the fs bridge.
func (r *Runtime) Probe(ctx context.Context, fs afero.Fs, path string) (Probe, error)

type Probe struct {
    DurationSec float64
}
```

Notes:
- `Run` does **not** error on a non-zero ffmpeg exit by itself — it returns `Result{ExitCode,
  Stderr}` and a nil error for "ffmpeg ran and exited non-zero"; it returns a non-nil error
  for host-side failures (module instantiation, bridge errors, ctx-cancel). The caller
  inspects `ExitCode`. (0005's `Renderer` maps a non-zero exit to a wrapped error, matching
  keyrx's current `errors.Wrapf(err, "ffmpeg: %s", tail(out, 1500))`.) — confirm in review.
- `Probe` is implemented either by running the embedded ffprobe entrypoint if the module
  provides one, or by an ffmpeg `-i` stderr parse — **decision D-0004-A** below.

## 4. Decisions (local)

- **D-0004-A — Probe mechanism.** ffprobe-as-a-second-entrypoint vs parsing ffmpeg `-i`
  stderr vs a tiny container demux. keyrx needs only `format=duration` (`probe.go`).
  *Proposed:* whichever the 0002 module exposes most cheaply; default to parsing
  `format=duration` from the ffprobe entrypoint if built, else ffmpeg `-i` stderr. Resolve
  during implementation against the real module.
- **D-0004-B — concurrency (resolves 0001 D-D).** *Proposed: one invocation at a time per
  `Runtime`* (a guarding mutex; compilation shared, instantiation per-`Run`). A
  `RuntimePool` (N module instances for parallel renders) is a documented follow-up
  (0006), not v1. Rationale: wazero module instances are cheap to instantiate from a shared
  `CompiledModule` but ffmpeg's single-threaded encode means parallelism is bounded by CPU
  anyway; keryx renders one reel at a time.
- **D-0004-C — module acquisition.** Since the GPL wasm is not embedded (D-C), `New` requires
  one of the `WithModule*` options; there is **no implicit default fetch** in v1 (avoids a
  surprise network call). A separate helper (or 0006) may add download-and-cache later.

## 5. Requirements

- `R-0004-1` (R-AF-1) Builds with `CGO_ENABLED=0` and cross-compiles (linux/macOS,
  amd64/arm64) — a hard CI gate.
- `R-0004-2` (R-AF-4) `Run` returns the exit code + stderr; a non-zero exit surfaces
  ffmpeg's error tail — no silent failures.
- `R-0004-3` (R-AF-5) `Probe` returns input duration over the fs bridge (keyrx VO timing).
- `R-0004-4` (R-AF-8) A cancelled `ctx` aborts a running invocation promptly (wazero
  `CloseWithExitCode`/context close), returning a context error.
- `R-0004-5` `New` compiles the module once; `Run` reuses the `CompiledModule` (compilation
  is the expensive step — 0001 §4).
- `R-0004-6` The module is loaded via a `WithModule*` option, never `//go:embed` (D-C);
  the package builds and tests **without** the GPL wasm present (using a stand-in/stub for
  unit tests).
- `R-0004-7` One `Run` at a time per `Runtime` (D-0004-B); concurrent `Run` calls serialise
  safely (no data race — `go test -race`).
- `R-0004-8` The R-AF-3 end-to-end: with a real ffmpeg module, `Run` executes a real ffmpeg
  command against a `MemMapFs` and produces a valid mp4 read back in memory — **no host fs
  access** (composes 0003's R-AF-2).
- `R-0004-9` **(Added 2026-06-27, via the spec-0002 de-risk.)** The runtime can load a real
  ffmpeg.wasm. A real FFmpeg build needs more than plain WASI: it imports an **`env` host
  module** providing `__wasm_setjmp` / `__wasm_longjmp` (FFmpeg uses C setjmp/longjmp, which
  WebAssembly lacks; the build lowers them to host calls), and it requires the WebAssembly
  **core feature set** SIMD + bulk-memory + non-trapping-float + mutable-global +
  reference-types + sign-extension + **extended-const + tail-call**. `New` enables that
  feature set and registers the `env` module (`setjmp.go`), implementing setjmp/longjmp with
  wazero's experimental Snapshotter — original code, no GPL imported.

### Resolved via de-risk (2026-06-27)
R-0004-9 was discovered by wiring an interim real module (go-ffmpreg's stock `ffmpreg.wasm`,
GPL — used only as a runtime test fixture, never a dependency) and finding a plain-WASI
runtime cannot even instantiate it. With the env module + features added, **afmpeg's own
`New`/`Run` transcodes testsrc → an H.264 (libx264) mp4 entirely in a `MemMapFs`, no host fs**
(ffmpeg n5.1.9). This closes the project's biggest integration risk *before* 0002 builds the
real artifact. The validation lives as a gated integration test (`AFMPEG_TEST_FFMPEG_WASM`).

## 6. Test strategy (TDD)

- **Unit** (no real wasm): a stub/stand-in module exercises `New`/`Run`/`Close` wiring,
  option handling, stderr capture, exit-code surfacing, ctx-cancel, and the serialise-on-`Run`
  guard (`-race`). The vfs bridge is 0003 (already tested); here it's composed.
- **Integration** (env-gated via `AFMPEG_TEST_FFMPEG_WASM=/path/to/ffmpeg.wasm`,
  `integration_test.go`): load a real ffmpeg.wasm and run a transcode over `MemMapFs`, assert
  a valid mp4 and no host-fs access. Skips when the env var is unset, so the default unit run
  stays fast and module-free. Implemented and passing against go-ffmpreg's stock build.
- Coverage ≥90% on new `pkg/afmpeg` code (excluding the gated integration path).

## 7. Definition of done

- `pkg/afmpeg` `New`/`Run`/`Probe`/`Close` implemented to the §3 signatures.
- Unit tests green under `-race`; the gated integration test transcodes end-to-end in memory.
- `CGO_ENABLED=0 go build` + cross-compile verified in CI (R-0004-1).
- Package doc + a runnable example (the 0001 §4 usage snippet) compile (`Example` test).
- D-0004-A/B/C confirmed in review and recorded here.

## 8. Sequencing

Depends on **0003** (the `sys.FS` interface) for the bridge and **0002** (the real module)
for the gated end-to-end test — but the unit layer can be built against 0003 + a stub module
before 0002 lands. Blocks **0005**.
