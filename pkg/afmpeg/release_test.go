package afmpeg

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/errors"
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

func cloneAssets(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}

func TestResolveTrust_embeddedDefault(t *testing.T) {
	t.Parallel()

	// nil keys → afmpeg's embedded trust set (the production default path).
	rc := &releaseConfig{client: http.DefaultClient}

	ts, err := rc.resolveTrust(context.Background())
	if err != nil {
		t.Fatalf("resolveTrust from embedded keys: %v", err)
	}

	if ts == nil || len(ts.Fingerprints()) == 0 {
		t.Fatal("embedded keys produced an empty trust set")
	}
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
		WithReleaseProvenance(&prov),
	} {
		opt(rc)
	}

	if rc.baseURL != "https://mirror.example/pkg" || rc.bundleDir != "/srv/bundle" ||
		rc.cacheDir != "/var/cache/afmpeg" || rc.client != client || rc.provOut != &prov {
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
}
