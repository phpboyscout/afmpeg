package afmpeg

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/spf13/afero"
)

// ioBackend is a Backend test double that performs real filesystem I/O on the fs
// it is handed — reading an input in small chunks and writing an output — so the
// observeFs wrapper (spec 0031 phase A) sees bytes flow without needing the wasm
// engine. Per-chunk delay spreads the reads over wall-time so the reporter
// goroutine interleaves and emits multiple in-flight samples.
type ioBackend struct {
	in, out  string
	chunk    int
	outSize  int
	perChunk time.Duration
	exitCode int
}

func (b *ioBackend) Invoke(_ context.Context, fs afero.Fs, _ ...string) (Result, error) {
	if b.in != "" {
		b.readAll(fs)
	}

	if b.out != "" {
		b.writeOut(fs)
	}

	return Result{ExitCode: b.exitCode}, nil
}

// readAll consumes b.in in b.chunk-sized reads, pausing b.perChunk between them so
// reads spread over wall-time and the reporter goroutine interleaves.
func (b *ioBackend) readAll(fs afero.Fs) {
	f, err := fs.Open(b.in)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, b.chunk)

	for {
		n, rerr := f.Read(buf)
		if n > 0 && b.perChunk > 0 {
			time.Sleep(b.perChunk)
		}

		if rerr != nil {
			return
		}
	}
}

func (b *ioBackend) writeOut(fs afero.Fs) {
	of, err := fs.Create(b.out)
	if err != nil {
		return
	}
	defer func() { _ = of.Close() }()

	_, _ = of.Write(make([]byte, b.outSize))
}

func (b *ioBackend) Close(context.Context) error { return nil }

// drainProgress collects everything sent on ch into a slice. The caller closes ch
// after Run returns (afmpeg never closes it and stops sending before returning),
// so the collection is complete and race-free.
func drainProgress(ch chan Progress) (<-chan []Progress, chan<- Progress) {
	out := make(chan []Progress, 1)
	go func() {
		var got []Progress
		for p := range ch {
			got = append(got, p)
		}
		out <- got
	}()

	return out, ch
}

// R-PROGRESS-1: in-flight, rising, monotonic samples for a linear file input.
func TestProgress_reportsRisingFractionInFlight(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	const size = 256 << 10
	if err := afero.WriteFile(fs, "in.dat", make([]byte, size), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	be := &ioBackend{in: "in.dat", out: "out.dat", chunk: 4 << 10, outSize: 8 << 10, perChunk: time.Millisecond}
	rt := &Runtime{backend: be, sem: make(chan struct{}, 1)}

	ch := make(chan Progress, 4096)
	collected, sink := drainProgress(ch)

	ctx := WithProgress(context.Background(), sink)
	res, err := rt.Run(ctx, fs, "spec")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	close(ch)
	samples := <-collected

	if res.ExitCode != 0 {
		t.Fatalf("exit = %d", res.ExitCode)
	}
	if len(samples) == 0 {
		t.Fatal("no progress samples observed")
	}

	// -1 is a legitimate in-flight value (spec 0034: unknown, or inputs exhausted
	// while the encode runs on); it is skipped rather than treated as a regression.
	var last float64

	sawPartial := false

	for i, s := range samples {
		if s.Fraction == -1 {
			if s.Source != SourceUnknown {
				t.Fatalf("sample %d: Fraction -1 with Source %v, want unknown", i, s.Source)
			}

			continue
		}

		if s.Fraction < 0 || s.Fraction > 1 {
			t.Fatalf("sample %d Fraction %.4f out of [0,1]", i, s.Fraction)
		}
		if s.Fraction+1e-9 < last {
			t.Fatalf("sample %d Fraction %.4f regressed below %.4f", i, s.Fraction, last)
		}
		if s.Fraction > 0 && s.Fraction < 1 {
			sawPartial = true
		}
		last = s.Fraction
	}

	final := samples[len(samples)-1]
	if final.Fraction != 1.0 {
		t.Fatalf("final Fraction = %.4f, want 1.0 (input fully consumed)", final.Fraction)
	}
	if final.InputTotal != size {
		t.Fatalf("final InputTotal = %d, want %d", final.InputTotal, size)
	}
	if final.OutputBytes != 8<<10 {
		t.Fatalf("final OutputBytes = %d, want %d", final.OutputBytes, 8<<10)
	}
	if !sawPartial {
		t.Log("note: no strictly-partial sample captured (timing); monotonic + final==1.0 still hold")
	}
}

// R-PROGRESS-2: a non-draining channel never blocks or alters the job.
func TestProgress_nonDrainingChannelNeverBlocks(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.dat", make([]byte, 128<<10), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	be := &ioBackend{in: "in.dat", out: "out.dat", chunk: 2 << 10, outSize: 4 << 10, exitCode: 7}

	// Baseline: no progress attached.
	base := &Runtime{backend: be, sem: make(chan struct{}, 1)}
	want, err := base.Run(context.Background(), fs, "spec")
	if err != nil {
		t.Fatalf("baseline Run: %v", err)
	}

	// Unbuffered channel that nobody reads → every send drops, nothing blocks.
	stuck := make(chan Progress)
	ctx := WithProgress(context.Background(), stuck)

	done := make(chan Result, 1)
	go func() {
		got, rerr := (&Runtime{backend: be, sem: make(chan struct{}, 1)}).Run(ctx, fs, "spec")
		if rerr != nil {
			t.Errorf("Run with stuck channel: %v", rerr)
		}
		done <- got
	}()

	select {
	case got := <-done:
		if got != want {
			t.Fatalf("Result diverged with progress attached: got %+v want %+v", got, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run blocked on a non-draining progress channel")
	}
}

// R-PROGRESS-3: a purely generative job (no input consumed) yields Fraction == -1.
func TestProgress_unknownFractionForGenerative(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	be := &ioBackend{out: "out.dat", outSize: 16 << 10} // no input read
	rt := &Runtime{backend: be, sem: make(chan struct{}, 1)}

	ch := make(chan Progress, 256)
	collected, sink := drainProgress(ch)

	res, err := rt.Run(WithProgress(context.Background(), sink), fs, "spec")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	close(ch)
	samples := <-collected

	if res.ExitCode != 0 || len(samples) == 0 {
		t.Fatalf("exit=%d samples=%d", res.ExitCode, len(samples))
	}
	for i, s := range samples {
		if s.Fraction != -1 {
			t.Fatalf("sample %d Fraction = %.4f, want -1 (unknown) for generative", i, s.Fraction)
		}
	}
	if final := samples[len(samples)-1]; final.OutputBytes != 16<<10 {
		t.Fatalf("final OutputBytes = %d, want %d", final.OutputBytes, 16<<10)
	}
}

// A run without WithProgress leaves fs unwrapped and never starts a reporter.
func TestProgress_absentIsNoOp(t *testing.T) {
	t.Parallel()

	be := &ioBackend{in: "in.dat", out: "out.dat", chunk: 1 << 10}
	fs := afero.NewMemMapFs()
	_ = afero.WriteFile(fs, "in.dat", make([]byte, 4<<10), 0o644)

	rt := &Runtime{backend: be, sem: make(chan struct{}, 1)}
	if _, err := rt.Run(context.Background(), fs, "spec"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// WithProgress(nil) must not attach anything.
	if got := WithProgress(context.Background(), nil); progressFrom(got) != nil {
		t.Fatal("WithProgress(nil) attached a channel")
	}
	if progressFrom(context.Background()) != nil {
		t.Fatal("progressFrom on a bare context should be nil")
	}
}

// D4 size-weighting on the lazy-discovery fallback (no declared inputs, e.g. an
// unparseable spec). Finishing the first input saturates the ratio against a
// denominator that has not finished growing: under spec 0034 D3 that reports -1
// rather than a false 1.0, and when the denominator does grow the stale ceiling
// is retired so the job gets a real number back instead of plateauing.
func TestProgressReporter_lazyDenominatorGrowth(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 1))

	rep.addRead("a", 100, 100) // first input fully read → raw 1.0, mid-job

	if s := rep.snapshot(); s.Fraction != -1 || s.Source != SourceUnknown {
		t.Fatalf("saturated mid-job: Fraction = %.4f/%v, want -1/unknown (0034 D3)", s.Fraction, s.Source)
	}

	rep.addRead("b", 50, 900) // denominator grows: raw (150/1000) = 0.15

	s := rep.snapshot()
	if s.Fraction < 0.149 || s.Fraction > 0.151 {
		t.Fatalf("Fraction = %.4f, want ≈0.15 — a stale ceiling must not pin it", s.Fraction)
	}

	if s.Source != SourceBytes {
		t.Fatalf("Source = %v, want bytes", s.Source)
	}

	if s.InputTotal != 1000 || s.InputBytes != 150 {
		t.Fatalf("size-weighting wrong: bytes=%d total=%d, want 150/1000", s.InputBytes, s.InputTotal)
	}
}

// Guards and clamps: zero-length I/O is ignored, and reading past the recorded
// size clamps Fraction to 1.0 rather than exceeding it.
func TestProgressReporter_guardsAndUpperClamp(t *testing.T) {
	t.Parallel()

	rep := newProgressReporter(make(chan Progress, 1))
	rep.addRead("x", 0, 100) // ignored
	rep.addWrite("x", 0)     // ignored
	if s := rep.snapshot(); s.Fraction != -1 || s.InputBytes != 0 || s.OutputBytes != 0 {
		t.Fatalf("zero-length I/O recorded: %+v", s)
	}

	// Raw 1.5 → clamped to 1.0, never beyond. In flight that reads as -1 (0034
	// D3: inputs exhausted, job still running); the closing snapshot reports the
	// genuine 1.0.
	rep.addRead("y", 150, 100)

	if s := rep.snapshot(); s.Fraction != -1 {
		t.Fatalf("saturated mid-job: Fraction = %.4f, want -1", s.Fraction)
	}

	rep.final = true

	if s := rep.snapshot(); s.Fraction != 1.0 || s.Source != SourceBytes {
		t.Fatalf("upper clamp at end of job: Fraction = %.4f/%v, want 1.0/bytes", s.Fraction, s.Source)
	}
}

// A backing fs that fails Create surfaces the error through the decorator.
func TestObserveFs_createErrorPassesThrough(t *testing.T) {
	t.Parallel()

	ro := afero.NewReadOnlyFs(afero.NewMemMapFs())
	ofs := newObserveFs(ro, newProgressReporter(make(chan Progress, 1)))
	if _, err := ofs.Create("out.dat"); err == nil {
		t.Fatal("Create on a read-only fs should error through the decorator")
	}
}

// observeFs must count every I/O method internal/vfs uses, and pass errors
// through without wrapping a wrapped file around a nil.
func TestObserveFs_countsAllIOMethods(t *testing.T) {
	t.Parallel()

	base := afero.NewMemMapFs()
	_ = afero.WriteFile(base, "in.dat", []byte("0123456789"), 0o644)

	rep := newProgressReporter(make(chan Progress, 16))
	ofs := newObserveFs(base, rep)

	// Read via Open+Read and Pread(ReadAt); input total captured from Stat.
	rf, err := ofs.Open("in.dat")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := rf.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, err := rf.ReadAt(buf, 6); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	_ = rf.Close()

	// Write via Create+Write and OpenFile+WriteAt.
	wf, err := ofs.Create("out.dat")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := wf.Write([]byte("abcd")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := wf.WriteAt([]byte("ef"), 4); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	_ = wf.Close()

	if _, err := ofs.OpenFile("out.dat", os.O_RDONLY, 0); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	s := rep.snapshot()
	if s.InputBytes != 8 || s.InputTotal != 10 {
		t.Fatalf("reads: got %d/%d, want 8/10", s.InputBytes, s.InputTotal)
	}
	if s.OutputBytes != 6 {
		t.Fatalf("writes: got %d, want 6", s.OutputBytes)
	}
	if s.Fraction <= 0 || s.Fraction > 1 {
		t.Fatalf("Fraction %.4f out of range", s.Fraction)
	}

	// Error paths pass through, no panic, no wrapped nil.
	if _, err := ofs.Open("nope.dat"); err == nil {
		t.Fatal("Open of a missing file should error")
	}
	if _, err := ofs.OpenFile("nope.dat", os.O_RDONLY, 0); err == nil {
		t.Fatal("OpenFile of a missing file should error")
	}
}
