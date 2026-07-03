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

- **[Obtain a wasm module](obtain-a-module.md)** — supply the ffmpeg-wasi module: a
  signature-verified certified release (`WithModuleRelease`, recommended), or a file, bytes, an
  afero fs, or a URL with checksum-verified caching (`WithModuleURL`).
- **[Verify a release by hand](verify-a-release-by-hand.md)** — check a release with just `gpg`,
  `curl`, and `sha256sum`: WKD key → OpenPGP signature over `checksums.txt` → per-asset SHA-256.
- **[Run ffmpeg over an in-memory filesystem](run-in-memory.md)** — build a `Runtime`,
  supply the module (`WithModuleFile` / `WithModuleBytes` / `WithModuleFS`),
  transcode against an `afero.MemMapFs`, probe a file, and cancel a render.
- **[Remux without re-encoding (stream copy)](remux-without-re-encoding.md)** — change a
  container, mix copy with transcode, or join like-codec segments packet-for-packet with the
  `CodecCopy` sentinel — no decode, no encode, no quality loss.
- **[Extract a clip (seek and time ranges)](extract-a-clip.md)** — cut a time window without
  decoding from the file start: a cheap keyframe cut, a frame-accurate one, or a
  no-re-encode copy-trim.
- **[Compose a command with the builder](compose-a-command.md)** — assemble any
  invocation (inputs / filtergraph / outputs) as typed data and run it with `RunJob`
  (`JobSpec()`).
- **[Reuse a Runtime across many invocations](reuse-a-runtime.md)** — compile the module once
  at startup, share one `Runtime` for the process lifetime, and parallelise with a fleet
  (invocations serialise one-at-a-time per `Runtime`).

The WASM engine itself — current FFmpeg compiled to `wasm32-wasi`, with its `process`/`probe`
job spec — is the companion [**ffmpeg-wasi**](https://ffmpeg-wasi.phpboyscout.uk) project;
grab a released module via [obtain a module](obtain-a-module.md).
