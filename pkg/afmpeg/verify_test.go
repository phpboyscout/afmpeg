package afmpeg

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
)

// assembleBundle builds a release bundle from the given parts and signs its
// checksums.txt with key, returning the bundle and a key-set that trusts key.
// Callers tamper individual parts to exercise each verification rule.
func assembleBundle(t *testing.T, key *rsa.PrivateKey, moduleFile string, module, provBytes []byte) (releaseBundle, keySet) {
	t.Helper()

	checksums := buildChecksums(map[string][]byte{
		moduleFile:        module,
		"provenance.json": provBytes,
	})

	id := keyID(&key.PublicKey)
	env := sigEnvelope{
		KeyID:     id,
		Algorithm: algRSASSAPSSSHA256,
		Signature: base64.StdEncoding.EncodeToString(signChecksums(t, key, checksums)),
	}
	envBytes, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal sig envelope: %v", err)
	}

	b := releaseBundle{
		module:     module,
		checksums:  checksums,
		signature:  envBytes,
		provenance: provBytes,
		moduleFile: moduleFile,
		provFile:   "provenance.json",
	}

	return b, keySet{id: &key.PublicKey}
}

// assembleBundleRaw signs the given checksums bytes verbatim (rather than
// computing them from files), so a test can present a signed-but-malformed
// checksums file to the parser.
func assembleBundleRaw(t *testing.T, key *rsa.PrivateKey, checksums, module, provBytes []byte) (releaseBundle, keySet) {
	t.Helper()

	id := keyID(&key.PublicKey)

	env := sigEnvelope{
		KeyID:     id,
		Algorithm: algRSASSAPSSSHA256,
		Signature: base64.StdEncoding.EncodeToString(signChecksums(t, key, checksums)),
	}

	envBytes, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal sig envelope: %v", err)
	}

	b := releaseBundle{
		module:     module,
		checksums:  checksums,
		signature:  envBytes,
		provenance: provBytes,
		moduleFile: "ffmpeg-wasi-lgpl.wasm",
		provFile:   "provenance.json",
	}

	return b, keySet{id: &key.PublicKey}
}

// validBundle is the happy-path bundle for a variant: a fake module plus a
// provenance whose entry for that variant names the module file.
func validBundle(t *testing.T, key *rsa.PrivateKey, variant Variant) (releaseBundle, keySet) {
	t.Helper()

	moduleFile := "ffmpeg-wasi-" + string(variant) + ".wasm"
	module := []byte("\x00asm fake " + string(variant))

	prov := Provenance{
		FFmpegVersion:  "n8.1.2",
		BuildTag:       "n8.1.2-2",
		Commit:         "deadbeef",
		ToolingLicense: "MIT",
		Variants: map[string]ProvenanceVariant{
			"lgpl": {File: "ffmpeg-wasi-lgpl.wasm", License: "LGPL-2.1-or-later", H264Encode: "openh264"},
			"gpl":  {File: "ffmpeg-wasi-gpl.wasm", License: "GPL-2.0-or-later", H264Encode: "libx264"},
		},
	}

	provBytes, err := json.Marshal(prov)
	if err != nil {
		t.Fatalf("marshal provenance: %v", err)
	}

	return assembleBundle(t, key, moduleFile, module, provBytes)
}

func buildChecksums(files map[string][]byte) []byte {
	var b strings.Builder

	for name, data := range files {
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}

	return []byte(b.String())
}

func signChecksums(t *testing.T, key *rsa.PrivateKey, checksums []byte) []byte {
	t.Helper()

	h := sha256.Sum256(checksums)

	sig, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, h[:], nil)
	if err != nil {
		t.Fatalf("sign checksums: %v", err)
	}

	return sig
}

func TestVerifyRelease(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	t.Run("valid bundle verifies and returns provenance", func(t *testing.T) {
		t.Parallel()

		b, keys := validBundle(t, key, VariantLGPL)

		prov, err := verifyRelease(b, VariantLGPL, keys)
		if err != nil {
			t.Fatalf("verifyRelease: %v", err)
		}

		if prov.FFmpegVersion != "n8.1.2" || prov.BuildTag != "n8.1.2-2" {
			t.Fatalf("provenance not surfaced: %+v", prov)
		}
	})

	t.Run("tampered module is a checksum mismatch", func(t *testing.T) {
		t.Parallel()

		b, keys := validBundle(t, key, VariantLGPL)
		b.module = append(b.module, 'x')

		if _, err := verifyRelease(b, VariantLGPL, keys); !errors.Is(err, ErrChecksumMismatch) {
			t.Fatalf("want ErrChecksumMismatch, got %v", err)
		}
	})

	t.Run("tampered checksums break the signature", func(t *testing.T) {
		t.Parallel()

		b, keys := validBundle(t, key, VariantLGPL)
		b.checksums = append(b.checksums, []byte("deadbeef  evil.wasm\n")...)

		if _, err := verifyRelease(b, VariantLGPL, keys); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("want ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("tampered provenance is a checksum mismatch", func(t *testing.T) {
		t.Parallel()

		b, keys := validBundle(t, key, VariantLGPL)
		b.provenance = append(b.provenance, ' ')

		if _, err := verifyRelease(b, VariantLGPL, keys); !errors.Is(err, ErrChecksumMismatch) {
			t.Fatalf("want ErrChecksumMismatch, got %v", err)
		}
	})

	t.Run("unknown key id is rejected", func(t *testing.T) {
		t.Parallel()

		b, _ := validBundle(t, key, VariantLGPL)

		if _, err := verifyRelease(b, VariantLGPL, keySet{}); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("want ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("signature from an untrusted key is rejected", func(t *testing.T) {
		t.Parallel()

		b, _ := validBundle(t, key, VariantLGPL)

		other, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}

		// The set maps the bundle's key-id to a DIFFERENT key — verification must fail.
		spoofed := keySet{keyID(&key.PublicKey): &other.PublicKey}

		if _, err := verifyRelease(b, VariantLGPL, spoofed); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("want ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("malformed signature envelope is rejected", func(t *testing.T) {
		t.Parallel()

		b, keys := validBundle(t, key, VariantLGPL)
		b.signature = []byte("not json")

		if _, err := verifyRelease(b, VariantLGPL, keys); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("want ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("wrong signature algorithm is rejected", func(t *testing.T) {
		t.Parallel()

		b, keys := validBundle(t, key, VariantLGPL)
		b.signature, _ = json.Marshal(sigEnvelope{KeyID: keyID(&key.PublicKey), Algorithm: "RSASSA_PKCS1_V1_5_SHA_256", Signature: "AAAA"})

		if _, err := verifyRelease(b, VariantLGPL, keys); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("want ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("non-base64 signature is rejected", func(t *testing.T) {
		t.Parallel()

		b, keys := validBundle(t, key, VariantLGPL)
		b.signature, _ = json.Marshal(sigEnvelope{KeyID: keyID(&key.PublicKey), Algorithm: algRSASSAPSSSHA256, Signature: "@@@ not base64 @@@"})

		if _, err := verifyRelease(b, VariantLGPL, keys); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("want ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("malformed checksums line is rejected", func(t *testing.T) {
		t.Parallel()

		// A signed-but-malformed checksums file: passes the signature, fails parsing.
		b, keys := assembleBundleRaw(t, key, []byte("only-one-field\n"), []byte("\x00asm"), []byte("{}"))

		if _, err := verifyRelease(b, VariantLGPL, keys); err == nil {
			t.Fatal("want an error for a malformed checksums line")
		}
	})

	t.Run("provenance that is not valid JSON is rejected", func(t *testing.T) {
		t.Parallel()

		b, keys := assembleBundle(t, key, "ffmpeg-wasi-lgpl.wasm", []byte("\x00asm"), []byte("{not valid json"))

		if _, err := verifyRelease(b, VariantLGPL, keys); err == nil {
			t.Fatal("want an error for non-JSON provenance")
		}
	})

	t.Run("provenance that disagrees with the variant is rejected", func(t *testing.T) {
		t.Parallel()

		// A signed-but-inconsistent release: provenance's lgpl entry names a
		// different file than the module we fetched for lgpl. Re-signed (valid sig,
		// valid checksums) so only the provenance cross-check can catch it.
		prov := Provenance{
			FFmpegVersion: "n8.1.2",
			Variants: map[string]ProvenanceVariant{
				"lgpl": {File: "ffmpeg-wasi-gpl.wasm", License: "LGPL-2.1-or-later"},
			},
		}

		provBytes, err := json.Marshal(prov)
		if err != nil {
			t.Fatalf("marshal provenance: %v", err)
		}

		b, keys := assembleBundle(t, key, "ffmpeg-wasi-lgpl.wasm", []byte("\x00asm"), provBytes)

		if _, err := verifyRelease(b, VariantLGPL, keys); !errors.Is(err, ErrProvenanceMismatch) {
			t.Fatalf("want ErrProvenanceMismatch, got %v", err)
		}
	})
}
