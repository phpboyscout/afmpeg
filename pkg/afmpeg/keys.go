package afmpeg

import (
	"embed"
	"io/fs"

	"github.com/cockroachdb/errors"
)

// keyFS holds afmpeg's pinned OpenPGP public keys (ASCII-armored): the
// ffmpeg-wasi release-signing key and the shared org rotation-authority key.
// Embedding them is the trust root (spec 0010 revised / 0011) — it ships inside
// the verifying binary, so it can't be tampered with after build, and the WKD
// cross-check (0011) compares the live domain copy against it.
//
//go:embed keys/*.asc
var keyFS embed.FS

// embeddedTrustKeys returns the armored trust keys, one []byte per .asc file, for
// signing/verify's LoadTrustSet / BuildKeyResolver.
func embeddedTrustKeys() ([][]byte, error) {
	names, err := fs.Glob(keyFS, "keys/*.asc")
	if err != nil {
		return nil, err
	}

	keys := make([][]byte, 0, len(names))

	for _, name := range names {
		data, err := fs.ReadFile(keyFS, name)
		if err != nil {
			return nil, err
		}

		keys = append(keys, data)
	}

	if len(keys) == 0 {
		return nil, errors.New("afmpeg: no embedded trust keys")
	}

	return keys, nil
}
