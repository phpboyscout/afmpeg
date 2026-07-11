---
title: Watch job progress
description: Receive live progress for a running job on a channel with WithProgress — a best-effort completion fraction and byte counters, observed at the filesystem boundary with no engine cooperation.
date: 2026-07-11
tags: [how-to, runtime, progress]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Watch job progress

Long jobs (a full-file transcode, a large remux) run for seconds or minutes. `afmpeg.WithProgress`
lets you receive **live progress** while the job runs — a completion fraction and byte counters —
so you can drive a progress bar. It works on any `Run`, `RunJob`, or `Frames` call, needs no
special module, and adds nothing to a job that doesn't ask for it.

## How it works

afmpeg implements the filesystem the engine reads and writes through, so it can watch bytes flow —
input consumed, output produced — without the engine emitting anything. The completion `Fraction`
is the input read position (`bytes_read / input_size`), which tracks a linear demuxer closely.
This is spec [0031](../development/specs/0031-job-progress-reporting.md) phase A.

## Attach a channel with WithProgress

Progress is **per-invocation** — a `Runtime` is shared and serialises its calls — so you attach the
channel to the call's *context*, not to `New`. afmpeg sends on the channel while the job runs and
**stops when the call returns**; it never closes your channel.

```go
ch := make(chan afmpeg.Progress, 64) // buffered: see back-pressure below

// Drain in a goroutine while the (blocking) Run executes.
go func() {
    for p := range ch {
        if p.Fraction >= 0 {
            fmt.Printf("\r%.0f%%  out=%d bytes", p.Fraction*100, p.OutputBytes)
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
    Fraction    float64       // completion in [0,1], or -1 when unknown
    Elapsed     time.Duration // since the invocation began
    InputBytes  int64         // bytes read from inputs so far
    InputTotal  int64         // total input size, 0 if unknown
    OutputBytes int64         // bytes written to outputs so far
    // Frame / OutTime / Speed are reserved for a future engine-side source; zero today.
}
```

`Fraction` is clamped to `[0,1]` and **never decreases** across a run, even when a demuxer seeks
backwards. Show a determinate bar when `Fraction >= 0`; fall back to an indeterminate spinner (and
use `OutputBytes` / `Elapsed`) when it is `-1`.

## Back-pressure — you cannot slow the job down

Delivery is **best-effort**: afmpeg sends with a non-blocking send, so a slow or non-draining
consumer simply **misses intermediate samples** — the job is never blocked or altered. Give the
channel a modest buffer and drain it promptly for the smoothest bar. A job run with a channel
nobody reads produces the identical result as one with no channel at all.

## Limits (today)

`Fraction` is **byte progress, not a time/ETA**, and not wall-clock. A few cases carry a weaker
signal — phase B (a future engine-side frame/time source, behind this same channel) will improve
them:

- **Generative inputs** (a filter source with no input file) and very short ops report
  `Fraction == -1`.
- **Seek-heavy inputs** (e.g. an MP4 whose index sits at the end) make the fraction lumpy; it is
  clamped so it never goes backwards, but it can jump.
- **Sequential multi-input** (concat) can sit near a plateau as each part completes, since the
  total to read grows as inputs open.
- The **native backend** ([use the native backend](use-the-native-backend.md)) does not report
  phase-A progress (its I/O does not cross this boundary); that arrives with phase B.
