---
title: Verifying a release
description: How afmpeg certifies an ffmpeg-wasi release — the KMS-signed checksums, the pinned key, what each layer defends, and the one gap a second layer will close.
date: 2026-06-29
tags: [explanation, security, releases, signing]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Verifying a release

afmpeg loads executable WebAssembly. Where those bytes come from matters, so afmpeg offers two
acquisition paths with deliberately different trust postures (spec
[0010](../../development/specs/0010-signed-release-acquisition.md)):

- **`WithModuleURL`** — *uncertified*, for a module you host or build yourself. Integrity is the
  caller-supplied `WithSHA256`. afmpeg can't vouch for bytes it didn't publish.
- **`WithModuleRelease`** — *certified*, for the project's own published releases. This page is
  about that path.

## The chain

Every ffmpeg-wasi release publishes `checksums.txt` (the SHA-256 of every asset, including
`provenance.json`) and `checksums.txt.sig` — a **detached RSASSA-PSS-SHA256 signature over
`checksums.txt`**. `WithModuleRelease` verifies, in order:

1. **The signature**, against a public key **embedded in afmpeg**. The private key lives in AWS
   KMS and can be wielded only by ffmpeg-wasi's tagged-release CI job (via GitLab OIDC) — no
   human, no long-lived credential. The signature envelope names a *key-id*; afmpeg looks it up
   in its pinned set.
2. **The module's checksum**, read from the now-trusted `checksums.txt`.
3. **`provenance.json`'s checksum**, binding it into the signed set.
4. **Provenance agrees with the variant** you requested (e.g. `VariantLGPL` ↔
   `ffmpeg-wasi-lgpl.wasm`).

Only then is the module compiled. Each failure is its own typed error —
`ErrSignatureInvalid`, `ErrChecksumMismatch`, `ErrProvenanceMismatch`. Because the signature
covers `checksums.txt` and `checksums.txt` covers everything else, **one signature certifies the
whole release**.

## Why these choices

- **The trust root ships *in* afmpeg.** The key is pinned (embedded), so verification is offline
  and non-circular — you never fetch the key you're verifying against. It's a *set* of keys, so
  rotation is graceful: a new key is added in an afmpeg release and the old one retired later,
  with no flag-day (and a compromised key can be dropped promptly).
- **A dedicated key, not a shared one.** ffmpeg-wasi signs with its own key, never go-tool-base's
  — a shared key would let one project's pipeline forge the other's releases (spec 0010
  D-0010-D).
- **No skip.** Verification is mandatory on this path; air-gapped use is served by
  `WithReleaseBundleDir` (verify a local directory of pre-downloaded assets), which still
  verifies fully — there is no "trust me" switch to misuse.

## What this does *not* defend — yet

The KMS signature defends against leaked credentials, the apply runner, and non-tag pipelines.
It does **not** defend against a **compromised GitLab account that can push a tag**: that
triggers the legitimate release pipeline, which signs a *malicious* build with the *real* key,
and verification would pass. This is the "poisoned well" scenario.

Closing it needs a **second, independent attestation rooted in a control plane GitLab can't
touch** (the `phpboyscout.uk` domain) — a committed fast-follow, spec
[0011](../../development/specs/0011-wkd-attestation.md). Until then, the KMS signature is a strong
first layer, and this gap is stated plainly rather than papered over.
