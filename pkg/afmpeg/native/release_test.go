package native

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime"
	"testing"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

func TestDriverAssetAndProvKey(t *testing.T) {
	t.Parallel()

	wantAsset := "ffmpeg-wasi-driver-" + runtime.GOOS + "-" + runtime.GOARCH + "-lgpl"
	if got := driverAsset(afmpeg.VariantLGPL); got != wantAsset {
		t.Fatalf("driverAsset = %q, want %q", got, wantAsset)
	}

	wantKey := "driver-" + runtime.GOOS + "-" + runtime.GOARCH + "-gpl"
	if got := driverProvKey(afmpeg.VariantGPL); got != wantKey {
		t.Fatalf("driverProvKey = %q, want %q", got, wantKey)
	}
}

func TestCacheDriver_writesExecutableAndReuses(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir()) // keep it out of the real user cache; t.Setenv forbids t.Parallel

	data := []byte("\x7fELF fake driver")

	p, err := cacheDriver("ffmpeg-wasi-driver-test", data)
	if err != nil {
		t.Fatalf("cacheDriver: %v", err)
	}

	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("driver is not executable: %v", info.Mode())
	}

	if got, _ := os.ReadFile(p); !bytes.Equal(got, data) { //nolint:gosec // p is our cache path
		t.Fatal("cached bytes differ")
	}

	// A repeat with the same bytes reuses the same path.
	if p2, err := cacheDriver("ffmpeg-wasi-driver-test", data); err != nil || p2 != p {
		t.Fatalf("reuse: p2=%q err=%v, want %q", p2, err, p)
	}
}

func TestNewFromRelease_unknownVariant(t *testing.T) {
	t.Parallel()

	if _, err := NewFromRelease(context.Background(), "n8.1.2-7", afmpeg.Variant("bogus")); err == nil {
		t.Fatal("want an error for an unknown variant")
	}
}

// The two below mutate the package-level fetch seam, so they are NOT parallel.

func TestNewFromRelease_fetchesAndCaches(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	driver := []byte("\x7fELF fake driver")
	orig := fetchReleaseAsset

	t.Cleanup(func() { fetchReleaseAsset = orig })

	var gotTag, gotAsset, gotKey string

	fetchReleaseAsset = func(_ context.Context, tag, asset, key string, _ ...afmpeg.ReleaseOption) ([]byte, afmpeg.Provenance, error) {
		gotTag, gotAsset, gotKey = tag, asset, key

		return driver, afmpeg.Provenance{}, nil
	}

	b, err := NewFromRelease(context.Background(), "n8.1.2-7", afmpeg.VariantLGPL)
	if err != nil {
		t.Fatalf("NewFromRelease: %v", err)
	}

	if b == nil || b.binary == "" {
		t.Fatal("no backend / empty driver path")
	}

	if gotTag != "n8.1.2-7" || gotAsset != driverAsset(afmpeg.VariantLGPL) || gotKey != driverProvKey(afmpeg.VariantLGPL) {
		t.Fatalf("fetch called with tag=%q asset=%q key=%q", gotTag, gotAsset, gotKey)
	}

	if info, err := os.Stat(b.binary); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("driver not cached executable: %v (%v)", info, err)
	}
}

func TestNewFromRelease_fetchError(t *testing.T) {
	orig := fetchReleaseAsset

	t.Cleanup(func() { fetchReleaseAsset = orig })

	sentinel := errors.New("fetch failed")
	fetchReleaseAsset = func(context.Context, string, string, string, ...afmpeg.ReleaseOption) ([]byte, afmpeg.Provenance, error) {
		return nil, afmpeg.Provenance{}, sentinel
	}

	if _, err := NewFromRelease(context.Background(), "n8.1.2-7", afmpeg.VariantGPL); !errors.Is(err, sentinel) {
		t.Fatalf("want the fetch error surfaced, got %v", err)
	}
}
