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

_None yet_ — afmpeg is at the scaffold stage and exports no packages. As `pkg/afmpeg`
lands (specs [0004](../../development/specs/0004-runtime-and-api.md) /
[0005](../../development/specs/0005-render-helper-and-keyrx-backend.md)), each exported
sentinel is added here as a row:

<!-- Template — copy per sentinel as they are introduced:
### `ErrName`

Returned when … . Wrap with context at the call site; callers match with
`errors.Is(err, afmpeg.ErrName)`.
-->

## Convention

- A **non-zero ffmpeg exit is not a Go error by itself** — `Run` returns a `Result`
  carrying the exit code + stderr tail, and a nil error. Host-side failures (module
  instantiation, the vfs bridge, context cancellation) return a non-nil error. The render
  helper / keryx adapter maps a non-zero exit to a wrapped error with the stderr tail.
- Wrap, don't reformat: `errors.Wrap`/`Wrapf` to add context; reserve sentinels for
  conditions callers branch on.
