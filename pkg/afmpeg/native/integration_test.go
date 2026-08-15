package native_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg/native"
)

// TestIntegration_NativeBackend_RemuxOverIPC is the end-to-end proof of spec 0028
// Backend B: afmpeg drives the native driver, and a real remux round-trips
// entirely through the caller's afero.Fs over the IPC bridge — no host-disk file
// for the input or the output.
//
// Gated on AFMPEG_TEST_NATIVE_DRIVER (a lean-or-richer native driver binary, built
// with ffmpeg-wasi's build/Dockerfile.native) and a host ffmpeg to synthesise a
// fixture; skipped otherwise, so CI stays green without the artifact.
func TestIntegration_NativeBackend_RemuxOverIPC(t *testing.T) {
	t.Parallel()

	driver := integrationDriver(t, afmpeg.ProfileLean, false)

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no host ffmpeg to synthesise a fixture")
	}

	// Synthesise a small H.264/AAC mp4 with the host ffmpeg, then load its bytes
	// into an in-memory filesystem — the input never touches the guest's disk.
	dir := t.TempDir()
	src := filepath.Join(dir, "src.mp4")

	gen := exec.CommandContext(context.Background(), ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("synthesise fixture: %v: %s", err, out)
	}

	fixture, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.mp4", fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	// Drive the native backend behind the unchanged afmpeg API.
	rt, err := afmpeg.New(context.Background(), afmpeg.WithBackend(native.New(native.WithNativeBinary(driver))))
	if err != nil {
		t.Fatalf("afmpeg.New(native backend): %v", err)
	}
	defer func() { _ = rt.Close(context.Background()) }()

	// Stream-copy remux mp4 → mkv: both streams read from afero over IPC, the
	// output muxed back into afero over IPC.
	cmd := afmpeg.Command{
		Inputs: []afmpeg.Input{{Path: "in.mp4"}},
		Outputs: []afmpeg.Output{{
			Path:       "out.mkv",
			Map:        []string{"0:v", "0:a"},
			VideoCodec: afmpeg.CodecCopy,
			AudioCodec: afmpeg.CodecCopy,
		}},
	}

	res, err := rt.RunJob(context.Background(), fs, cmd)
	if err != nil {
		t.Fatalf("RunJob over native backend: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("driver exit %d: %s", res.ExitCode, res.Stderr)
	}

	// The output must exist in afero — proving the muxer's writes (and its
	// backward seeks) round-tripped through the bridge, not to host disk.
	out, err := afero.ReadFile(fs, "out.mkv")
	if err != nil {
		t.Fatalf("read out.mkv from afero: %v", err)
	}

	if len(out) == 0 {
		t.Fatal("out.mkv is empty")
	}

	// Matroska starts with the EBML magic 0x1A45DFA3.
	if !bytes.HasPrefix(out, []byte{0x1A, 0x45, 0xDF, 0xA3}) {
		t.Fatalf("out.mkv is not a Matroska file (first bytes %x)", out[:min(8, len(out))])
	}
}
