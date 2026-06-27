package afmpeg

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
)

const (
	// maxModuleBytes caps a downloaded (and decompressed) module to guard against
	// a runaway or hostile response; a real ffmpeg.wasm is tens of MB.
	maxModuleBytes = 256 << 20 // 256 MiB

	cacheDirPerm  = 0o755
	cacheFilePerm = 0o644
)

// ErrChecksumMismatch is returned by a URL-sourced module whose downloaded bytes
// do not match the expected SHA-256.
var ErrChecksumMismatch = errors.New("afmpeg: module checksum mismatch")

// fetchConfig configures a module download.
type fetchConfig struct {
	sha256   string
	gunzip   bool
	cacheDir string
	client   *http.Client
}

// FetchOption configures WithModuleURL.
type FetchOption func(*fetchConfig)

// WithSHA256 verifies the downloaded module against an expected hex SHA-256.
// Strongly recommended — the module is executable code.
func WithSHA256(hexsum string) FetchOption {
	return func(c *fetchConfig) { c.sha256 = hexsum }
}

// WithGunzip decompresses a gzip-compressed download (e.g. an ffmpeg.wasm.gz).
func WithGunzip() FetchOption {
	return func(c *fetchConfig) { c.gunzip = true }
}

// WithCacheDir overrides where the module is cached (default: the OS user cache
// dir, under "afmpeg").
func WithCacheDir(dir string) FetchOption {
	return func(c *fetchConfig) { c.cacheDir = dir }
}

// WithHTTPClient overrides the HTTP client used for the download.
func WithHTTPClient(client *http.Client) FetchOption {
	return func(c *fetchConfig) { c.client = client }
}

// WithModuleURL downloads the wasm module from url and uses it, caching it
// locally so it is fetched only once. The consumer chooses the URL and accepts
// its licence — afmpeg bundles no module (spec 0004 D-C). Pair it with
// WithSHA256: the module is executable code, so verify it.
func WithModuleURL(url string, opts ...FetchOption) Option {
	return func(c *config) error {
		c.fetch = func(ctx context.Context) ([]byte, error) {
			return fetchModule(ctx, url, opts...)
		}

		return nil
	}
}

func fetchModule(ctx context.Context, url string, opts ...FetchOption) ([]byte, error) {
	fc := &fetchConfig{client: http.DefaultClient}
	for _, opt := range opts {
		opt(fc)
	}

	cacheDir, err := resolveCacheDir(fc.cacheDir)
	if err != nil {
		return nil, err
	}

	cachePath := filepath.Join(cacheDir, cacheKey(url, fc.sha256)+".wasm")

	if module, ok := readCache(cachePath, fc.sha256); ok {
		return module, nil
	}

	module, err := download(ctx, fc, url)
	if err != nil {
		return nil, err
	}

	writeCache(cacheDir, cachePath, module) // best-effort

	return module, nil
}

func download(ctx context.Context, fc *fetchConfig, url string) ([]byte, error) {
	//nolint:gosec // the module URL is caller-supplied by design (spec 0004 D-C)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "afmpeg: build module request")
	}

	resp, err := fc.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "afmpeg: download module")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("afmpeg: download module: unexpected status %s", resp.Status)
	}

	module, err := readLimited(resp.Body)
	if err != nil {
		return nil, err
	}

	if fc.gunzip {
		if module, err = gunzipBytes(module); err != nil {
			return nil, err
		}
	}

	if err := verifyChecksum(module, fc.sha256); err != nil {
		return nil, err
	}

	return module, nil
}

func readLimited(r io.Reader) ([]byte, error) {
	module, err := io.ReadAll(io.LimitReader(r, maxModuleBytes+1))
	if err != nil {
		return nil, errors.Wrap(err, "afmpeg: read module")
	}

	if int64(len(module)) > maxModuleBytes {
		return nil, errors.Newf("afmpeg: module exceeds the %d-byte limit", maxModuleBytes)
	}

	return module, nil
}

func gunzipBytes(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.Wrap(err, "afmpeg: gunzip module")
	}
	defer func() { _ = zr.Close() }()

	module, err := readLimited(zr)
	if err != nil {
		return nil, errors.Wrap(err, "afmpeg: gunzip module")
	}

	return module, nil
}

func verifyChecksum(module []byte, want string) error {
	if want == "" {
		return nil
	}

	got := sha256.Sum256(module)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return errors.Wrapf(ErrChecksumMismatch, "got %s", hex.EncodeToString(got[:]))
	}

	return nil
}

func resolveCacheDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}

	base, err := os.UserCacheDir()
	if err != nil {
		return "", errors.Wrap(err, "afmpeg: resolve cache dir")
	}

	return filepath.Join(base, "afmpeg"), nil
}

// cacheKey is the cache filename stem: the expected SHA (content-addressed) when
// known, else a hash of the URL.
func cacheKey(url, sha string) string {
	if sha != "" {
		return strings.ToLower(sha)
	}

	sum := sha256.Sum256([]byte(url))

	return hex.EncodeToString(sum[:])
}

func readCache(path, sha string) ([]byte, bool) {
	module, err := os.ReadFile(path) //nolint:gosec // cache path derived from the caller's URL/sha
	if err != nil {
		return nil, false
	}

	if verifyChecksum(module, sha) != nil {
		return nil, false
	}

	return module, true
}

func writeCache(dir, path string, module []byte) {
	if err := os.MkdirAll(dir, cacheDirPerm); err != nil {
		return
	}

	_ = os.WriteFile(path, module, cacheFilePerm)
}
