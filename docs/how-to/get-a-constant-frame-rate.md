---
title: Get a constant frame rate from a variable-frame-rate source
description: "Turn variable-frame-rate video, such as phone or screen recordings, into constant-frame-rate output with the audio kept in sync, using the fps and aresample filters."
date: 2026-10-14
tags: [how-to, command, filter, frame-rate, sync]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Get a constant frame rate from a variable-frame-rate source

Put `fps=<rate>` on the video chain and `aresample=async=1` on the audio chain of the
filtergraph. The first gives every output frame the same duration, and the second keeps the
audio on the same timeline as that video:

```go
cmd := afmpeg.NewCommand(
    afmpeg.WithInput("in.mkv"),
    afmpeg.WithFilterComplex("[0:v]fps=25[v];[0:a]aresample=async=1[a]"),
    afmpeg.WithOutput("out.mp4",
        afmpeg.Map("[v]"), afmpeg.Map("[a]"),
        afmpeg.VideoCodec("libopenh264"), afmpeg.AudioCodec("aac")),
)
```

Both filters are in every capability profile, lean included, and behave the same on the WASM
and native backends.

## Why the engine does not do this on its own

The `ffmpeg` command line holds a constant rate for you (`-r`, `-fps_mode cfr`) by dropping and
duplicating frames as it encodes. That machinery lives in the command-line tool, not in the libav
libraries, and the ffmpeg-wasi engine drives the libraries directly. It hands each frame's
timestamp from the filtergraph to the encoder unchanged, so a variable-frame-rate source comes
out variable unless the graph makes it constant
([spec 0025](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0025-av-sync-and-framerate)).

## Spotting a variable-frame-rate source

Do not rely on the frame rate a probe reports. A container can advertise 30 fps while its frames
arrive at 10 fps for a while and 30 fps for another stretch. What gives a VFR source away is
unequal gaps between consecutive frame timestamps. Phone cameras, screen recorders and browser
`MediaRecorder` output are the usual sources.

The audio has the matching failure: a hole in its timestamps, where the recorder dropped samples.
An MP4 muxer records the hole as one very long audio sample. A player that plays samples back to
back closes the hole, and everything after it then plays early.

## What each filter does

- **`fps=25`** drops or duplicates frames so that there is exactly one every 1/25 s, picking each
  output frame by its timestamp. Any rate `fps` accepts works: `fps=30000/1001`, `fps=ntsc`.
- **`aresample=async=1`** fills holes in the audio with silence and trims overlaps, so the samples
  line up with their timestamps. A value above 1 additionally lets it stretch or squeeze the audio
  by up to that many samples per second, for gradual drift rather than holes.

Both filters work from the source timestamps, so the video and audio stay on the same timeline.
That is what keeps them in sync, and why you want both: `fps` alone gives you constant-rate video
beside audio that still has the hole in it.

## What it does to a real source

The integration test `TestIntegration_VFRToCFR` builds a 3-second source whose video runs at
10 fps, then 30 fps from 1s to 2s, then 10 fps again, with audio missing from 1.0s to 1.5s. It
then transcodes it to H.264 and AAC in MP4, with and without the two filters:

| | Video frame durations | Audio |
|---|---|---|
| `null` / `anull` | 50 frames: 0.033s, 0.034s and 0.100s | one sample 0.628s long where the hole is |
| `fps=25` / `aresample=async=1` | 73 frames, every one 0.040s | every sample but the last 0.128s; the hole is silence |

The results are the same on both backends, measured against ffmpeg-wasi `n9.0.1-6`.

## Where it fits in a larger graph

Put `fps` and `aresample` last, just before the pads the output maps, so nothing after them can
make the timing irregular again. A filter that rewrites timestamps, such as `setpts`, undoes a
constant rate if it comes after `fps`.

A stream copy (`CodecCopy`) cannot change its frame rate, since its packets are passed through
without being decoded. Frame-rate conversion always means re-encoding.

## Cost

Duplicated frames are encoded like any others, so converting 10 fps material to 30 fps roughly
triples the encode work for that stretch. On the WASM backend, which encodes on a single thread,
that is the expensive part.

## See also

- [Compose a command with the builder](compose-a-command.md)
- [Limitations](../reference/limitations.md): what the job spec does and does not model
