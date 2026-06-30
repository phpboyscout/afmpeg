package afmpeg

import (
	"slices"
	"testing"

	"gitlab.com/phpboyscout/signing/verify"
)

// signingKeyFingerprint pins afmpeg's trust root: the OpenPGP fingerprint of
// ffmpeg-wasi's release-signing key (minted from KMS key
// alias/ffmpeg-wasi-release-signing-v1, creation time 2026-06-30T00:00:00Z). A
// mismatch means the wrong key was shipped — the whole point of an embedded,
// pinned trust root.
const signingKeyFingerprint = "710881C1DDAEABD138E53004A2166E59EB6060E1"

// TestEmbeddedTrustKeys checks the embedded .asc keys load into a valid trust set
// and that the pinned ffmpeg-wasi signing key is present.
func TestEmbeddedTrustKeys(t *testing.T) {
	t.Parallel()

	keys, err := embeddedTrustKeys()
	if err != nil {
		t.Fatalf("embeddedTrustKeys: %v", err)
	}

	ts, err := verify.LoadTrustSet(keys...)
	if err != nil {
		t.Fatalf("embedded keys do not form a valid trust set: %v", err)
	}

	if fps := ts.Fingerprints(); !slices.Contains(fps, signingKeyFingerprint) {
		t.Fatalf("pinned signing-key fingerprint %s absent; embedded set has %v", signingKeyFingerprint, fps)
	}
}
