---
title: Verify a release by hand
description: Check an ffmpeg-wasi release without afmpeg — fetch the signing key from WKD, verify the OpenPGP signature over checksums.txt, then sha256sum -c.
date: 2026-06-30
tags: [how-to, security, releases, signing]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Verify a release by hand

afmpeg's `WithModuleRelease` verifies releases for you. This page shows the same checks with
nothing but `gpg`, `curl`, and `sha256sum` — useful for auditing, CI in another language, or just
confirming the chain yourself. It mirrors exactly what afmpeg does (spec
[0010](../development/specs/0010-signed-release-acquisition.md) /
[0011](../development/specs/0011-wkd-attestation.md)).

Set the release you want:

```sh
TAG=n8.1.2-11
BASE="https://gitlab.com/api/v4/projects/83847809/packages/generic/ffmpeg-wasi/$TAG"
```

## 1. Get the signing key from WKD

The release-signing key is published via the **Web Key Directory** on the `phpboyscout.uk`
domain — a control plane independent of the GitLab repo that hosts the releases. Fetch it by
email; `gpg` resolves WKD automatically:

```sh
gpg --locate-external-keys ffmpeg-wasi-release@phpboyscout.uk
```

Confirm the fingerprint is **exactly** this (the value afmpeg pins):

```
710881C1 DDAE ABD1 38E5 3004  A216 6E59 EB60 60E1
```

```sh
gpg --fingerprint ffmpeg-wasi-release@phpboyscout.uk
```

If the fingerprint differs, stop — do not trust the release.

## 2. Verify the signature over the manifest

`checksums.txt.sig` is an OpenPGP detached signature over `checksums.txt`:

```sh
curl -fsSLO "$BASE/checksums.txt"
curl -fsSLO "$BASE/checksums.txt.sig"

gpg --verify checksums.txt.sig checksums.txt
```

Expect `Good signature from "ffmpeg-wasi Release Signing <ffmpeg-wasi-release@phpboyscout.uk>"`.
(`gpg` may add a "not certified with a trusted signature" web-of-trust warning — that is about
*your* local trust DB, not the cryptographic validity; the fingerprint check in step 1 is the
trust decision.)

## 3. Check the assets against the signed manifest

`checksums.txt` covers every asset, including `provenance.json` — so the one verified signature
transitively certifies whatever you download. Grab what you need and check it:

```sh
curl -fsSLO "$BASE/ffmpeg-wasi-lgpl.wasm"
curl -fsSLO "$BASE/provenance.json"

sha256sum --ignore-missing -c checksums.txt
```

Expect `ffmpeg-wasi-lgpl.wasm: OK` and `provenance.json: OK`. (`--ignore-missing` checks only the
files present; drop it once you've downloaded every asset.)

That's the whole chain: **WKD key → OpenPGP signature over `checksums.txt` → SHA-256 of each
asset**. `provenance.json` then tells you exactly what went into the build.
