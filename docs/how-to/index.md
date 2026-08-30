---
title: How-to guides
description: Task-oriented guides for solving specific problems with afmpeg.
date: 2026-06-26
tags: [how-to]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# How-to guides

Goal-oriented, practical recipes for someone who already knows the basics. Each guide
solves one specific problem.

Available:

- **[Obtain a wasm module](obtain-a-module.md)**: supply the ffmpeg-wasi module: a
  signature-verified certified release (`WithModuleRelease`, recommended), or a file, bytes, an
  afero fs, or a URL with checksum-verified caching (`WithModuleURL`).
- **[Verify a release by hand](verify-a-release-by-hand.md)**: check a release with just `gpg`,
  `curl`, and `sha256sum`: WKD key → OpenPGP signature over `checksums.txt` → per-asset SHA-256.
- **[Run ffmpeg over an in-memory filesystem](run-in-memory.md)**: build a `Runtime`,
  supply the module (`WithModuleFile` / `WithModuleBytes` / `WithModuleFS`),
  transcode against an `afero.MemMapFs`, probe a file, and cancel a render.
- **[Remux without re-encoding (stream copy)](remux-without-re-encoding.md)**: change a
  container, mix copy with transcode, or join like-codec segments packet-for-packet with the
  `CodecCopy` sentinel: no decode, no encode, no quality loss.
- **[Extract a clip (seek and time ranges)](extract-a-clip.md)**: cut a time window without
  decoding from the file start: a cheap keyframe cut, a frame-accurate one, or a
  no-re-encode copy-trim.
- **[Read a raw or headerless input](read-a-raw-input.md)**: decode raw `.yuv`/`.pcm` or a
  mislabelled file by forcing the demuxer and supplying its geometry via demuxer options.
- **[Package for streaming (MPEG-TS, HLS, fragmented MP4)](package-for-streaming.md)**: write
  broadcast/adaptive-streaming containers and HLS segment sets to the in-memory fs, no network
  (needs the intermediate-profile module).
- **[Work with subtitle tracks](work-with-subtitles.md)**: extract, convert, copy, or burn in
  subtitle streams: sidecar `.srt`/`.vtt`, embedded tracks (`SubtitleCodec`), and hard-subs via
  the `subtitles`/`drawtext` filters (needs the intermediate-profile module).
- **[Read & write metadata and chapters](edit-metadata-and-chapters.md)**: read container/stream
  tags and chapters with `Probe`, and set `Metadata`/`StreamMetadata`/`Chapters` on an output
  (rides the mux, so it pairs with a copy).
- **[Extract frames and thumbnails](extract-frames.md)**: pull stills with the `frames` op
  (`FrameJob`/`Runtime.Frames`): a single frame, an interval, scene changes, or thumbnails.
- **[Read analysis-filter measurements](read-analysis-measurements.md)**: get cropdetect /
  ebur128 / silencedetect / … data back as structured `ProcessResult.Analysis` (`ParseResult`),
  not scraped from the log.
- **[Compose a command with the builder](compose-a-command.md)**: assemble any
  invocation (inputs / filtergraph / outputs) as typed data and run it with `RunJob`
  (`JobSpec()`).
- **[Watch job progress](watch-job-progress.md)**: receive live progress for a running job on a
  channel with `WithProgress`: a completion `Fraction` with the `Source` it was derived from, plus
  byte counters observed at the filesystem boundary and, on a v9+ engine, the engine's own
  `Frame` / `OutTime` / `Speed`. Best-effort; never blocks the job.
- **[Reuse a Runtime across many invocations](reuse-a-runtime.md)**: compile the module once
  at startup, share one `Runtime` for the process lifetime, and parallelise with a fleet
  (invocations serialise one-at-a-time per `Runtime`).
- **[Use the native backend](use-the-native-backend.md)**: swap the sandboxed WASM module for
  the signed **native driver** (`native.NewFromRelease` + `WithBackend`): native-speed software
  encode (~50× openh264, ~170× libx264) and the **full** profile's HEVC/AV1 encoders. linux/amd64, opt-in, CGO-free.

New to afmpeg? Do **[your first in-memory transcode](../tutorials/first-in-memory-transcode.md)**
first, because these guides assume you already have a `Runtime`.

Looking for a fact rather than a task: a default, a field, an error, or whether something is
supported at all? That is the **[reference](../reference/index.md)**, and in particular
[limitations](../reference/limitations.md).

The WASM engine itself (current FFmpeg compiled to `wasm32-wasi`, with its `process`/`probe`
job spec) is the companion [**ffmpeg-wasi**](https://ffmpeg-wasi.phpboyscout.uk) project;
grab a released module via [obtain a module](obtain-a-module.md).
