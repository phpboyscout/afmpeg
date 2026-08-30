---
title: Read a raw or headerless input (forced format & demuxer options)
description: Decode raw video or PCM that carries no probeable header by forcing the demuxer and supplying its geometry, and pass options to any demuxer.
date: 2026-07-03
tags: [how-to, command, input, raw]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Read a raw or headerless input (forced format & demuxer options)

Some inputs carry no header for the demuxer to probe: raw `.yuv` video, raw `.pcm` audio, a
stream with a misleading extension. For those, name the demuxer with `InputFormat` and hand it
the geometry it can't infer through `DemuxerOption` (spec
[0024](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0024-input-options-and-formats)).

## Raw video

A raw `rawvideo` stream is pixels and nothing else, so the demuxer needs the size, pixel format,
and frame rate:

```go
cmd := afmpeg.NewCommand(
    afmpeg.WithInput("frames.yuv", afmpeg.InputFormat("rawvideo"),
        afmpeg.DemuxerOption("video_size", "1280x720"),
        afmpeg.DemuxerOption("pixel_format", "yuv420p"),
        afmpeg.DemuxerOption("framerate", "25")),
    afmpeg.WithFilterComplex("[0:v]null[v]"),
    afmpeg.WithOutput("out.mp4", afmpeg.Map("[v]"), afmpeg.VideoCodec("libopenh264")),
)
```

## Raw audio

Raw PCM is the same shape: a sample format demuxer (`s16le`, `f32le`, …) plus the rate and
channel layout:

```go
afmpeg.WithInput("tone.pcm", afmpeg.InputFormat("s16le"),
    afmpeg.DemuxerOption("sample_rate", "48000"),
    afmpeg.DemuxerOption("ch_layout", "mono"))
```

## Forcing the demuxer for a mislabelled file

`InputFormat` also overrides auto-probing when a container's extension would mislead it, such as an mp4
served as `clip.bin`, say:

```go
afmpeg.WithInput("clip.bin", afmpeg.InputFormat("mp4"))
```

## Options are checked, not swallowed

A `DemuxerOption` the demuxer doesn't recognise is a **typed error**, not a silent no-op, so a typo
or a wrong-demuxer option fails loudly rather than producing quietly-wrong output. That is the
same "exactly as capable as it claims" posture the whole job spec takes.

## Selecting a specific stream

When an input has several streams of one type, index the one you want in a filtergraph pad:
`[0:v:1]` feeds the graph from the **second** video stream (the unindexed `[0:v]` takes the best
one). The same `in:type:index` grammar names a
[copied](remux-without-re-encoding.md) stream in `Map`.
