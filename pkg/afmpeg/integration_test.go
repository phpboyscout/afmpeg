package afmpeg_test

import (
	"context"
	"os"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// TestIntegration_RealFFmpeg runs a real ffmpeg.wasm over the bridge end-to-end.
// It is gated on AFMPEG_TEST_FFMPEG_WASM (a path to a real ffmpeg.wasm), so the
// default unit run stays fast and needs no module. It proves the whole stack —
// the env setjmp/longjmp module, the core features, the vfs bridge — runs real
// ffmpeg, decoding/filtering/encoding/muxing entirely in memory.
func TestIntegration_RealFFmpeg(t *testing.T) {
	t.Parallel()

	module := os.Getenv("AFMPEG_TEST_FFMPEG_WASM")
	if module == "" {
		t.Skip("set AFMPEG_TEST_FFMPEG_WASM to a real ffmpeg.wasm to run this test")
	}

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()

	// Generate a tiny H.264 mp4 entirely in memory (no input file) — real decode/
	// filter/encode/mux plus the vfs write path (the moov-atom seek-on-write that
	// libx264's mp4 muxer performs).
	res, err := rt.Run(ctx, fs,
		"-f", "lavfi", "-i", "testsrc=size=64x64:rate=5:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "out.mp4")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("ffmpeg exit %d:\n%s", res.ExitCode, res.Stderr)
	}

	out, err := afero.ReadFile(fs, "out.mp4")
	if err != nil || len(out) == 0 {
		t.Fatalf("no output mp4 in the in-memory fs: err=%v len=%d", err, len(out))
	}

	// An mp4 begins with a size-prefixed "ftyp" box.
	if len(out) < 12 || string(out[4:8]) != "ftyp" {
		t.Fatalf("output is not an mp4 (first bytes: %q)", out[:min(12, len(out))])
	}

	// The output exists only in the in-memory fs, never on the host disk.
	if _, err := os.Stat("out.mp4"); !os.IsNotExist(err) {
		t.Fatalf("output leaked to the host filesystem: %v", err)
	}

	// Probe the duration of what we just rendered — real `ffmpeg -i` over the
	// bridge (the input was 1s of testsrc).
	p, err := rt.Probe(ctx, fs, "out.mp4")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	if p.DurationSec < 0.9 || p.DurationSec > 1.5 {
		t.Fatalf("Probe duration = %v, want ~1.0", p.DurationSec)
	}
}
