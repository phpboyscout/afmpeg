---
title: Reference
description: Accurate, structured facts — config, CLI, and the Go API surface.
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

The intended shape (subject to the spec [§10](../development/specs/0001-afmpeg.md)
decisions) is sketched in [`pkg/afmpeg/doc.go`](https://gitlab.com/phpboyscout/afmpeg/-/blob/main/pkg/afmpeg/doc.go).

## Non-code surfaces

These land as the features ship:

- **WASM module artifacts** — the published `ffmpeg.wasm` variants (full/GPL, LGPL),
  their provenance manifest fields, sizes, and checksums (spec
  [0002](../development/specs/0002-wasm-build-pipeline.md)).
- **CLI** — `cmd/afmpeg` flags and subcommands, once it exists (spec
  [0006](../development/specs/0006-hardening-roadmap.md), item 2D).
- **Errors** — the sentinel-error catalogue lives under
  [Explanation › Components › Errors](../explanation/components/errors.md).
