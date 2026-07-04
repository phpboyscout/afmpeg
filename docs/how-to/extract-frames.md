---
title: Extract frames and thumbnails
description: Pull still images from a video with afmpeg's frames op — a single frame, an interval, scene changes, or thumbnails.
date: 2026-07-04
tags: [how-to, frames, thumbnails]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Extract frames and thumbnails

The `frames` op (spec [0021](../development/specs/0021-frame-extraction-op.md)) writes still
images from a video without a full transcode. Drive it with a `FrameJob` and
`Runtime.Frames`; the encoders it uses (`png`, `mjpeg`, `webp`) are in every profile, and the
scene/thumbnail selectors need the intermediate profile's native filters.

A `FrameJob` names the input, a `Path` template with an optional integer token
(`"out/frame_%03d.png"`), an image `Codec`, an optional `Scale`, and exactly one selector in
`FrameSelect`.

## One frame at a timestamp

```go
at := 12.5
job := afmpeg.FrameJob{
    Input:  "in.mp4",
    Select: afmpeg.FrameSelect{Timestamp: &at}, // pointer: &0.0 is the first frame, not "unset"
    Path:   "poster.png",                       // no token: a single frame
    Codec:  "png",
}
res, err := rt.Frames(ctx, fs, job)
// res.Count, res.Frames[i].Path / .Index / .Timestamp
```

`Timestamp` is a `*float64` so that `0` (the first frame) is distinct from an unset selector.

## Several specific timestamps

```go
job := afmpeg.FrameJob{
    Input:  "in.mp4",
    Select: afmpeg.FrameSelect{Timestamps: []float64{0, 30, 60}},
    Path:   "out/shot_%02d.png",
}
```

## A frame every N seconds

```go
Select: afmpeg.FrameSelect{Interval: 5}, // one frame per 5s across the input
```

## Scene changes or representative thumbnails

```go
thresh := 0.4
Select: afmpeg.FrameSelect{SceneThreshold: &thresh} // frames whose scene score exceeds 0.4
// or
Select: afmpeg.FrameSelect{Thumbnail: true}         // representative frames via the thumbnail filter
```

## Scale and cap the output

`Scale` applies ffmpeg scale args to each frame, and `Count` caps how many are emitted:

```go
job := afmpeg.FrameJob{
    Input:  "in.mp4",
    Select: afmpeg.FrameSelect{Interval: 2},
    Path:   "out/thumb_%03d.webp",
    Codec:  "webp",
    Scale:  "320:-2", // 320px wide, height keeps the aspect ratio
    Count:  20,       // stop after 20 frames
}
```

Set **exactly one** selector — `Timestamp`, `Timestamps`, `Interval`, `SceneThreshold`, or
`Thumbnail`. Setting none or more than one is a validation error before the engine runs.

## See also

- [Obtain a module](obtain-a-module.md) — the scene/thumbnail selectors need the intermediate profile.
- [Read a raw or headerless input](read-a-raw-input.md) — `FrameJob.Format`/`Options` force the
  demuxer, same as `Input`.
