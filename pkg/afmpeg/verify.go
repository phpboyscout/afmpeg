package afmpeg

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/cockroachdb/errors"
)

// Variant selects which licence build of the ffmpeg-wasi engine to load
// (spec 0010 D-0010-H).
type Variant string

const (
	// VariantLGPL is the default, proprietary-compatible build (H.264 via openh264).
	VariantLGPL Variant = "lgpl"
	// VariantGPL adds libx264 and is GPL-licensed.
	VariantGPL Variant = "gpl"
)

// Provenance mirrors the subset of ffmpeg-wasi's provenance.json that afmpeg
// surfaces and asserts on a certified release.
type Provenance struct {
	FFmpegVersion  string                       `json:"ffmpeg_version"`
	BuildTag       string                       `json:"build_tag"`
	Commit         string                       `json:"commit"`
	Variants       map[string]ProvenanceVariant `json:"variants"`
	ToolingLicense string                       `json:"tooling_license"`
}

// ProvenanceVariant is one variant's record in the provenance manifest.
type ProvenanceVariant struct {
	File       string `json:"file"`
	License    string `json:"license"`
	H264Encode string `json:"h264_encode"`
}

// algRSASSAPSSSHA256 is the only signature algorithm afmpeg accepts (D-0010-E).
const algRSASSAPSSSHA256 = "RSASSA_PSS_SHA_256"

var (
	// ErrSignatureInvalid is returned when a release's checksums signature does not
	// verify against a trusted, embedded public key.
	ErrSignatureInvalid = errors.New("afmpeg: release signature invalid")
	// ErrProvenanceMismatch is returned when a release's provenance does not
	// corroborate the requested variant.
	ErrProvenanceMismatch = errors.New("afmpeg: release provenance mismatch")
)

// keySet maps a key-id to a trusted release-signing public key. afmpeg embeds the
// set (D-0010-F); tests inject their own.
type keySet map[string]*rsa.PublicKey

// sigEnvelope is the JSON content of checksums.txt.sig: a key-id naming which
// embedded key signed, the algorithm, and the base64 raw KMS signature.
type sigEnvelope struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
}

// releaseBundle is the set of fetched (or locally supplied) assets for one
// (tag, variant) release.
type releaseBundle struct {
	module     []byte
	checksums  []byte
	signature  []byte // checksums.txt.sig (a sigEnvelope JSON document)
	provenance []byte
	moduleFile string // canonical asset name, e.g. "ffmpeg-wasi-lgpl.wasm"
	provFile   string // "provenance.json"
}

// keyID is the stable identifier for a public key: the hex SHA-256 of its
// SubjectPublicKeyInfo DER. It is what `checksums.txt.sig` names and what the
// embedded key-set is keyed by.
func keyID(pub *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}

	sum := sha256.Sum256(der)

	return hex.EncodeToString(sum[:])
}

// verifyRelease enforces the spec-0010 trust chain on a fetched bundle, in order:
//
//  1. the signature over checksums.txt, by a trusted key (D-0010-E/F);
//  2. the module's checksum, read from the now-trusted checksums.txt;
//  3. provenance.json's checksum, binding it into the signed set;
//  4. provenance agrees with the requested variant (D-0010-H).
//
// Verification is offline — it needs only the embedded key-set. It returns the
// parsed provenance on success, or a typed error identifying the failed rule.
func verifyRelease(b releaseBundle, variant Variant, keys keySet) (Provenance, error) {
	prov, moduleSHA, err := verifyManifest(b.checksums, b.signature, b.provenance, variant, b.moduleFile, b.provFile, keys)
	if err != nil {
		return Provenance{}, err
	}

	if err := verifyChecksum(b.module, moduleSHA); err != nil {
		return Provenance{}, err
	}

	return prov, nil
}

// verifyManifest verifies the signed manifest of a release (steps 1, 3, 4 of the
// chain) and returns the parsed provenance and the module's trusted SHA-256 — so
// the (large) module can then be fetched through the content-addressed cache and
// checked against that SHA, instead of being held in memory for verifyRelease.
func verifyManifest(checksums, signature, provenance []byte, variant Variant, moduleFile, provFile string, keys keySet) (Provenance, string, error) {
	if err := verifySignature(checksums, signature, keys); err != nil {
		return Provenance{}, "", err
	}

	sums, err := parseChecksums(checksums)
	if err != nil {
		return Provenance{}, "", err
	}

	if err := checkSum(sums, provFile, provenance); err != nil {
		return Provenance{}, "", err
	}

	moduleSHA, ok := sums[moduleFile]
	if !ok {
		return Provenance{}, "", errors.Wrapf(ErrChecksumMismatch, "%s absent from checksums", moduleFile)
	}

	var prov Provenance
	if err := json.Unmarshal(provenance, &prov); err != nil {
		return Provenance{}, "", errors.Wrap(err, "afmpeg: parse provenance")
	}

	if pv, ok := prov.Variants[string(variant)]; !ok || pv.File != moduleFile {
		return Provenance{}, "", errors.Wrapf(ErrProvenanceMismatch,
			"variant %q names file %q, fetched %q", variant, pv.File, moduleFile)
	}

	return prov, moduleSHA, nil
}

// verifySignature checks the detached signature envelope over checksums.txt: the
// algorithm is the one we accept, the named key-id is trusted, and the RSASSA-PSS
// signature validates against it.
func verifySignature(checksums, envelope []byte, keys keySet) error {
	var env sigEnvelope
	if err := json.Unmarshal(envelope, &env); err != nil {
		return errors.Wrap(ErrSignatureInvalid, "parse signature envelope")
	}

	if env.Algorithm != algRSASSAPSSSHA256 {
		return errors.Wrapf(ErrSignatureInvalid, "unexpected algorithm %q", env.Algorithm)
	}

	key, ok := keys[env.KeyID]
	if !ok {
		return errors.Wrapf(ErrSignatureInvalid, "unknown key id %q", env.KeyID)
	}

	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		return errors.Wrap(ErrSignatureInvalid, "decode signature")
	}

	digest := sha256.Sum256(checksums)
	if err := rsa.VerifyPSS(key, crypto.SHA256, digest[:], sig, nil); err != nil {
		return errors.Wrap(ErrSignatureInvalid, "rsa-pss verify")
	}

	return nil
}

// parseChecksums parses sha256sum-style "<hex>  <name>" lines into name→hex.
func parseChecksums(data []byte) (map[string]string, error) {
	sums := map[string]string{}

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// "<hex>  <name>" — exactly a digest and a filename.
		const checksumLineFields = 2

		fields := strings.Fields(line)
		if len(fields) != checksumLineFields {
			return nil, errors.Newf("afmpeg: malformed checksums line %q", line)
		}

		sums[fields[1]] = strings.ToLower(fields[0])
	}

	return sums, nil
}

// checkSum verifies data's SHA-256 against its entry in the (signed) checksums map.
func checkSum(sums map[string]string, name string, data []byte) error {
	want, ok := sums[name]
	if !ok {
		return errors.Wrapf(ErrChecksumMismatch, "%s absent from checksums", name)
	}

	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return errors.Wrapf(ErrChecksumMismatch, "%s checksum mismatch", name)
	}

	return nil
}
