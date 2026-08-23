---
title: Compose a command with the builder
description: Assemble a media job — inputs, a filtergraph, outputs — as typed data, then run it on the ffmpeg-wasi engine.
date: 2026-06-27
tags: [how-to, command, builder]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Compose a command with the builder

`afmpeg.Command` is a declarative description of a media job for the
[ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) engine — a sequence of inputs, an
optional filtergraph, and a sequence of outputs. It is use-case-agnostic: it models the
*structure* of a job, so it expresses any workflow (transcode, scale, overlay, concat,
thumbnail, audio extract, …). `JobSpec()` renders it to the engine's job spec; `RunJob`
runs it.

There are two equally valid ways to build one.

## As a struct — explicit, inspectable data

Fill the struct directly when you want full control (it's copyable and serialisable — a
pipeline can come from YAML/JSON):

```go
cmd := afmpeg.Command{
    Inputs:        []afmpeg.Input{{Path: "in.mkv"}},
    FilterComplex: "[0:v]scale=1280:-2[vout]",
    Outputs: []afmpeg.Output{{
        Path:          "out.mp4",
        Map:           []string{"[vout]"},
        VideoCodec:    "libx264",
        Options:       map[string]string{"crf": "23"},
        FormatOptions: map[string]string{"movflags": "+faststart"},
    }},
}
```

!!! note "Which H.264 encoder?"
    `libx264` needs the **GPL** module. The default **LGPL** module encodes H.264 via
    `"libopenh264"` instead — swap `VideoCodec` accordingly. Both emit H.264/mp4; libx264 is
    higher quality, openh264 is permissively licensed. See ffmpeg-wasi's variant docs.

## With NewCommand — functional options

```go
cmd := afmpeg.NewCommand(
    afmpeg.WithInput("bg.mp4"),
    afmpeg.WithInput("logo.png"),
    afmpeg.WithFilterComplex("[0:v][1:v]overlay=10:10[v]"),
    afmpeg.WithOutput("out.mp4",
        afmpeg.Map("[v]"), afmpeg.VideoCodec("libx264"), afmpeg.WithOption("crf", "23")),
)
```

Both forms produce the same `Command`.

## Run it

```go
res, err := rt.RunJob(ctx, fs, cmd) // sugar for rt.Run(ctx, fs, string(spec)) from cmd.JobSpec()
if err != nil {
    return err
}
if res.ExitCode != 0 {
    return fmt.Errorf("engine failed: %s", res.Stderr)
}
// Structured results (process status, a probe's info) come back on res.Stdout.
```

`JobSpec()` emits `{op:"process", inputs, filter, outputs}`. The `filter` is the full ffmpeg
`filter_complex` (the engine parses it with libav); each output's `Options` map becomes the
encoder options. See [obtain a module](obtain-a-module.md) for the ffmpeg-wasi release.

## Encoder options

Any encoder setting goes in an output's `Options` map (struct) or via `WithOption` (builder).
Nothing is filtered by afmpeg — the dictionary reaches the encoder as it stands:

```go
cmd := afmpeg.NewCommand(
    afmpeg.WithInput("in.mp4"),
    afmpeg.WithFilterComplex("[0:v]scale=1280:-2[v]"),
    afmpeg.WithOutput("out.mp4", afmpeg.Map("[v]"), afmpeg.VideoCodec("libx264"),
        afmpeg.WithOption("crf", "23"), afmpeg.WithOption("preset", "slow")),
)
```

These are **libav option names**, not ffmpeg command-line ones. `-b:v 300k` on the command line is
`WithOption("b", "300k")` here: the CLI parses the `:v` suffix itself and libav never sees it. A
name no encoder has fails the job rather than being ignored.

!!! note "Not everything on an ffmpeg command line is an encoder option"
    `-movflags` is a **muxer** option — `FormatOption` / `FormatOptions`, as above. `-frames:v` is
    a command-line output limit with no libav equivalent; for stills, use the
    [frames op](extract-frames.md) rather than a `process` job.

## Where do trims, frame rate and pixel format go?

- Output **duration and start** are first-class options — `Duration`, `End`, and input-side
  `SeekTo`/`SeekAccurateTo` ([extract a clip](extract-a-clip.md)) — not a filtergraph `trim`.
  Pixel/sample format and output framerate live in the **filtergraph** (e.g. `format=yuv420p`,
  `fps=30`); the engine derives the container from the output path and the pixel/sample format
  from the graph + encoder.
- A higher-level workflow (a "reel", a thumbnail sheet, …) is *your* code composed on this
  builder — afmpeg ships no opinionated workflow types.

Every field, its default and the combinations that are rejected are in the
[command reference](../reference/command.md).
