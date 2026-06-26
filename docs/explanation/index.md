---
title: Explanation
description: Understanding-oriented material — the architecture, design philosophy, and the why.
date: 2026-06-26
tags: [explanation]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Explanation

Understanding-oriented discussion: how afmpeg works, why it is built this way, and the
alternatives that were weighed.

- **[Architecture](concepts/architecture.md)** — the three layers (embedded WASM module,
  the afero↔wazero vfs bridge, the Go API) and how a call flows through them.
- **[Components › Errors](components/errors.md)** — the sentinel-error catalogue and the
  error-handling convention.

For the full design thesis, requirements, and the decision record, see the
[specs](../development/specs/0001-afmpeg.md) — the source of truth.
