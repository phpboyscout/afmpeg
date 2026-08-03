package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// fetchReleaseAsset is the verified-fetch seam (afmpeg.FetchReleaseAsset); a var so
// tests can drive NewFromRelease without a real signed release.
var fetchReleaseAsset = afmpeg.FetchReleaseAsset

const (
	// driverExecPerm is the mode of the cached driver (it must be executable).
	driverExecPerm = 0o755
	// shaTagBytes is how many bytes of the driver's SHA-256 tag its cache filename,
	// making the cache content-addressed (different builds → different files).
	shaTagBytes = 8
)

// NewFromRelease fetches and signature-verifies the certified native driver for
// (tag, variant) matching the host OS/arch from a published ffmpeg-wasi release,
// caches it as an executable, and returns a Backend that spawns it — the native
// equivalent of afmpeg.WithModuleRelease (spec 0028 D-0028-D). The driver's bytes
// are verified against the KMS/OpenPGP-signed checksums (afmpeg's pinned trust
// keys, with the WKD cross-check) before it is written or run, so a consumer never
// executes an unverified binary. The afmpeg ReleaseOptions apply (mirror base URL,
// air-gapped bundle dir, cache dir, HTTP client, WKD identity) — including
// WithReleaseProfile to select a heavier driver: ProfileIntermediate (the full
// software-codec batch — Opus/MP3/Vorbis/WebP/VP8-9 + subtitles, at native speed) or
// ProfileFull (intermediate + the heavy native-only encoders — SVT-AV1 in both
// variants, x265/HEVC in gpl). Full is native-only, so this is the only way to reach
// it. Defaults to ProfileLean.
//
//	rt, err := afmpeg.New(ctx, afmpeg.WithBackend(
//	    must(native.NewFromRelease(ctx, "n8.1.2-8", afmpeg.VariantGPL,
//	        afmpeg.WithReleaseProfile(afmpeg.ProfileFull)))))
func NewFromRelease(ctx context.Context, tag string, variant afmpeg.Variant, opts ...afmpeg.ReleaseOption) (*Backend, error) {
	if variant != afmpeg.VariantLGPL && variant != afmpeg.VariantGPL {
		return nil, errors.Newf("native: unknown variant %q (want %q or %q)", variant, afmpeg.VariantLGPL, afmpeg.VariantGPL)
	}

	profile, err := afmpeg.ResolveReleaseProfile(opts...)
	if err != nil {
		return nil, err
	}

	asset := driverAsset(variant, profile)

	data, _, err := fetchReleaseAsset(ctx, tag, asset, driverProvKey(variant, profile), opts...)
	if err != nil {
		return nil, errors.Wrapf(err, "native: acquire driver %s", asset)
	}

	path, err := cacheDriver(asset, data)
	if err != nil {
		return nil, err
	}

	return New(WithNativeBinary(path)), nil
}

// profileInfix is the naming segment a non-lean profile carries in the driver
// asset name / provenance key ("intermediate-" / "full-"); lean carries nothing,
// keeping the legacy platform-only names. Mirrors ffmpeg-wasi's build/sign-release.sh
// and the wasm-side ffmpeg-wasi-intermediate-<variant>.wasm convention (0022 parity).
func profileInfix(profile afmpeg.Profile) string {
	if profile == afmpeg.ProfileLean || profile == "" {
		return ""
	}

	return string(profile) + "-"
}

// driverAsset is the release filename of the native driver for the host platform +
// profile, mirroring ffmpeg-wasi's build/sign-release.sh (lean:
// ffmpeg-wasi-driver-linux-amd64-lgpl; intermediate:
// ffmpeg-wasi-driver-linux-amd64-intermediate-lgpl).
func driverAsset(variant afmpeg.Variant, profile afmpeg.Profile) string {
	return fmt.Sprintf("ffmpeg-wasi-driver-%s-%s-%s%s", runtime.GOOS, runtime.GOARCH, profileInfix(profile), variant)
}

// driverProvKey is the provenance `variants` key for that driver.
func driverProvKey(variant afmpeg.Variant, profile afmpeg.Profile) string {
	return fmt.Sprintf("driver-%s-%s-%s%s", runtime.GOOS, runtime.GOARCH, profileInfix(profile), variant)
}

// cacheDriver writes the already-verified driver bytes to a content-addressed
// executable under the user cache dir and returns its path, reusing an intact
// cached copy on a repeat load.
func cacheDriver(asset string, data []byte) (string, error) {
	sum := sha256.Sum256(data)

	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}

	dir := filepath.Join(base, "afmpeg", "native-driver")
	if err := os.MkdirAll(dir, driverExecPerm); err != nil { //nolint:gosec // a driver dir must be traversable
		return "", errors.Wrap(err, "native: cache dir")
	}

	path := filepath.Join(dir, asset+"-"+hex.EncodeToString(sum[:shaTagBytes]))

	// Reuse an intact cached copy (the bytes are already signature-verified).
	if existing, rerr := os.ReadFile(path); rerr == nil && sha256.Sum256(existing) == sum { //nolint:gosec // path is content-addressed in our cache
		return path, nil
	}

	if err := os.WriteFile(path, data, driverExecPerm); err != nil { //nolint:gosec // the driver must be executable
		return "", errors.Wrap(err, "native: cache driver")
	}

	return path, nil
}
