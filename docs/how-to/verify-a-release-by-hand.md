---
title: Verify a release by hand
description: "Check an ffmpeg-wasi release without afmpeg: fetch the signing key from WKD, check it against the pinned fingerprints, verify the OpenPGP signature over checksums.txt, then sha256sum -c."
date: 2026-06-30
tags: [how-to, security, releases, signing]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Verify a release by hand

afmpeg's `WithModuleRelease` verifies releases for you. This page shows the same checks with
nothing but `gpg`, `curl`, and `sha256sum`, useful for auditing, CI in another language, or just
confirming the chain yourself. It mirrors exactly what afmpeg does (spec
[0010](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0010-signed-release-acquisition) /
[0011](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0011-wkd-attestation)).

Set the release you want:

```sh
TAG=n9.0.1-8
BASE="https://pkg.phpboyscout.uk/ffmpeg-wasi/$TAG"
```

## 1. Get the signing keys from WKD

The release-signing keys are published via the **Web Key Directory** on the `phpboyscout.uk`
domain. Fetch them by email; `gpg` resolves WKD automatically:

```sh
gpg --locate-external-keys ffmpeg-wasi-release-v2@phpboyscout.uk
```

That address is the one to use. The older `ffmpeg-wasi-release@phpboyscout.uk` still resolves,
but it serves only the first of the two keys below, enough to verify an older release and not a
current one. `ffmpeg-wasi-release-v2@phpboyscout.uk` is also the identity afmpeg itself
cross-checks against by default.

Two keys come back, and afmpeg pins both. Confirm the fingerprints are **exactly** these:

```
7108 81C1 DDAE ABD1 38E5  3004 A216 6E59 EB60 60E1
4C96 ECB3 5C74 4661 9FF7  8EB1 ED13 44E5 76B7 BBBF
```

```sh
gpg --fingerprint ffmpeg-wasi-release-v2@phpboyscout.uk
```

If a fingerprint differs, stop. Do not trust the release.

That comparison is the step that protects you, because WKD is not an independent source. The keys
come from `openpgpkey.phpboyscout.uk` and the release files from `pkg.phpboyscout.uk`, so whoever
controls the `phpboyscout.uk` zone, or the Cloudflare account serving it, can replace the keys and
the files together, and the signature would still check out. This page is served from that zone
as well. Take the fingerprints from somewhere the zone cannot reach: the keys afmpeg pins, in
[`pkg/afmpeg/keys`](https://gitlab.com/phpboyscout/afmpeg/-/tree/main/pkg/afmpeg/keys) on
gitlab.com, or the copy in the afmpeg module you already build against.

## 2. Verify the signature over the manifest

`checksums.txt.sig` is an OpenPGP detached signature over `checksums.txt`:

```sh
curl -fsSLO "$BASE/checksums.txt"
curl -fsSLO "$BASE/checksums.txt.sig"

gpg --verify checksums.txt.sig checksums.txt
```

A current release is signed by **both** keys, so `gpg` reports two signatures, and expect a
`Good signature from "ffmpeg-wasi Release Signing <ffmpeg-wasi-release-v2@phpboyscout.uk>"` for
each. (`gpg` may add a "not certified with a trusted signature" web-of-trust warning; that is
about *your* local trust DB, not the cryptographic validity; the fingerprint check in step 1 is
the trust decision.)

## 3. Check the assets against the signed manifest

`checksums.txt` covers every asset, including `provenance.json`, so the one verified signature
transitively certifies whatever you download. Grab what you need and check it:

```sh
curl -fsSLO "$BASE/ffmpeg-wasi-lgpl.wasm"
curl -fsSLO "$BASE/provenance.json"

sha256sum --ignore-missing -c checksums.txt
```

Expect `ffmpeg-wasi-lgpl.wasm: OK` and `provenance.json: OK`. (`--ignore-missing` checks only the
files present; drop it once you've downloaded every asset.)

Then confirm the now-verified provenance names the tag you asked for, as `WithModuleRelease` does:

```sh
grep -q "\"build_tag\": *\"$TAG\"" provenance.json && echo "build_tag: OK"
```

That's the whole chain: **pinned fingerprint → OpenPGP signature over `checksums.txt` → SHA-256
of each asset → provenance naming the tag you asked for**, with WKD only as the convenient way to
fetch the keys. `provenance.json` then tells you exactly what went into the build.
