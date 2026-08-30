---
title: Run ffmpeg over an in-memory filesystem
description: Transcode with afmpeg using an afero.MemMapFs, so inputs and outputs stay in RAM, no host disk.
date: 2026-06-27
tags: [how-to, runtime, afero, in-memory]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Run ffmpeg over an in-memory filesystem

This guide runs an ffmpeg invocation whose inputs and outputs live entirely in an
in-memory [`afero.Fs`](https://github.com/spf13/afero), with no host disk and no temp
files. It assumes you already have a wasm FFmpeg module: a released
[ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) engine (see
[obtain a module](obtain-a-module.md)) or another build. It is deliberately **not**
embedded in the package; see the
[licensing posture](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0001-afmpeg).

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

## 3. Build a command and run it

Paths resolve against `fs`. Build a `Command` and run it on the engine:

```go
cmd := afmpeg.NewCommand(
    afmpeg.WithInput("in/clip.mp4"),
    afmpeg.WithFilterComplex("[0:v]scale=1280:-2[v]"),
    afmpeg.WithOutput("out/reel.mp4", afmpeg.Map("[v]"), afmpeg.VideoCodec("libx264")),
)
// Using the default LGPL module? Use VideoCodec("libopenh264") — libx264 needs the GPL module.

res, err := rt.RunJob(ctx, fs, cmd)
if err != nil {
    return err // host-side failure (bad module, cancelled context, …)
}
if res.ExitCode != 0 {
    return fmt.Errorf("engine failed: %s", res.Stderr)
}

out, _ := afero.ReadFile(fs, "out/reel.mp4") // the encoded mp4, in memory
```

A **non-zero exit is not a Go error**. It is reported in `res.ExitCode` with the
error tail in `res.Stderr`. Only host-side failures return a non-nil `error`.
(`RunJob` is sugar for `Run(ctx, fs, string(spec))` where `spec` is `cmd.JobSpec()`.)

## Probe a file

`Probe` reports a file's container, duration, and streams via the engine's probe op:

```go
p, err := rt.Probe(ctx, fs, "in/clip.mp4")
// p.Format, p.DurationSec, and p.Streams
//   (each stream: Type, Codec, Width/Height or SampleRate/Channels)
```

## Cancel a long render

The `context` passed to `Run` aborts the invocation promptly when cancelled:

```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()
_, err := rt.Run(ctx, fs, /* … */) // returns a context error if it overruns
```

## Can I run several jobs at once on one Runtime?

Not in parallel. A `Runtime` runs **one invocation at a time**: concurrent `Run` calls are safe
and they serialise, so calling from ten goroutines gives you ten queued jobs rather than ten
running ones. For parallel renders, construct more than one `Runtime`; see
[reuse a Runtime](reuse-a-runtime.md), and
[why it works that way](../explanation/concepts/safe-defaults.md#why-one-invocation-at-a-time).
There is no built-in pool.

## Where the details are

- Every option to `New`, with its default: [runtime options](../reference/runtime-options.md).
- Every field of `Command`/`Input`/`Output`: [command reference](../reference/command.md).
- Every field of `Result` and `Probe`: [results reference](../reference/results.md).
- The per-symbol Go API is on
  [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg/pkg/afmpeg).
