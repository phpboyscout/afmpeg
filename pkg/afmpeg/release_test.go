package afmpeg

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cockroachdb/errors"
	"gitlab.com/phpboyscout/signing/openpgpkey"
	"gitlab.com/phpboyscout/signing/verify"
)

// releaseAssets builds the three manifest assets for a (variant, module), signed
// by priv — mirroring what `gtb sign` produces in ffmpeg-wasi.
func releaseAssets(t *testing.T, priv *rsa.PrivateKey, pub []byte, variant Variant, module []byte) (checksums, signature, provenance []byte) {
	t.Helper()

	moduleFile := "ffmpeg-wasi-" + string(variant) + ".wasm"

	prov := Provenance{
		FFmpegVersion:  "n8.1.2",
		BuildTag:       "n8.1.2-4",
		Commit:         "abc123",
		ToolingLicense: "MIT",
		Variants: map[string]ProvenanceVariant{
			"lgpl": {File: "ffmpeg-wasi-lgpl.wasm", License: "LGPL-2.1-or-later", H264Encode: "openh264"},
			"gpl":  {File: "ffmpeg-wasi-gpl.wasm", License: "GPL-2.0-or-later", H264Encode: "libx264"},
		},
	}

	provenance, err := json.Marshal(prov)
	if err != nil {
		t.Fatalf("marshal provenance: %v", err)
	}

	checksums = buildChecksums(map[string][]byte{moduleFile: module, "provenance.json": provenance})
	signature = detachSign(t, priv, pub, checksums)

	return checksums, signature, provenance
}

// serveAssets serves a name→bytes map by basename (the release layout is
// /<tag>/<file>); an unknown name 404s.
func serveAssets(t *testing.T, assets map[string][]byte) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := assets[path.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestFetchRelease(t *testing.T) {
	t.Parallel()

	priv, pub := testSigningKey(t)
	module := []byte("\x00asm pretend-module-lgpl")
	checksums, signature, provenance := releaseAssets(t, priv, pub, VariantLGPL, module)
	moduleFile := "ffmpeg-wasi-lgpl.wasm"

	assets := map[string][]byte{
		moduleFile:          module,
		"checksums.txt":     checksums,
		"checksums.txt.sig": signature,
		"provenance.json":   provenance,
	}

	t.Run("verifies and returns the module + provenance", func(t *testing.T) {
		t.Parallel()

		srv := serveAssets(t, assets)

		var gotProv Provenance
		rc := &releaseConfig{baseURL: srv.URL, client: srv.Client(), keys: [][]byte{pub}, cacheDir: t.TempDir(), provOut: &gotProv}

		got, err := fetchRelease(context.Background(), "n8.1.2-4", VariantLGPL, rc)
		if err != nil {
			t.Fatalf("fetchRelease: %v", err)
		}

		if !bytes.Equal(got, module) {
			t.Fatal("returned module bytes differ from served module")
		}

		if gotProv.FFmpegVersion != "n8.1.2" || gotProv.BuildTag != "n8.1.2-4" {
			t.Fatalf("provenance not surfaced: %+v", gotProv)
		}
	})

	t.Run("rejects a tampered module", func(t *testing.T) {
		t.Parallel()

		tampered := cloneAssets(assets)
		tampered[moduleFile] = append(append([]byte{}, module...), 'x')
		srv := serveAssets(t, tampered)

		rc := &releaseConfig{baseURL: srv.URL, client: srv.Client(), keys: [][]byte{pub}, cacheDir: t.TempDir()}
		if _, err := fetchRelease(context.Background(), "n8.1.2-4", VariantLGPL, rc); !errors.Is(err, ErrChecksumMismatch) {
			t.Fatalf("want ErrChecksumMismatch, got %v", err)
		}
	})

	t.Run("rejects a signature from an untrusted key", func(t *testing.T) {
		t.Parallel()

		srv := serveAssets(t, assets)
		_, otherPub := testSigningKey(t) // a key that did not sign these assets
		rc := &releaseConfig{baseURL: srv.URL, client: srv.Client(), keys: [][]byte{otherPub}, cacheDir: t.TempDir()}

		if _, err := fetchRelease(context.Background(), "n8.1.2-4", VariantLGPL, rc); !errors.Is(err, verify.ErrSignatureInvalid) {
			t.Fatalf("want ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("errors when an online asset is missing", func(t *testing.T) {
		t.Parallel()

		srv := serveAssets(t, map[string][]byte{moduleFile: module}) // checksums.txt 404s
		rc := &releaseConfig{baseURL: srv.URL, client: srv.Client(), keys: [][]byte{pub}, cacheDir: t.TempDir()}

		if _, err := fetchRelease(context.Background(), "n8.1.2-4", VariantLGPL, rc); err == nil {
			t.Fatal("want an error when checksums.txt is absent")
		}
	})

	t.Run("errors when an offline bundle file is missing", func(t *testing.T) {
		t.Parallel()

		rc := &releaseConfig{bundleDir: t.TempDir(), keys: [][]byte{pub}} // empty dir
		if _, err := fetchRelease(context.Background(), "n8.1.2-4", VariantLGPL, rc); err == nil {
			t.Fatal("want an error when the bundle is incomplete")
		}
	})

	t.Run("verifies an offline bundle directory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		for name, data := range assets {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatalf("write bundle file: %v", err)
			}
		}

		rc := &releaseConfig{bundleDir: dir, keys: [][]byte{pub}}

		got, err := fetchRelease(context.Background(), "n8.1.2-4", VariantLGPL, rc)
		if err != nil {
			t.Fatalf("offline fetchRelease: %v", err)
		}

		if !bytes.Equal(got, module) {
			t.Fatal("offline module bytes differ")
		}
	})
}

// intermediateAssets builds the manifest for the intermediate LGPL profile: the
// module is ffmpeg-wasi-intermediate-lgpl.wasm and provenance carries both the
// lean and the intermediate-lgpl entries (as a real release does).
func intermediateAssets(t *testing.T, priv *rsa.PrivateKey, pub, module []byte) (checksums, signature, provenance []byte) {
	t.Helper()

	moduleFile := "ffmpeg-wasi-intermediate-lgpl.wasm"

	prov := Provenance{
		FFmpegVersion:  "n8.1.2",
		BuildTag:       "n8.1.2-6",
		Commit:         "abc123",
		ToolingLicense: "MIT",
		Variants: map[string]ProvenanceVariant{
			"lgpl":              {File: "ffmpeg-wasi-lgpl.wasm", License: "LGPL-2.1-or-later", H264Encode: "openh264", Profile: "lean"},
			"intermediate-lgpl": {File: moduleFile, License: "LGPL-2.1-or-later", H264Encode: "openh264", Profile: "intermediate"},
		},
	}

	provenance, err := json.Marshal(prov)
	if err != nil {
		t.Fatalf("marshal provenance: %v", err)
	}

	checksums = buildChecksums(map[string][]byte{moduleFile: module, "provenance.json": provenance})
	signature = detachSign(t, priv, pub, checksums)

	return checksums, signature, provenance
}

func TestFetchRelease_intermediateProfile(t *testing.T) {
	t.Parallel()

	priv, pub := testSigningKey(t)
	module := []byte("\x00asm pretend-intermediate-lgpl")
	checksums, signature, provenance := intermediateAssets(t, priv, pub, module)
	moduleFile := "ffmpeg-wasi-intermediate-lgpl.wasm"

	assets := map[string][]byte{
		moduleFile:          module,
		"checksums.txt":     checksums,
		"checksums.txt.sig": signature,
		"provenance.json":   provenance,
	}

	t.Run("online fetch resolves the intermediate module + provenance key", func(t *testing.T) {
		t.Parallel()

		srv := serveAssets(t, assets)

		var gotProv Provenance
		rc := &releaseConfig{baseURL: srv.URL, client: srv.Client(), keys: [][]byte{pub}, cacheDir: t.TempDir(), profile: ProfileIntermediate, provOut: &gotProv}

		got, err := fetchRelease(context.Background(), "n8.1.2-6", VariantLGPL, rc)
		if err != nil {
			t.Fatalf("fetchRelease (intermediate): %v", err)
		}

		if !bytes.Equal(got, module) {
			t.Fatal("returned module bytes differ from served intermediate module")
		}

		if gotProv.Variants["intermediate-lgpl"].Profile != "intermediate" {
			t.Fatalf("intermediate profile not surfaced: %+v", gotProv.Variants)
		}
	})

	t.Run("offline bundle resolves the intermediate module", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		for name, data := range assets {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatalf("write bundle file: %v", err)
			}
		}

		rc := &releaseConfig{bundleDir: dir, keys: [][]byte{pub}, profile: ProfileIntermediate}

		got, err := fetchRelease(context.Background(), "n8.1.2-6", VariantLGPL, rc)
		if err != nil {
			t.Fatalf("offline fetchRelease (intermediate): %v", err)
		}

		if !bytes.Equal(got, module) {
			t.Fatal("offline intermediate module bytes differ")
		}
	})

	t.Run("default (lean) profile does not resolve the intermediate module", func(t *testing.T) {
		t.Parallel()

		// Serving only the intermediate assets, a lean fetch looks for
		// ffmpeg-wasi-lgpl.wasm and its lean provenance entry — which name a file
		// absent from checksums. It must not silently fall back to intermediate.
		srv := serveAssets(t, assets)
		rc := &releaseConfig{baseURL: srv.URL, client: srv.Client(), keys: [][]byte{pub}, cacheDir: t.TempDir()} // profile "" → lean

		if _, err := fetchRelease(context.Background(), "n8.1.2-6", VariantLGPL, rc); err == nil {
			t.Fatal("lean fetch must not resolve against an intermediate-only release")
		}
	})
}

func cloneAssets(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}

func TestResolveTrust_embeddedDefault(t *testing.T) {
	t.Parallel()

	// nil keys → afmpeg's embedded trust set; useWKD=false → no network.
	rc := &releaseConfig{client: http.DefaultClient}

	ts, err := rc.resolveTrust(context.Background(), false)
	if err != nil {
		t.Fatalf("resolveTrust from embedded keys: %v", err)
	}

	if ts == nil || len(ts.Fingerprints()) == 0 {
		t.Fatal("embedded keys produced an empty trust set")
	}
}

// wkdRoundTripper serves one fixed body for any request (the WKD fetch), or an
// error to simulate an outage — so the cross-check can be driven without the
// network or the real domain.
type wkdRoundTripper struct {
	body []byte
	err  error
}

func (rt wkdRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	if rt.err != nil {
		return nil, rt.err
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(rt.body)),
		Header:     make(http.Header),
	}, nil
}

func wkdClient(rt wkdRoundTripper) *http.Client { return &http.Client{Transport: rt} }

// binaryWKDKey serializes priv's public key into the binary form a WKD hu file
// serves (UID ffmpeg-wasi-release@phpboyscout.uk, matching defaultWKDEmail).
func binaryWKDKey(t *testing.T, priv *rsa.PrivateKey) []byte {
	t.Helper()

	e, err := openpgpkey.Entity(priv, "ffmpeg-wasi Release Signing", defaultWKDEmail, testKeyEpoch)
	if err != nil {
		t.Fatalf("entity: %v", err)
	}

	var buf bytes.Buffer
	if err := e.Serialize(&buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}

	return buf.Bytes()
}

func TestResolveTrust_WKDCrosscheck(t *testing.T) {
	t.Parallel()

	priv, pub := testSigningKey(t) // UID is defaultWKDEmail

	t.Run("embedded key matching WKD resolves", func(t *testing.T) {
		t.Parallel()

		rc := &releaseConfig{keys: [][]byte{pub}, wkdEmail: defaultWKDEmail, client: wkdClient(wkdRoundTripper{body: binaryWKDKey(t, priv)})}

		ts, err := rc.resolveTrust(context.Background(), true)
		if err != nil {
			t.Fatalf("matching cross-check should succeed: %v", err)
		}

		if !slices.Contains(ts.Fingerprints(), signingKeyFingerprintOf(t, pub)) {
			t.Fatalf("resolved trust set lacks the signing key: %v", ts.Fingerprints())
		}
	})

	t.Run("WKD serving a different key is a hard mismatch", func(t *testing.T) {
		t.Parallel()

		otherPriv, _ := testSigningKey(t) // same UID, different key

		rc := &releaseConfig{keys: [][]byte{pub}, wkdEmail: defaultWKDEmail, client: wkdClient(wkdRoundTripper{body: binaryWKDKey(t, otherPriv)})}

		if _, err := rc.resolveTrust(context.Background(), true); !errors.Is(err, verify.ErrKeyResolverMismatch) {
			t.Fatalf("want ErrKeyResolverMismatch, got %v", err)
		}
	})

	t.Run("WKD outage degrades to the embedded key", func(t *testing.T) {
		t.Parallel()

		rc := &releaseConfig{keys: [][]byte{pub}, wkdEmail: defaultWKDEmail, client: wkdClient(wkdRoundTripper{err: errors.New("network down")})}

		ts, err := rc.resolveTrust(context.Background(), true)
		if err != nil {
			t.Fatalf("a WKD outage must not fail the load: %v", err)
		}

		if !slices.Contains(ts.Fingerprints(), signingKeyFingerprintOf(t, pub)) {
			t.Fatal("degraded trust set lost the embedded key")
		}
	})

	t.Run("offline path never touches WKD", func(t *testing.T) {
		t.Parallel()

		// The transport errors if used; useWKD=false must avoid it entirely.
		rc := &releaseConfig{keys: [][]byte{pub}, wkdEmail: defaultWKDEmail, client: wkdClient(wkdRoundTripper{err: errors.New("WKD must not be called offline")})}

		if _, err := rc.resolveTrust(context.Background(), false); err != nil {
			t.Fatalf("offline resolveTrust should not reach WKD: %v", err)
		}
	})
}

// signingKeyFingerprintOf returns the fingerprint of an armored key, for asserting
// a resolved trust set contains the expected key.
func signingKeyFingerprintOf(t *testing.T, armored []byte) string {
	t.Helper()

	ts, err := verify.LoadTrustSet(armored)
	if err != nil {
		t.Fatalf("load trust set: %v", err)
	}

	fps := ts.Fingerprints()
	if len(fps) != 1 {
		t.Fatalf("expected one fingerprint, got %v", fps)
	}

	return fps[0]
}

func TestReleaseOptions(t *testing.T) {
	t.Parallel()

	client := &http.Client{}

	var prov Provenance

	rc := &releaseConfig{}
	for _, opt := range []ReleaseOption{
		WithReleaseBaseURL("https://mirror.example/pkg"),
		WithReleaseBundleDir("/srv/bundle"),
		WithReleaseCacheDir("/var/cache/afmpeg"),
		WithReleaseHTTPClient(client),
		WithReleaseProfile(ProfileIntermediate),
		WithReleaseProvenance(&prov),
		WithReleaseWKDEmail("mirror-release@example.test"),
	} {
		opt(rc)
	}

	if rc.baseURL != "https://mirror.example/pkg" || rc.bundleDir != "/srv/bundle" ||
		rc.cacheDir != "/var/cache/afmpeg" || rc.client != client || rc.provOut != &prov ||
		rc.profile != ProfileIntermediate || rc.wkdEmail != "mirror-release@example.test" {
		t.Fatalf("release options did not apply: %+v", rc)
	}
}

func TestWithModuleRelease_validatesVariant(t *testing.T) {
	t.Parallel()

	if err := WithModuleRelease("n8.1.2-4", Variant("bogus"))(&config{}); err == nil {
		t.Fatal("want an error for an unknown variant, got nil")
	}

	cfg := &config{}
	if err := WithModuleRelease("n8.1.2-4", VariantLGPL)(cfg); err != nil {
		t.Fatalf("valid variant: %v", err)
	}

	if cfg.fetch == nil {
		t.Fatal("WithModuleRelease did not set the deferred fetch")
	}

	if err := WithModuleRelease("n8.1.2-4", VariantLGPL, WithReleaseProfile(Profile("bogus")))(&config{}); err == nil {
		t.Fatal("want an error for an unknown profile, got nil")
	}

	if err := WithModuleRelease("n8.1.2-4", VariantLGPL, WithReleaseProfile(ProfileIntermediate))(&config{}); err != nil {
		t.Fatalf("valid intermediate profile: %v", err)
	}
}
