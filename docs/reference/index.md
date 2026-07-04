---
title: Reference
description: Accurate, structured facts — the Go API surface and the engine artifacts.
date: 2026-06-26
tags: [reference]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Reference

Information-oriented, accurate facts about afmpeg's surfaces. Reference is a promise of
accuracy — every documented option matches the implementation.

## Go API

The Go API reference is **not** duplicated here — it is authoritatively generated from
the source and published on the package registry:

➡️ **[pkg.go.dev/gitlab.com/phpboyscout/afmpeg](https://pkg.go.dev/gitlab.com/phpboyscout/afmpeg)**

The API is also summarised in [`pkg/afmpeg/doc.go`](https://gitlab.com/phpboyscout/afmpeg/-/blob/main/pkg/afmpeg/doc.go).

## Non-code surfaces

- **WASM engine artifacts** — the [ffmpeg-wasi](https://ffmpeg-wasi.phpboyscout.uk) `lgpl`/`gpl`
  modules afmpeg runs, with their checksums and provenance (see
  [obtain a module](../how-to/obtain-a-module.md)).
- **Errors** — the sentinel-error catalogue lives under
  [Explanation › Components › Errors](../explanation/components/errors.md).
- **CLI** — `cmd/afmpeg` flags and subcommands, *if/when* it ships (spec
  [0006](../development/specs/0006-hardening-roadmap.md), item 2D). Deferred.
