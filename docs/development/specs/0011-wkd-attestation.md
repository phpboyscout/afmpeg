# 0011 — WKD-discovered independent attestation (the second layer)

Status: **DRAFT / FAST-FOLLOW** (a committed follow-on to [0010](0010-signed-release-acquisition.md);
the design is to be completed before build. Sequenced *after* 0010 ships, because it attests the
artifacts 0010 publishes.)
Date: 2026-06-29
Parent: [0010](0010-signed-release-acquisition.md) §6, D-0010-F
Owns: **R-AF-15** (independent, domain-rooted release attestation)

## 1. The gap this closes

0010's KMS signature is a strong first layer, but it has one structural hole: **it trusts "a tag
pipeline ran."** A maintainer's **compromised GitLab account** can push a tag, which triggers the
legitimate release pipeline, which assumes the signer role via OIDC and signs a *malicious* build
with the **real** KMS key. afmpeg's 0010 verification would pass — the signature is genuine. This
is the **poisoned-well** scenario, and it is a deliberate, named part of the phpboyscout security
posture to defend against it.

KMS+OIDC defends against leaked credentials, the apply runner, and non-tag pipelines. It cannot
defend against a control-plane (GitLab) compromise, because signing authority is *delegated to*
that control plane.

## 2. The principle

Add a **second, independent attestation** that:

1. is **rooted in a control plane GitLab cannot touch** — the `phpboyscout.uk` domain (DNS /
   domain-hosted content), and
2. attests the release **content** (the `checksums.txt` digest), not merely the key, and
3. is **required** — afmpeg accepts a release only if **both** the KMS signature (0010) *and* this
   attestation pass.

A GitLab-account compromise can mint layer 1 but cannot produce layer 2 (different control plane,
ideally a different credential and an offline/human step).

### Why publishing the key via WKD is not enough

A naïve reading of "WKD" is "publish the public key on the domain." That alone does **not** close
the gap: the malicious release already carries a valid layer-1 signature, and re-publishing the
same key changes nothing. The second layer must bind to the **specific release content** with a
key the GitLab pipeline **cannot wield**. WKD is the *discovery mechanism* for that independent
key; the attestation itself is the payload.

## 3. Candidate designs (to resolve before build)

- **(a) Offline/human counter-signature.** A maintainer signs the release `checksums.txt` digest
  out-of-band with an **offline/hardware key never present in CI**, and publishes the signature +
  key via the domain (WKD). afmpeg requires both signatures. *Strongest* (a GitLab compromise
  can't reach the offline key); cost is a manual step per release.
- **(b) Independent automated signer.** A second signer on a *different* control plane (separate
  cloud account / CI provider) attests releases. Less toil, but the independence is only as strong
  as the separation, and it adds infra.
- **(c) Domain-published content digest.** Publish the `checksums.txt` digest as a signed
  domain-hosted file or DNS record; afmpeg cross-checks. Lighter; independence rooted purely in
  domain control.

WKD is the discovery layer for (a)/(b)'s public key; (c) is the lightest variant.

## 4. Open questions

- **Manual vs automated** second signer (the (a)/(b)/(c) choice) — the core decision; trades
  release toil against the strength of the independence.
- **What is published** — a full detached signature over `checksums.txt`, or just an
  attested digest.
- **WKD for raw-RSA keys.** WKD is OpenPGP-oriented; 0010 chose raw KMS/RSA (D-0010-E). Either
  adapt WKD, or use a plain domain `.well-known/` JSON document — decide for consistency.
- **Failure mode.** If the second layer is unreachable, does `WithModuleRelease` **fail closed**
  (safer, but a domain outage blocks loads) or fall back to layer 1 with a warning? Posture
  suggests fail-closed for fresh fetches, with the verified cache still usable.
- **Discovery & caching** of the attestation in afmpeg, alongside the existing module cache.

## 5. Non-goals

- **Replacing layer 1.** The KMS signature (0010) stays; this is *additive*. Defence in depth.
- **A transparency log / sigstore.** Out of scope (consistent with 0010 §7).

## 6. Requirements

- **R-AF-15** — A release is accepted by `WithModuleRelease` only if, in addition to the 0010 KMS
  signature, it carries a valid **independent attestation rooted in the `phpboyscout.uk` domain**
  over the release content, discovered via WKD. A compromise of GitLab alone must not produce a
  release afmpeg will load.
- **R-AF-16** — The release-signing **public key is published at the domain (WKD) location, not in
  a GitLab repository** — a key fetched from the same platform that hosts the releases is not an
  independent anchor. afmpeg keeps pinning the key (its trust root); WKD serves third-party / by-
  hand verification. A **by-hand verification how-to** (extract the signature envelope, verify the
  RSASSA-PSS signature against the WKD key, then `sha256sum -c checksums.txt`) ships with this
  spec — it was deliberately deferred from the 0010 signing docs for exactly this reason.

## 7. Sequencing & DoD

Fast-follow after 0010 (it needs 0010's `checksums.txt`/signature in place). **Done** when a
release must pass *both* layers, the second layer is rooted off-GitLab, and a simulated
GitLab-only compromise (a tag-signed but un-attested release) is rejected — proven by TDD + a BDD
scenario, with the trust model documented (the explanation page from 0010 §10 extended to cover
layer 2).
