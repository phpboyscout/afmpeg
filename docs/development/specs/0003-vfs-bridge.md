# 0003 — the afero ↔ wazero vfs bridge

Status: **DRAFT** (component spec; not started. Implements spec 0001 §3.2 — the heart of
afmpeg. Review before building.)
Date: 2026-06-26
Parent: [0001-afmpeg.md](0001-afmpeg.md) §3.2, §9 (writable-fs risk)
Owns: **R-AF-2** (afero-backed I/O, no host-fs access)

## 1. Purpose

Implement the **novel core**: an adapter that presents an `afero.Fs` to the WASM guest as a
wazero `experimental/sys.FS`, so ffmpeg-in-the-guest's WASI filesystem syscalls
(`path_open`, `fd_read`, `fd_write`, `fd_seek`, `fd_filestat_get`, …) read and write the
caller's afero filesystem — including a fully in-memory `MemMapFs` — with **no host disk
touched**. This is what every other Go ffmpeg-wasm binding lacks (0001 §1, §11) and is the
single most de-risking piece to validate early (0001 §9).

This spec is **independent of the real `ffmpeg.wasm`** (0002): it is tested directly against
the `sys.FS` contract and a thin WASI guest harness, so it can be built in parallel with the
build pipeline.

## 2. Scope

In scope (package `internal/vfs`):
- An `afero.Fs` → `experimental/sys.FS` adapter: open, read, write, seek, stat, readdir,
  truncate, rename, unlink, mkdir, sync, close — the subset ffmpeg's I/O actually exercises.
- **Seek-on-write** correctness — the mp4 muxer rewrites the `moov` atom after writing
  `mdat` (the `+faststart` path); the write path must support seek-back + overwrite. This is
  the highest-risk behaviour (0001 §9) and is validated *first*.
- Synthetic nodes the guest expects: a writable `/tmp` (memfs) and `/dev/null`.
- Path mapping: guest paths (`in/cover.png`, `out/reel.mp4`) resolve against the mounted
  afero root.
- Error mapping: afero/os errors → the correct WASI `sys.Errno` (ENOENT, EBADF, EISDIR, …)
  so the guest sees POSIX-faithful failures, not opaque ones.

Out of scope:
- Runtime instantiation, module compilation, `Run`/`Probe` (0004).
- The real ffmpeg module (0002).
- Network, sockets, clocks (not needed by the render path).

## 3. Design

```
afero.Fs  ──(internal/vfs.Adapter)──►  experimental/sys.FS  ──(wazero mount)──►  WASI guest
 MemMapFs                                  File / Errno                          ffmpeg.wasm
```

- `vfs.New(fs afero.Fs, opts...) sys.FS` returns a `sys.FS` backed by `fs`.
- Each `OpenFile` returns a `sys.File` wrapping the `afero.File`, translating the `sys.File`
  method set (`Read`, `Write`, `Seek`, `Pwrite`, `Pread`, `Stat`, `Truncate`, `Sync`,
  `Datasync`, `Close`) onto the afero handle.
- A small **overlay/mount table** composes the caller's `fs` at `/` (or a configured root)
  with an internal `MemMapFs` for `/tmp` and a null device for `/dev/null`, so the guest's
  scratch writes never escape to the caller's fs unless intended.
- All time/permission metadata is synthesised deterministically where afero lacks it (so the
  in-memory path is reproducible).

### The no-host-fs guarantee (R-AF-2)
The adapter never calls `os` directly. Tests assert this structurally: the only fs handed to
the guest is the injected `afero.Fs`; a test using `MemMapFs` plus a wazero config with **no**
host preopens proves the guest cannot reach the host disk. (A `failingFs` that panics on any
host-path access is the belt-and-braces check.)

## 4. Requirements

- `R-0003-1` A guest performing read → write → **seek-back → overwrite → stat** against a
  `MemMapFs` round-trips correctly (the moov-atom / `+faststart` shape). Validated first.
- `R-0003-2` End-to-end the guest's reads/writes hit only the injected `afero.Fs`; with a
  `MemMapFs` and no host preopens, **no host filesystem access occurs** (R-AF-2), asserted
  in tests.
- `R-0003-3` `/tmp` is a writable memfs and `/dev/null` discards — both available to the
  guest without touching the caller's fs.
- `R-0003-4` afero/os errors map to the correct `sys.Errno`; readdir, rename, unlink, mkdir,
  truncate behave POSIX-faithfully for the render path.
- `R-0003-5` The adapter works against any `afero.Fs` backend (`MemMapFs`, `OsFs`,
  `BasePathFs`), selected by the caller — not just in-memory.
- `R-0003-6` Pure Go, `CGO_ENABLED=0` (R-AF-1, inherited).

## 5. Test strategy (TDD, per library-first-tdd)

- **Contract tests** against `sys.FS`/`sys.File` directly (no ffmpeg): table-driven, the
  full op set, `t.Parallel()`.
- **A minimal WASI guest fixture** — a tiny hand-written `wasm32-wasi` program (or
  `wasmtime`/wazero-run snippet) that does the seek-on-write dance — proves the bridge
  *through an actual WASI host* without needing the 7 MB ffmpeg module. This keeps 0003
  decoupled from 0002.
- **No-host-fs assertion** via `MemMapFs` + a host-denying config + a `failingFs` guard.
- Coverage: ≥90% on `internal/vfs` (the go-tool-base bar; this is the core, so hold the line).

## 6. Definition of done

- `internal/vfs` passes contract + WASI-fixture tests under `go test -race`.
- R-0003-1 (seek-on-write) and R-0003-2 (no-host-fs) demonstrably green — these are the two
  that de-risk the whole project.
- Linter clean (the go-tool-base `.golangci.yaml` set).
- Package doc explains the mount model and the no-host-fs guarantee.

## 7. Risks

- **wazero `experimental/sys.FS` write-path maturity** (0001 §9) — the largest unknown.
  R-0003-1 validates it *before* 0004 depends on it; if a gap exists, surface it here and
  decide (patch/workaround/upstream) rather than discovering it during the end-to-end render.
- **`experimental` API churn** — pin the wazero version; isolate the adapter so an API bump
  is a single-package change.

## 8. Sequencing

Parallel with 0002. Lands the `sys.FS` the runtime mounts; **0004 depends on this interface**.
Start with R-0003-1 (seek-on-write) as the first failing test.
