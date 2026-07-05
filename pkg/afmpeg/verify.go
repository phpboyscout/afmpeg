package afmpeg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/cockroachdb/errors"
	"gitlab.com/phpboyscout/signing/verify"
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

// Profile selects a release's capability profile (spec 0022): lean (the default,
// web-delivery essentials), intermediate (lean + every practical software
// codec/filter), or full (intermediate + the heavy native-only encoders x265/HEVC
// and SVT-AV1). A profile is a distinct, separately-signed asset published in the
// same release; the trust chain is identical. lean and intermediate exist as both
// WASM modules and native drivers; full is native-only (0022 §4 — the heavy
// encoders need threads/SIMD), so it is selectable only via native.NewFromRelease.
type Profile string

const (
	// ProfileLean is the default: web-delivery essentials at the smallest size,
	// published as ffmpeg-wasi-<variant>.wasm.
	ProfileLean Profile = "lean"
	// ProfileIntermediate is lean + every practical software codec/format/filter
	// (no hardware, no heavy encoders), published as
	// ffmpeg-wasi-intermediate-<variant>.wasm.
	ProfileIntermediate Profile = "intermediate"
	// ProfileFull is intermediate + the heavy native-only encoders (x265/HEVC in
	// the gpl variant, SVT-AV1 in both; spec 0023). Native only — there is no WASM
	// full module; select it through native.NewFromRelease.
	ProfileFull Profile = "full"
)

// moduleFileFor is the canonical asset name for a (variant, profile): lean keeps
// the legacy ffmpeg-wasi-<variant>.wasm; a non-lean profile carries the profile
// in the name, matching ffmpeg-wasi's release layout (spec 0022).
func moduleFileFor(variant Variant, profile Profile) string {
	if profile == ProfileIntermediate {
		return "ffmpeg-wasi-intermediate-" + string(variant) + ".wasm"
	}

	return "ffmpeg-wasi-" + string(variant) + ".wasm"
}

// variantKeyFor is the provenance `variants` map key for a (variant, profile):
// lean is keyed by the bare variant ("lgpl"), intermediate by "intermediate-<variant>"
// — mirroring ffmpeg-wasi's provenance.json.
func variantKeyFor(variant Variant, profile Profile) string {
	if profile == ProfileIntermediate {
		return "intermediate-" + string(variant)
	}

	return string(variant)
}

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
	// Profile is the capability class ("lean"/"intermediate"); empty for pre-0022
	// releases that predate the field.
	Profile string `json:"profile,omitempty"`
}

// ErrProvenanceMismatch is returned when a release's provenance does not
// corroborate the requested variant. (Signature failures surface as
// signing/verify's ErrSignatureInvalid; checksum failures as ErrChecksumMismatch.)
var ErrProvenanceMismatch = errors.New("afmpeg: release provenance mismatch")

// releaseBundle is the set of fetched (or locally supplied) assets for one
// (tag, variant) release. signature is an ASCII-armored OpenPGP detached
// signature over checksums.txt (spec 0010 revised).
type releaseBundle struct {
	module     []byte
	checksums  []byte
	signature  []byte // checksums.txt.sig — armored OpenPGP detached signature
	provenance []byte
	moduleFile string // canonical asset name, e.g. "ffmpeg-wasi-lgpl.wasm"
	provFile   string // "provenance.json"
}

// verifyRelease enforces the trust chain on a complete (in-memory) bundle —
// the OpenPGP signature over checksums.txt by a trusted key, then the module's
// and provenance's checksums, then that provenance's entry under variantKey names
// the module file. Used by the offline-bundle path and the unit tests; the online
// path uses verifyManifest + a cached module fetch.
func verifyRelease(b releaseBundle, variantKey string, trust *verify.TrustSet) (Provenance, error) {
	prov, moduleSHA, err := verifyManifest(trust, b.checksums, b.signature, b.provenance, variantKey, b.moduleFile, b.provFile)
	if err != nil {
		return Provenance{}, err
	}

	if err := verifyChecksum(b.module, moduleSHA); err != nil {
		return Provenance{}, err
	}

	return prov, nil
}

// verifyManifest verifies the signed manifest (steps 1, 3, 4 of the chain): the
// OpenPGP detached signature over checksums.txt against the trust set, then
// provenance.json's checksum and the variant. It returns the parsed provenance
// and the module's trusted SHA-256 — so the (large) module can be fetched through
// the content-addressed cache and checked against that SHA. variantKey is the
// provenance `variants` map key (e.g. "lgpl" or "intermediate-lgpl").
func verifyManifest(trust *verify.TrustSet, checksums, signature, provenance []byte, variantKey, moduleFile, provFile string) (Provenance, string, error) {
	if err := trust.VerifyManifestSignature(checksums, signature); err != nil {
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

	if pv, ok := prov.Variants[variantKey]; !ok || pv.File != moduleFile {
		return Provenance{}, "", errors.Wrapf(ErrProvenanceMismatch,
			"variant %q names file %q, fetched %q", variantKey, pv.File, moduleFile)
	}

	return prov, moduleSHA, nil
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
