# 0010 — signed, release-aware module acquisition

Status: **DRAFT** (design for a second, *certified* module-acquisition path alongside the
existing bring-your-own URL path. Spans two repos — the publisher side is ffmpeg-wasi, the
consumer side is afmpeg. Review before building.)
Date: 2026-06-29
Parent: [0001-afmpeg.md](0001-afmpeg.md) §3 (module acquisition); [0004](0004-runtime-and-api.md) D-0004-C
Refines: [0006-hardening-roadmap.md](0006-hardening-roadmap.md) §2F (the "done, with residual" note)
Owns: **R-AF-14** (certified release acquisition)

## 1. Why

§2F shipped `WithModuleURL` — fetch by URL, verify a caller-supplied SHA-256, cache. That is the
right primitive for **bring-your-own** modules (a user hosting their own ffmpeg-wasi build, or a
fork), and it stays exactly as-is: *we did not publish those bytes, so there is nothing for us to
certify.*

But most consumers take **our** canonical ffmpeg-wasi releases. For those we can do much better
than "hand-copy a URL and a SHA": we publish `checksums.txt` and `provenance.json` with every
release, and we can **sign** them. A release-aware path lets a consumer say *"give me `n8.1.2-2`
lgpl"* and get a module that is checksum-verified, provenance-surfaced, and **signature-verified
against a key only our tag pipeline can wield**.

Two paths, two postures — this is the core decision:

- **By URL** (`WithModuleURL`) — *uncertified.* For self-hosted / custom builds. Unchanged.
- **By release** (`WithModuleRelease`, new) — *certified.* For our published artifacts:
  signature + checksum + provenance, all verified.

## 2. Decisions

- **D-0010-A — certification is exclusive to the release path.** Provenance and signature checks
  apply *only* to `WithModuleRelease`, because they assert facts about *our* published release.
  `WithModuleURL` carries no provenance/signature (we can't vouch for bytes we didn't publish);
  its integrity guarantee remains the caller-supplied `WithSHA256`. We do not bolt a fake
  "provenance" onto arbitrary URLs.
- **D-0010-B — signing mirrors the GTB release chain.** Reuse the proven
  [`terraform-aws-signing-kms`](https://gitlab.com/phpboyscout/terraform-aws-signing-kms) model:
  an **AWS KMS asymmetric key** whose private half never leaves KMS, signable **only** by the
  release CI job via OIDC (trust policy scoped to ffmpeg-wasi's `n*` tag `sub`). No human and no
  long-lived credential can mint a signature. We publish a **detached signature over
  `checksums.txt`** (`checksums.txt.sig`), exactly as GTB signs its checksums for self-update.
- **D-0010-C — the trust root ships in the consumer.** afmpeg **pins the ffmpeg-wasi
  release-signing public key** (embedded), so verification is offline and non-circular — you
  never fetch the key you're verifying against. Key rotation ships as an afmpeg release.
- **D-0010-D — a dedicated ffmpeg-wasi key, NOT the go-tool-base key.** We instantiate a
  *separate* `terraform-aws-signing-kms` key (e.g. `ffmpeg-wasi-release-signing-v1`) rather than
  reuse GTB's. We share the *infrastructure* — the same module, the account's OIDC IDP
  (`terraform-aws-bootstrap`), the operator role (`terraform-aws-security-baseline`) — but **not
  the key**. Reasons:
    - **Cross-project signature confusion (decisive).** One shared key means *any* pipeline
      authorised to sign with it can mint signatures the *other* project's verifier accepts —
      same key, same trust root. A compromised ffmpeg-wasi pipeline could forge a go-tool-base
      self-update (and vice versa). Separate keys give cryptographic domain separation for free;
      a shared key would force fragile in-payload domain separation that verifiers must remember
      to enforce.
    - **Blast radius.** A key (or trust-policy) compromise is contained to one product's releases.
    - **Independent rotation.** ffmpeg-wasi can roll its key without forcing a re-pin in GTB
      consumers, and vice versa.
    - **Clean provenance.** afmpeg pins *only* the ffmpeg-wasi key, so the GTB key is never a
      valid signer for an afmpeg module — "signed by the ffmpeg-wasi release key" is a
      self-contained claim.
  The module is explicitly built for this — its `ci_subject_filters` guidance is "one
  tag-pipeline pattern per consuming project," and `name`/`key_spec` are immutable per instance.
  A second KMS key costs ~a dollar a month; the isolation does not.

## 3. Consumer side (afmpeg)

### 3.1 API

```go
rt, err := afmpeg.New(ctx, afmpeg.WithModuleRelease("n8.1.2-2", afmpeg.VariantLGPL))
```

`WithModuleRelease(tag string, variant Variant, opts ...ReleaseOption)`:

1. **Resolves** the canonical package URL for `(tag, variant)` (the GitLab generic-package
   layout already used in the docs).
2. **Fetches** the module, `checksums.txt`, `checksums.txt.sig`, and `provenance.json`.
3. **Verifies the signature** of `checksums.txt` against the pinned public key (D-0010-C).
4. **Verifies the module** SHA-256 against its `checksums.txt` entry. (`provenance.json` is also
   listed in `checksums.txt`, so the one signature transitively certifies the whole asset set.)
5. **Surfaces provenance** (ffmpeg version, variant, licence) and **asserts** the variant matches
   the request.
6. **Caches** the verified module — reusing the §2F cache (`WithCacheDir`, `WithHTTPClient`,
   `WithGunzip` all apply).

New typed errors: `ErrSignatureInvalid`, `ErrProvenanceMismatch` (reuse `ErrChecksumMismatch`).
A new `Variant` enum (`VariantLGPL`, `VariantGPL`). Provenance is exposed (e.g. a `Provenance`
struct) so a consumer can log/assert exactly what they loaded.

### 3.2 What stays the same

`WithModuleURL` is untouched — same signature, same `WithSHA256` integrity, same cache. The two
options are mutually exclusive (exactly one `WithModule*` per `New`, as today).

## 4. Publisher side (ffmpeg-wasi)

1. **Infra** — provision a **dedicated** signing key + signer role via `terraform-aws-signing-kms`
   (D-0010-D; e.g. `name = "ffmpeg-wasi-release-signing-v1"`), `ci_subject_filters` scoped to
   `project_path:phpboyscout/ffmpeg-wasi:ref_type:tag:ref:n*`. Reuses the *shared* account infra —
   `terraform-aws-bootstrap` (OIDC IDP), `terraform-aws-security-baseline` (operator role) — but
   its own key, mirroring the `gtb-release-signing` instantiation pattern, not its key.
2. **Release CI** (the existing tag-gated `release` job) — add a GitLab `id_tokens` OIDC claim,
   assume the signer role, and **sign `checksums.txt`** → `checksums.txt.sig`. Publish the
   `.sig` alongside the other assets (package registry + release links). `checksums.txt` already
   enumerates every asset incl. `provenance.json`, so signing it certifies the whole release.
3. **Publish the public key** once, and pin it into afmpeg (D-0010-C).

Note: ffmpeg-wasi's release job is a **custom shell job**, not goreleaser, so it can't reuse
goreleaser's `signs:` block directly — it invokes the KMS sign step itself (the `gtb`
signer as a standalone tool, or `aws kms sign`). The exact invocation is §6.

## 5. Trust model

The private key lives in KMS and is wieldable **only** by the ffmpeg-wasi tag pipeline (OIDC
`sub` match) — not a maintainer, not the apply runner, not a leaked token. The consumer pins the
public key, so a tampered release (swapped module, edited `checksums.txt`, forged
`provenance.json`) fails signature verification offline. This is a materially stronger guarantee
than today's "trust the SHA the user pasted."

## 6. Open questions

- **Signature scheme / tooling reuse.** GTB emits a detached, ASCII-armored signature over
  `checksums.txt` via `gtb sign --backend aws-kms`. Do we reuse the `gtb` signer + a matching
  verifier in afmpeg (consistency, less crypto to own), or implement a minimal
  `crypto/rsa`+`crypto/x509` verify against a raw KMS `RSASSA_*_SHA_256` signature (no dependency,
  but we own the format)? Lock afmpeg's verifier to whatever ffmpeg-wasi emits.
- **Public-key distribution & rotation.** Embed-and-pin in afmpeg (recommended: offline,
  tamper-proof) vs a Web Key Directory (explicitly out of scope for `terraform-aws-signing-kms`).
  Rotation cadence and how afmpeg carries old+new keys across a rotation window.
- **Mandatory vs strict-mode.** Is signature verification always-on for `WithModuleRelease`
  (recommended — it's the whole point), or is there an escape hatch for air-gapped mirrors of our
  releases? If the latter, it must be loud and explicit, never a silent downgrade.
- **`Variant` surface.** Enum vs string; how a future third variant (or per-release variant set
  from `provenance.json`) is represented.
- **Tag/URL coupling.** `WithModuleRelease` encodes the GitLab package URL layout; if that
  layout ever changes, the resolver must version with it.

## 7. Non-goals

- **Signing or "provenance" on the URL path** (D-0010-A) — we certify only what we publish.
- **A general PKI / WKD / transparency-log (sigstore) model** — the KMS-pinned-key approach is
  the chosen, already-proven (GTB) model; we are not introducing a second trust system.
- **Embedding the module** (still — 0001 D-C). `WithModuleRelease` fetches+caches; it never
  `//go:embed`s a GPL build.

## 8. Requirements

- **R-AF-14** — A certified, release-aware acquisition path: given `(tag, variant)`, afmpeg
  fetches the canonical ffmpeg-wasi artifact and verifies it against a **KMS-backed signature**
  (pinned public key), its **published checksum**, and its **provenance**, before it is cached or
  executed. The bring-your-own `WithModuleURL` path remains uncertified and unchanged.

## 9. Phasing

- **Phase 1 — ffmpeg-wasi (publisher).** Signing infra (`terraform-aws-signing-kms` instance) +
  release-CI signing of `checksums.txt` → `checksums.txt.sig` + publish the public key. Until
  this lands there is nothing for afmpeg to verify.
- **Phase 2 — afmpeg (consumer).** `WithModuleRelease` + the pinned public key + signature /
  checksum / provenance verification + the `Variant`/`Provenance` surface + how-to docs.

## 10. Definition of done

A consumer calls `WithModuleRelease("n8.1.2-N", VariantLGPL)` and afmpeg loads a module only
after verifying the KMS signature, the checksum, and the provenance — offline against the pinned
key — with a clear, typed failure for each tampered case. `WithModuleURL` is unchanged. Both
paths are documented in [obtain-a-module](../how-to/obtain-a-module.md).
