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

	plat := runtime.GOOS + "-" + runtime.GOARCH

	// Lean carries the platform-only name (unchanged legacy convention).
	wantAsset := "ffmpeg-wasi-driver-" + plat + "-lgpl"
	if got := driverAsset(afmpeg.VariantLGPL, afmpeg.ProfileLean); got != wantAsset {
		t.Fatalf("driverAsset(lean) = %q, want %q", got, wantAsset)
	}

	wantKey := "driver-" + plat + "-gpl"
	if got := driverProvKey(afmpeg.VariantGPL, afmpeg.ProfileLean); got != wantKey {
		t.Fatalf("driverProvKey(lean) = %q, want %q", got, wantKey)
	}

	// The empty profile is treated as lean (NewFromRelease resolves "" → lean).
	if got := driverAsset(afmpeg.VariantLGPL, ""); got != wantAsset {
		t.Fatalf("driverAsset(\"\") = %q, want %q", got, wantAsset)
	}

	// Intermediate slots the profile before the variant, mirroring the wasm module.
	wantIntAsset := "ffmpeg-wasi-driver-" + plat + "-intermediate-lgpl"
	if got := driverAsset(afmpeg.VariantLGPL, afmpeg.ProfileIntermediate); got != wantIntAsset {
		t.Fatalf("driverAsset(intermediate) = %q, want %q", got, wantIntAsset)
	}

	wantIntKey := "driver-" + plat + "-intermediate-gpl"
	if got := driverProvKey(afmpeg.VariantGPL, afmpeg.ProfileIntermediate); got != wantIntKey {
		t.Fatalf("driverProvKey(intermediate) = %q, want %q", got, wantIntKey)
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

	if gotTag != "n8.1.2-7" || gotAsset != driverAsset(afmpeg.VariantLGPL, afmpeg.ProfileLean) || gotKey != driverProvKey(afmpeg.VariantLGPL, afmpeg.ProfileLean) {
		t.Fatalf("fetch called with tag=%q asset=%q key=%q", gotTag, gotAsset, gotKey)
	}

	if info, err := os.Stat(b.binary); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("driver not cached executable: %v (%v)", info, err)
	}
}

// WithReleaseProfile(intermediate) resolves the intermediate driver asset + key.
func TestNewFromRelease_intermediateProfile(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	orig := fetchReleaseAsset
	t.Cleanup(func() { fetchReleaseAsset = orig })

	var gotAsset, gotKey string

	fetchReleaseAsset = func(_ context.Context, _, asset, key string, _ ...afmpeg.ReleaseOption) ([]byte, afmpeg.Provenance, error) {
		gotAsset, gotKey = asset, key

		return []byte("\x7fELF fake driver"), afmpeg.Provenance{}, nil
	}

	if _, err := NewFromRelease(context.Background(), "n8.1.2-7", afmpeg.VariantLGPL,
		afmpeg.WithReleaseProfile(afmpeg.ProfileIntermediate)); err != nil {
		t.Fatalf("NewFromRelease: %v", err)
	}

	if want := driverAsset(afmpeg.VariantLGPL, afmpeg.ProfileIntermediate); gotAsset != want {
		t.Fatalf("asset = %q, want %q", gotAsset, want)
	}

	if want := driverProvKey(afmpeg.VariantLGPL, afmpeg.ProfileIntermediate); gotKey != want {
		t.Fatalf("provKey = %q, want %q", gotKey, want)
	}
}

// An unknown profile is rejected before any fetch.
func TestNewFromRelease_unknownProfile(t *testing.T) {
	t.Parallel()

	if _, err := NewFromRelease(context.Background(), "n8.1.2-7", afmpeg.VariantLGPL,
		afmpeg.WithReleaseProfile(afmpeg.Profile("bogus"))); err == nil {
		t.Fatal("want an error for an unknown profile")
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
