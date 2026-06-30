# 0011 — WKD key distribution + the embedded↔WKD cross-check

Status: **DRAFT** (revised 2026-06-30 to the org signing module
[`gitlab.com/phpboyscout/signing`](https://gitlab.com/phpboyscout/signing) and the go-tool-base
WKD model. Supersedes this spec's earlier "independent per-release content attestation" framing,
which mis-stated the threat it closes.)
Date: 2026-06-29 (revised 2026-06-30)
Parent: [0010](0010-signed-release-acquisition.md) (revised); the `signing` module
Owns: **R-AF-15** (WKD cross-check + rotation authority), **R-AF-16** (key published via WKD)

## 1. What this adds — and the threat-model correction

0010 (revised) gives afmpeg an **embedded** OpenPGP trust key and a detached-signature check.
This spec adds the org's **second trust anchor (WKD)** and the **offline rotation authority** —
exactly what go-tool-base's `gtb update` does, reused via
`gitlab.com/phpboyscout/signing/verify`. afmpeg embeds the publisher's keys *and* fetches them
live from WKD on the domain, requiring the two to **agree by fingerprint**.

**Correction (important).** This spec previously claimed WKD "closes the poisoned-well" — a
compromised GitLab account pushing a tag that the pipeline then validly signs. **That was wrong.**
Neither layer stops a compromised GitLab account from triggering a *genuinely-signed* malicious
release; that is GitLab account hardening (protected tags, 2FA, required approvals) and/or
reproducible builds — out of scope here. What the WKD cross-check **actually** delivers (per the
go-tool-base [signing concept](https://gitlab.com/phpboyscout/go-tool-base/-/blob/main/docs/explanation/concepts/release-binary-signing.md)):

- **Key/registry-substitution defense.** An attacker who poisons the release registry can't also
  change the key embedded in an already-built afmpeg; and to defeat the cross-check they must
  *also* compromise the **independently-administered WKD host** (Cloudflare Pages, separate
  account/MFA). Two independent control planes must fall, not one.
- **Independent key distribution.** Third parties (not using afmpeg) discover the trusted key from
  the **domain** (WKD), never the GitLab repo — the R-AF-16 fix.
- **Offline rotation authority.** A shared offline key is the *only* thing that can certify a new
  signing key; a KMS or GitLab compromise cannot mint a trusted replacement.

## 2. The model (via the `signing` module)

Two OpenPGP keys, both public halves **embedded in afmpeg** *and* **published via WKD**:

1. **Signing key** — ffmpeg-wasi's dedicated KMS key (D-0010-D), minted to OpenPGP with
   `gtb keys mint --backend aws-kms`. Signs every release.
2. **Rotation-authority key** — a **shared, org-wide** offline Ed25519 key (D-0011-A); break-glass
   only, certifies each project's signing key. Paperkey + offline storage.

Verification, in afmpeg, is entirely the module's:

```go
resolver, _ := verify.BuildKeyResolver(verify.KeyResolverConfig{
    KeySource:                 "both",
    ExternalKeyEmail:          "ffmpeg-wasi-release@phpboyscout.uk",
    RequireExternalCrosscheck: false, // see D-0011-D
    HTTPClient:                hardenedClient, // optional
}, embeddedSigningASC, embeddedRotationASC)

ts, err := resolver.Resolve(ctx)              // embedded + WKD, fingerprint-checked
err = ts.VerifyManifestSignature(checksumsTxt, checksumsTxtSig)
```

Publication: ffmpeg-wasi's key lands in the **`wkd-staging`** repo (the
`openpgpkey.phpboyscout.uk` content) and is pushed to **Cloudflare Pages** by the operator from an
isolated env (wrangler). The by-hand verification how-to (R-AF-16) ships with this.

## 3. Decisions

- **D-0011-A — shared org-wide rotation authority.** One offline Ed25519 rotation key certifies
  every project's signing key. Signing keys stay **per-project** (D-0010-D), so cross-project
  forgery is still impossible — the shared key only certifies/rotates, it never signs releases.
- **D-0011-B — per-project WKD identity.** ffmpeg-wasi publishes under
  `ffmpeg-wasi-release@phpboyscout.uk` (its own `hu/<hash>` WKD entry), separate from
  go-tool-base's `release@phpboyscout.uk`.
- **D-0011-C — reuse `signing/verify`.** No bespoke crypto in afmpeg; the embedded↔WKD composite
  and the OpenPGP verification are the module's.
- **D-0011-D — `RequireExternalCrosscheck = false`.** The module's `CompositeResolver` **always
  rejects a fingerprint mismatch** (a tamper signal) regardless of this flag; the flag only governs
  a WKD *fetch failure*. `false` therefore gives full tamper protection while a transient WKD/domain
  **outage degrades gracefully** to the (already strong) embedded anchor with a logged warning,
  rather than blocking legitimate loads. Operators wanting strict fail-closed-on-outage can set it
  `true`.

## 4. Open questions

- **WKD caching** in afmpeg alongside the module cache (TTL, offline reuse of a previously-fetched
  WKD key).
- **Key-rollover ergonomics:** the embedded set carries old+new during a rotation overlap (the
  module's `RotationOverlap` path); confirm afmpeg's release cadence for shipping embedded-key
  updates.

## 5. Non-goals

- **Closing the compromised-GitLab-account case.** Out of scope (account hardening / reproducible
  builds) — and now stated honestly, not claimed here.
- **A transparency log / sigstore.** Out of scope (consistent with 0010).

## 6. Requirements

- **R-AF-15** *(reframed)* — afmpeg verifies a release through the **embedded↔WKD composite
  cross-check** (`signing/verify`): a fingerprint mismatch between the embedded key and the
  WKD-served key is rejected. The **shared offline rotation authority** is the sole certifier of
  signing keys.
- **R-AF-16** *(stands)* — the release-signing public key is published at the **WKD** domain
  location, **not** a GitLab repo. A **by-hand verification how-to** (fetch the WKD key, verify the
  OpenPGP detached signature over `checksums.txt`, then `sha256sum -c`) ships with this spec.

## 7. Sequencing & DoD

Depends on 0010 (revised): OpenPGP signing live from `n8.1.2-4`, and afmpeg already importing
`signing/verify` for the embedded check. **Done** when: afmpeg's `WithModuleRelease` resolves via
the embedded+WKD cross-check; an embedded↔WKD fingerprint mismatch is rejected (proven by TDD + a
BDD scenario); ffmpeg-wasi's key is published at `openpgpkey.phpboyscout.uk`; the by-hand how-to
ships; and the trust-model docs (0010's *release-verification* explanation + ffmpeg-wasi's
*signing* explanation) are extended to the WKD anchor.
