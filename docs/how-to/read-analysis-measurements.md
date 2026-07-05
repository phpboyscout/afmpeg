---
title: Read analysis-filter measurements
description: Get cropdetect / ebur128 / silencedetect / … measurements back as structured data, not scraped from the log.
date: 2026-07-05
tags: [how-to, analysis, filters]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Read analysis-filter measurements

FFmpeg's analysis filters — `cropdetect`, `blackdetect`, `silencedetect`, `ebur128`,
`signalstats`, `astats`, … — measure the media as it flows through the graph. afmpeg
surfaces those measurements as **structured data** on the result (spec 0017 §Q): put the
filter in your graph, run the job, and read `ProcessResult.Analysis` via
[`afmpeg.ParseResult`](../reference/index.md) — no scraping stderr.

## Detect the content crop

```go
res, err := rt.RunJob(ctx, fs, afmpeg.Command{
    Inputs:        []afmpeg.Input{{Path: "in.mp4"}},
    FilterComplex: "[0:v]cropdetect[v]",
    Outputs:       []afmpeg.Output{{Path: "out.mp4", Map: []string{"[v]"}, VideoCodec: "libx264"}},
})
if err != nil { /* ... */ }

pr, err := afmpeg.ParseResult(res)
if err != nil { /* ... */ }

for _, m := range pr.Analysis {
    fmt.Printf("t=%.2fs %s=%s\n", m.Time, m.Key, m.Value)
    // t=0.08s cropdetect.w=160
    // t=0.08s cropdetect.h=112
    // t=0.08s cropdetect.x=80 …
}
```

Each `Measurement` is `{Time float64, Key string, Value string}` — the `lavfi.` prefix
dropped, values as the filters' own strings (parse the numeric ones with `strconv`). The
series is **consecutive-deduplicated per key**, so a stable measurement (a fixed crop)
records once, while discrete events each record on their own.

## Loudness, silence, black frames

The same pattern reads any analysis filter. A few need their `metadata=1` option to
populate the data (they only log otherwise):

```go
// Integrated loudness (EBU R128). ebur128 needs metadata=1.
FilterComplex: "[0:a]ebur128=metadata=1[a]",
// → Analysis keys r128.I (integrated LUFS), r128.LRA, r128.TPK (true peak), …

// Silence spans — silence_start / silence_end events with their timestamps.
FilterComplex: "[0:a]silencedetect=n=-30dB:d=0.5[a]",

// Black segments.
FilterComplex: "[0:v]blackdetect=d=0.1[v]",
```

For a cumulative metric (e.g. `ebur128`'s integrated `r128.I`), the **last** value for that
key is the final measurement; for event filters, each `*_start`/`*_end` pair is a span.

!!! note "Put the analysis filter late in the chain"
    Measurements are read off the frames **at the sink**, so the analysis filter should sit
    at (or near) the end of its branch — a later filter that replaces frame metadata would
    drop them. If you only want the numbers and not the re-encoded output, encode the branch
    to a small throwaway output; the measurements ride along regardless.

## Capability

The analysis filters live in the **intermediate** profile (and up) — see the
[ffmpeg-wasi filter reference](https://ffmpeg-wasi.phpboyscout.uk/reference/filters/). Works
on both the WASM module and the [native backend](use-the-native-backend.md); `Analysis` is
empty (nil) when the job ran no analysis filter.
