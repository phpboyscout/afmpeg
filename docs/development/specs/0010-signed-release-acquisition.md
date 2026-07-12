# 0010 — signed, release-aware module acquisition

Status: **IMPLEMENTED + SHIPPED** (2026-06-30 — the certified `WithModuleRelease` path alongside
the bring-your-own URL path. First OpenPGP-signed release `n8.1.2-4` is live; afmpeg verifies via
the `signing` module. Spans two repos — publisher ffmpeg-wasi, consumer afmpeg. **Note:** the
signing model was revised from the initial raw-KMS scheme to OpenPGP/WKD — see the Revision block
below; the cross-check second layer is [0011](0011-wkd-attestation.md).)
Date: 2026-06-29
Parent: [0001-afmpeg.md](0001-afmpeg.md) §3 (module acquisition); [0004](0004-runtime-and-api.md) D-0004-C
Refines: [0006-hardening-roadmap.md](0006-hardening-roadmap.md) §2F (the "done, with residual" note)
Owns: **R-AF-14** (certified release acquisition)

## Revision 2026-06-30 — adopt the org `signing` module (OpenPGP/WKD)

The initial implementation (releases `n8.1.2-1`..`-3`) used a **bespoke** signature: a raw AWS
KMS RSASSA-PSS signature in a custom JSON envelope, verified with afmpeg's own stdlib crypto
(decisions D-0010-E/F below). That worked and is secure, but it **diverged from the org's
established OpenPGP/WKD signing model** (go-tool-base + `openpgpkey.phpboyscout.uk`). That model
is now a standalone, dependency-light module — **[`gitlab.com/phpboyscout/go/signing`](https://gitlab.com/phpboyscout/go/signing)**
(library `go.mod` = `go-crypto` + `cockroachdb/errors` only) — so there is no reason to keep a
parallel scheme. This spec is revised to use it:

- **D-0010-E is superseded.** Releases are signed with an **ASCII-armored OpenPGP detached
  signature** over `checksums.txt` (`checksums.txt.sig`), produced by the **`gtb sign --backend
  aws-kms`** CLI (same KMS key, OIDC-gated, as today). afmpeg verifies with
  **`gitlab.com/phpboyscout/go/signing/verify`** — `LoadTrustSet(...).VerifyManifestSignature(checksums, sig)`.
- **D-0010-F is superseded.** OpenPGP identifies the signing key natively (key fingerprint), so
  the hand-rolled JSON envelope, `key_id`, and key-*set* logic are dropped. The **embedded** trust
  keys + the **WKD cross-check** + the **rotation key** are the GTB model, specified in
  [0011](0011-wkd-attestation.md) (revised) via `verify.BuildKeyResolver` / `CompositeResolver`.
- **D-0010-D stands** — ffmpeg-wasi keeps its own dedicated signing key, now minted as an OpenPGP
  public key (`gtb keys mint --backend aws-kms`) under its own WKD identity
  (`ffmpeg-wasi-release@phpboyscout.uk`). The **rotation-authority key is shared org-wide** (one
  offline Ed25519 key certifies every project's separate signing key).
- **Unchanged:** the two-path model (§2), the `WithModuleRelease` API and options (§3), the
  provenance + variant checks, the offline-bundle mode, and the content-addressed cache. Only the
  *signature/key format* and the *verifier internals* change.
- **Cutover:** OpenPGP from `n8.1.2-4`; the interim raw-signed `n8.1.2-1`..`-3` are left as-is
  (not re-signed).

Sections below describing the JSON envelope / stdlib verify (D-0010-E/F, §3.1's key-set, §4's
`aws kms sign`, §10's stdlib tests) are retained as the record of the initial implementation;
read them through this revision.

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
- **D-0010-E — raw KMS signature, stdlib verification (resolves Q1).** ffmpeg-wasi signs
  `checksums.txt` with `aws kms sign --signing-algorithm RSASSA_PSS_SHA_256`, publishing the
  signature base64-encoded as `checksums.txt.sig`. afmpeg verifies with the **Go standard library
  only** (`crypto/rsa`, `crypto/sha256`, `crypto/x509`) — no OpenPGP or other third-party crypto
  dependency. Same KMS key and OIDC gating as GTB; only the envelope differs. Stdlib-only verify
  is the more auditable surface, and afmpeg's verifier is programmatic (no human `gpg` step to
  serve).
- **D-0010-F — embedded key-*set* with overlap rotation (resolves Q2).** afmpeg embeds a **set**
  of accepted public keys, each with a stable key-id; `checksums.txt.sig` names the signing
  key-id, and verification passes iff that id is in the set *and* its key validates the signature.
  Rotation is graceful: mint v2 → ship an afmpeg release adding v2 to the set → switch signing to
  v2 → drop v1 in a later release once consumers have upgraded. No flag-day; a compromised key is
  retired by dropping it. The independent **WKD second layer is a committed fast-follow** —
  spec [0011](0011-wkd-attestation.md) (see §6).
- **D-0010-G — verification is mandatory (resolves Q3).** `WithModuleRelease` *always* verifies
  signature + checksum + provenance; there is **no skip flag** (an opt-out is a silent-downgrade
  footgun). Verification is offline (the key-set is embedded), so air-gap is served by an
  **offline-bundle mode**: point `WithModuleRelease` at a local directory of pre-fetched assets
  (`.wasm`, `checksums.txt`, `checksums.txt.sig`, `provenance.json`) and it verifies them fully,
  no network — certification without exemption.
- **D-0010-H — `Variant` is a typed enum (resolves Q4).** `type Variant string` with
  `VariantLGPL`/`VariantGPL`, validated against the known set (unknown → error) and cross-checked
  against `provenance.json`. A future variant is one new constant.
- **D-0010-I — hardcoded layout + base override (resolves Q5).** The resolver knows the canonical
  GitLab generic-package layout and exposes a `WithReleaseBaseURL`-style override so a consumer
  can fetch *our* release from a mirror / internal store and still verify *our* signature (the
  signature is over content, so the URL is untrusted input). A GitLab layout change is a small
  afmpeg release.

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

Note: ffmpeg-wasi's release job is a **custom shell job**, not goreleaser, so it signs with a
direct `aws kms sign --signing-algorithm RSASSA_PSS_SHA_256` over `checksums.txt` (D-0010-E) —
no goreleaser `signs:` block, no `gtb` signer to run out of context.

## 5. Trust model

The private key lives in KMS and is wieldable **only** by the ffmpeg-wasi tag pipeline (OIDC
`sub` match) — not a maintainer, not the apply runner, not a leaked token. The consumer pins the
public key, so a tampered release (swapped module, edited `checksums.txt`, forged
`provenance.json`) fails signature verification offline. This is a materially stronger guarantee
than today's "trust the SHA the user pasted."

## 6. Open questions

**Resolved (2026-06-29):** Q1 signature scheme → D-0010-E (raw KMS RSASSA-PSS, stdlib verify);
Q2 key distribution/rotation → D-0010-F (embedded key-set, overlap rotation); Q3 mandatory verify
→ D-0010-G (always-on + offline-bundle); Q4 variant surface → D-0010-H (typed enum); Q5 tag/URL →
D-0010-I (hardcoded layout + base override).

**Still open:**

- **The WKD second layer (→ spec [0011](0011-wkd-attestation.md)).** The KMS signature does *not*
  defend against a **compromised GitLab account that can push a tag** — that triggers the legit
  release pipeline, which signs a malicious build with the real key. Closing this "poisoned well"
  needs an **independent attestation of release content, rooted in a control plane GitLab cannot
  touch** (the `phpboyscout.uk` domain). Merely *publishing* the key via WKD is necessary but not
  sufficient — the bad release would carry a valid signature — so 0011 must design the layer to
  attest *content* (likely an out-of-band/offline signature discovered via WKD), not just the key.
  A committed fast-follow; sequenced after this spec ships.
- **Key-id derivation.** How a key-id is computed and carried in `checksums.txt.sig` (proposed:
  a SHA-256 fingerprint of the SubjectPublicKeyInfo DER) — pin in implementation.
- **Rotation cadence.** Operational, decided at first rotation; the *mechanism* (D-0010-F) is set.

## 7. Non-goals

- **Signing or "provenance" on the URL path** (D-0010-A) — we certify only what we publish.
- **A transparency-log / sigstore model** — the KMS-pinned-key approach (+ the domain-rooted WKD
  layer in 0011) is the chosen trust model; we are not adding sigstore on top. (Note: WKD itself is
  *no longer* a non-goal — it is the committed second layer, spec 0011.)
- **Embedding the module** (still — 0001 D-C). `WithModuleRelease` fetches+caches; it never
  `//go:embed`s a GPL build.

## 8. Requirements

- **R-AF-14** — A certified, release-aware acquisition path: given `(tag, variant)`, afmpeg
  fetches the canonical ffmpeg-wasi artifact and verifies it against a **KMS-backed signature**
  (pinned public key), its **published checksum**, and its **provenance**, before it is cached or
  executed. The bring-your-own `WithModuleURL` path remains uncertified and unchanged.

## 9. Phasing

- **Phase 2a — afmpeg verifier (consumer), buildable now.** The verification logic is
  **key-agnostic**, so it is built and fully tested *before* any real KMS key exists: tests
  generate an RSA keypair, sign fixture `checksums.txt` with it, and drive the verifier and every
  tamper case. Delivers the `Variant`/`Provenance` types, the resolver, the key-set, signature /
  checksum / provenance verification, the offline-bundle mode, and `WithModuleRelease` — gated
  behind a not-yet-populated embedded key-set.
- **Phase 1 — ffmpeg-wasi (publisher).** Signing infra (a dedicated `terraform-aws-signing-kms`
  instance, D-0010-D) + release-CI `aws kms sign` of `checksums.txt` → `checksums.txt.sig` +
  publish the public key. Needs an AWS apply (operator action), so it runs in parallel with 2a.
- **Phase 2b — pin the real key.** Once Phase 1 publishes the production public key, add it to
  afmpeg's embedded key-set and cut the first end-to-end verified release.

## 10. Test & docs strategy (a key deliverable, per the dev method)

- **TDD, test-first.** Every verification rule lands as a failing test first: a *valid* bundle
  passes; each tampered input (swapped module, edited `checksums.txt`, forged `provenance.json`,
  bad/wrong-key signature, unknown key-id, variant mismatch) fails with its **own typed error**
  (`ErrSignatureInvalid`, `ErrChecksumMismatch`, `ErrProvenanceMismatch`). Table-driven,
  `t.Parallel()`, **≥90%** coverage on new `pkg/` code, `go test -race` clean.
- **BDD acceptance (godog).** The trust behaviours are also expressed as Gherkin scenarios —
  *"Given a release signed by a retired key, When I load it, Then it is rejected"* — so the
  security contract reads as executable specification, not just unit assertions.
- **Docs land in the same MR (Diátaxis):** the [obtain-a-module](../how-to/obtain-a-module.md)
  how-to gains the certified-release path; a **reference** page documents `WithModuleRelease` /
  `Variant` / `Provenance` / the typed errors; an **explanation** page covers the trust model
  (KMS+OIDC, embedded key-set, what each layer does and does *not* defend — incl. the GitLab-
  compromise gap that 0011 closes). Package `doc.go` updated.

## 11. Definition of done

A consumer calls `WithModuleRelease("n8.1.2-N", VariantLGPL)` and afmpeg loads a module only
after verifying the KMS signature, the checksum, and the provenance — offline against the pinned
key-set — with a clear, typed failure for each tampered case, proven by both unit (TDD) and
Gherkin (BDD) tests at ≥90% coverage. `WithModuleURL` is unchanged. All three doc types (§10)
ship in the same MRs. The WKD second layer is specced (0011) and sequenced as the fast-follow.
