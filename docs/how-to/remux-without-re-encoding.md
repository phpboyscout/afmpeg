---
title: Remux without re-encoding (stream copy)
description: Change a file's container, or join like-codec segments, by copying the streams packet-for-packet — no decode, no encode, no quality loss.
date: 2026-07-02
tags: [how-to, command, copy, remux]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Remux without re-encoding (stream copy)

Most container changes don't need a transcode. Moving `mp4 → mkv`, or joining segments that
already share a codec, is a **stream copy**: read the demuxed packets, optionally fix their
bitstream framing, and mux them straight back out — no decode, no encode. It's fast, lossless,
and needs *no codec at all*, so it works for any codec in either licence variant, including
streams the engine can't itself decode (spec [0013](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0013-remux-and-stream-copy)).

The mechanism: name the input streams to copy in `Map` with **unbracketed specifiers**
(`"0:v"`, `"0:a:0"`, `"0:0"` — an input index, a media type, an optional per-type index) and set
the codec to the **`CodecCopy`** sentinel. Bracketed labels (`"[vout]"`) still mean graph pads
that get encoded, so copy and transcode mix freely in one job.

## Change the container, keep the bytes

```go
cmd := afmpeg.Command{
    Inputs: []afmpeg.Input{{Path: "in.mp4"}},
    Outputs: []afmpeg.Output{{
        Path:       "out.mkv",
        Map:        []string{"0:v", "0:a"},
        VideoCodec: afmpeg.CodecCopy,
        AudioCodec: afmpeg.CodecCopy,
    }},
}
_, err := rt.RunJob(ctx, fs, cmd)
```

The video and audio are copied through byte-for-byte; only the wrapper changes. Any bitstream
filter the target container requires (for example H.264's NAL framing differs between mp4 and
MPEG-TS) is **auto-inserted by the muxer** — you don't have to reason about it for the common
case.

## Copy one stream, re-encode another

Because an unbracketed `Map` entry means copy and a bracketed one means a graph pad, a single
output can do both — here the video is remuxed untouched while the audio is normalised and
re-encoded:

```go
cmd := afmpeg.Command{
    Inputs:        []afmpeg.Input{{Path: "in.mp4"}},
    FilterComplex: "[0:a]loudnorm[aout]",
    Outputs: []afmpeg.Output{{
        Path:       "out.mp4",
        Map:        []string{"0:v", "[aout]"},
        VideoCodec: afmpeg.CodecCopy, // copied
        AudioCodec: "aac",            // encoded from the [aout] pad
    }},
}
```

## Join like-codec segments (concat)

`WithConcatInput` presents a playlist of like-codec files as one continuous input via the
concat **demuxer** — a stream-copy join, distinct from the `concat` filter (which decodes and
re-encodes):

```go
cmd := afmpeg.NewCommand(
    afmpeg.WithConcatInput("part0.ts", "part1.ts", "part2.ts"),
    afmpeg.WithOutput("joined.ts",
        afmpeg.Map("0:v"), afmpeg.Map("0:a"),
        afmpeg.VideoCodec(afmpeg.CodecCopy), afmpeg.AudioCodec(afmpeg.CodecCopy)),
)
```

The segments must share codec and parameters — that's the demuxer's requirement, and a mismatch
surfaces as a typed error. MPEG-TS is the natural concat-copy source (its packets carry
continuous timestamps); joining mp4 segments with audio can hit mp4's priming-sample timestamp
discontinuity at a boundary, so concat-copy is happiest on stream-friendly containers.

## Override or disable the bitstream filter

The muxer's auto-insertion is right for the common case. When you need to force a specific
filter (or force *none*), set it per copied stream, keyed by the `Map` entry:

```go
afmpeg.WithOutput("out.ts",
    afmpeg.Map("0:v"), afmpeg.VideoCodec(afmpeg.CodecCopy),
    afmpeg.BitstreamFilter("0:v", "h264_mp4toannexb"), // explicit; "none" disables
)
```

## Notes

- **Copy can only cut on keyframes.** Trimming a copied stream to an exact frame needs a decode
  the copy path skips; for a frame-accurate cut re-encode with `SeekAccurateTo`
  ([extract a clip](extract-a-clip.md), spec [0014](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0014-seeking-and-time-ranges)).
- **Container reach.** Copy lands on mp4/mkv/webm in the lean profile; MPEG-TS, HLS/DASH
  segmenting, and fragmented MP4 are available in the intermediate profile (spec
  [0015](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0015-container-coverage)) — see
  [package for streaming](package-for-streaming.md).
- **Licensing.** Copy touches no codec library, so it stays in the LGPL default and even reaches
  codecs the engine doesn't ship a decoder or encoder for.
