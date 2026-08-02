---
title: Limitations — what afmpeg does not do
description: The things afmpeg cannot do, will not do, or does only under conditions — combinations that are rejected, capabilities that live in one backend only, and absences that are deliberate.
date: 2026-08-02
tags: [reference, limitations, constraints, unsupported]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Limitations — what afmpeg does not do

Everything on this page is a deliberate boundary or a known constraint, not a bug and not a
to-do list. If you are about to ask "can afmpeg do X", start here.

## What afmpeg cannot do at all

**No network I/O.** The engine reaches nothing but the filesystem you hand it. You cannot give
it an `https://` or `rtmp://` input, and it cannot publish a stream. HLS and DASH work as
*packaging* — the playlists and segments are written into your `afero.Fs`, and delivering them
is your problem. Anything remote you must fetch yourself and write into the filesystem first.

**No hardware acceleration.** There is no NVENC, VAAPI, QSV or VideoToolbox path on either
backend. Every encode and decode is software.

**No device or capture input.** No cameras, no screen capture, no `/dev/video0`. There is
nothing to open.

**No symbolic or hard links.** The filesystem bridge returns `ENOSYS` for `link`, `symlink` and
`readlink`, and for `chmod` and `utimens`. A workflow that resolves inputs through a symlink
will not work.

**No shelling out.** afmpeg does not run a host `ffmpeg` and cannot be pointed at one — that is
the whole reason it exists. Command-line syntax is not accepted either: jobs are described as
a `Command` (or the JSON job spec), not as an ffmpeg argv.

**No CLI.** afmpeg is a library. There is no `afmpeg` binary to install, and the `afmpeg-bench`
command in the repository is a benchmark harness, not a general tool.

## What is not supported in the WASM backend

The WASM module is the default and the only sandboxed backend. Two things it will never have:

**HEVC and AV1 *encode*.** `libx265` and `libsvtav1` are impractical single-threaded, so they
exist only in the native driver's **full** profile. AV1 *decode* is available in both runtimes
from the intermediate profile onward. There is no WASM `full` module, and `WithModuleRelease`
refuses `ProfileFull` outright rather than fetching something that would fail later.

**Threads.** The module is single-threaded, which is where the large encode-speed gap with the
native driver comes from. Raising the memory limit does not change this.

## What is not supported in the native backend

**Anything but linux/amd64.** Drivers are published for that platform only. `NewFromRelease`
resolves the asset from the host's `GOOS`/`GOARCH`, so on macOS or arm64 it fails on a missing
asset. Use the WASM module there.

**Engine-reported progress.** The native driver reports byte-observed progress only —
`Fraction`, `InputBytes` and `OutputBytes` work, but `Frame`, `OutTime` and `Speed` stay zero,
because the `/dev/afmpeg-progress` device is served by the WASM backend. On an encode-heavy job
that also means `Fraction` falls back to the byte source, which runs out of signal.

**The sandbox.** The driver is a native subprocess running with your process's privileges, not a
WASM sandbox. `WithMemoryLimit` has no effect on it. This is the trade you are making when you
opt in, and it is why the default stays WASM.

## Combinations that are rejected

| You asked for | What happens |
|---|---|
| `SeekAccurate` on an input feeding a **copied** stream | rejected in Go by `JobSpec()` — a copy can only cut on keyframes; use `SeekFast` |
| both `Duration` and `End` on one output | rejected in Go — they are mutually exclusive |
| a negative `Duration` or `End` | rejected in Go |
| more than one, or none, of the five `FrameSelect` selectors | rejected in Go |
| `ProfileFull` via `WithModuleRelease` | rejected — native only |
| a variant that is not `lgpl` or `gpl` | rejected before any network access |
| `libx265` on the **lgpl** full driver | rejected by the engine — HEVC is GPL, so it exists only in the `gpl` variant |
| `libx264` on an **lgpl** module | rejected by the engine — use `libopenh264` |

## One job at a time, one engine at a time

**A `Runtime` runs a single invocation at a time.** `Run`, `RunJob`, `Probe` and `Frames` are
safe to call concurrently, but they queue rather than run in parallel. One `Runtime` gives you
safety, not throughput; for parallelism build several. There is no built-in pool.

**A `Runtime` has exactly one engine.** The module (or backend) is fixed at `New`. You cannot
swap it, and you cannot mix profiles or variants within one `Runtime` — a job needing the
intermediate profile and a job needing a GPL encoder need two.

**Passing two module options does not fail.** One silently wins, and it is not necessarily the
last one written. See
[which module does afmpeg run](runtime-options.md#which-module-does-afmpeg-run).

## Things the job spec does not model

- **Chapters can be carried across, not authored.** `Output.Chapters` copies from an input or
  drops them; there is no way to write a chapter list from scratch.
- **Pixel format and output frame rate live in the filtergraph**, not in fields — `format=yuv420p`,
  `fps=30`. The engine derives them from the graph and the encoder.
- **There are no workflow types.** No reel, no timeline, no thumbnail sheet. The builder models
  the structure of an ffmpeg job and stops there; higher-level shapes are yours to compose.
- **`Concat` and `Path` are not combined.** Setting `Concat` makes `Path` inert on that input.

## Progress is best-effort, and says so

- **Samples can be dropped.** Delivery is a non-blocking send, so a slow consumer misses
  intermediate values. You cannot slow the job down by not reading, and you cannot fail it.
- **`Fraction` is not an ETA**, and it is `-1` more often than callers expect — including for
  the whole tail of an encode-bound job on the byte source. `Fraction == 1` is not a completion
  signal.
- **`OutputBytes` counts bytes written, not file size.** A muxer that seeks back to patch a
  header has those bytes counted twice.
- **Progress is per-invocation**, attached to the call's context. There is no runtime-wide
  progress channel.

## Deliberate absences

**No embedded engine.** afmpeg will never bundle a `.wasm`. The engine's licence would follow it
into every consumer, so it stays a separate artifact you choose and fetch. `New` fails with
`ErrNoModule` rather than downloading something behind your back.

**No way to skip verification on the certified path.** `WithModuleRelease` has no insecure flag.
If you want unverified bytes, that is what `WithModuleURL` and `WithModuleFile` are for — and
they are honest about being unverified.

**No automatic retry, and no automatic fallback between backends.** A failed download fails.
A native driver that will not run does not fall back to WASM.

## See also

- [Runtime options](runtime-options.md) — the defaults these constraints sit on
- [The guest filesystem](guest-filesystem.md) — the filesystem-level limits in detail
- [Why a Runtime is capped, deadlined and serialised](../explanation/concepts/safe-defaults.md)
- [Use the native backend](../how-to/use-the-native-backend.md) — the opt-in trade in full
