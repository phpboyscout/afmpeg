---
title: External review
description: The commissioned 2026-07-01 external review and its per-finding disposition, now on the project wiki.
date: 2026-08-02
tags: [development, review]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# External review

The review lives in the [project wiki](https://gitlab.com/phpboyscout/afmpeg/-/wikis/external-review/home),
not in this repository.

A commissioned review is a point-in-time record — true of the code as it stood on
2026-07-01, read afterwards for the findings it reached and what we decided about
each. Keeping it here buried the living documentation it sat beside, so it moved,
the same way the specs did.

| Page | Scope |
|---|---|
| [validation & disposition](https://gitlab.com/phpboyscout/afmpeg/-/wikis/external-review/home) | What we validated against the code, and where each finding went |
| [architecture review](https://gitlab.com/phpboyscout/afmpeg/-/wikis/external-review/architecture-review) | The consolidated report — perf, security, parity, and the dual-backend proposal |
| [feature parity](https://gitlab.com/phpboyscout/afmpeg/-/wikis/external-review/feature-parity-review) · [supplementary](https://gitlab.com/phpboyscout/afmpeg/-/wikis/external-review/feature-parity-review-supplementary) | Feature gaps vs native FFmpeg |
| [performance](https://gitlab.com/phpboyscout/afmpeg/-/wikis/external-review/performance-review) · [supplementary](https://gitlab.com/phpboyscout/afmpeg/-/wikis/external-review/performance-review-supplementary) | Perf characteristics and code-level bottlenecks |
| [security](https://gitlab.com/phpboyscout/afmpeg/-/wikis/external-review/security-review) · [supplementary](https://gitlab.com/phpboyscout/afmpeg/-/wikis/external-review/security-review-supplementary) | Sandbox posture and hardening gaps |
| [architecture review](https://gitlab.com/phpboyscout/afmpeg/-/wikis/reports/2026-08-architecture-review) · [cross-review](https://gitlab.com/phpboyscout/afmpeg/-/wikis/reports/2026-08-architecture-cross-review) | Two independent models on whether thirty-seven defects were mistakes or symptoms |

Filenames were normalised to the wiki's kebab-case on the move: `ARCHITECTURE_REVIEW.md`
became `architecture-review`, and each `*_supplementary.md` became `*-supplementary`.
