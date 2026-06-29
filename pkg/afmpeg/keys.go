package afmpeg

import (
	"crypto/rsa"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"io/fs"

	"github.com/cockroachdb/errors"
)

// keyFS holds the trusted ffmpeg-wasi release-signing public keys, embedded into
// afmpeg so verification is offline and the trust root ships in the consumer
// (spec 0010 D-0010-C). Rotation adds a PEM here and ships an afmpeg release; the
// set form (D-0010-F) means old and new keys are both trusted during the overlap.
//
//go:embed keys/*.pem
var keyFS embed.FS

// releaseSigningKeys is the parsed embedded key-set, keyed by key-id. Built once
// at init; a malformed embedded key is a build-time invariant, so it panics.
var releaseSigningKeys = mustLoadKeys(keyFS)

func mustLoadKeys(fsys fs.FS) keySet {
	ks, err := loadKeys(fsys)
	if err != nil {
		panic(err)
	}

	return ks
}

// loadKeys parses every keys/*.pem in fsys into a key-set keyed by key-id.
func loadKeys(fsys fs.FS) (keySet, error) {
	names, err := fs.Glob(fsys, "keys/*.pem")
	if err != nil {
		return nil, err
	}

	ks := keySet{}

	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}

		block, _ := pem.Decode(data)
		if block == nil {
			return nil, errors.Newf("afmpeg: %s is not PEM", name)
		}

		pub, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, errors.Wrapf(err, "afmpeg: parse %s", name)
		}

		rsaPub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return nil, errors.Newf("afmpeg: %s is not an RSA public key", name)
		}

		ks[keyID(rsaPub)] = rsaPub
	}

	if len(ks) == 0 {
		return nil, errors.New("afmpeg: no embedded release-signing keys")
	}

	return ks, nil
}
