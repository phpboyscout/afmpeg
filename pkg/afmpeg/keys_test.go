package afmpeg

import (
	"slices"
	"testing"

	"gitlab.com/phpboyscout/go/signing/verify"
)

// signingKeyFingerprint pins afmpeg's trust root: the OpenPGP fingerprint of
// ffmpeg-wasi's v1 release-signing key (minted from KMS key
// alias/ffmpeg-wasi-release-signing-v1, creation time 2026-06-30T00:00:00Z). A
// mismatch means the wrong key was shipped — the whole point of an embedded,
// pinned trust root. Kept forever so v1-signed releases verify forever.
const signingKeyFingerprint = "710881C1DDAEABD138E53004A2166E59EB6060E1"

// signingKeyV2Fingerprint pins the v2 release-signing key
// (alias/ffmpeg-wasi-release-signing-v2 in prod 060730976733), added in
// the 2026-07-24 dual-anchor key rotation. The embedded set must equal
// the WKD bucket for ffmpeg-wasi-release-v2@phpboyscout.uk exactly.
const signingKeyV2Fingerprint = "4C96ECB35C7446619FF78EB1ED1344E576B7BBBF" // gitleaks:allow — public-key fingerprint, not a secret

// TestEmbeddedTrustKeys checks afmpeg's trust root: the embedded .asc keys load
// into a valid trust set that is *exactly* the pinned ffmpeg-wasi signing key.
// The set must equal the WKD bucket for the cross-check (spec 0011), so the
// offline rotation-authority key is deliberately not embedded here.
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

	want := []string{signingKeyV2Fingerprint, signingKeyFingerprint}
	slices.Sort(want)

	if fps := ts.Fingerprints(); !slices.Equal(fps, want) {
		t.Fatalf("embedded trust set = %v, want exactly %v", fps, want)
	}
}
