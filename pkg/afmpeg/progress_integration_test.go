package afmpeg_test

import (
	"context"
	"os"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// TestIntegration_ProgressReporting drives a real transcode on the ffmpeg-wasi
// engine and asserts that WithProgress delivers live, monotonic, in-flight
// progress (spec 0031 phase A, R-PROGRESS-1/3) — the observed-fs mechanism the
// spike (docs/development/spikes/0031-progress-observed-fs) prototyped, now
// through the public API. Gated on AFMPEG_TEST_FFMPEG_WASI.
func TestIntegration_ProgressReporting(t *testing.T) {
	t.Parallel()

	module := os.Getenv("AFMPEG_TEST_FFMPEG_WASI")
	if module == "" {
		t.Skip("set AFMPEG_TEST_FFMPEG_WASI to a built ffmpeg-wasi driver to run this test")
	}

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close(ctx) })

	// Enough audio that the encode spans real wall-time to observe.
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.wav", makeWAVMono(44100, 20.0), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	ch := make(chan afmpeg.Progress, 8192)
	collected := make(chan []afmpeg.Progress, 1)
	go func() {
		var got []afmpeg.Progress
		for p := range ch {
			got = append(got, p)
		}
		collected <- got
	}()

	spec := `{"op":"process","inputs":[{"path":"in.wav"}],"outputs":[{"path":"out.mp4","audio_codec":"aac"}]}`
	res, err := rt.Run(afmpeg.WithProgress(ctx, ch), fs, spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	close(ch)
	samples := <-collected

	if res.ExitCode != 0 {
		t.Fatalf("process exit %d:\n%s", res.ExitCode, res.Stderr)
	}
	if len(samples) < 2 {
		t.Fatalf("want multiple in-flight progress samples, got %d", len(samples))
	}

	var last float64
	var sawPartial, sawElapsed bool
	for i, s := range samples {
		if s.Fraction < 0 || s.Fraction > 1 {
			t.Fatalf("sample %d Fraction %.4f out of [0,1]", i, s.Fraction)
		}
		if s.Fraction+1e-9 < last {
			t.Fatalf("sample %d Fraction %.4f regressed below %.4f", i, s.Fraction, last)
		}
		if s.Fraction > 0.05 && s.Fraction < 0.95 {
			sawPartial = true
		}
		if s.Elapsed > 0 {
			sawElapsed = true
		}
		last = s.Fraction
	}

	final := samples[len(samples)-1]
	if final.Fraction < 0.99 {
		t.Fatalf("final Fraction = %.4f, want ≈1.0 (input consumed)", final.Fraction)
	}
	if final.OutputBytes <= 0 {
		t.Fatalf("final OutputBytes = %d, want > 0", final.OutputBytes)
	}
	if !sawPartial {
		t.Fatal("no mid-run (partial) progress sample — progress not delivered in flight")
	}
	if !sawElapsed {
		t.Fatal("Elapsed never advanced")
	}
}
