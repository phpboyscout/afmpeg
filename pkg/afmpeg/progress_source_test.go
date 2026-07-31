package afmpeg

import (
	"context"
	"testing"
	"time"

	"github.com/spf13/afero"
)

// The keryx shape (afmpeg#2, spec 0034 §5): inputs tiny next to the output, so
// every input byte is consumed in the first moments while the encode — which the
// byte source cannot see — carries the rest of the job. Before 0034 the byte
// ratio saturated at 1.000 on the first sample and the shared monotone clamp
// pinned it there, so the engine's honest out_time/duration could never pull it
// down. Fraction must now track the engine.
func TestFraction_engineWinsOverSaturatedBytes(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 64))
	rep.expectEngine = true
	rep.declared = map[string]int64{"/card0.png": 367, "/card1.png": 367, "/music.wav": 32044}
	rep.declaredTotal = 32778

	// Three small inputs, each read to completion — keryx's 367/367 → 734/734 →
	// 32778/32778 trace.
	rep.addRead("card0.png", 367, 367)
	rep.addRead("card1.png", 367, 367)
	rep.addRead("music.wav", 32044, 32044)

	// Inputs exhausted, engine not yet heard from: -1, not a premature 1.0 (D1).
	if s := rep.snapshot(); s.Fraction != -1 || s.Source != SourceUnknown {
		t.Fatalf("before engine record: Fraction = %.4f/%v, want -1/unknown", s.Fraction, s.Source)
	}

	// The engine reports against a 30s output.
	const durUs = int64(30_000_000)

	for _, tc := range []struct {
		outUs int64
		want  float64
	}{
		{1_000_000, 0.0333},
		{5_000_000, 0.1667},
		{15_000_000, 0.5000},
		{30_000_000, 1.0000},
	} {
		frame, out, dur := tc.outUs/40000, tc.outUs, durUs
		rep.noteEngine(engineRecord{Frame: &frame, OutTimeUs: &out, DurationUs: &dur})

		s := rep.snapshot()
		if s.Fraction < tc.want-0.001 || s.Fraction > tc.want+0.001 {
			t.Errorf("out_time %v: Fraction = %.4f, want ≈%.4f", time.Duration(tc.outUs)*time.Microsecond, s.Fraction, tc.want)
		}

		if s.Source != SourceEngine {
			t.Errorf("out_time %v: Source = %v, want engine", time.Duration(tc.outUs)*time.Microsecond, s.Source)
		}
	}
}

// D6: an output that runs past the engine's reported duration (a looped image
// input extending a reel beyond its audio bed) proves the duration wrong. Fraction
// must not sit at a confident 1.0 while such a job runs on — the closing snapshot
// still reports the genuine 1.0.
func TestFraction_engineDurationOverrunReportsUnknown(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 8))
	rep.expectEngine = true

	dur := int64(3_000_000)

	for _, tc := range []struct {
		outUs int64
		want  float64
		src   FractionSource
	}{
		{1_500_000, 0.5, SourceEngine},  // halfway
		{3_000_000, 1.0, SourceEngine},  // duration met exactly — genuinely at the end
		{3_030_000, 1.0, SourceEngine},  // 1% over — rounding, still trusted
		{9_000_000, -1, SourceUnknown},  // 3× over — the duration was wrong
		{30_000_000, -1, SourceUnknown}, // and it keeps running
	} {
		out := tc.outUs
		rep.noteEngine(engineRecord{OutTimeUs: &out, DurationUs: &dur})

		s := rep.snapshot()
		if s.Fraction != tc.want || s.Source != tc.src {
			t.Errorf("out_time %v of 3s: Fraction = %.4f/%v, want %.4f/%v",
				time.Duration(tc.outUs)*time.Microsecond, s.Fraction, s.Source, tc.want, tc.src)
		}
	}

	// The job ends: the final snapshot reports the true 1.0 rather than -1.
	rep.final = true

	if s := rep.snapshot(); s.Fraction != 1.0 || s.Source != SourceEngine {
		t.Fatalf("final: Fraction = %.4f/%v, want 1.0/engine", s.Fraction, s.Source)
	}
}

// D2: the denominator is fixed from the declared inputs at invocation start, so
// the first sample of a multi-input job is a real fraction of the whole rather
// than bytes-read ÷ bytes-known-so-far (which is ≈1 throughout).
func TestFraction_denominatorFixedUpfront(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 8))
	rep.declared = map[string]int64{"/a.png": 367, "/b.wav": 32411}
	rep.declaredTotal = 32778

	rep.addRead("a.png", 367, 367)

	s := rep.snapshot()
	if s.Fraction < 0.010 || s.Fraction > 0.012 {
		t.Fatalf("Fraction = %.4f, want ≈0.011 (367/32778), not a saturated 1.0", s.Fraction)
	}

	if s.InputTotal != 32778 {
		t.Fatalf("InputTotal = %d, want the full declared 32778", s.InputTotal)
	}
}

// D2: a demuxer that seeks and re-reads can read more bytes than a file holds;
// the per-input cap keeps that from inflating the numerator past the denominator.
func TestFraction_rereadCappedAtDeclaredSize(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 8))
	rep.declared = map[string]int64{"/a.mp4": 1000, "/b.mp4": 1000}
	rep.declaredTotal = 2000

	rep.addRead("a.mp4", 2500, 1000) // index scan: re-reads the whole file twice over

	s := rep.snapshot()
	if s.InputBytes != 1000 {
		t.Fatalf("InputBytes = %d, want 1000 (capped at the input's size)", s.InputBytes)
	}

	if s.Fraction < 0.49 || s.Fraction > 0.51 {
		t.Fatalf("Fraction = %.4f, want ≈0.5", s.Fraction)
	}
}

// D1 fallback: a backend that cannot deliver engine records must not make
// Fraction wait for them — it reports byte progress immediately.
func TestFraction_noEngineExpectedReportsBytesImmediately(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 8))
	rep.expectEngine = false
	rep.declared = map[string]int64{"/in.wav": 1000}
	rep.declaredTotal = 1000

	rep.addRead("in.wav", 250, 1000)

	if s := rep.snapshot(); s.Fraction < 0.24 || s.Fraction > 0.26 || s.Source != SourceBytes {
		t.Fatalf("Fraction = %.4f/%v, want ≈0.25/bytes with no engine expected", s.Fraction, s.Source)
	}
}

// D1 fallback: an engine that emits records but never a duration (a pre-n8.1.2-10
// engine, or a shape it cannot measure) must not strand Fraction at -1 — the
// byte-observed source takes over once the engine has ruled itself out.
func TestFraction_fallsBackWhenEngineReportsNoDuration(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 8))
	rep.expectEngine = true
	rep.declared = map[string]int64{"/in.wav": 1000}
	rep.declaredTotal = 1000

	rep.addRead("in.wav", 300, 1000)

	if s := rep.snapshot(); s.Fraction != -1 {
		t.Fatalf("before any engine record: Fraction = %.4f, want -1", s.Fraction)
	}

	frame, out := int64(10), int64(400_000)
	rep.noteEngine(engineRecord{Frame: &frame, OutTimeUs: &out}) // no duration_us

	s := rep.snapshot()
	if s.Fraction < 0.29 || s.Fraction > 0.31 || s.Source != SourceBytes {
		t.Fatalf("Fraction = %.4f/%v, want ≈0.3/bytes once the engine reports no duration", s.Fraction, s.Source)
	}

	if s.OutTime != 400*time.Millisecond || s.Frame != 10 {
		t.Fatalf("engine-only fields lost: Frame=%d OutTime=%v", s.Frame, s.OutTime)
	}
}

// D5: reported Fraction never decreases, including across a source switch. Bytes
// run ahead of the engine here, so the engine's lower value must be floored
// rather than allowed to regress a number already shown to the caller.
func TestFraction_neverRegressesAcrossSourceSwitch(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 8))
	rep.expectEngine = false
	rep.declared = map[string]int64{"/in.wav": 1000}
	rep.declaredTotal = 1000

	rep.addRead("in.wav", 800, 1000)

	first := rep.snapshot()
	if first.Fraction < 0.79 || first.Fraction > 0.81 {
		t.Fatalf("Fraction = %.4f, want ≈0.8", first.Fraction)
	}

	// Engine now reports 10% — genuinely behind what bytes suggested.
	out, dur := int64(3_000_000), int64(30_000_000)
	rep.noteEngine(engineRecord{OutTimeUs: &out, DurationUs: &dur})

	if s := rep.snapshot(); s.Fraction+1e-9 < first.Fraction {
		t.Fatalf("Fraction regressed %.4f → %.4f across the source switch", first.Fraction, s.Fraction)
	}
}

// D1 end to end: engine records reach the reporter through Run, and a job whose
// declared inputs are fully consumed still reports the engine's fraction.
func TestRun_engineFractionSurvivesConsumedInputs(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.png", make([]byte, 512), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	be := &capableProgressBackend{progressBackend{records: []string{
		`{"frame":1,"out_time_us":3000000,"total_size":1000,"duration_us":30000000}`,
	}}}
	rt := &Runtime{backend: be, sem: make(chan struct{}, 1)}

	ch := make(chan Progress, 64)
	collected, sink := drainProgress(ch)

	ctx := WithProgress(context.Background(), sink)

	spec := `{"op":"process","inputs":[{"path":"in.png"}],"outputs":[{"path":"out.mp4"}]}`
	if _, err := rt.Run(ctx, fs, spec); err != nil {
		t.Fatalf("Run: %v", err)
	}

	close(ch)

	samples := <-collected
	if len(samples) == 0 {
		t.Fatal("no progress samples")
	}

	final := samples[len(samples)-1]
	if final.Source != SourceEngine {
		t.Fatalf("final Source = %v, want engine", final.Source)
	}

	if final.Fraction < 0.09 || final.Fraction > 0.11 {
		t.Fatalf("final Fraction = %.4f, want ≈0.1 (3s/30s), not a byte-saturated 1.0", final.Fraction)
	}
}

// planProgress reads the declared inputs (including concat members) and reserves
// engine progress for a process job on a capable backend (0031 D6, 0034 D1).
func TestPlanProgress(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		spec       string
		capable    bool
		wantEngine bool
		wantInputs int
	}{
		{"process job", `{"op":"process","inputs":[{"path":"a.wav"}]}`, true, true, 1},
		{"concat members counted", `{"op":"process","inputs":[{"concat":["a.ts","b.ts"]}]}`, true, true, 2},
		{"incapable backend", `{"op":"process","inputs":[{"path":"a.wav"}]}`, false, false, 1},
		{"probe carries no engine progress", `{"op":"probe","inputs":[{"path":"a.wav"}]}`, true, false, 1},
		{"frames still gets byte inputs", `{"op":"frames","inputs":[{"path":"a.mp4"}]}`, true, false, 1},
		{"unparseable spec", `not json`, true, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := planProgress([]string{tc.spec}, tc.capable)
			if got.engineProgress != tc.wantEngine {
				t.Errorf("engineProgress = %v, want %v", got.engineProgress, tc.wantEngine)
			}

			if len(got.inputs) != tc.wantInputs {
				t.Errorf("inputs = %v, want %d", got.inputs, tc.wantInputs)
			}
		})
	}
}

// capableProgressBackend is a progressBackend that advertises phase-B support,
// standing in for the wasm backend's /dev/afmpeg-progress device.
type capableProgressBackend struct {
	progressBackend
}

func (*capableProgressBackend) supportsEngineProgress() bool { return true }
