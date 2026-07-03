---
title: Package for streaming (MPEG-TS, HLS, fragmented MP4)
description: Write broadcast and adaptive-streaming containers — an MPEG-TS remux, an HLS segment set, a fragmented MP4 — to an in-memory filesystem, no network.
date: 2026-07-03
tags: [how-to, command, containers, hls, streaming]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Package for streaming (MPEG-TS, HLS, fragmented MP4)

The intermediate-profile engine (spec [0015](../development/specs/0015-container-coverage.md))
writes the web-delivery containers — MPEG-TS, HLS, DASH, fragmented MP4 — all to the mounted
`afero.Fs`, with **no network**: segment files and playlists are written as files for you to
serve however you like.

Two knobs drive it: **`OutputFormat`** forces a muxer the path extension wouldn't imply, and
**`FormatOption`** passes options to that muxer (as opposed to `WithOption`, which configures the
encoder).

## Remux to MPEG-TS, no re-encode

Container conversion to TS is a [stream copy](remux-without-re-encoding.md) — the engine
auto-inserts the `h264_mp4toannexb` bitstream filter TS needs:

```go
cmd := afmpeg.Command{
    Inputs: []afmpeg.Input{{Path: "in.mp4"}},
    Outputs: []afmpeg.Output{{
        Path: "out.ts", Map: []string{"0:v", "0:a"},
        VideoCodec: afmpeg.CodecCopy, AudioCodec: afmpeg.CodecCopy,
    }},
}
```

Because TS segments carry continuous timestamps, they also **concat-copy cleanly** — the full
audio+video join that mp4's audio priming makes impossible: `WithConcatInput("a.ts", "b.ts")`
with copy codecs.

## HLS: one output, a set of files

An HLS output is a single `outputs[]` entry whose path is the **playlist**; the muxer writes the
segment files itself:

```go
cmd := afmpeg.NewCommand(
    afmpeg.WithInput("in.mp4"),
    afmpeg.WithFilterComplex("[0:v]null[v]"),
    afmpeg.WithOutput("stream.m3u8", afmpeg.Map("[v]"),
        afmpeg.OutputFormat("hls"), afmpeg.VideoCodec("libopenh264"),
        afmpeg.FormatOption("hls_time", "4"),
        afmpeg.FormatOption("hls_segment_filename", "seg_%03d.ts"),
        afmpeg.FormatOption("hls_list_size", "0")),
)
_, _ = rt.RunJob(ctx, fs, cmd)
// fs now holds stream.m3u8 + seg_000.ts, seg_001.ts, …
```

The result entry for a segmenting output carries `segmented: true`; the segments are discoverable
on the fs by the pattern you gave. DASH is the same shape with `OutputFormat("dash")` and a
`.mpd` manifest.

## Fragmented MP4 / CMAF

The `mp4` muxer fragments via `movflags` — passed as a **format option**, not an encoder one:

```go
afmpeg.WithOutput("frag.mp4", afmpeg.Map("[v]"), afmpeg.VideoCodec("libopenh264"),
    afmpeg.FormatOption("movflags", "+frag_keyframe+empty_moov+default_base_moof"))
```

## Which build has these?

The container batch lands in the **intermediate** profile (`ffmpeg-wasi-intermediate-<variant>.wasm`),
not the minimal lean build — see [profiles](https://ffmpeg-wasi.phpboyscout.uk/reference/variants/).
Point `WithModuleFile`/`WithModuleRelease` at an intermediate module to use them.
