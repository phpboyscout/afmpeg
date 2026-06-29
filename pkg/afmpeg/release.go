package afmpeg

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cockroachdb/errors"
)

// defaultReleaseBaseURL is the canonical ffmpeg-wasi generic-package layout on
// GitLab (project 83847809). WithReleaseBaseURL overrides it for a mirror or an
// internal store — the signature is over content, so the URL is untrusted input
// (spec 0010 D-0010-I).
const defaultReleaseBaseURL = "https://gitlab.com/api/v4/projects/83847809/packages/generic/ffmpeg-wasi"

const provenanceFile = "provenance.json"

// releaseConfig configures a certified release fetch.
type releaseConfig struct {
	baseURL   string
	bundleDir string // offline mode: a local dir of pre-fetched assets
	cacheDir  string
	client    *http.Client
	provOut   *Provenance
	keys      keySet // the trusted set; defaults to the embedded keys (tests inject)
}

// ReleaseOption configures WithModuleRelease.
type ReleaseOption func(*releaseConfig)

// WithReleaseBaseURL fetches the release from an alternate base (a mirror or an
// internal artifact store). The signature is still verified against the pinned
// key, so the source is untrusted — only the bytes' authenticity matters.
func WithReleaseBaseURL(url string) ReleaseOption {
	return func(c *releaseConfig) { c.baseURL = url }
}

// WithReleaseBundleDir verifies a release from a local directory of pre-fetched
// assets (the module, checksums.txt, checksums.txt.sig, provenance.json) — for
// air-gapped use. Verification is identical and still mandatory (D-0010-G); only
// the fetch is replaced.
func WithReleaseBundleDir(dir string) ReleaseOption {
	return func(c *releaseConfig) { c.bundleDir = dir }
}

// WithReleaseCacheDir overrides where the verified module is cached.
func WithReleaseCacheDir(dir string) ReleaseOption {
	return func(c *releaseConfig) { c.cacheDir = dir }
}

// WithReleaseHTTPClient overrides the HTTP client used to fetch the release.
func WithReleaseHTTPClient(client *http.Client) ReleaseOption {
	return func(c *releaseConfig) { c.client = client }
}

// WithReleaseProvenance writes the verified provenance of the loaded release into
// into — so a consumer can log or assert exactly what it loaded.
func WithReleaseProvenance(into *Provenance) ReleaseOption {
	return func(c *releaseConfig) { c.provOut = into }
}

// withReleaseKeys overrides the trusted key-set. Unexported: only tests use it,
// to drive the public WithModuleRelease path against a generated key instead of
// the embedded production key (which can only be signed for by KMS).
func withReleaseKeys(keys keySet) ReleaseOption {
	return func(c *releaseConfig) { c.keys = keys }
}

// WithModuleRelease loads a certified ffmpeg-wasi release for (tag, variant): it
// fetches the module plus its checksums, signature, and provenance, and verifies
// — against afmpeg's pinned signing key — the KMS signature over the checksums,
// the module's and provenance's checksums, and that provenance names this variant
// (spec 0010). Only then is the module compiled. Any tamper fails with a typed
// error (ErrSignatureInvalid, ErrChecksumMismatch, ErrProvenanceMismatch).
//
// Unlike WithModuleURL (bring-your-own, uncertified), this path is for the
// project's own published releases — there is no way to skip verification.
func WithModuleRelease(tag string, variant Variant, opts ...ReleaseOption) Option {
	return func(c *config) error {
		if variant != VariantLGPL && variant != VariantGPL {
			return errors.Newf("afmpeg: unknown variant %q (want %q or %q)", variant, VariantLGPL, VariantGPL)
		}

		rc := &releaseConfig{
			baseURL: defaultReleaseBaseURL,
			client:  http.DefaultClient,
			keys:    releaseSigningKeys,
		}
		for _, opt := range opts {
			opt(rc)
		}

		c.fetch = func(ctx context.Context) ([]byte, error) {
			return fetchRelease(ctx, tag, variant, rc)
		}

		return nil
	}
}

func fetchRelease(ctx context.Context, tag string, variant Variant, rc *releaseConfig) ([]byte, error) {
	moduleFile := "ffmpeg-wasi-" + string(variant) + ".wasm"

	if rc.bundleDir != "" {
		return fetchReleaseOffline(rc, variant, moduleFile)
	}

	return fetchReleaseOnline(ctx, tag, variant, moduleFile, rc)
}

// fetchReleaseOffline verifies a complete bundle read from a local directory
// (D-0010-G air-gap): everything is in memory, so the whole bundle goes through
// verifyRelease.
func fetchReleaseOffline(rc *releaseConfig, variant Variant, moduleFile string) ([]byte, error) {
	module, err := readBundleFile(rc.bundleDir, moduleFile)
	if err != nil {
		return nil, err
	}

	checksums, err := readBundleFile(rc.bundleDir, "checksums.txt")
	if err != nil {
		return nil, err
	}

	signature, err := readBundleFile(rc.bundleDir, "checksums.txt.sig")
	if err != nil {
		return nil, err
	}

	provenance, err := readBundleFile(rc.bundleDir, provenanceFile)
	if err != nil {
		return nil, err
	}

	bundle := releaseBundle{
		module:     module,
		checksums:  checksums,
		signature:  signature,
		provenance: provenance,
		moduleFile: moduleFile,
		provFile:   provenanceFile,
	}

	prov, err := verifyRelease(bundle, variant, rc.keys)
	if err != nil {
		return nil, err
	}

	if rc.provOut != nil {
		*rc.provOut = prov
	}

	return module, nil
}

// fetchReleaseOnline verifies the signed manifest first — establishing the
// module's trusted SHA before any module byte is trusted — then fetches the
// module through the content-addressed cache (a repeat load skips the download;
// a corrupt cache entry self-heals on its checksum).
func fetchReleaseOnline(ctx context.Context, tag string, variant Variant, moduleFile string, rc *releaseConfig) ([]byte, error) {
	checksums, err := downloadAsset(ctx, rc, tag, "checksums.txt")
	if err != nil {
		return nil, err
	}

	signature, err := downloadAsset(ctx, rc, tag, "checksums.txt.sig")
	if err != nil {
		return nil, err
	}

	provenance, err := downloadAsset(ctx, rc, tag, provenanceFile)
	if err != nil {
		return nil, err
	}

	prov, moduleSHA, err := verifyManifest(checksums, signature, provenance, variant, moduleFile, provenanceFile, rc.keys)
	if err != nil {
		return nil, err
	}

	module, err := fetchModule(ctx, assetURL(rc.baseURL, tag, moduleFile),
		WithSHA256(moduleSHA), WithCacheDir(rc.cacheDir), WithHTTPClient(rc.client))
	if err != nil {
		return nil, err
	}

	if rc.provOut != nil {
		*rc.provOut = prov
	}

	return module, nil
}

func downloadAsset(ctx context.Context, rc *releaseConfig, tag, name string) ([]byte, error) {
	return download(ctx, &fetchConfig{client: rc.client}, assetURL(rc.baseURL, tag, name))
}

func assetURL(base, tag, name string) string {
	return base + "/" + tag + "/" + name
}

func readBundleFile(dir, name string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // dir is caller-supplied by design
	if err != nil {
		return nil, errors.Wrapf(err, "afmpeg: read %s from bundle", name)
	}

	return data, nil
}
