package afmpeg

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:])
}

func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer

	w := gzip.NewWriter(&buf)
	_, _ = w.Write(b)
	_ = w.Close()

	return buf.Bytes()
}

func serve(body []byte, hits *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits != nil {
			hits.Add(1)
		}

		_, _ = w.Write(body)
	}))
}

func TestFetchModule_DownloadThenCache(t *testing.T) {
	t.Parallel()

	content := []byte("fake wasm module bytes")

	var hits atomic.Int32

	srv := serve(content, &hits)
	t.Cleanup(srv.Close)

	dir := t.TempDir()

	for range 2 { // second call must hit the cache, not the server
		got, err := fetchModule(context.Background(), srv.URL, WithCacheDir(dir), WithSHA256(sha256hex(content)))
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("fetchModule: err=%v got=%q", err, got)
		}
	}

	if hits.Load() != 1 {
		t.Fatalf("server hit %d times, want 1 (second call should be cached)", hits.Load())
	}
}

func TestFetchModule_ChecksumMismatch(t *testing.T) {
	t.Parallel()

	srv := serve([]byte("actual content"), nil)
	t.Cleanup(srv.Close)

	_, err := fetchModule(context.Background(), srv.URL, WithCacheDir(t.TempDir()), WithSHA256(sha256hex([]byte("expected something else"))))
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
}

func TestFetchModule_Gunzip(t *testing.T) {
	t.Parallel()

	content := []byte("decompressed wasm content")

	srv := serve(gzipBytes(content), nil)
	t.Cleanup(srv.Close)

	got, err := fetchModule(context.Background(), srv.URL, WithCacheDir(t.TempDir()), WithGunzip(), WithSHA256(sha256hex(content)))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("gunzip fetch: err=%v got=%q", err, got)
	}
}

func TestFetchModule_BadStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	if _, err := fetchModule(context.Background(), srv.URL, WithCacheDir(t.TempDir())); err == nil {
		t.Fatal("want an error for a 404 response")
	}
}

func TestFetchModule_BadGzip(t *testing.T) {
	t.Parallel()

	srv := serve([]byte("not gzip"), nil)
	t.Cleanup(srv.Close)

	if _, err := fetchModule(context.Background(), srv.URL, WithCacheDir(t.TempDir()), WithGunzip()); err == nil {
		t.Fatal("want an error for non-gzip data with WithGunzip")
	}
}

func TestWithHTTPClientAndDefaultCacheDir(t *testing.T) {
	t.Parallel()

	if dir, err := resolveCacheDir(""); err != nil || dir == "" {
		t.Fatalf("resolveCacheDir(\"\") = %q err=%v", dir, err)
	}

	content := []byte("client-fetched wasm")

	srv := serve(content, nil)
	t.Cleanup(srv.Close)

	got, err := fetchModule(context.Background(), srv.URL,
		WithCacheDir(t.TempDir()), WithHTTPClient(srv.Client()), WithSHA256(sha256hex(content)))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("WithHTTPClient fetch: err=%v got=%q", err, got)
	}
}

func TestCacheKey(t *testing.T) {
	t.Parallel()

	if got := cacheKey("https://x/m.wasm", "ABCDEF"); got != "abcdef" {
		t.Fatalf("cacheKey with sha = %q, want lowercased sha", got)
	}

	if cacheKey("https://a", "") == cacheKey("https://b", "") {
		t.Fatal("distinct URLs should yield distinct cache keys")
	}
}
