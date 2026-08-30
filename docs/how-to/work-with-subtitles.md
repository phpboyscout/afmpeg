---
title: Work with subtitle tracks
description: Extract, convert, copy, and burn in subtitle streams with afmpeg: sidecar files, embedded tracks, and hard-subs.
date: 2026-07-04
tags: [how-to, subtitles]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Work with subtitle tracks

afmpeg handles subtitles two ways: as a **stream** (transcode or copy a subtitle track,
spec [0019](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0019-text-and-subtitles)) or **burned in** (rendered
into the video pixels). Both need the
[intermediate-profile module](obtain-a-module.md), because the subtitle codecs and the
`subtitles`/`ass`/`drawtext` filters aren't in the lean build.

Name a subtitle stream in `Map` with an `N:s` specifier (`"0:s"` = the first input's first
subtitle stream), then set `Output.SubtitleCodec` to an encoder: `srt`, `webvtt`,
`mov_text` (MP4 timed text), `ass`, or [`CodecCopy`](remux-without-re-encoding.md) to remux
it unchanged.

## Convert to a sidecar file

An output can carry a subtitle stream *alone*, as a standalone `.srt`/`.vtt`:

```go
cmd := afmpeg.Command{
    Inputs:  []afmpeg.Input{{Path: "episode.mkv"}},
    Outputs: []afmpeg.Output{{
        Path:          "episode.vtt",
        Map:           []string{"0:s"},
        SubtitleCodec: "webvtt",
    }},
}
res, err := rt.RunJob(ctx, fs, cmd)
```

## Keep an embedded track

Mux the subtitle alongside video/audio into one container, here a copy-through of video
and audio with the subtitle transcoded to MP4 timed text:

```go
cmd := afmpeg.Command{
    Inputs: []afmpeg.Input{{Path: "in.mkv"}},
    Outputs: []afmpeg.Output{{
        Path:          "out.mp4",
        Map:           []string{"0:v", "0:a", "0:s"},
        VideoCodec:    afmpeg.CodecCopy,
        AudioCodec:    afmpeg.CodecCopy,
        SubtitleCodec: "mov_text", // MP4 wants mov_text, not srt
    }},
}
```

To copy a subtitle stream verbatim (same codec in, same out), set
`SubtitleCodec: afmpeg.CodecCopy`.

## Burn subtitles into the video (hard-subs)

Burning renders the text into the pixels with a filter, so it's a normal `process` job with
a `FilterComplex`, not a subtitle stream. Mount the font and (for file-based subtitles) the
subtitle file in the `afero.Fs`; the sandbox has no system fonts or fontconfig, so point the
filter at a path.

`drawtext` for a text overlay:

```go
cmd := afmpeg.Command{
    Inputs:        []afmpeg.Input{{Path: "in.mp4"}},
    FilterComplex: "[0:v]drawtext=fontfile=font.ttf:text='afmpeg':x=8:y=8:fontcolor=white[v]",
    Outputs:       []afmpeg.Output{{Path: "out.mp4", Map: []string{"[v]"}, VideoCodec: "libx264"}},
}
```

`subtitles` (or `ass`) burns a subtitle file through libass, so pass a fonts directory, since
libass resolves font names:

```go
FilterComplex: "[0:v]subtitles=filename=subs.srt:fontsdir=/fonts:force_style='FontName=DejaVu Sans'[v]",
```

A missing font fails the render cleanly (non-zero `Result.ExitCode`) rather than rendering
blank, so check `res.ExitCode` / `res.Stderr`.

## See also

- [Compose a command with the builder](compose-a-command.md): the `Command`/`Output` shape.
- [Codecs reference](https://ffmpeg-wasi.phpboyscout.uk/reference/codecs/#subtitles-spec-0019):
  the subtitle codec batch the intermediate profile enables.
