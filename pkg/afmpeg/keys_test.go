package afmpeg

import "testing"

// TestEmbeddedReleaseSigningKeys pins the trust root: the embedded set must
// contain the ffmpeg-wasi release-signing key with exactly the expected key-id
// (the hex SHA-256 of its SPKI DER), an RSA-4096 key. A mismatch means the wrong
// key was shipped — the whole point of an embedded, pinned trust root.
func TestEmbeddedReleaseSigningKeys(t *testing.T) {
	t.Parallel()

	const wantKeyID = "1698ceea3728c7e5cc89288675e643c1e9b6110ae88575aeaa15148eb9630a76"

	pub, ok := releaseSigningKeys[wantKeyID]
	if !ok {
		got := make([]string, 0, len(releaseSigningKeys))
		for id := range releaseSigningKeys {
			got = append(got, id)
		}

		t.Fatalf("pinned key-id %s absent; embedded set has %v", wantKeyID, got)
	}

	if pub.Size() != 512 { // RSA-4096 modulus is 512 bytes
		t.Fatalf("pinned key is %d-byte modulus, want 512 (RSA-4096)", pub.Size())
	}
}
