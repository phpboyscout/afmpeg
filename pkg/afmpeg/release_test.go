package afmpeg

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/errors"
)

// releaseAssets builds the three manifest assets for a (variant, module), signed
// by key — mirroring exactly what build/sign-release.sh produces in ffmpeg-wasi.
func releaseAssets(t *testing.T, key *rsa.PrivateKey, variant Variant, module []byte) (checksums, signature, provenance []byte) {
	t.Helper()

	moduleFile := "ffmpeg-wasi-" + string(variant) + ".wasm"

	prov := Provenance{
		FFmpegVersion:  "n8.1.2",
		BuildTag:       "n8.1.2-3",
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

	env := sigEnvelope{
		KeyID:     keyID(&key.PublicKey),
		Algorithm: algRSASSAPSSSHA256,
		Signature: base64.StdEncoding.EncodeToString(signChecksums(t, key, checksums)),
	}

	signature, err = json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal signature: %v", err)
	}

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

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	trusted := keySet{keyID(&key.PublicKey): &key.PublicKey}
	module := []byte("\x00asm pretend-module-lgpl")
	checksums, signature, provenance := releaseAssets(t, key, VariantLGPL, module)
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
		rc := &releaseConfig{baseURL: srv.URL, client: srv.Client(), keys: trusted, cacheDir: t.TempDir(), provOut: &gotProv}

		got, err := fetchRelease(context.Background(), "n8.1.2-3", VariantLGPL, rc)
		if err != nil {
			t.Fatalf("fetchRelease: %v", err)
		}

		if !bytes.Equal(got, module) {
			t.Fatal("returned module bytes differ from served module")
		}

		if gotProv.FFmpegVersion != "n8.1.2" || gotProv.BuildTag != "n8.1.2-3" {
			t.Fatalf("provenance not surfaced: %+v", gotProv)
		}
	})

	t.Run("rejects a tampered module", func(t *testing.T) {
		t.Parallel()

		tampered := cloneAssets(assets)
		tampered[moduleFile] = append(append([]byte{}, module...), 'x')
		srv := serveAssets(t, tampered)

		rc := &releaseConfig{baseURL: srv.URL, client: srv.Client(), keys: trusted, cacheDir: t.TempDir()}
		if _, err := fetchRelease(context.Background(), "n8.1.2-3", VariantLGPL, rc); !errors.Is(err, ErrChecksumMismatch) {
			t.Fatalf("want ErrChecksumMismatch, got %v", err)
		}
	})

	t.Run("rejects a signature from an untrusted key", func(t *testing.T) {
		t.Parallel()

		srv := serveAssets(t, assets)
		// Empty trust set — the (valid) signature's key-id is not embedded.
		rc := &releaseConfig{baseURL: srv.URL, client: srv.Client(), keys: keySet{}, cacheDir: t.TempDir()}

		if _, err := fetchRelease(context.Background(), "n8.1.2-3", VariantLGPL, rc); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("want ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("errors when an online asset is missing", func(t *testing.T) {
		t.Parallel()

		// A server with only the module — checksums.txt 404s.
		srv := serveAssets(t, map[string][]byte{moduleFile: module})
		rc := &releaseConfig{baseURL: srv.URL, client: srv.Client(), keys: trusted, cacheDir: t.TempDir()}

		if _, err := fetchRelease(context.Background(), "n8.1.2-3", VariantLGPL, rc); err == nil {
			t.Fatal("want an error when checksums.txt is absent")
		}
	})

	t.Run("errors when an offline bundle file is missing", func(t *testing.T) {
		t.Parallel()

		rc := &releaseConfig{bundleDir: t.TempDir(), keys: trusted} // empty dir
		if _, err := fetchRelease(context.Background(), "n8.1.2-3", VariantLGPL, rc); err == nil {
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

		rc := &releaseConfig{bundleDir: dir, keys: trusted}

		got, err := fetchRelease(context.Background(), "n8.1.2-3", VariantLGPL, rc)
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

	if err := WithModuleRelease("n8.1.2-3", Variant("bogus"))(&config{}); err == nil {
		t.Fatal("want an error for an unknown variant, got nil")
	}

	cfg := &config{}
	if err := WithModuleRelease("n8.1.2-3", VariantLGPL)(cfg); err != nil {
		t.Fatalf("valid variant: %v", err)
	}

	if cfg.fetch == nil {
		t.Fatal("WithModuleRelease did not set the deferred fetch")
	}
}
