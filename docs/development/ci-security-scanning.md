---
title: CI security scanning
description: How afmpeg's MR security gate works (govulncheck, osv-scanner, trivy, gitleaks), and the decisions behind the osv-scanner GO-2026-5932 waiver (now shipped in the go-security component).
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

The waiver is **not** carried here per-repo: it ships in the `go-security`
component itself (from `cicd v0.21.0`), which applies it to every consumer. afmpeg
therefore keeps no local `.osv-scanner.toml`. Revisit when x/crypto ships a fix —
that removal happens in the component, not here.

## The two osv-scanner gotchas (now handled by the component)

Getting osv-scanner green took more than an ignore entry, and the debugging is
worth keeping even though the fix now lives upstream. The `go-security` component
used to run `/osv-scanner -L go.mod`, and two things broke — both fixed in
`cicd v0.21.0` (spec: the component's `osv-scanner-toolchain-and-waiver`):

1. **Config not applied.** osv-scanner does not auto-discover `.osv-scanner.toml`
   for a `-L` (lockfile) scan, and the job passed no `--config`, so any ignore
   list was silently inert. The component now appends its global waivers to the
   repo's `.osv-scanner.toml` (creating it if absent) and always passes
   `--config`, so per-repo ignores work too.

2. **Exit 127 from a failed internal build.** osv-scanner runs its *own*
   `govulncheck` for call-analysis, which **builds the module using the Go
   toolchain bundled in the scanner image** (`GOTOOLCHAIN=local`). Once
   [`go.mod` required Go 1.26.5](#the-go-1265-bump), that build failed inside the
   older-Go scanner image (`go.mod requires go >= 1.26.5 (running go 1.26.4;
   GOTOOLCHAIN=local)`), and osv-scanner exited **127** — but only *after* the
   ignore emptied the result set (with findings present it exits 1, masking the
   error). The component now defaults `osv_scanner_call_analysis` to false
   (passing `--no-call-analysis=all`): that reachability step is redundant (the
   dedicated `govulncheck` job already provides it) and it fragilely couples the
   scanner image's Go to `go.mod`'s requirement.

afmpeg briefly carried these as a local `osv-scanner:` job override; that override
(and the local `.osv-scanner.toml`) were removed once it adopted `go-security
@v0.21.0`.

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
