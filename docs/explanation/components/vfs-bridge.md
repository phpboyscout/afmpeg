---
title: The vfs bridge
description: How afmpeg routes a WebAssembly guest's filesystem syscalls onto an afero.Fs, with no host disk access.
date: 2026-06-27
tags: [explanation, components, vfs, wazero, afero]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# The vfs bridge

The vfs bridge (`internal/vfs`) is the heart of afmpeg and the one thing every
other Go ffmpeg binding lacks: it presents the caller's
[`afero.Fs`](https://github.com/spf13/afero) to the WebAssembly guest as wazero's
[`experimental/sys.FS`](https://pkg.go.dev/github.com/tetratelabs/wazero/experimental/sys),
so the guest ffmpeg's WASI filesystem syscalls read and write the caller's
filesystem — including a fully in-memory `MemMapFs` — without touching the host
disk. It implements [spec 0003](../../development/specs/0003-vfs-bridge.md).

## Where it sits

```
caller's afero.Fs ──► internal/vfs (sys.FS adapter) ──► wazero ──► ffmpeg.wasm
   MemMapFs / OsFs        path_open, fd_read, fd_write, fd_seek, …      (guest)
```

The guest issues POSIX-shaped WASI calls; wazero routes them to a mounted
`sys.FS`; afmpeg's adapter turns each one into the equivalent `afero.Fs` /
`afero.File` operation. The adapter holds no runtime state and knows nothing
about ffmpeg — it is a pure translation layer, which is what makes it testable
in isolation against the `sys.FS` contract.

## Faithful syscall semantics

The adapter mirrors wazero's own `os.File` behaviour rather than inventing its
own, so the guest cannot tell it apart from a real filesystem:

- **EOF is `n == 0` with a zero `Errno`**, not an error — matching the POSIX
  `read` convention wazero expects.
- **Zero-length reads and writes short-circuit** to `(0, 0)`.
- **Errors are mapped through wazero's own `UnwrapOSError`**, so an afero
  `*os.PathError` becomes the correct `Errno` (`ENOENT`, `EEXIST`, `EISDIR`, …)
  the guest would see on a host filesystem.
- **`Unlink` and `Rmdir` are POSIX-faithful**: unlinking a directory returns
  `EISDIR`, removing a non-directory with rmdir returns `ENOTDIR`.

## Seek-on-write: the de-risking case

The highest-risk behaviour, and the first test written, is **seek-on-write**.
The mp4 muxer under `-movflags +faststart` writes the media data, then seeks
back and overwrites the `moov` atom header with its final size. If the bridge
could not seek backwards and overwrite over an `afero.Fs`, the whole approach
would be unworkable. It can, and the round-trip — write placeholder → append
payload → seek back → overwrite → read back — is verified against `MemMapFs`,
`BasePathFs`, and `OsFs`. This is gate **G1** in the
[execution plan](../../development/specs/0003-vfs-bridge.md).

## The synthetic overlays (`/tmp` and the device files)

A guest ffmpeg expects a few POSIX locations that a bare `afero.Fs` does not
provide. The bridge overlays them on top of the caller's filesystem:

- **`/tmp`** is routed to an isolated in-memory scratch filesystem, so the
  guest's temporary writes never pollute the caller's `afero.Fs`. (Callers can
  supply their own scratch fs to inspect what the guest wrote.)
- **`/dev/null`** is a discard sink: writes succeed and vanish, reads report
  EOF.
- **`/dev/urandom`** (and `/dev/random`) serve cryptographically-random bytes
  from the host's `crypto/rand`. This one is not a convenience — it is
  **load-bearing**, and the reason is a genuine WASI gotcha (below).

Everything else resolves against the caller's filesystem.

### Why `/dev/urandom` is load-bearing

Several libav muxers need a random identifier — the Matroska muxer, for
instance, gives each track a random UID, which means seeding a PRNG during muxer
init. That PRNG is seeded by libavutil's `av_get_random_seed()`.

In the wasm build, `av_get_random_seed()` has exactly **one** compiled entropy
source: reading `/dev/urandom` (the Windows, arc4random, gcrypt and openssl paths
are all configured out). When that read fails, it falls back to
`get_generic_seed()` — a loop that harvests entropy from **`clock()` jitter** and
only exits once the process clock has advanced far enough. Under WASI there is no
`/dev/urandom`, *and* `clock()` does not advance, so the fallback **never
returns**. The symptom is stark: any attempt to write a Matroska/WebM file hangs
forever — the muxer stalls in init, seeding, before it emits its first byte (the
loop that would *consume* the random UID is never even reached).

Serving `/dev/urandom` from the vfs closes the hole at its source: the very first
entropy read succeeds with real randomness, `av_get_random_seed()` returns
immediately, and the fallback loop is never reached. It fixes not just Matroska
but *every* format that needs a random seed. This is the recurring shape of WASI
work — it is a smaller world than POSIX, and each missing device is a place a
"portable" C fallback can quietly misbehave (see also ffmpeg-wasi's
[build shims](https://ffmpeg-wasi.phpboyscout.uk/explanation/the-build/), which
fill the *link-time* gaps the same spirit fills here at runtime).

## The no-host-filesystem guarantee

The adapter never calls the `os` package directly — it only invokes methods on
the injected `afero.Fs`. With a `MemMapFs` and no host preopens, a guest write
resolves entirely in memory; a test asserts a canary write reaches the in-memory
fs and is absent from the host disk. This is the property (R-AF-2) that lets
keryx hand afmpeg an in-memory worktree and render without a local checkout.

## What lives elsewhere

The bridge is deliberately runtime-agnostic. Mounting it into a wazero module
(`WithSysFSMount`) and driving an actual guest is the job of the afmpeg runtime
([spec 0004](../../development/specs/0004-runtime-and-api.md)), which composes
this package with the embedded `ffmpeg.wasm`. The end-to-end test that exercises
the bridge *through* a real WASI host therefore lands with that runtime; the
contract tests here drive the exact `sys.FS` / `sys.File` methods wazero
invokes.

The Go API is documented at
[pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg/internal/vfs) and is
not duplicated here.
