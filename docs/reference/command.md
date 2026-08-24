---
title: Command, Input, Output and FrameJob fields
description: Every field of afmpeg's job types — what it means, what it defaults to, the builder option that sets it, and what is rejected.
date: 2026-08-02
tags: [reference, command, job-spec, fields]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Command, Input, Output and FrameJob fields

The declarative job types and every field on them. A `Command` is plain data: fill the struct
directly, or build it with `NewCommand` and the functional options — both produce the same value,
and `JobSpec()` renders either to the JSON the [ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk)
engine consumes.

Task-shaped versions of this material live in the [how-to guides](../how-to/index.md); this page
is the field list.

## Command

```go
type Command struct {
    Inputs        []Input
    FilterComplex string
    Outputs       []Output
}
```

| Field | Builder option | Default | Meaning |
|---|---|---|---|
| `Inputs` | `WithInput`, `WithConcatInput` | empty | Inputs in order. Index 0 is the first, and that index is what a map specifier like `0:v` refers to. |
| `FilterComplex` | `WithFilterComplex` | empty | The whole `filter_complex` graph as one string, parsed by libav's `avfilter_graph_parse2`. Omit it to mux or copy without a graph. |
| `Outputs` | `WithOutput` | empty | Outputs in order. |

`JobSpec()` is pure — no I/O — so it is safe to render and log a job before running it.
`RunJob(ctx, fs, cmd)` is `Run` with the rendered spec.

## Input

```go
type Input struct {
    Path    string
    Concat  []string
    Seek    *Seek
    Format  string
    Options map[string]string
}
```

| Field | Builder option | Default | Meaning |
|---|---|---|---|
| `Path` | `WithInput(path)` | — | The input path, resolved against the `afero.Fs` you pass to `Run`. |
| `Concat` | `WithConcatInput(paths...)` | nil | Like-codec files joined into one continuous input through the concat **demuxer** (a packet-level join, no re-encode). **When set, `Path` is ignored.** Distinct from the concat *filter*, which decodes and re-encodes. |
| `Seek` | `SeekTo(sec)`, `SeekAccurateTo(sec)` | nil | Start the input at a point instead of decoding from the beginning. |
| `Format` | `InputFormat(name)` | auto-probe | Force the demuxer by name (`rawvideo`, `s16le`, `mp4`, …). Required for headerless input, and for a file whose extension would mislead the probe. |
| `Options` | `DemuxerOption(k, v)` | nil | Demuxer options as an `AVDictionary`. Raw geometry rides here: `video_size`, `pixel_format`, `framerate` for `rawvideo`; `sample_rate`, `ch_layout` for PCM. |

An option key the demuxer does not consume is an error from the engine, not a silent no-op — a
misspelled `pixel_fmt` fails the job rather than producing wrong output.

### Seek

```go
type Seek struct {
    Start float64 // seconds
    Mode  string  // SeekFast (default when empty) or SeekAccurate
}
```

| Mode | Constant | What it does | Cost |
|---|---|---|---|
| fast | `SeekFast` — the default when `Mode` is empty | Demuxer jumps to the keyframe **at or before** `Start`; earlier packets are never read. | cheap |
| accurate | `SeekAccurate` | Fast seek, then decode and discard up to the exact frame. | a fraction of a GOP of decoding |

`SeekAccurate` **cannot feed a copied stream**. A stream copy can only cut on keyframes, so
`Command.JobSpec()` rejects the combination before the engine sees it:

```
afmpeg: output "out.mp4" maps copied stream "0:v" from an accurate-seek input
(copy cuts on keyframes; use SeekFast)
```

## Output

```go
type Output struct {
    Path             string
    Map              []string
    VideoCodec       string
    AudioCodec       string
    SubtitleCodec    string
    Options          map[string]string
    BitstreamFilters map[string]string
    Duration         float64
    End              float64
    CopyTS           bool
    Format           string
    FormatOptions    map[string]string
    Metadata         map[string]string
    Chapters         string
    StreamMetadata   map[string]StreamMeta
}
```

| Field | Builder option | Default | Meaning |
|---|---|---|---|
| `Path` | `WithOutput(path)` | — | Output path in the `afero.Fs`. The muxer is guessed from its extension unless `Format` says otherwise. |
| `Map` | `Map(label)`, repeated | nil | What to mux: graph output pads in brackets (`[vout]`) and/or input streams as `in:type[:idx]` specifiers (`0:v`, `0:a:0`, `0:s`). |
| `VideoCodec` | `VideoCodec(name)` | engine default for the container | The video encoder (`libopenh264`, `libx264`, …) or `CodecCopy` to remux. |
| `AudioCodec` | `AudioCodec(name)` | engine default for the container | The audio encoder (`aac`, `libopus`, …) or `CodecCopy`. |
| `SubtitleCodec` | — (set the field) | none | Encoder for a subtitle stream mapped as `N:s` (`srt`, `webvtt`, `mov_text`), or `CodecCopy`. Works alone (a sidecar `.srt`) or alongside video and audio (an embedded track). |
| `Options` | `EncoderOption(k, v)` | nil | Options offered to **every encoder this output opens**, by their **libav** names. Reserve it for what they share (`threads`, `flags`); an option only one of them has fails the job when it reaches the others. See [option names](#option-names) below. |
| `VideoOptions` / `AudioOptions` / `SubtitleOptions` | `VideoOption(k, v)` / `AudioOption(k, v)` / `SubtitleOption(k, v)` | nil | Options for that kind's encoder only, winning over `Options` on a key collision. An output names at most one encoder per kind, so this is as precise as the codec selection itself. |
| `BitstreamFilters` | `BitstreamFilter(mapKey, name)` | auto | Override the bitstream filter for one copied stream, keyed by its `Map` entry. `"none"` force-disables. Absent, the muxer inserts whatever the container requires. |
| `Duration` | `Duration(sec)` | 0 (to the end) | Stop after this many seconds (ffmpeg's `-t`). |
| `End` | `End(sec)` | 0 (to the end) | Stop at this position (ffmpeg's `-to`). |
| `CopyTS` | `CopyTS()` | false | Keep source timestamps instead of zero-basing the output. Under `CopyTS`, `End` is an absolute source position. |
| `Format` | `OutputFormat(name)` | from the path extension | Force the muxer (`hls`, `dash`, `segment`, `mpegts`, …). |
| `FormatOptions` | `FormatOption(k, v)` | nil | **Muxer** options passed to `write_header` — `hls_time`, `hls_segment_filename`, `movflags`, … Distinct from `Options`, which reach the encoder. |
| `Metadata` | — (set the field) | nil | Container-level tags on the output (`title`, `artist`, …). |
| `Chapters` | — (set the field) | drop chapters | `"copy"` carries the first input's chapters across; an input index (`"1"`) picks another; `""` or `"none"` drops them. Authoring chapters inline is not supported. |
| `StreamMetadata` | — (set the field) | nil | Per-stream language, disposition flags and tags, keyed by the `Map` entry the stream comes from. |

Fields with no builder option are set on the struct. Both forms are first-class — the builder
covers the common ones, the struct covers all of them.

### Option names

`Options`, `FormatOptions` and `Input.Options` are **libav** option dictionaries. They are not
ffmpeg command-line flags, and the two vocabularies differ in ways that are easy to miss:

| On an ffmpeg command line | Here |
|---|---|
| `-b:v 300k` | `Options{"b": "300k"}` — the CLI parses the `:v` itself; libav never sees it |
| `-crf 23` | `Options{"crf": "23"}` — same name |
| `-movflags +faststart` | `FormatOptions{"movflags": "+faststart"}` — a **muxer** option |
| `-frames:v 1` | no equivalent — it is a CLI output limit; use [`FrameJob`](#framejob) for stills |

An option name that no encoder on the output has **fails the job** rather than being ignored, so a
misspelling is loud. Two things that check cannot cover:

- **`Options` reaches every encoder the output opens.** On an output with both `VideoCodec` and
  `AudioCodec`, `{"crf": "23"}` is offered to the audio encoder too, and `aac` refuses it — so the
  job fails even though the option was meant for the video encoder. Address it with `VideoOptions`
  instead ([spec 0045](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0045-which-encoder-an-option-is-for)).
- **A generic option set on the wrong kind is accepted and does nothing.** Every encoder inherits
  the whole generic `AVCodecContext` table, so `g` (a GOP size) in `AudioOptions` passes the check
  and has no effect. The check catches unknown *names*, not wrong *kinds* — addressing the option
  to the right map is the only thing that prevents it.

### StreamMeta

```go
type StreamMeta struct {
    Language    string            // e.g. "eng"
    Disposition []string          // e.g. {"default", "forced"}
    Tags        map[string]string // arbitrary per-stream tags
}
```

Applied to the output stream before the header is written.

### CodecCopy

`afmpeg.CodecCopy` is the string `"copy"`. Set it on `VideoCodec`, `AudioCodec` or
`SubtitleCodec` and name the source stream in `Map` — the packets pass through untouched, with
no decode and no encode.

## What `Command.JobSpec()` rejects

Validation happens in Go, before the engine is invoked. Three things fail here:

| Condition | Error |
|---|---|
| `Duration` or `End` negative | `output "…": Duration and End must be non-negative` |
| both `Duration` and `End` set | `output "…": Duration and End are mutually exclusive` |
| a copied `Map` entry whose input has `SeekAccurate` | `output "…" maps copied stream "…" from an accurate-seek input` |

Everything else — an unknown codec, a map entry naming a stream that does not exist, a
filtergraph that will not parse — is the engine's to reject, and comes back as a non-zero
`Result.ExitCode` with the detail in `Result.Stderr`.

## FrameJob

The `frames` op: pull still images out of one video input, without hand-tuning an `fps`/`select`
graph into the `image2` muxer.

```go
type FrameJob struct {
    Input   string
    Format  string
    Options map[string]string
    Select  FrameSelect
    Path    string
    Codec   string
    Scale   string
    Count   int
}
```

| Field | Default | Meaning |
|---|---|---|
| `Input` | — | The video path. |
| `Format` / `Options` | auto-probe | Force and parameterise the demuxer, exactly as on `Input`. |
| `Select` | — | Which frames. Exactly one selector, see below. |
| `Path` | — | **Required.** Output template with an optional integer token, e.g. `out/frame_%03d.png`. A template with no token is valid only for a single frame. |
| `Codec` | `png` | Image encoder — `png`, `mjpeg`, `webp`, … |
| `Scale` | none | ffmpeg scale arguments applied to each frame, e.g. `320:-2`. |
| `Count` | 0 (engine default) | Caps how many frames are emitted. |

An empty `Path` fails before the engine is reached:
`afmpeg: FrameJob.Path (output template) is required`.

### FrameSelect — exactly one

```go
type FrameSelect struct {
    Timestamp      *float64
    Timestamps     []float64
    Interval       float64
    SceneThreshold *float64
    Thumbnail      bool
}
```

| Selector | Picks |
|---|---|
| `Timestamp` | the single frame at that second (a pointer, so `0` means the first frame rather than "unset") |
| `Timestamps` | one frame at each listed second |
| `Interval` | a frame every N seconds across the input (N > 0) |
| `SceneThreshold` | scene-change frames scoring above the threshold (`select='gt(scene,T)'`) |
| `Thumbnail` | representative frames via the `thumbnail` filter |

Setting none or more than one fails in Go:

```
afmpeg: FrameSelect must set exactly one of
Timestamp/Timestamps/Interval/SceneThreshold/Thumbnail
```

## See also

- [Compose a command with the builder](../how-to/compose-a-command.md)
- [Results, probes and progress values](results.md) — what comes back
- [Limitations](limitations.md) — what these types deliberately cannot express
