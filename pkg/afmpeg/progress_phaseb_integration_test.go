package afmpeg_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// TestIntegration_ProgressPhaseB drives a real transcode on a v9+ ffmpeg-wasi
// engine and asserts the engine-sourced progress fields arrive over the
// /dev/afmpeg-progress side-channel (spec 0032, R-PROGRESS-B1): samples carry a
// non-zero, rising Frame and OutTime, and a host-derived Speed. Gated on
// AFMPEG_TEST_FFMPEG_WASI pointing at a v9 build (n8.1.2-9+); an older engine is
// rejected by New's vocab preflight, which is itself the compatibility contract.
func TestIntegration_ProgressPhaseB(t *testing.T) {
	t.Parallel()

	module := os.Getenv("AFMPEG_TEST_FFMPEG_WASI")
	if module == "" {
		t.Skip("set AFMPEG_TEST_FFMPEG_WASI to a v9 ffmpeg-wasi build to run this test")
	}

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close(ctx) })

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

	// The engine streams OutTime and Speed over /dev/afmpeg-progress; assert they
	// arrive and rise in flight. (Frame is video-only — this is an audio transcode,
	// so it legitimately stays 0; OutTime/Speed are what prove the side-channel.)
	// A pre-v9 engine would have been rejected at New, so reaching here with no
	// engine-sourced sample is a real failure of the side-channel.
	var (
		lastOut              time.Duration
		sawSpeed             bool
		sawRisingOutInterior bool
	)
	for i, s := range samples {
		if s.OutTime < lastOut { // never regresses (spec 0031 D3)
			t.Fatalf("sample %d OutTime %v regressed below %v", i, s.OutTime, lastOut)
		}
		if s.OutTime > lastOut && i < len(samples)-1 {
			sawRisingOutInterior = true
		}
		if s.Speed > 0 {
			sawSpeed = true
		}
		lastOut = s.OutTime
	}

	if !sawRisingOutInterior {
		t.Fatal("OutTime never rose in flight — engine progress not streaming over /dev/afmpeg-progress")
	}
	if !sawSpeed {
		t.Fatal("Speed never derived (>0) from engine out_time")
	}

	final := samples[len(samples)-1]
	if final.OutTime <= 0 {
		t.Fatalf("final OutTime = %v, want > 0", final.OutTime)
	}
	if final.OutputBytes <= 0 {
		t.Fatalf("final OutputBytes = %d, want > 0 (observed output)", final.OutputBytes)
	}
	t.Logf("phase B: %d samples, finalOutTime=%v, finalSpeed=%.2f×, finalFraction=%.3f",
		len(samples), final.OutTime, final.Speed, final.Fraction)
}
