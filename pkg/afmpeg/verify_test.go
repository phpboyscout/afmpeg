package afmpeg

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"gitlab.com/phpboyscout/go/signing/openpgpkey"
	"gitlab.com/phpboyscout/go/signing/verify"
)

var testKeyEpoch = time.Unix(0, 0)

// testSigningKey returns a generated RSA-3072 key (the minimum signing/verify
// accepts) and its armored OpenPGP public half — the publisher's keypair stand-in.
func testSigningKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pub, err := openpgpkey.ArmoredPublicKey(priv, "ffmpeg-wasi Release", "ffmpeg-wasi-release@phpboyscout.uk", testKeyEpoch)
	if err != nil {
		t.Fatalf("armor public key: %v", err)
	}

	return priv, pub
}

// detachSign produces an ASCII-armored OpenPGP detached signature over msg —
// exactly what `gtb sign` emits for checksums.txt.
func detachSign(t *testing.T, priv *rsa.PrivateKey, armoredPub, msg []byte) []byte {
	t.Helper()

	sig, err := openpgpkey.DetachSign(priv, armoredPub, bytes.NewReader(msg), testKeyEpoch)
	if err != nil {
		t.Fatalf("detach-sign: %v", err)
	}

	return sig
}

func mustTrust(t *testing.T, armoredPubs ...[]byte) *verify.TrustSet {
	t.Helper()

	ts, err := verify.LoadTrustSet(armoredPubs...)
	if err != nil {
		t.Fatalf("load trust set: %v", err)
	}

	return ts
}

func buildChecksums(files map[string][]byte) []byte {
	var b strings.Builder

	for name, data := range files {
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}

	return []byte(b.String())
}

// assembleBundle signs the given module + provenance with priv into a bundle.
func assembleBundle(t *testing.T, priv *rsa.PrivateKey, pub []byte, moduleFile string, module, provBytes []byte) releaseBundle {
	t.Helper()

	checksums := buildChecksums(map[string][]byte{moduleFile: module, "provenance.json": provBytes})

	return releaseBundle{
		module:     module,
		checksums:  checksums,
		signature:  detachSign(t, priv, pub, checksums),
		provenance: provBytes,
		moduleFile: moduleFile,
		provFile:   "provenance.json",
	}
}

// assembleBundleRaw signs the given checksums bytes verbatim, so a test can
// present a signed-but-malformed checksums file to the parser.
func assembleBundleRaw(t *testing.T, priv *rsa.PrivateKey, pub, checksums, module, provBytes []byte) releaseBundle {
	t.Helper()

	return releaseBundle{
		module:     module,
		checksums:  checksums,
		signature:  detachSign(t, priv, pub, checksums),
		provenance: provBytes,
		moduleFile: "ffmpeg-wasi-lgpl.wasm",
		provFile:   "provenance.json",
	}
}

// validBundle is the happy-path bundle for a variant: a fake module plus a
// provenance whose entry for that variant names the module file.
func validBundle(t *testing.T, priv *rsa.PrivateKey, pub []byte, variant Variant) releaseBundle {
	t.Helper()

	moduleFile := "ffmpeg-wasi-" + string(variant) + ".wasm"
	module := []byte("\x00asm fake " + string(variant))

	prov := Provenance{
		FFmpegVersion:  "n8.1.2",
		BuildTag:       "n8.1.2-4",
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

	return assembleBundle(t, priv, pub, moduleFile, module, provBytes)
}

func TestVerifyRelease(t *testing.T) {
	t.Parallel()

	priv, pub := testSigningKey(t)
	trust := mustTrust(t, pub)

	t.Run("valid bundle verifies and returns provenance", func(t *testing.T) {
		t.Parallel()

		b := validBundle(t, priv, pub, VariantLGPL)

		prov, err := verifyRelease(b, string(VariantLGPL), trust)
		if err != nil {
			t.Fatalf("verifyRelease: %v", err)
		}

		if prov.FFmpegVersion != "n8.1.2" || prov.BuildTag != "n8.1.2-4" {
			t.Fatalf("provenance not surfaced: %+v", prov)
		}
	})

	t.Run("tampered module is a checksum mismatch", func(t *testing.T) {
		t.Parallel()

		b := validBundle(t, priv, pub, VariantLGPL)
		b.module = append(b.module, 'x')

		if _, err := verifyRelease(b, string(VariantLGPL), trust); !errors.Is(err, ErrChecksumMismatch) {
			t.Fatalf("want ErrChecksumMismatch, got %v", err)
		}
	})

	t.Run("tampered checksums break the signature", func(t *testing.T) {
		t.Parallel()

		b := validBundle(t, priv, pub, VariantLGPL)
		b.checksums = append(b.checksums, []byte("deadbeef  evil.wasm\n")...)

		if _, err := verifyRelease(b, string(VariantLGPL), trust); !errors.Is(err, verify.ErrSignatureInvalid) {
			t.Fatalf("want ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("tampered provenance is a checksum mismatch", func(t *testing.T) {
		t.Parallel()

		b := validBundle(t, priv, pub, VariantLGPL)
		b.provenance = append(b.provenance, ' ')

		if _, err := verifyRelease(b, string(VariantLGPL), trust); !errors.Is(err, ErrChecksumMismatch) {
			t.Fatalf("want ErrChecksumMismatch, got %v", err)
		}
	})

	t.Run("signature from a key not in the trust set is rejected", func(t *testing.T) {
		t.Parallel()

		other, otherPub := testSigningKey(t)
		b := validBundle(t, other, otherPub, VariantLGPL) // signed by an untrusted key

		if _, err := verifyRelease(b, string(VariantLGPL), trust); !errors.Is(err, verify.ErrSignatureInvalid) {
			t.Fatalf("want ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("a malformed signature is rejected", func(t *testing.T) {
		t.Parallel()

		b := validBundle(t, priv, pub, VariantLGPL)
		b.signature = []byte("-----BEGIN PGP SIGNATURE-----\nnot a real sig\n-----END PGP SIGNATURE-----\n")

		if _, err := verifyRelease(b, string(VariantLGPL), trust); !errors.Is(err, verify.ErrSignatureInvalid) {
			t.Fatalf("want ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("malformed checksums line is rejected", func(t *testing.T) {
		t.Parallel()

		b := assembleBundleRaw(t, priv, pub, []byte("only-one-field\n"), []byte("\x00asm"), []byte("{}"))

		if _, err := verifyRelease(b, string(VariantLGPL), trust); err == nil {
			t.Fatal("want an error for a malformed checksums line")
		}
	})

	t.Run("provenance that is not valid JSON is rejected", func(t *testing.T) {
		t.Parallel()

		b := assembleBundle(t, priv, pub, "ffmpeg-wasi-lgpl.wasm", []byte("\x00asm"), []byte("{not valid json"))

		if _, err := verifyRelease(b, string(VariantLGPL), trust); err == nil {
			t.Fatal("want an error for non-JSON provenance")
		}
	})

	t.Run("intermediate profile verifies against its provenance key", func(t *testing.T) {
		t.Parallel()

		moduleFile := "ffmpeg-wasi-intermediate-lgpl.wasm"
		module := []byte("\x00asm fake intermediate-lgpl")

		prov := Provenance{
			FFmpegVersion: "n8.1.2",
			Variants: map[string]ProvenanceVariant{
				"lgpl":              {File: "ffmpeg-wasi-lgpl.wasm", License: "LGPL-2.1-or-later", H264Encode: "openh264", Profile: "lean"},
				"intermediate-lgpl": {File: moduleFile, License: "LGPL-2.1-or-later", H264Encode: "openh264", Profile: "intermediate"},
			},
		}

		provBytes, err := json.Marshal(prov)
		if err != nil {
			t.Fatalf("marshal provenance: %v", err)
		}

		b := assembleBundle(t, priv, pub, moduleFile, module, provBytes)

		got, err := verifyRelease(b, variantKeyFor(VariantLGPL, ProfileIntermediate), trust)
		if err != nil {
			t.Fatalf("verifyRelease (intermediate): %v", err)
		}

		if got.Variants["intermediate-lgpl"].Profile != "intermediate" {
			t.Fatalf("intermediate profile not surfaced: %+v", got.Variants)
		}

		// The lean key must not satisfy the intermediate module: the keys are distinct.
		if _, err := verifyRelease(b, variantKeyFor(VariantLGPL, ProfileLean), trust); !errors.Is(err, ErrProvenanceMismatch) {
			t.Fatalf("lean key should not name the intermediate module; got %v", err)
		}
	})

	t.Run("provenance that disagrees with the variant is rejected", func(t *testing.T) {
		t.Parallel()

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

		b := assembleBundle(t, priv, pub, "ffmpeg-wasi-lgpl.wasm", []byte("\x00asm"), provBytes)

		if _, err := verifyRelease(b, string(VariantLGPL), trust); !errors.Is(err, ErrProvenanceMismatch) {
			t.Fatalf("want ErrProvenanceMismatch, got %v", err)
		}
	})
}
