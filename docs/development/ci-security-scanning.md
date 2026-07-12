---
title: CI security scanning
description: How afmpeg's MR security gate works (govulncheck, osv-scanner, trivy, gitleaks), and the decisions behind the osv-scanner ignore + overrides.
date: 2026-07-12
tags: [development, ci, security]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# CI security scanning

afmpeg's merge-request gate runs several security scanners from the
`phpboyscout/cicd` `go-security` component. This page explains what each does and
records the non-obvious decisions — especially the `osv-scanner` handling, which
cost real debugging time and is worth not re-deriving.

## The scanners

| Job | Tool | Scope | Reachability-aware? |
|-----|------|-------|---------------------|
| `govulncheck` | Go's official scanner | stdlib + imported packages | **Yes** — call-graph aware |
| `osv-scanner` | Google OSV-DB | `go.mod` (declared deps) | No — module/lockfile level |
| `trivy` | Aqua vuln DB | filesystem | No |
| `gitleaks` | secret-pattern scan | git history | n/a |

`govulncheck` and `osv-scanner` overlap deliberately: `govulncheck` is precise
(it only reports advisories your code actually reaches), `osv-scanner` is broad
(it reports every advisory against a declared dependency, reachable or not). A
finding in `osv-scanner` that `govulncheck` stays silent on is almost always an
**unreachable** advisory.

## The `GO-2026-5932` decision (x/crypto)

**Symptom.** Since a vuln-DB update, `osv-scanner` failed every MR on
`GO-2026-5932` in `golang.org/x/crypto` (v0.53.0), while `govulncheck` did not
flag it.

**Why it can't be dropped.** `x/crypto` is not a stray dependency — it is the
foundation of the OpenPGP release-verification stack:

```
pkg/afmpeg
  → gitlab.com/phpboyscout/signing/verify        (WithModuleRelease / WKD — specs 0010/0011)
    → github.com/ProtonMail/go-crypto/openpgp     (OpenPGP)
      → golang.org/x/crypto/hkdf
```

It is additionally pulled by `cloudflare/circl`, `golang.org/x/net` (the HTTP/2
download path) and `golang.org/x/mod`. Removing it means removing signed-release
verification. And there is **no fixed version** upstream (`FIXED VERSION: --`),
so a bump cannot help either.

**Why ignoring it is correct, not a shortcut.** `govulncheck` — which *is*
call-graph aware — confirms afmpeg never calls the affected symbol: only
`x/crypto/hkdf` is linked, not the vulnerable function. So the finding is an
unreachable, unfixable, undroppable module-level false positive. That is exactly
what an ignore entry is for. Reachability is still enforced, by `govulncheck`.

The ignore lives in [`.osv-scanner.toml`](../../.osv-scanner.toml) with an
`ignoreUntil` review date — revisit when x/crypto ships a fix (Renovate will bump
the dep) and delete the entry.

## The two osv-scanner gotchas (why `.gitlab-ci.yml` overrides the job)

Adding the ignore file was not enough. The `go-security` component runs
`/osv-scanner -L go.mod`, and two things broke:

1. **Config not applied.** The component passes no `--config`, and osv-scanner
   does not auto-discover `.osv-scanner.toml` for a `-L` (lockfile) scan. The
   ignore had no effect until the job's script was overridden to pass
   `--config .osv-scanner.toml` explicitly.

2. **Exit 127 from a failed internal build.** osv-scanner runs its *own*
   `govulncheck` for call-analysis, which **builds the module using the Go
   toolchain bundled in the scanner image** (`GOTOOLCHAIN=local`). Once
   [`go.mod` required Go 1.26.5](#the-go-1265-bump), that build failed inside the
   older-Go scanner image (`go.mod requires go >= 1.26.5 (running go 1.26.4;
   GOTOOLCHAIN=local)`), and osv-scanner exited **127** — but only *after* the
   ignore emptied the result set (with findings present it exits 1, masking the
   error). The fix is `--no-call-analysis=all`: osv-scanner's reachability step
   is redundant here (the dedicated `govulncheck` job already provides it) and it
   couples the scanner image's Go version to `go.mod`'s requirement, which is
   fragile. Disabling it loses no coverage.

Both live as an `osv-scanner:` job override in
[`.gitlab-ci.yml`](../../.gitlab-ci.yml) (only `script` is overridden;
stage/image/rules stay inherited), plus an `osv_scanner_image` bump to v2.4.0.

## The Go 1.26.5 bump

Separately, `GO-2026-5856` and `GO-2026-4970` are **standard-library** advisories
fixed in Go 1.26.5, so they can only be cleared by the toolchain. Bumping
`go.mod`'s `go` directive to `1.26.5` cleared both (`govulncheck` green,
`osv-scanner` stdlib findings gone). The shared dev-tools image resolves the
1.26.5 toolchain fine (`GOTOOLCHAIN=auto`), so the bump did not break the Go
jobs — it only tripped osv-scanner's *bundled*-Go call-analysis, per gotcha 2.

## Rule of thumb for the next scanner failure

1. Does `govulncheck` also flag it? If yes, it's reachable — **fix it** (bump or
   patch), don't ignore.
2. `osv-scanner`-only + a fixed version exists → **bump the dep**.
3. `osv-scanner`-only + no fix + `govulncheck` silent + undroppable → **ignore
   with a dated `ignoreUntil`** and record why here.
4. A scanner failing on infrastructure (toolchain version, exit-code quirk),
   not a real finding → fix it in `.gitlab-ci.yml`, and note the cause so the
   next person doesn't re-debug it.
