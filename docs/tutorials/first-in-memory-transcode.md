---
title: Your first in-memory transcode
description: Build a working Go program that fetches a verified FFmpeg engine, encodes video you generate in code, and reads the MP4 back — without ever touching the disk.
date: 2026-08-02
tags: [tutorial, getting-started, in-memory, transcode]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Your first in-memory transcode

By the end of this you'll have a Go program that produces a playable H.264 MP4 without an FFmpeg
install, without CGO, and without a single file on disk. The video goes in as bytes you generate
in Go and comes out as bytes you can write wherever you like.

Allow about fifteen minutes. The first run downloads a 5.5 MB engine and caches it, so every run
after that starts in well under a second.

## What you'll need

- **Go 1.26 or newer.** afmpeg is a library; there is nothing to install separately.
- **Network access for the first run only.** afmpeg fetches the engine from GitLab and caches it
  under your user cache directory (`~/.cache/afmpeg` on Linux).
- **No FFmpeg.** If you have one installed it will be ignored — afmpeg never shells out to it.

You do not need to know FFmpeg's command-line syntax. afmpeg doesn't accept it; jobs are
described as Go values.

## Start a module and add afmpeg

```sh
mkdir first-transcode && cd first-transcode
go mod init example.com/first-transcode
go get gitlab.com/phpboyscout/afmpeg
go get github.com/spf13/afero
```

`afero` is the filesystem abstraction afmpeg does its I/O through. You'll use its in-memory
backend, which is what keeps everything off the disk.

## Fetch a verified engine

afmpeg ships no FFmpeg. You choose which engine build to load, and for the project's own
releases the certified path fetches and verifies one for you.

Create `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/afero"
	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

func run(ctx context.Context) error {
	var prov afmpeg.Provenance

	rt, err := afmpeg.New(ctx,
		afmpeg.WithModuleRelease("n9.0.1-1", afmpeg.VariantLGPL,
			afmpeg.WithReleaseProvenance(&prov)),
	)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close(ctx) }()

	fmt.Printf("engine: FFmpeg %s (build %s)\n", prov.FFmpegVersion, prov.BuildTag)

	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

Run it:

```sh
go run .
```

```
engine: FFmpeg n9.0.1 (build n9.0.1-1)
```

Quite a lot happened in that one call. afmpeg downloaded the release's checksum manifest and its
OpenPGP signature, checked the signature against keys compiled into your binary, checked the
module's digest against the signed manifest, confirmed the release's provenance names the
variant you asked for — and only then compiled the module. There is no flag to skip any of it.

Compiling is the expensive part, which is why `rt` is built once and reused. Hold onto it for the
life of your program rather than building one per job.

!!! note "`VariantLGPL` and `VariantGPL`"
    Both encode H.264. The LGPL build does it with openh264 and is the safer default for
    proprietary software; the GPL build uses libx264 and makes the combined program GPL. This
    tutorial uses LGPL throughout, so the encoder name below is `libopenh264`.

## Make some video to encode

Real inputs come from somewhere — an upload, an object store, a git worktree in RAM. To keep
this self-contained you'll generate raw frames in Go instead, which also shows how afmpeg reads
input that has no container or header at all.

Add this above `run`:

```go
// makeRawYUV420p builds frames of raw yuv420p video: a diagonal gradient that
// shifts each frame, with flat mid-grey colour.
func makeRawYUV420p(w, h, frames int) []byte {
	ySize, cSize := w*h, (w/2)*(h/2)
	buf := make([]byte, 0, frames*(ySize+2*cSize))

	for f := range frames {
		y := make([]byte, ySize)
		for i := range y {
			y[i] = byte((i + f*8) & 0xff)
		}

		grey := make([]byte, cSize)
		for i := range grey {
			grey[i] = 128
		}

		buf = append(buf, y...)
		buf = append(buf, grey...) // U plane
		buf = append(buf, grey...) // V plane
	}

	return buf
}
```

That's two seconds of 320×240 video at 25 fps once you ask for 50 frames — about 5.8 MB of raw
bytes, which is exactly why nobody stores video this way.

## Put the input in an in-memory filesystem

Every path in an afmpeg job resolves against an `afero.Fs` you supply. Give it the in-memory one
and nothing reaches the disk. Add this to `run`, after the engine is built:

```go
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in/frames.yuv", makeRawYUV420p(320, 240, 50), 0o644); err != nil {
		return err
	}
```

Any afero backend works here — `OsFs` for real files, `BasePathFs` to sandbox a directory. The
job doesn't change; only where the bytes live does.

## Describe the job

An afmpeg job is data: inputs, an optional filtergraph, outputs. Add this next:

```go
	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("in/frames.yuv",
			afmpeg.InputFormat("rawvideo"),
			afmpeg.DemuxerOption("video_size", "320x240"),
			afmpeg.DemuxerOption("pixel_format", "yuv420p"),
			afmpeg.DemuxerOption("framerate", "25"),
		),
		afmpeg.WithFilterComplex("[0:v]scale=160:-2,format=yuv420p[v]"),
		afmpeg.WithOutput("out/clip.mp4",
			afmpeg.Map("[v]"),
			afmpeg.VideoCodec("libopenh264"),
			afmpeg.WithOption("b", "300k"),
		),
	)
```

Reading that in order:

- **`InputFormat("rawvideo")`** forces the demuxer. Headerless bytes carry no clue about what
  they are, so without this the engine has nothing to probe. The three `DemuxerOption` calls
  supply what the header would have: size, pixel format, frame rate. Get one wrong and the job
  fails rather than producing garbage.
- **The filtergraph** scales to 160 pixels wide, `-2` meaning "whatever keeps the aspect ratio
  and stays even". `[0:v]` is the video stream of input 0; `[v]` is the name of what comes out.
- **`Map("[v]")`** says which graph output to mux. **`WithOption("b", "300k")`** is an encoder
  option: the bitrate, 300 kbit/s. These are **libav option names**, not ffmpeg command-line ones,
  so it is `b` and not `-b:v` — the CLI parses that `:v` suffix itself and libav never sees it. A
  name no encoder has fails the job rather than being ignored, so a typo here is loud.

The container comes from the `.mp4` extension. You never write an ffmpeg command line.

## Run it and read the bytes back

```go
	res, err := rt.RunJob(ctx, fs, cmd)
	if err != nil {
		return err
	}

	if res.ExitCode != 0 {
		return fmt.Errorf("engine failed: %s", res.Stderr)
	}

	out, err := afero.ReadFile(fs, "out/clip.mp4")
	if err != nil {
		return err
	}

	fmt.Printf("out/clip.mp4: %d bytes\n", len(out))
```

```
engine: FFmpeg n9.0.1 (build n9.0.1-1)
out/clip.mp4: 84560 bytes
```

Note the two error checks, because they mean different things. A **non-zero exit code is not a Go
error** — that's the engine rejecting the job, and `res.Stderr` says why. A non-`nil` `err` is a
host-side failure: a broken module, a cancelled context, a filesystem that wouldn't cooperate.
Checking only one of them will eventually bite you.

`out` is an ordinary `[]byte`. Write it to disk, put it in an HTTP response, upload it —
whichever, the encode itself never needed a file.

## Confirm it's really an MP4

Trusting the byte count is optimistic. Ask the engine what it produced:

```go
	probe, err := rt.Probe(ctx, fs, "out/clip.mp4")
	if err != nil {
		return err
	}

	fmt.Printf("format=%s duration=%.2fs\n", probe.Format, probe.DurationSec)

	for _, s := range probe.Streams {
		fmt.Printf("  stream %d: %s %s %dx%d\n", s.Index, s.Type, s.Codec, s.Width, s.Height)
	}
```

```
format=mov,mp4,m4a,3gp,3g2,mj2 duration=1.96s
  stream 0: video h264 160x120
```

That's H.264 in an MP4 container, two seconds long, scaled to 160×120 as asked. `Format` is a
comma-separated family rather than a single name — that's how libav reports the MP4 demuxer, so
match on a substring rather than comparing for equality.

If you want to watch it, write `out` to a file and open it in any player.

## Watch it happen

Two seconds of video encodes fast. A real job runs for minutes, and you'll want a progress bar.
Attach a channel to the call's context — not to the runtime, since progress is per-invocation:

```go
	ch := make(chan afmpeg.Progress, 64)

	go func() {
		for p := range ch {
			if p.Fraction < 0 {
				fmt.Printf("\rworking… %s", p.Elapsed.Round(time.Millisecond))
				continue
			}

			fmt.Printf("\r%3.0f%% (%s) frame=%d", p.Fraction*100, p.Source, p.Frame)
		}
	}()

	res, err := rt.RunJob(afmpeg.WithProgress(ctx, ch), fs, cmd)
	close(ch)
```

Add `"time"` to your imports and swap this in for the earlier `RunJob` call. You'll see something
like:

```
 12% (engine) frame=7
 48% (engine) frame=25
 96% (engine) frame=49
```

Two things to expect rather than debug. `Fraction` is `-1` for the first moments while afmpeg
waits for the engine's first report — showing a byte-based guess it would immediately contradict
is worse than showing nothing. And the last sample you see is usually a little under 1.0, because
the engine stops reporting before the muxer finishes. **Use the return of `RunJob` to know a job
finished, never `Fraction == 1`.**

Delivery is best-effort: if your goroutine is slow, samples are dropped rather than the job being
held up. You cannot slow an encode down by not reading the channel, and you cannot fail one
either.

## Where to go next

You've now done the whole loop — acquire a verified engine, describe a job, run it over a
filesystem you control, and read the result. From here:

- **[Compose a command with the builder](../how-to/compose-a-command.md)** — inputs, graphs and
  outputs in their general form.
- **[Reuse a Runtime across many invocations](../how-to/reuse-a-runtime.md)** — the pattern for a
  long-lived service, and why one runtime doesn't give you parallelism.
- **[Runtime options](../reference/runtime-options.md)** — every option to `New`, what it defaults
  to, and what happens when it's wrong.
- **[Limitations](../reference/limitations.md)** — worth reading early. It's a short list, and it
  will save you an afternoon.

## The whole program

```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/afero"
	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// makeRawYUV420p builds frames of raw yuv420p video: a diagonal gradient that
// shifts each frame, with flat mid-grey colour.
func makeRawYUV420p(w, h, frames int) []byte {
	ySize, cSize := w*h, (w/2)*(h/2)
	buf := make([]byte, 0, frames*(ySize+2*cSize))

	for f := range frames {
		y := make([]byte, ySize)
		for i := range y {
			y[i] = byte((i + f*8) & 0xff)
		}

		grey := make([]byte, cSize)
		for i := range grey {
			grey[i] = 128
		}

		buf = append(buf, y...)
		buf = append(buf, grey...) // U plane
		buf = append(buf, grey...) // V plane
	}

	return buf
}

func run(ctx context.Context) error {
	var prov afmpeg.Provenance

	rt, err := afmpeg.New(ctx,
		afmpeg.WithModuleRelease("n9.0.1-1", afmpeg.VariantLGPL,
			afmpeg.WithReleaseProvenance(&prov)),
	)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close(ctx) }()

	fmt.Printf("engine: FFmpeg %s (build %s)\n", prov.FFmpegVersion, prov.BuildTag)

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in/frames.yuv", makeRawYUV420p(320, 240, 50), 0o644); err != nil {
		return err
	}

	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("in/frames.yuv",
			afmpeg.InputFormat("rawvideo"),
			afmpeg.DemuxerOption("video_size", "320x240"),
			afmpeg.DemuxerOption("pixel_format", "yuv420p"),
			afmpeg.DemuxerOption("framerate", "25"),
		),
		afmpeg.WithFilterComplex("[0:v]scale=160:-2,format=yuv420p[v]"),
		afmpeg.WithOutput("out/clip.mp4",
			afmpeg.Map("[v]"),
			afmpeg.VideoCodec("libopenh264"),
			afmpeg.WithOption("b", "300k"),
		),
	)

	ch := make(chan afmpeg.Progress, 64)

	go func() {
		for p := range ch {
			if p.Fraction < 0 {
				fmt.Printf("\rworking… %s", p.Elapsed.Round(time.Millisecond))
				continue
			}

			fmt.Printf("\r%3.0f%% (%s) frame=%d", p.Fraction*100, p.Source, p.Frame)
		}
	}()

	res, err := rt.RunJob(afmpeg.WithProgress(ctx, ch), fs, cmd)
	close(ch)

	if err != nil {
		return err
	}

	if res.ExitCode != 0 {
		return fmt.Errorf("engine failed: %s", res.Stderr)
	}

	out, err := afero.ReadFile(fs, "out/clip.mp4")
	if err != nil {
		return err
	}

	fmt.Printf("\nout/clip.mp4: %d bytes\n", len(out))

	probe, err := rt.Probe(ctx, fs, "out/clip.mp4")
	if err != nil {
		return err
	}

	fmt.Printf("format=%s duration=%.2fs\n", probe.Format, probe.DurationSec)

	for _, s := range probe.Streams {
		fmt.Printf("  stream %d: %s %s %dx%d\n", s.Index, s.Type, s.Codec, s.Width, s.Height)
	}

	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```
