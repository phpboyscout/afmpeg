package afmpeg_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// Proves the dav1d-WASM spike: the single-threaded (atomics-free) libdav1d built
// into the intermediate .wasm (a) LOADS in wazero — if it emitted wasm atomics,
// afmpeg.New would fail — and (b) DECODES AV1. Gated on AFMPEG_TEST_FFMPEG_WASI
// pointing at the spike intermediate module + a host ffmpeg to synthesise AV1.
func TestIntegration_WASM_AV1Decode(t *testing.T) {
	module := integrationModule(t, afmpeg.ProfileIntermediate)

	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no host ffmpeg to synthesise an AV1 fixture")
	}

	dir := t.TempDir()
	av1 := filepath.Join(dir, "in.mkv")

	// A tiny AV1 clip (host libsvtav1) — the input the wasm dav1d must decode.
	gen := exec.CommandContext(context.Background(), ff, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=25:duration=1",
		"-c:v", "libsvtav1", "-preset", "12", "-pix_fmt", "yuv420p", av1)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("synthesise AV1 fixture: %v: %s", err, out)
	}

	fixture, err := os.ReadFile(av1)
	if err != nil {
		t.Fatal(err)
	}

	// (a) LOAD — if the module carried wasm atomics, wazero would reject it here.
	ctx := context.Background()
	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("afmpeg.New(spike module): %v — did the module carry atomics?", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.mkv", fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	// (b) DECODE — AV1 in → H.264 out (openh264). The decode step exercises dav1d.
	cmd := afmpeg.Command{
		Inputs:        []afmpeg.Input{{Path: "in.mkv"}},
		FilterComplex: "[0:v]format=yuv420p[v]",
		Outputs: []afmpeg.Output{{
			Path: "out.mp4", Map: []string{"[v]"}, VideoCodec: "libopenh264",
		}},
	}

	res, err := rt.RunJob(ctx, fs, cmd)
	if err != nil {
		t.Fatalf("RunJob (AV1 decode): %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("AV1 decode exit %d: %s", res.ExitCode, res.Stderr)
	}

	out, err := afero.ReadFile(fs, "out.mp4")
	if err != nil || len(out) == 0 {
		t.Fatalf("out.mp4 missing/empty: %v", err)
	}

	if !bytes.Contains(out[:min(64, len(out))], []byte("ftyp")) {
		t.Fatal("out.mp4 is not an MP4")
	}

	t.Logf("dav1d-WASM spike OK: module loaded in wazero + decoded AV1 → H.264 (%d bytes)", len(out))
}
