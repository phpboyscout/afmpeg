---
title: Read & write metadata and chapters
description: Read container/stream tags and chapters with Probe, and set them on an output with afmpeg.
date: 2026-07-04
tags: [how-to, metadata, chapters]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Read & write metadata and chapters

afmpeg reads and writes container-level tags, per-stream language/disposition/tags, and
chapters (spec [0020](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0020-metadata-and-chapters)). Writing tags and
chapters works in any profile; it rides the mux, so it pairs naturally with a
[stream copy](remux-without-re-encoding.md).

## Read what's there

[`Probe`](run-in-memory.md) surfaces the container tags, its chapters, and per-stream
metadata:

```go
p, err := rt.Probe(ctx, fs, "in.mkv")
// p.Tags["title"], p.Tags["artist"]        — container tags
// p.Chapters[i].Start / .End / .Title      — chapters (seconds + title)
for _, s := range p.Streams {
    // s.Language, s.Disposition (["default"], ["forced"], …), s.Tags
}
```

## Write container tags

Set `Output.Metadata`; a copy is enough, no re-encode needed:

```go
cmd := afmpeg.Command{
    Inputs: []afmpeg.Input{{Path: "in.mp4"}},
    Outputs: []afmpeg.Output{{
        Path:       "out.mp4",
        Map:        []string{"0:v", "0:a"},
        VideoCodec: afmpeg.CodecCopy,
        AudioCodec: afmpeg.CodecCopy,
        Metadata:   map[string]string{"title": "My Film", "artist": "Me"},
    }},
}
res, err := rt.RunJob(ctx, fs, cmd)
```

## Set per-stream language, disposition, and tags

`Output.StreamMetadata` is keyed by the stream's `Map` entry (a graph pad like `"[vout]"` or
a copied input stream like `"0:a"`):

```go
Outputs: []afmpeg.Output{{
    Path:       "out.mkv",
    Map:        []string{"0:v", "0:a"},
    VideoCodec: afmpeg.CodecCopy,
    AudioCodec: afmpeg.CodecCopy,
    StreamMetadata: map[string]afmpeg.StreamMeta{
        "0:a": {
            Language:    "eng",
            Disposition: []string{"default"},
            Tags:        map[string]string{"title": "Commentary"},
        },
    },
}},
```

## Carry chapters across

`Output.Chapters` is a passthrough directive: `"copy"` carries the first input's chapters
onto the output, an input index (`"1"`) picks another input, and `""` / `"none"` drops them.
Authoring chapters inline is not supported.

```go
Outputs: []afmpeg.Output{{
    Path:       "out.mp4",
    Map:        []string{"0:v", "0:a"},
    VideoCodec: afmpeg.CodecCopy,
    AudioCodec: afmpeg.CodecCopy,
    Chapters:   "copy",
}},
```

## See also

- [Compose a command with the builder](compose-a-command.md): the `Command`/`Output` shape.
- [Remux without re-encoding](remux-without-re-encoding.md): the copy path these examples build on.
