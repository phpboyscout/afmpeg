---
title: Compose an ffmpeg command with the builder
description: Assemble any ffmpeg invocation — inputs, a filtergraph, outputs — as typed data, instead of hand-writing arg strings.
date: 2026-06-27
tags: [how-to, command, builder]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Compose an ffmpeg command with the builder

`afmpeg.Command` is a declarative description of an ffmpeg invocation — global
options, a sequence of inputs, an optional `-filter_complex` graph, and a sequence
of outputs. It is use-case-agnostic: it models ffmpeg's *structure*, so it
expresses any workflow (transcode, scale, overlay, concat, thumbnail, audio
extract, …). `Run(fs, args...)` stays the universal primitive; the builder is
ergonomic sugar over it.

There are two equally valid ways to build one.

## As a struct — explicit, inspectable data

Fill the struct directly when you want full control (it's comparable, copyable,
and serialisable — a pipeline can come from YAML/JSON):

```go
cmd := afmpeg.Command{
    Inputs:        []afmpeg.Input{{Path: "in.mkv"}},
    FilterComplex: "scale=1280:-2",
    Outputs: []afmpeg.Output{{
        Path: "out.mp4", VideoCodec: "libx264", AudioCodec: "aac",
        Raw: []string{"-crf", "23"}, // escape hatch for any untyped flag
    }},
}
```

A zero-value `Command{}` bakes no defaults — it renders exactly what you set.

## With NewCommand — sane defaults + options

`NewCommand` applies sane defaults (`-y` overwrite and `-loglevel error`) plus
functional options, for callers who want the `With*` feel or the baked defaults:

```go
cmd := afmpeg.NewCommand(
    afmpeg.WithInput("bg.mp4"),
    afmpeg.WithInput("logo.png", afmpeg.Loop(), afmpeg.Duration(5)),
    afmpeg.WithFilterComplex("[0:v][1:v]overlay=10:10[v]"),
    afmpeg.WithOutput("out.mp4", afmpeg.Map("[v]"), afmpeg.VideoCodec("libx264")),
)
```

Both forms render the same args (`cmd.Args()`); the struct can express anything the
constructor produces, defaults included.

## Run it

```go
res, err := rt.RunCommand(ctx, fs, cmd) // sugar for rt.Run(ctx, fs, cmd.Args()...)
if err != nil {
    return err
}
if res.ExitCode != 0 {
    return fmt.Errorf("ffmpeg failed: %s", res.Stderr)
}
```

## The escape hatch

The struct carries every common field; for any flag without a typed field, use the
`Raw` slice at the right scope — `Global.Raw`, an input's `Raw`, or an output's
`Raw` (or `GlobalRaw`/`InputRaw`/`OutputRaw` as options). Nothing about a workflow
is ever blocked:

```go
// a single-frame thumbnail
cmd := afmpeg.NewCommand(
    afmpeg.WithInput("in.mp4"),
    afmpeg.WithOutput("thumb.png", afmpeg.OutputRaw("-frames:v", "1")),
)
```

## Notes

- **Ordering is handled for you** — `Args()` always emits globals → inputs →
  filtergraph → outputs, the order ffmpeg requires.
- A higher-level workflow (a "reel", a thumbnail sheet, …) is *your* code composed
  on this builder — afmpeg ships no opinionated workflow types.
- The full Go API reference is on
  [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg/pkg/afmpeg).
