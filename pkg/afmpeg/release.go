package afmpeg

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/verify"
)

// defaultReleaseBaseURL is the canonical ffmpeg-wasi generic-package layout on
// GitLab (project 83847809). WithReleaseBaseURL overrides it for a mirror or an
// internal store — the signature is over content, so the URL is untrusted input
// (spec 0010 D-0010-I).
const defaultReleaseBaseURL = "https://gitlab.com/api/v4/projects/83847809/packages/generic/ffmpeg-wasi"

const provenanceFile = "provenance.json"

// defaultWKDEmail is the release-signing identity whose key is published via the
// Web Key Directory on phpboyscout.uk. On every online fetch afmpeg cross-checks
// its embedded signing key against the copy served here — a control plane
// independent of GitLab (spec 0011). Empty disables the cross-check.
const defaultWKDEmail = "ffmpeg-wasi-release-v2@phpboyscout.uk"

// releaseConfig configures a certified release fetch.
type releaseConfig struct {
	baseURL   string
	bundleDir string // offline mode: a local dir of pre-fetched assets
	cacheDir  string
	client    *http.Client
	profile   Profile // capability profile; "" defaults to ProfileLean
	provOut   *Provenance

	// keys are armored OpenPGP public keys; nil → afmpeg's embedded set (tests
	// inject their own).
	keys [][]byte

	// wkdEmail drives the embedded↔WKD cross-check (spec 0011); empty disables it.
	// Defaults to defaultWKDEmail. The offline-bundle path never performs WKD.
	wkdEmail string
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

// WithReleaseProfile selects the capability profile to load: ProfileLean (the
// default — web-delivery essentials) or ProfileIntermediate (lean + every
// practical software codec/filter, spec 0022). The intermediate module is a
// distinct, separately-signed asset (ffmpeg-wasi-intermediate-<variant>.wasm)
// verified through the same trust chain — this is the certified equivalent of
// reaching for the intermediate build with WithModuleURL. ProfileFull is
// native-only (the heavy HEVC/AV1 encoders); WithModuleRelease rejects it — load it
// with native.NewFromRelease instead.
func WithReleaseProfile(profile Profile) ReleaseOption {
	return func(c *releaseConfig) { c.profile = profile }
}

// ResolveReleaseProfile applies the given release options and reports the selected
// capability profile, defaulting to ProfileLean. It lets a caller that must build a
// profile-dependent asset name outside this package — the native backend's driver
// filename (spec 0028) — honour WithReleaseProfile exactly as WithModuleRelease
// does, instead of re-implementing the option plumbing. An unknown profile is a
// typed error, matching WithModuleRelease's validation.
func ResolveReleaseProfile(opts ...ReleaseOption) (Profile, error) {
	rc := &releaseConfig{profile: ProfileLean}
	for _, opt := range opts {
		opt(rc)
	}

	switch rc.profile {
	case ProfileLean, ProfileIntermediate, ProfileFull:
		return rc.profile, nil
	default:
		return "", errors.Newf("afmpeg: unknown profile %q (want %q, %q or %q)", rc.profile, ProfileLean, ProfileIntermediate, ProfileFull)
	}
}

// WithReleaseHTTPClient overrides the HTTP client used to fetch the release (and,
// when the WKD cross-check is enabled, to fetch the WKD key).
func WithReleaseHTTPClient(client *http.Client) ReleaseOption {
	return func(c *releaseConfig) { c.client = client }
}

// WithReleaseProvenance writes the verified provenance of the loaded release into
// into — so a consumer can log or assert exactly what it loaded.
func WithReleaseProvenance(into *Provenance) ReleaseOption {
	return func(c *releaseConfig) { c.provOut = into }
}

// WithReleaseWKDEmail overrides the Web Key Directory identity afmpeg cross-checks
// its embedded signing key against (spec 0011). The default
// (ffmpeg-wasi-release@phpboyscout.uk) is correct for canonical releases; override
// it for a mirror that publishes its own WKD, or pass "" to disable the
// cross-check entirely (verification still happens against the pinned embedded
// key — the cross-check is a second, independent anchor, not the trust root).
func WithReleaseWKDEmail(email string) ReleaseOption {
	return func(c *releaseConfig) { c.wkdEmail = email }
}

// withReleaseKeys overrides the embedded trust keys (armored OpenPGP public
// keys). Unexported: only tests use it, to drive the public WithModuleRelease
// path against a generated key instead of the embedded production keys.
func withReleaseKeys(armoredKeys ...[]byte) ReleaseOption {
	return func(c *releaseConfig) { c.keys = armoredKeys }
}

// WithModuleRelease loads a certified ffmpeg-wasi release for (tag, variant): it
// fetches the module plus its checksums, signature, and provenance, and verifies
// — against afmpeg's pinned OpenPGP trust keys — the detached signature over the
// checksums, the module's and provenance's checksums, and that provenance names
// this variant (spec 0010, revised to gitlab.com/phpboyscout/go/signing). Only then
// is the module compiled. Any tamper fails with a typed error
// (signing/verify.ErrSignatureInvalid, ErrChecksumMismatch, ErrProvenanceMismatch).
//
// Unlike WithModuleURL (bring-your-own, uncertified), this path is for the
// project's own published releases — there is no way to skip verification.
//
// The default profile is ProfileLean; pass WithReleaseProfile(ProfileIntermediate)
// to load the full software-codec build. Either way the module, its provenance
// entry, and its checksum are verified through the same trust chain.
func WithModuleRelease(tag string, variant Variant, opts ...ReleaseOption) Option {
	return func(c *config) error {
		if variant != VariantLGPL && variant != VariantGPL {
			return errors.Newf("afmpeg: unknown variant %q (want %q or %q)", variant, VariantLGPL, VariantGPL)
		}

		rc := &releaseConfig{baseURL: defaultReleaseBaseURL, client: http.DefaultClient, profile: ProfileLean, wkdEmail: defaultWKDEmail}
		for _, opt := range opts {
			opt(rc)
		}

		if rc.profile != ProfileLean && rc.profile != ProfileIntermediate {
			if rc.profile == ProfileFull {
				return errors.Newf("afmpeg: profile %q is native-only — load it with native.NewFromRelease, not WithModuleRelease (there is no WASM full module)", ProfileFull)
			}

			return errors.Newf("afmpeg: unknown profile %q (want %q or %q)", rc.profile, ProfileLean, ProfileIntermediate)
		}

		c.fetch = func(ctx context.Context) ([]byte, error) {
			return fetchRelease(ctx, tag, variant, rc)
		}

		return nil
	}
}

// resolveTrust builds the trust set afmpeg verifies against. The trust root is
// always the embedded (or test-injected) OpenPGP signing key. When useWKD and a
// wkdEmail are set, the resolver also fetches that key from the Web Key Directory
// and requires the two to agree by fingerprint (spec 0011): a mismatch is a hard
// failure (ErrKeyResolverMismatch), but a WKD outage degrades gracefully to the
// embedded key (RequireExternalCrosscheck=false). The offline-bundle path passes
// useWKD=false — it must not reach the network.
func (rc *releaseConfig) resolveTrust(ctx context.Context, useWKD bool) (*verify.TrustSet, error) {
	keys := rc.keys
	if keys == nil {
		var err error
		if keys, err = embeddedTrustKeys(); err != nil {
			return nil, err
		}
	}

	cfg := verify.KeyResolverConfig{KeySource: "embedded"}
	if useWKD && rc.wkdEmail != "" {
		cfg = verify.KeyResolverConfig{
			KeySource:                 "both",
			ExternalKeyEmail:          rc.wkdEmail,
			RequireExternalCrosscheck: false,
			HTTPClient:                rc.client,
		}
	}

	resolver, err := verify.BuildKeyResolver(cfg, keys...)
	if err != nil {
		return nil, err
	}

	return resolver.Resolve(ctx)
}

func fetchRelease(ctx context.Context, tag string, variant Variant, rc *releaseConfig) ([]byte, error) {
	profile := rc.profile
	if profile == "" {
		profile = ProfileLean
	}

	return fetchAsset(ctx, tag, variantKeyFor(variant, profile), moduleFileFor(variant, profile), rc)
}

// fetchAsset verifies and returns one release asset: provKey names it in the
// signed provenance, assetFile is its filename. It runs the full trust chain
// (signature over checksums.txt, the asset's checksum, the provenance match) via
// the offline-bundle or the online path, honouring rc.
func fetchAsset(ctx context.Context, tag, provKey, assetFile string, rc *releaseConfig) ([]byte, error) {
	if rc.bundleDir != "" {
		trust, err := rc.resolveTrust(ctx, false) // offline: embedded only, no network
		if err != nil {
			return nil, err
		}

		return fetchReleaseOffline(rc, provKey, assetFile, trust)
	}

	trust, err := rc.resolveTrust(ctx, true) // online: embedded↔WKD cross-check
	if err != nil {
		return nil, err
	}

	return fetchReleaseOnline(ctx, tag, provKey, assetFile, rc, trust)
}

// FetchReleaseAsset fetches and verifies a single named asset from an ffmpeg-wasi
// release — the identical trust chain as WithModuleRelease: the KMS/OpenPGP
// signature over checksums.txt against afmpeg's pinned keys (with the WKD
// cross-check online), the asset's checksum, and that provenance names it under
// provKey. It returns the verified bytes and the release provenance.
//
// This is the general primitive behind acquiring any signed release artifact; it
// exists so the opt-in native backend (pkg/afmpeg/native) can fetch a certified
// native driver (spec 0028 D-0028-D) through the same trust root. Most consumers
// use WithModuleRelease (for the .wasm) or native.NewFromRelease (for the driver)
// rather than calling this directly. The ReleaseOptions (WithReleaseBaseURL,
// WithReleaseBundleDir, WithReleaseCacheDir, WithReleaseHTTPClient,
// WithReleaseWKDEmail, WithReleaseProvenance) all apply.
func FetchReleaseAsset(ctx context.Context, tag, assetFile, provKey string, opts ...ReleaseOption) ([]byte, Provenance, error) {
	rc := &releaseConfig{baseURL: defaultReleaseBaseURL, client: http.DefaultClient, wkdEmail: defaultWKDEmail}
	for _, opt := range opts {
		opt(rc)
	}

	var prov Provenance
	if rc.provOut == nil {
		rc.provOut = &prov
	}

	data, err := fetchAsset(ctx, tag, provKey, assetFile, rc)
	if err != nil {
		return nil, Provenance{}, err
	}

	return data, *rc.provOut, nil
}

// fetchReleaseOffline verifies a complete bundle read from a local directory
// (D-0010-G air-gap): everything is in memory, so the whole bundle goes through
// verifyRelease.
func fetchReleaseOffline(rc *releaseConfig, variantKey, moduleFile string, trust *verify.TrustSet) ([]byte, error) {
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

	prov, err := verifyRelease(bundle, variantKey, trust)
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
func fetchReleaseOnline(ctx context.Context, tag, variantKey, moduleFile string, rc *releaseConfig, trust *verify.TrustSet) ([]byte, error) {
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

	prov, moduleSHA, err := verifyManifest(trust, checksums, signature, provenance, variantKey, moduleFile, provenanceFile)
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
