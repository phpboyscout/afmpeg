---
title: Reference
description: Accurate, structured facts — every option and its default, every job field, every result, the release artifacts, and the limits.
date: 2026-06-26
tags: [reference]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Reference

Information-oriented, accurate facts about afmpeg's surfaces. Reference is a promise of
accuracy — every documented option, default and error here matches the implementation.

## The pages

- **[Runtime options](runtime-options.md)** — every option `afmpeg.New` accepts: what it does,
  what it defaults to, and what happens when it is wrong or omitted. Start here for "what is the
  default memory cap" and "how do I turn the invocation timeout off".
- **[Command, Input, Output and FrameJob fields](command.md)** — every field of the job types,
  the builder option that sets it, and the combinations rejected before the engine sees them.
- **[Results, probes and progress values](results.md)** — everything afmpeg hands back, field by
  field, including when a field is zero rather than meaningful.
- **[Engine releases](release-artifacts.md)** — variants, profiles, exact asset filenames,
  provenance keys, trust keys, cache locations, and which engine versions a given afmpeg accepts.
- **[The guest filesystem](guest-filesystem.md)** — how paths resolve, which locations are
  synthetic, which operations are supported, and what returns `ENOSYS`.
- **[Limitations](limitations.md)** — what afmpeg does not do, will not do, or does on only one
  backend.

## Go API

The per-symbol Go API reference is **not** duplicated here — it is authoritatively generated from
the source and published on the package registry:

➡️ **[pkg.go.dev/gitlab.com/phpboyscout/afmpeg](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg)**

The pages above carry what a symbol list does not: defaults, failure modes, cross-cutting
constraints, and the artifact naming contract with the engine project.

## Elsewhere

- **Errors** — the sentinel-error catalogue lives under
  [Explanation › Components › Errors](../explanation/components/errors.md), beside the
  error-handling convention it belongs to.
- **Engine capability tables** — which codecs, filters and formats each profile carries is the
  [ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk/reference/variants/) project's reference, not
  this one. afmpeg documents which artifact it loads; ffmpeg-wasi documents what is inside it.
- **CLI** — there is none. `cmd/afmpeg-bench` in this repository is a benchmark harness, not a
  general-purpose tool, and a user-facing CLI remains deferred.
