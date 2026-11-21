package native_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg/native"
)

// TestIntegration_NativeBackend_ReportsItsSandbox is spec 0047 against a real
// driver: the job result and --capabilities agree, and only an unconfined run
// warns. AFMPEG_TEST_REQUIRE_LANDLOCK=1 makes anything but landlock a failure,
// for a host known to have it.
func TestIntegration_NativeBackend_ReportsItsSandbox(t *testing.T) {
	t.Parallel()

	driver := integrationDriver(t, afmpeg.ProfileLean, false)

	var logs bytes.Buffer
	b := native.New(native.WithNativeBinary(driver), native.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))

	rt, err := afmpeg.New(context.Background(), afmpeg.WithBackend(b))
	if err != nil {
		t.Fatalf("afmpeg.New(native backend): %v", err)
	}
	defer func() { _ = rt.Close(context.Background()) }()

	host, err := b.Sandbox(context.Background())
	if err != nil {
		t.Fatalf("Sandbox: %v", err)
	}
	t.Logf("this host gives the native driver sandbox %q", host)

	if os.Getenv("AFMPEG_TEST_REQUIRE_LANDLOCK") == "1" && host != afmpeg.SandboxLandlock {
		t.Fatalf("Sandbox = %q, but AFMPEG_TEST_REQUIRE_LANDLOCK=1 says this host has Landlock", host)
	}

	fs := afero.NewMemMapFs()
	var in bytes.Buffer
	if err := png.Encode(&in, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, "in.png", in.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		res, err := rt.Run(context.Background(), fs,
			`{"op":"process","inputs":[{"path":"in.png","format":"image2"}],`+
				`"outputs":[{"path":"out.png","video_codec":"png"}]}`)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("Run exited %d: %s", res.ExitCode, res.Stderr)
		}
		if res.Sandbox != host {
			t.Fatalf("job Sandbox = %q, --capabilities said %q", res.Sandbox, host)
		}
	}

	wantWarnings := 0
	if host == afmpeg.SandboxUnavailable {
		wantWarnings = 1
	}
	if got := strings.Count(logs.String(), "level=WARN"); got != wantWarnings {
		t.Errorf("warnings over two jobs = %d, want %d\n%s", got, wantWarnings, logs.String())
	}
}
