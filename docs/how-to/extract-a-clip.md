---
title: Extract a clip (seek and time ranges)
description: Cut a time window out of a source — a cheap keyframe cut, a frame-accurate one, or a no-re-encode copy-trim — without decoding from the start of the file.
date: 2026-07-03
tags: [how-to, command, seek, clip]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Extract a clip (seek and time ranges)

"Give me 5 seconds from 0:12" should not cost a decode of the first 12 seconds. `Input.Seek`
jumps the demuxer straight to the target (mirroring ffmpeg's `-ss`), and `Output.Duration` /
`Output.End` stop the output early (`-t` / `-to`) — so a clip costs roughly its own length,
not the whole file (spec [0014](../development/specs/0014-seeking-and-time-ranges.md)).

## The cheap cut (fast seek — the default)

A fast seek lands on the **keyframe at-or-before** the requested start — nothing before it is
even read. The clip may start a fraction of a second early (up to one GOP); the output is
zero-based on the keyframe actually landed on:

```go
cmd := afmpeg.NewCommand(
    afmpeg.WithInput("in.mp4", afmpeg.SeekTo(12.5)),
    afmpeg.WithFilterComplex("[0:v]null[v]"),
    afmpeg.WithOutput("clip.mp4",
        afmpeg.Map("[v]"), afmpeg.VideoCodec("libopenh264"),
        afmpeg.Duration(5)), // stop after 5 seconds
)
```

## The exact cut (accurate seek)

`SeekAccurateTo` starts the clip at **exactly** the requested time: a fast seek plus
decode-and-discard of the keyframe-to-target gap (a fraction of a GOP — still nowhere near
decoding from the file start):

```go
afmpeg.WithInput("in.mp4", afmpeg.SeekAccurateTo(12.5))
```

## The free cut (copy-trim, no re-encode)

Compose a fast seek with [stream copy](remux-without-re-encoding.md) and the cut costs no
decode *or* encode — the packets from the landing keyframe onward are passed straight through:

```go
cmd := afmpeg.Command{
    Inputs: []afmpeg.Input{{Path: "in.mp4", Seek: &afmpeg.Seek{Start: 12.5}}},
    Outputs: []afmpeg.Output{{
        Path: "cut.mp4", Map: []string{"0:v", "0:a"},
        VideoCodec: afmpeg.CodecCopy, AudioCodec: afmpeg.CodecCopy,
        Duration:   5,
    }},
}
```

A copy can **only cut on keyframes** — that's physics, not a limitation of the API: producing
an exact frame would require the re-encode the copy path skips. Asking for an accurate seek on
a copied stream is therefore a validation error, not a silently-fudged cut.

## Duration vs End, and the timeline

- **`Duration(5)`** — stop after 5 seconds of output (`-t`).
- **`End(17.5)`** — stop at position 17.5 (`-to`). Mutually exclusive with Duration.
- By default the output is **zero-based** — the clip starts at t=0, which is what players and
  consumers expect. On that timeline `End` and `Duration` coincide (the output starts at 0).
- **`CopyTS()`** preserves the source timestamps instead — the clip starts at the seek point's
  PTS, and `End` becomes an absolute source position. Use it when timelines must stay aligned
  across multiple outputs. `Probe`'s `StartSec` reports the resulting start offset.

## Where `trim`/`atrim` still fit

The `trim` filter is not replaced: input seek is the cheap whole-input window, while `trim`
remains the scalpel for windowing **one stream among several** or cutting **mid-graph** (after
a concat or overlay), operating on the already-rebased timeline.
