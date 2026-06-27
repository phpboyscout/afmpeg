package afmpeg_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// TestWithModuleURL downloads the module through New end-to-end (served over
// httptest) and compiles it.
func TestWithModuleURL(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(guestModule)
	}))
	t.Cleanup(srv.Close)

	rt, err := afmpeg.New(context.Background(),
		afmpeg.WithModuleURL(srv.URL, afmpeg.WithCacheDir(t.TempDir())))
	if err != nil {
		t.Fatalf("New WithModuleURL: %v", err)
	}

	_ = rt.Close(context.Background())
}

// TestWithModuleURL_DownloadError surfaces a fetch failure from New.
func TestWithModuleURL_DownloadError(t *testing.T) {
	t.Parallel()

	_, err := afmpeg.New(context.Background(),
		afmpeg.WithModuleURL("http://127.0.0.1:0/nope.wasm", afmpeg.WithCacheDir(t.TempDir())))
	if err == nil {
		t.Fatal("New with an unreachable module URL should error")
	}
}
