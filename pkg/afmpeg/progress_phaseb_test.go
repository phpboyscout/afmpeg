package afmpeg

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/afero"
)

// ptr is a helper for the pointer fields of engineRecord.
func ptr[T any](v T) *T { return &v }

// A well-formed engine record fills the fields the byte-observer cannot see
// (spec 0032 D-B4): Frame/OutTime come from the engine, Speed is host-derived
// (>0), and Fraction derives from out_time/duration — even with no input file
// read (the generative case, R-PROGRESS-B2).
func TestProgressReporter_engineRecordSupersedes(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 1))
	// Backdate origin so Elapsed is safely > 0 for the Speed derivation.
	rep.origin = time.Now().Add(-2 * time.Second)

	rep.noteEngine(engineRecord{
		Frame:      ptr(int64(120)),
		OutTimeUs:  ptr(int64(500_000)),   // 0.5 s of media
		TotalSize:  ptr(int64(64 << 10)),  // parsed, not surfaced (OutputBytes stays observed)
		DurationUs: ptr(int64(1_000_000)), // 1.0 s total → Fraction 0.5
	})

	s := rep.snapshot()
	if s.Frame != 120 {
		t.Fatalf("Frame = %d, want 120", s.Frame)
	}
	if s.OutTime != 500*time.Millisecond {
		t.Fatalf("OutTime = %v, want 500ms", s.OutTime)
	}
	if s.Fraction < 0.49 || s.Fraction > 0.51 {
		t.Fatalf("Fraction = %.4f, want ≈0.5 (out_time/duration)", s.Fraction)
	}
	if s.Speed <= 0 {
		t.Fatalf("Speed = %.4f, want > 0 (host-derived)", s.Speed)
	}
	// total_size is not surfaced as OutputBytes; with no observed writes it is 0.
	if s.OutputBytes != 0 {
		t.Fatalf("OutputBytes = %d, want 0 (observed-fs only, engine total_size ignored)", s.OutputBytes)
	}
}

// out_time never regresses across records (a demuxer seek must not rewind the
// bar); absent fields (nil pointers) leave prior state untouched.
func TestProgressReporter_engineMonotonicAndPartial(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 1))

	rep.noteEngine(engineRecord{Frame: ptr(int64(10)), OutTimeUs: ptr(int64(900_000))})
	// A later record carrying only total_size must keep frame and out_time.
	rep.noteEngine(engineRecord{TotalSize: ptr(int64(2048))})
	// A regressed out_time is ignored.
	rep.noteEngine(engineRecord{OutTimeUs: ptr(int64(100_000))})

	s := rep.snapshot()
	if s.Frame != 10 {
		t.Fatalf("Frame = %d, want 10 (preserved)", s.Frame)
	}
	if s.OutTime != 900*time.Millisecond {
		t.Fatalf("OutTime = %v, want 900ms (monotonic, no regression)", s.OutTime)
	}
}

// Without a duration the engine cannot give a fraction, so Fraction falls back to
// the byte-observed value; both sources feed the same monotone clamp so it never
// regresses when the engine record refines the sample.
func TestProgressReporter_fractionFallsBackToBytesWithoutDuration(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 1))

	rep.addRead("in", 30, 100) // byte fraction 0.30
	rep.noteEngine(engineRecord{Frame: ptr(int64(5)), OutTimeUs: ptr(int64(250_000))})

	s := rep.snapshot()
	if s.Fraction < 0.29 || s.Fraction > 0.31 {
		t.Fatalf("Fraction = %.4f, want ≈0.30 (byte fallback, no engine duration)", s.Fraction)
	}
	if s.Frame != 5 || s.OutTime != 250*time.Millisecond {
		t.Fatalf("engine Frame/OutTime not surfaced: %+v", s)
	}
}

// engineSink parses NDJSON lines and folds them in; a malformed or empty line is
// dropped without affecting state (R-PROGRESS-B4).
func TestProgressReporter_engineSinkDropsMalformed(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 1))
	sink := rep.engineSink()

	sink([]byte("not json"))
	sink([]byte(""))
	if rep.snapshot().Frame != 0 {
		t.Fatal("a malformed line changed state")
	}

	sink([]byte(`{"frame":7,"out_time_us":7}`))
	if s := rep.snapshot(); s.Frame != 7 {
		t.Fatalf("Frame = %d, want 7 after a valid line", s.Frame)
	}
}

// withProgressRequested stamps a process spec and leaves everything else alone.
func TestWithProgressRequested(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   []string
		want bool // expect progress:true present
	}{
		{"process job", []string{`{"op":"process","inputs":[{"path":"a"}]}`}, true},
		{"probe untouched", []string{`{"op":"probe","inputs":[{"path":"a"}]}`}, false},
		{"version untouched", []string{`{"op":"version"}`}, false},
		{"frames untouched", []string{`{"op":"frames","inputs":[{"path":"a"}]}`}, false},
		{"non-json untouched", []string{"ffmpeg-style-arg"}, false},
		{"multi-arg untouched", []string{`{"op":"process"}`, "extra"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := withProgressRequested(tc.in)

			has := len(got) == 1 && containsProgressTrue(got[0])
			if has != tc.want {
				t.Fatalf("progress:true present = %v, want %v (got %q)", has, tc.want, got)
			}
			// A non-process / non-JSON input must be returned byte-identical.
			if !tc.want && (len(got) != len(tc.in) || got[0] != tc.in[0]) {
				t.Fatalf("input was altered: %q -> %q", tc.in, got)
			}
		})
	}
}

func containsProgressTrue(spec string) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(spec), &m); err != nil {
		return false
	}
	v, ok := m["progress"].(bool)

	return ok && v
}

// progressBackend is a Backend double that emits engine NDJSON records to the
// per-invocation sink threaded onto ctx (spec 0032) — standing in for a v9 engine
// writing to /dev/afmpeg-progress — so Run's end-to-end phase-B wiring is exercised
// without the wasm module.
type progressBackend struct {
	records []string
}

func (b *progressBackend) Invoke(ctx context.Context, _ afero.Fs, _ ...string) (Result, error) {
	if sink := progressSinkFrom(ctx); sink != nil {
		for _, r := range b.records {
			sink([]byte(r + "\n"))
		}
	}

	return Result{ExitCode: 0}, nil
}

func (b *progressBackend) Close(context.Context) error { return nil }

// Run threads the engine-record sink from WithProgress down to the backend and
// surfaces the engine's Frame/OutTime/Fraction on the caller's channel.
func TestRun_threadsEngineSinkToChannel(t *testing.T) {
	t.Parallel()

	be := &progressBackend{records: []string{
		`{"frame":1,"out_time_us":250000,"total_size":1000,"duration_us":1000000}`,
		`{"frame":2,"out_time_us":500000,"total_size":2000,"duration_us":1000000}`,
	}}
	rt := &Runtime{backend: be, sem: make(chan struct{}, 1)}

	ch := make(chan Progress, 64)
	collected, sink := drainProgress(ch)

	ctx := WithProgress(context.Background(), sink)
	if _, err := rt.Run(ctx, afero.NewMemMapFs(), `{"op":"process","inputs":[{"path":"gen"}]}`); err != nil {
		t.Fatalf("Run: %v", err)
	}
	close(ch)
	samples := <-collected

	if len(samples) == 0 {
		t.Fatal("no progress samples")
	}
	final := samples[len(samples)-1]
	if final.Frame != 2 {
		t.Fatalf("final Frame = %d, want 2", final.Frame)
	}
	if final.OutTime != 500*time.Millisecond {
		t.Fatalf("final OutTime = %v, want 500ms", final.OutTime)
	}
	if final.Fraction < 0.49 || final.Fraction > 0.51 {
		t.Fatalf("final Fraction = %.4f, want ≈0.5 (out_time/duration, generative input)", final.Fraction)
	}
}
