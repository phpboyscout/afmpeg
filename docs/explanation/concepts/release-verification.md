---
title: Verifying a release
description: How afmpeg certifies an ffmpeg-wasi release — the OpenPGP-signed checksums, the embedded signing key, the WKD second anchor, and what each layer does and does not defend.
date: 2026-06-30
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

Verification reuses the org signing module, **`gitlab.com/phpboyscout/go/signing`** — the same
OpenPGP/WKD machinery go-tool-base uses — rather than any afmpeg-specific crypto.

## The chain

Every ffmpeg-wasi release publishes `checksums.txt` (the SHA-256 of every asset, including
`provenance.json`) and `checksums.txt.sig` — an **ASCII-armored OpenPGP detached signature over
`checksums.txt`**, produced by `gtb sign` from a key held in AWS KMS. `WithModuleRelease`
verifies, in order:

1. **The signature**, against the release-signing public key **embedded in afmpeg**
   (`signing/verify`'s `VerifyManifestSignature`). The private key lives in AWS KMS and can be
   wielded only by ffmpeg-wasi's tagged-release CI job (via GitLab OIDC) — no human, no long-lived
   credential. OpenPGP identifies the signing key by fingerprint; afmpeg's embedded key is the
   trust anchor.
2. **The module's checksum**, read from the now-trusted `checksums.txt`.
3. **`provenance.json`'s checksum**, binding it into the signed set.
4. **Provenance agrees with the variant** you requested (e.g. `VariantLGPL` ↔
   `ffmpeg-wasi-lgpl.wasm`).

Only then is the module compiled. Each failure is its own typed error —
`signing/verify.ErrSignatureInvalid`, `ErrChecksumMismatch`, `ErrProvenanceMismatch`. Because the
signature covers `checksums.txt` and `checksums.txt` covers everything else, **one signature
certifies the whole release**.

## The second anchor: WKD (spec 0011)

The embedded key is pinned in the binary, but where did *that* key come from? On every **online**
fetch afmpeg adds a second, independent check: it fetches the signing key from the **Web Key
Directory** on `openpgpkey.phpboyscout.uk` and requires it to **match the embedded key by
fingerprint** (`signing/verify`'s composite resolver). The domain is a control plane administered
separately from GitLab and AWS, so the key has an anchor that does not depend on the platform that
hosts the releases.

- A **fingerprint mismatch** (embedded vs WKD) is a hard failure — a tamper signal.
- A **WKD outage** degrades gracefully to the pinned embedded key (which is already a strong
  anchor): a transient domain problem never blocks a legitimate load.
- The **offline-bundle** path (`WithReleaseBundleDir`) skips WKD entirely — it must not touch the
  network. `WithReleaseWKDEmail` overrides the WKD identity (for a mirror) or disables it (`""`).

There are **two** OpenPGP keys in the model, with distinct roles. The **signing key**
(`ffmpeg-wasi-release@phpboyscout.uk`) is what afmpeg embeds and cross-checks — it signs releases.
The **rotation-authority key** (`release@phpboyscout.uk`) is a shared, *offline* break-glass key
that certifies and rotates signing keys; it never signs releases and is **not** in afmpeg's
runtime trust set (afmpeg pins the signing key directly, so the rotation key adds no runtime
verification — it is org infrastructure).

## Why these choices

- **The trust root ships *in* afmpeg.** The signing key is pinned (embedded), so verification is
  non-circular — you never fetch *only* the key you're verifying against. Rotation ships a new
  embedded key in an afmpeg release (old + new can overlap), and the WKD bucket is republished.
- **A dedicated key, not a shared one.** ffmpeg-wasi signs with its own key, never go-tool-base's
  — a shared key would let one project's pipeline forge the other's releases (spec 0010 D-0010-D).
- **No skip.** Verification is mandatory on this path; air-gapped use is served by
  `WithReleaseBundleDir` (verify a local directory of pre-downloaded assets), which still verifies
  the OpenPGP signature fully — there is no "trust me" switch to misuse.

## What this does — and does not — defend

The signature defends against a swapped or tampered artifact: leaked credentials, the apply
runner, and non-tag pipelines all **cannot** produce a valid signature. The WKD cross-check adds
**key/registry-substitution defense** (an attacker must compromise *both* the release platform and
the independently-administered domain) and **independent key distribution** (third parties
discover the key from the domain, not the repo).

What **no** signing scheme here closes is a **compromised GitLab account that can push a tag**:
that triggers the legitimate release pipeline, which signs a genuine-but-malicious build with the
real key. That is the domain of GitLab account hardening (protected tags, 2FA, required approvals)
and reproducible builds — out of scope, and stated plainly rather than papered over. The WKD
anchor narrows the attack surface; it does not eliminate that case.

## Verifying by hand

You don't need afmpeg (or Go) to check a release — see
[Verify a release by hand](../../how-to/verify-a-release-by-hand.md): fetch the key from WKD,
verify the OpenPGP signature over `checksums.txt`, then `sha256sum -c`.
