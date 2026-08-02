---
title: Results, probes and progress values
description: Every value afmpeg hands back — Result, ProcessResult, Probe, FramesResult and Progress — field by field, including when each is zero.
date: 2026-08-02
tags: [reference, results, probe, progress]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Results, probes and progress values

What each call returns, field by field, and when a field is zero rather than meaningful.

## When is a failure an error, and when is it a Result?

This distinction runs through every call, so it is worth stating once:

- **A non-zero engine exit is not a Go error.** `Run`/`RunJob` return a `Result` with the exit
  code and the captured output, and a `nil` error. A bad codec name, an unparseable filtergraph
  or a missing input all arrive this way.
- **Only host-side failures return a non-nil error** — module instantiation, the filesystem
  bridge, a cancelled or expired context, a driver that would not start.
- **The typed helpers invert this for convenience.** `Probe`, `ProbeInput` and `Frames` turn a
  non-zero exit into an error, because there is no useful typed value to return. The error
  carries the **last 1,500 bytes** of stderr, not the whole log.

So a `RunJob` caller checks `err` *and* `res.ExitCode`; a `Probe` caller checks `err` only.

## Result

```go
type Result struct {
    ExitCode int
    Stdout   string
    Stderr   string
}
```

| Field | Meaning |
|---|---|
| `ExitCode` | The engine's process exit status. `0` is success. |
| `Stdout` | The engine's structured JSON result. Parse it with `ParseResult` for a process job; `Probe` and `Frames` parse it for you. |
| `Stderr` | The engine's log, captured in full. This is where a failure explains itself. |

## ProcessResult

`afmpeg.ParseResult(res)` decodes `Result.Stdout` from a process job.

```go
type ProcessResult struct {
    Outputs  []OutputResult
    Analysis []Measurement
}

type OutputResult struct {
    Path      string
    Segmented bool
    Streams   []OutputStream
}

type OutputStream struct {
    Type        string // "video", "audio", …
    Codec       string // the encoder actually used
    Disposition string // "copy" for a stream-copied stream
}
```

| Field | Meaning |
|---|---|
| `Outputs[].Path` | The output that was written. For a segmenting muxer (`hls`, `dash`, `segment`) this is the playlist or manifest, not the segments. |
| `Outputs[].Segmented` | True when the output is a segment set rather than a single file. |
| `Outputs[].Streams[].Codec` | The encoder the engine used — worth logging, because it confirms which H.264 encoder a module actually has. |
| `Analysis` | Measurements emitted by analysis filters. Empty unless the graph contains one. |

`ParseResult` on an empty `Stdout` returns a zero `ProcessResult` and **no error**, so it is safe
to call on any result. It does not look at the exit code — check that first, because the engine
only emits this JSON on success.

### Measurement

```go
type Measurement struct {
    Time  float64 // seconds into the source
    Key   string  // metadata key, "lavfi." prefix dropped
    Value string  // the filter's own string
}
```

Keys arrive as the filter names them minus the `lavfi.` prefix: `cropdetect.w`, `r128.I`,
`silence_start`, `black_start`. Values are the filters' raw strings — parse the numeric ones
yourself.

Two behaviours to plan for:

- **The series is consecutive-deduplicated per key.** A stable measurement such as `cropdetect`
  appears once; discrete events such as `silence_start` / `silence_end` each appear.
- **Some filters log without emitting metadata.** `ebur128` and `astats` need their `metadata=1`
  option before anything reaches `Analysis` — for example `ebur128=metadata=1`.

## Probe

```go
type Probe struct {
    Format      string
    DurationSec float64
    StartSec    float64
    Streams     []ProbeStream
    Tags        map[string]string
    Chapters    []Chapter
}
```

| Field | Meaning |
|---|---|
| `Format` | The demuxer name as libav reports it. Often a comma-separated family rather than one name — an MP4 probes as `mov,mp4,m4a,3gp,3g2,mj2`. Match on a substring, not on equality. |
| `DurationSec` | Container duration in seconds. |
| `StartSec` | The container's start time. Non-zero after, for instance, a `CopyTS` trim. |
| `Streams` | One entry per stream, in container order. |
| `Tags` | Container-level metadata (`title`, `artist`, …). |
| `Chapters` | The container's chapters. |

```go
type ProbeStream struct {
    Index       int
    Type        string // "video" or "audio"
    Codec       string
    Width       int    // video
    Height      int    // video
    SampleRate  int    // audio
    Channels    int    // audio
    Language    string
    Disposition []string // set flags: "default", "forced", "attached_pic", …
    Tags        map[string]string
}

type Chapter struct {
    Start float64
    End   float64
    Title string
}
```

The video and audio fields are mutually irrelevant: `Width`/`Height` are zero on an audio
stream, `SampleRate`/`Channels` are zero on a video stream. `Language` is commonly `und`.

`Probe(ctx, fs, path)` auto-probes. A headerless or raw input only opens with a forced demuxer,
so use `ProbeInput(ctx, fs, Input{Path: …, Format: …, Options: …})` for those — it forwards
`Format` and `Options` exactly as a process job would.

Probing needs the ffmpeg-wasi engine: it is the engine's `probe` op, not something afmpeg
computes. A generic wasm module cannot answer it.

## FramesResult

```go
type FramesResult struct {
    Frames []ExtractedFrame
    Count  int
}

type ExtractedFrame struct {
    Path      string  // the file written, with the template expanded
    Index     int     // 0-based position in this extraction
    Timestamp float64 // source position, in seconds
}
```

The files are already in the `afero.Fs` you passed; `Frames` tells you what they are called and
where each came from.

## Progress

Delivered on the channel you attach with `WithProgress(ctx, ch)`, one value per sample.

```go
type Progress struct {
    Fraction    float64
    Source      FractionSource
    Elapsed     time.Duration
    InputBytes  int64
    InputTotal  int64
    OutputBytes int64
    Frame       int64
    OutTime     time.Duration
    Speed       float64
}
```

| Field | Meaning | Zero when |
|---|---|---|
| `Fraction` | Completion in `[0,1]`, or **`-1`** when it cannot be determined. Never decreases across a run. | — (it is `-1`, not `0`, when unknown) |
| `Source` | How `Fraction` was derived. `SourceUnknown` exactly when `Fraction` is `-1`. | — |
| `Elapsed` | Since the invocation began. | — |
| `InputBytes` | Input bytes read so far, counted at the filesystem bridge. | — |
| `InputTotal` | Total size of the declared inputs, fixed before the run starts. | `0` when the job spec declared nothing statable |
| `OutputBytes` | Output bytes written, counted at the filesystem bridge. | — |
| `Frame` | Frames processed. | before the first engine record, and always on an engine that emits none |
| `OutTime` | Media timestamp reached. | as `Frame` |
| `Speed` | ×realtime, derived by the host as `OutTime / Elapsed`. | as `Frame` |

`OutputBytes` counts **bytes written**, which is not the same as the output file's final size.
An MP4 muxer under `+faststart` seeks back and rewrites its header, so those bytes are counted
twice and `OutputBytes` finishes a little above the file length.

### FractionSource

| Value | `String()` | Meaning |
|---|---|---|
| `SourceEngine` | `"engine"` | Derived from the engine's `out_time / duration`. Accurate regardless of input size, including for generative inputs. Preferred whenever present. |
| `SourceBytes` | `"bytes"` | Derived at the filesystem bridge as `bytes_read / input_size`. Good when the inputs dominate the work, poor when the encode does. |
| `SourceUnknown` | `"unknown"` | `Fraction` is `-1`. |

### When Fraction is -1

Deliberately, in five situations. Each is a case where a number could be produced but would be
wrong within a second or two:

1. **The first moment of a job** on a backend that can deliver engine records — up to a 2-second
   grace period — rather than showing a byte ratio the engine is about to contradict.
2. **Every declared input byte has been read while the job is still encoding.** A render whose
   inputs are small next to its output exhausts them early; `-1` beats a full bar for the rest of
   the run.
3. **A purely generative input** with no file to measure, on an engine that reports no duration.
4. **The engine's `out_time` has overrun the duration it reported** by more than 2% — the job is
   demonstrably longer than predicted, so the ratio is withheld rather than pinned at 1.0.
5. **No statable input at all** and no engine duration.

`Fraction == 1` is not a completion signal either way — use the invocation's return.

## See also

- [Watch job progress](../how-to/watch-job-progress.md) — the task-shaped guide, with a worked loop
- [Read analysis-filter measurements](../how-to/read-analysis-measurements.md)
- [Error catalogue](../explanation/components/errors.md) — the sentinel errors and how to match them
