package afmpeg

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"testing/fstest"
)

// TestLoadKeys_rejects covers the embedded-key loader's failure modes — a
// malformed key shipped in the binary is a build-time invariant we want loudly
// caught, not silently skipped.
func TestLoadKeys_rejects(t *testing.T) {
	t.Parallel()

	t.Run("empty set", func(t *testing.T) {
		t.Parallel()

		if _, err := loadKeys(fstest.MapFS{}); err == nil {
			t.Fatal("want an error when no keys are embedded")
		}
	})

	t.Run("not PEM", func(t *testing.T) {
		t.Parallel()

		fsys := fstest.MapFS{"keys/bad.pem": {Data: []byte("definitely not pem")}}
		if _, err := loadKeys(fsys); err == nil {
			t.Fatal("want an error for non-PEM content")
		}
	})

	t.Run("not an RSA key", func(t *testing.T) {
		t.Parallel()

		ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generate ec key: %v", err)
		}

		der, err := x509.MarshalPKIXPublicKey(&ec.PublicKey)
		if err != nil {
			t.Fatalf("marshal ec key: %v", err)
		}

		pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

		fsys := fstest.MapFS{"keys/ec.pem": {Data: pemBytes}}
		if _, err := loadKeys(fsys); err == nil {
			t.Fatal("want an error for a non-RSA key")
		}
	})
}
