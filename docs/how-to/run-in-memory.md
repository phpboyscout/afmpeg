---
title: Run ffmpeg over an in-memory filesystem
description: Transcode with afmpeg using an afero.MemMapFs — inputs and outputs stay in RAM, no host disk.
date: 2026-06-27
tags: [how-to, runtime, afero, in-memory]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Run ffmpeg over an in-memory filesystem

This guide runs an ffmpeg invocation whose inputs and outputs live entirely in an
in-memory [`afero.Fs`](https://github.com/spf13/afero) — no host disk, no temp
files. It assumes you already have a wasm FFmpeg module — a released
[ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) engine (see
[obtain a module](obtain-a-module.md)) or another build. It is deliberately **not**
embedded in the package; see the
[licensing posture](../development/specs/0001-afmpeg.md).

## 1. Build a Runtime once

Compiling the module is the expensive step, so build a `Runtime` once and reuse
it. Supply the module with `WithModuleFile`, `WithModuleBytes`, or `WithModuleFS`:

```go
rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile("ffmpeg.wasm"))
if err != nil {
    return err // e.g. afmpeg.ErrNoModule if no module option was given
}
defer rt.Close(ctx)
```

## 2. Put your inputs in an afero filesystem

```go
fs := afero.NewMemMapFs()
_ = afero.WriteFile(fs, "in/clip.mp4", inputBytes, 0o644)
```

Any afero backend works (`OsFs`, `BasePathFs`, …); `MemMapFs` keeps the whole
pipeline in RAM.

## 3. Run, and read the result back

Paths in the arguments resolve against `fs`:

```go
res, err := rt.Run(ctx, fs, "-i", "in/clip.mp4", "-c:v", "libx264", "out/reel.mp4")
if err != nil {
    return err // host-side failure (bad module, cancelled context, …)
}
if res.ExitCode != 0 {
    return fmt.Errorf("ffmpeg failed: %s", res.Stderr)
}

out, _ := afero.ReadFile(fs, "out/reel.mp4") // the encoded mp4, in memory
```

A **non-zero ffmpeg exit is not a Go error** — it is reported in `res.ExitCode`
with the error tail in `res.Stderr`. Only host-side failures return a non-nil
`error`.

## Probe a duration

`Probe` runs an ffprobe-shaped query over the same bridge:

```go
p, err := rt.Probe(ctx, fs, "in/clip.mp4")
// p.DurationSec
```

## Cancel a long render

The `context` passed to `Run` aborts the invocation promptly when cancelled:

```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()
_, err := rt.Run(ctx, fs, /* … */) // returns a context error if it overruns
```

## Notes

- **One invocation at a time per `Runtime`.** Concurrent `Run` calls serialise
  safely; for parallel renders, construct more than one `Runtime` (a pool is on
  the [roadmap](../development/specs/0006-hardening-roadmap.md)).
- The full Go API reference is on
  [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg/pkg/afmpeg).
