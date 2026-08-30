---
title: Explanation
description: Understanding-oriented material: the architecture, design philosophy, and the why.
date: 2026-06-26
tags: [explanation]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Explanation

Understanding-oriented discussion: how afmpeg works, why it is built this way, and the
alternatives that were weighed.

- **[Architecture](concepts/architecture.md)**: the three layers (embedded WASM module,
  the afero↔wazero vfs bridge, the Go API) and how a call flows through them.
- **[Components › The vfs bridge](components/vfs-bridge.md)**: how the guest's filesystem
  syscalls are routed onto an `afero.Fs`, the synthetic overlays (`/tmp`, `/dev/null`,
  `/dev/urandom`, and the progress device), seek-on-write, and the no-host-filesystem
  guarantee.
- **[Components › Errors](components/errors.md)**: the sentinel-error catalogue and the
  error-handling convention.
- **[Verifying a release](concepts/release-verification.md)**: how `WithModuleRelease`
  certifies a published module: the KMS-signed checksums, the pinned key, what each layer
  defends, and the WKD second anchor that cross-checks the pinned key.
- **[Why a Runtime is capped, deadlined and serialised](concepts/safe-defaults.md)**: the
  reasoning behind the 512 MB memory cap, the one-hour invocation deadline, and one-job-at-a-time
  execution, and what none of them protects you from.

The point-in-time design records (the original thesis, the requirements, the decisions taken and
rejected along the way) live in the
[project wiki](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/home). They are a record of
what was decided when, not documentation of how afmpeg behaves now; where the two disagree, these
pages and the code are what count.
