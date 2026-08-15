package native_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg/native"
)

// TestIntegration_AnalysisFilterOutput proves structured analysis-filter output
// (spec 0017 §Q): a cropdetect filter's per-frame lavfi.* measurements come back in
// the result JSON's `analysis` array (afmpeg.ParseResult → ProcessResult.Analysis),
// not only the log.
//
// Needs an intermediate **gpl** driver on both counts: cropdetect carries
// `cropdetect_filter_deps="gpl"` upstream, and the output is encoded with libx264.
// No lgpl build carries either, whatever its profile — so set
// AFMPEG_TEST_NATIVE_DRIVER_INTERMEDIATE_GPL (or _FULL_GPL). Also needs a host
// ffmpeg to synthesise the black-bordered fixture cropdetect measures.
func TestIntegration_AnalysisFilterOutput(t *testing.T) {
	t.Parallel()

	driver := integrationDriver(t, afmpeg.ProfileIntermediate, true)

	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no host ffmpeg to synthesise a fixture")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src.mp4")

	// 160x120 content padded into a 320x240 black frame → cropdetect finds the crop.
	gen := exec.CommandContext(context.Background(), ff, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=25:duration=1",
		"-vf", "pad=320:240:80:60:black", "-c:v", "libx264", "-pix_fmt", "yuv420p", src)
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

	rt, err := afmpeg.New(context.Background(), afmpeg.WithBackend(native.New(native.WithNativeBinary(driver))))
	if err != nil {
		t.Fatalf("afmpeg.New(native): %v", err)
	}
	defer func() { _ = rt.Close(context.Background()) }()

	cmd := afmpeg.Command{
		Inputs:        []afmpeg.Input{{Path: "in.mp4"}},
		FilterComplex: "[0:v]cropdetect[v]",
		Outputs: []afmpeg.Output{{
			Path: "out.mp4", Map: []string{"[v]"}, VideoCodec: "libx264",
		}},
	}

	res, err := rt.RunJob(context.Background(), fs, cmd)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("RunJob: err=%v exit=%d stderr=%s", err, res.ExitCode, res.Stderr)
	}

	pr, err := afmpeg.ParseResult(res)
	if err != nil {
		t.Fatalf("ParseResult: %v", err)
	}

	if len(pr.Analysis) == 0 {
		t.Fatal("no analysis measurements — cropdetect metadata not captured")
	}

	crop := map[string]string{}
	for _, m := range pr.Analysis {
		if k, ok := strings.CutPrefix(m.Key, "cropdetect."); ok {
			crop[k] = m.Value
		}
	}

	// cropdetect should report the 160-wide content (rounded to even), not 320.
	if crop["w"] == "" || crop["w"] == "320" {
		t.Fatalf("cropdetect.w = %q — expected the padded content width (~160), not the full frame; crop=%v", crop["w"], crop)
	}

	t.Logf("analysis OK: %d measurements; cropdetect w=%s h=%s x=%s y=%s",
		len(pr.Analysis), crop["w"], crop["h"], crop["x"], crop["y"])
}
