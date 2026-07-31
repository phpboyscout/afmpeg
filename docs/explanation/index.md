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
- **[Components › The vfs bridge](components/vfs-bridge.md)** — how the guest's filesystem
  syscalls are routed onto an `afero.Fs`, the synthetic overlays (`/tmp`, `/dev/null`,
  `/dev/urandom`, and the progress device), seek-on-write, and the no-host-filesystem
  guarantee.
- **[Components › Errors](components/errors.md)** — the sentinel-error catalogue and the
  error-handling convention.
- **[Verifying a release](concepts/release-verification.md)** — how `WithModuleRelease`
  certifies a published module: the KMS-signed checksums, the pinned key, what each layer
  defends, and the WKD second anchor that cross-checks the pinned key.

For the full design thesis, requirements, and the decision record, see the
[specs](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0001-afmpeg) — the source of truth.
