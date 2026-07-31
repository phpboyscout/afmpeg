---
title: Watch job progress
description: Receive live progress for a running job on a channel with WithProgress — a best-effort completion fraction and byte counters observed at the filesystem boundary, plus frame/time/speed from a v9+ engine's progress side-channel.
date: 2026-07-16
tags: [how-to, runtime, progress]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Watch job progress

Long jobs (a full-file transcode, a large remux) run for seconds or minutes. `afmpeg.WithProgress`
lets you receive **live progress** while the job runs — a completion fraction, byte counters, and
(on a v9+ engine) the frame count, media time, and encode speed — so you can drive a progress bar.
It works on any `Run`, `RunJob`, or `Frames` call, needs no special module, and adds nothing to a
job that doesn't ask for it.

## How it works

afmpeg gathers progress two ways and merges both onto the one channel:

- **Phase A — observed filesystem** (any module, either backend). afmpeg implements the filesystem
  the engine reads and writes through, so it watches bytes flow — input consumed, output produced —
  without the engine emitting anything. The completion `Fraction` is the input read position
  (`bytes_read / input_size`), which tracks a linear demuxer closely. Spec
  [0031](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0031-job-progress-reporting) phase A.
- **Phase B — engine side-channel** (a **v9+** ffmpeg-wasi engine, WASM backend). When the module
  supports it, setting `progress:true` makes the engine emit NDJSON records to a
  `/dev/afmpeg-progress` device afmpeg serves — filling `Frame`, `OutTime`, and a host-derived
  `Speed`. When the engine also reports the media duration (**n8.1.2-10+**), `Fraction` is derived
  from `out_time / duration`, accurate even for a generative input with no file to measure. Spec
  [0032](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0032-engine-progress-side-channel).

Both feed the same `Progress` value: afmpeg turns on the engine channel automatically when you
attach `WithProgress`, phase-B records **refine** the phase-A samples as soon as the first one
arrives, and it falls back to byte progress on an older engine.

When both sources are live the **engine's is authoritative** — it measures the work that actually
remains, where the byte source only measures input already read. `Source` on each sample tells you
which one produced the number. Spec
[0034](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0034-fraction-source-precedence).

## Attach a channel with WithProgress

Progress is **per-invocation** — a `Runtime` is shared and serialises its calls — so you attach the
channel to the call's *context*, not to `New`. afmpeg sends on the channel while the job runs and
**stops when the call returns**; it never closes your channel.

```go
ch := make(chan afmpeg.Progress, 64) // buffered: see back-pressure below

// Drain in a goroutine while the (blocking) Run executes. Switch on Source:
// it tells you what the number is worth, so you don't have to infer it.
go func() {
    for p := range ch {
        switch p.Source {
        case afmpeg.SourceEngine: // the engine's own clock — trust this one
            fmt.Printf("\r%.0f%%  frame=%d  t=%s  %.1f×realtime",
                p.Fraction*100, p.Frame, p.OutTime.Round(time.Second), p.Speed)
        case afmpeg.SourceBytes: // input consumed; good when the inputs dominate
            fmt.Printf("\r%.0f%%  out=%d bytes", p.Fraction*100, p.OutputBytes)
        default: // SourceUnknown — Fraction is -1; show an indeterminate spinner
            fmt.Printf("\rworking… out=%d bytes  %s",
                p.OutputBytes, p.Elapsed.Round(time.Second))
        }
    }
}()

ctx := afmpeg.WithProgress(context.Background(), ch)
res, err := rt.Run(ctx, fs,
    `{"op":"process","inputs":[{"path":"in.mov"}],"outputs":[{"path":"out.mp4","video_codec":"libx264","audio_codec":"aac"}]}`)

close(ch) // safe once Run has returned — afmpeg has stopped sending
if err != nil {
    return err
}
```

The same `ctx` works with the typed builder and the frames op:

```go
res, err := rt.RunJob(afmpeg.WithProgress(ctx, ch), fs, cmd)
out, err := rt.Frames(afmpeg.WithProgress(ctx, ch), fs, job)
```

## The Progress value

```go
type Progress struct {
    Fraction    float64        // completion in [0,1], or -1 when unknown
    Source      FractionSource // how Fraction was derived: unknown / bytes / engine
    Elapsed     time.Duration  // since the invocation began
    InputBytes  int64         // bytes read from inputs so far
    InputTotal  int64         // total input size, 0 if unknown
    OutputBytes int64         // bytes written to outputs so far
    // Populated once a phase-B engine record has arrived (a v9+ engine on the WASM
    // backend); zero before then, and on an engine that emits nothing:
    Frame   int64         // frames processed
    OutTime time.Duration // media timestamp reached
    Speed   float64       // ×realtime encode speed (host-derived: OutTime/Elapsed)
}
```

`Fraction` is clamped to `[0,1]` and **never decreases** across a run, even when a demuxer seeks
backwards. Show a determinate bar when `Fraction >= 0`; fall back to an indeterminate spinner (and
use `OutputBytes` / `Elapsed`) when it is `-1`.

`Source` says where the number came from, so you can decide how far to trust it:

| `Source` | Meaning |
| --- | --- |
| `SourceEngine` | Engine-derived from `out_time / duration`. Accurate regardless of input size. Preferred. |
| `SourceBytes` | Byte-observed at the filesystem bridge (`bytes_read / input_size`). Good when the inputs dominate the work, poor when the encode does. |
| `SourceUnknown` | `Fraction` is `-1`. |

!!! warning "`Fraction == 1` is not a completion signal"

    Use the invocation's return to know a job finished. afmpeg deliberately reports `-1` rather
    than `1.0` when the inputs are exhausted but the job is still encoding — a render whose inputs
    are small next to its output (a few cards and a music bed becoming a 30 s reel) consumes every
    input byte in the first moments. Treating `1.0` as "done" would show a full bar for almost the
    whole render.

## Back-pressure — you cannot slow the job down

Delivery is **best-effort**: afmpeg sends with a non-blocking send, so a slow or non-draining
consumer simply **misses intermediate samples** — the job is never blocked or altered. Give the
channel a modest buffer and drain it promptly for the smoothest bar. A job run with a channel
nobody reads produces the identical result as one with no channel at all.

## Limits

`Fraction` is a completion fraction, not a wall-clock ETA. Its quality depends on what the engine
tells afmpeg:

- **Byte-only fallback.** Without a phase-B duration (a pre-v9 engine, or the native backend below)
  `Fraction` is byte progress (`bytes_read / input_size`), reported as `SourceBytes`. **Seek-heavy
  inputs** (e.g. an MP4 whose index sits at the end) make it lumpy — it is clamped so it never goes
  backwards, but it can jump. The denominator is the total size of the inputs the job spec declares,
  fixed before the run starts, so a concat job measures against the whole set rather than the parts
  opened so far.
- **Encode-bound work.** When the output is long relative to the inputs, the byte source runs out of
  signal: every input byte is read while most of the encode remains. `Fraction` reports `-1` for
  that tail rather than a saturated `1.0`. On an engine that reports the media duration you get a
  real number throughout instead — this is the case where `SourceEngine` matters most.
- **A brief `-1` at startup.** When engine progress is expected, `Fraction` is `-1` until the
  engine's first record arrives rather than showing a byte ratio the engine would immediately
  contradict.
- **Generative inputs** (a filter source with no input file) report `Fraction == -1` on the
  byte-only path; on a **v9 engine at n8.1.2-10+** the engine reports the media duration, so
  `Fraction` is accurate even there (the `out_time / duration` derivation).
- **The native backend** ([use the native backend](use-the-native-backend.md)) reports phase-A byte
  progress (`Fraction`, `InputBytes`, `OutputBytes` — its IPC I/O crosses the same filesystem
  boundary), but not the phase-B engine record: its `Frame` / `OutTime` / `Speed` stay zero, because
  the `/dev/afmpeg-progress` device is served by the WASM backend only.
