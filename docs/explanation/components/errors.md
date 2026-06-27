---
title: Error catalogue
description: The exported sentinel errors afmpeg returns, and the error-handling convention.
date: 2026-06-26
tags: [explanation, errors]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Error catalogue

afmpeg standardises on a single error library — [`cockroachdb/errors`](https://github.com/cockroachdb/errors)
— for creation and wrapping, with user-facing hints where useful. Every **exported
sentinel error** (`var ErrX = errors.New(...)` in `pkg/`) is documented here; this is
enforced advisorily by `scripts/lint-docs-errors.sh` (the `just check` target), which
fails if a sentinel in `pkg/` is missing from this page.

## Sentinels

### `ErrNoModule`

Returned by `afmpeg.New` when no wasm module source is configured. The GPL
`ffmpeg.wasm` is never embedded (spec 0004 D-C), so one of `WithModuleFile`,
`WithModuleBytes`, `WithModuleFS`, or `WithModuleURL` is mandatory. Callers match
with `errors.Is(err, afmpeg.ErrNoModule)`.

### `ErrChecksumMismatch`

Returned when a module obtained via `WithModuleURL` + `WithSHA256` does not match
the expected SHA-256 — the download is rejected rather than executed. Callers
match with `errors.Is(err, afmpeg.ErrChecksumMismatch)`.

## Convention

- A **non-zero ffmpeg exit is not a Go error by itself** — `Run` returns a `Result`
  carrying the exit code + stderr tail, and a nil error. Host-side failures (module
  instantiation, the vfs bridge, context cancellation) return a non-nil error. A consumer
  that prefers an error on failure inspects `Result.ExitCode` and wraps it with the
  stderr tail itself.
- Wrap, don't reformat: `errors.Wrap`/`Wrapf` to add context; reserve sentinels for
  conditions callers branch on.
