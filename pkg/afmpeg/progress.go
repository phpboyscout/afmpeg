package afmpeg

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/spf13/afero"
)

// Progress is one best-effort progress sample for an in-flight invocation
// (spec 0031). It is emitted while Run/RunJob/Frames execute, to a channel the
// caller attaches with WithProgress.
//
// Phase A derives progress from the bytes flowing through the filesystem bridge:
// afmpeg implements the fs the engine reads and writes, so it can watch input
// consumed and output produced without any engine cooperation. Fraction comes
// from input read position (bytes_read / input_size), which tracks completion
// closely for linear demuxers.
//
// Phase B (spec 0032) refines this from the engine itself: a v9+ engine streams
// NDJSON progress records over the /dev/afmpeg-progress device, which fill
// Frame/OutTime/Speed and — when the engine also reports the media duration —
// derive Fraction from out_time/duration (accurate even for a generative input
// with no source file). When no engine record has arrived (early startup, or an
// engine that reports no duration) the byte-observed values remain the fallback.
type Progress struct {
	// Fraction is completion in [0,1], or -1 when it cannot be determined
	// (e.g. a purely generative input with no source file to consume and an
	// engine that does not report duration). It never decreases across an
	// invocation, even when a demuxer seeks backwards.
	Fraction float64

	Elapsed     time.Duration // since the invocation began
	InputBytes  int64         // bytes read from inputs so far
	InputTotal  int64         // total size of the inputs being read, 0 if unknown
	OutputBytes int64         // bytes written to outputs so far (observed at the fs bridge)

	// Populated once a phase-B engine record has arrived (zero before then, and
	// for a pre-v9 engine that emits nothing):
	Frame   int64         // frames processed
	OutTime time.Duration // media timestamp reached
	Speed   float64       // ×realtime encode speed (host-derived: OutTime/Elapsed)
}

type progressKey struct{}

// WithProgress returns a context that carries ch, so a subsequent Run, RunJob,
// or Frames on a Runtime reports live progress on it (spec 0031, D1). Progress
// is per-invocation — a Runtime is shared and serialises its calls — so it is
// attached to the call's context rather than to New.
//
// Delivery is best-effort and never interferes with the job (D2): afmpeg sends
// on ch with a non-blocking send, so a slow or non-draining consumer simply
// misses intermediate samples; the invocation is never blocked or failed by it.
// afmpeg does not close ch — the caller owns its lifecycle — and stops sending
// once the call returns. A nil ch is a no-op.
func WithProgress(ctx context.Context, ch chan<- Progress) context.Context {
	if ch == nil {
		return ctx
	}

	return context.WithValue(ctx, progressKey{}, ch)
}

// progressFrom returns the channel attached by WithProgress, or nil.
func progressFrom(ctx context.Context) chan<- Progress {
	ch, _ := ctx.Value(progressKey{}).(chan<- Progress)

	return ch
}

// progressSinkKey carries the per-invocation engine-progress sink (spec 0032)
// from Run down to the wasm backend, which mounts it on /dev/afmpeg-progress.
type progressSinkKey struct{}

// withProgressSink attaches sink to ctx so the wasm backend serves the engine's
// /dev/afmpeg-progress device against it (spec 0032). A nil sink is a no-op.
func withProgressSink(ctx context.Context, sink func([]byte)) context.Context {
	if sink == nil {
		return ctx
	}

	return context.WithValue(ctx, progressSinkKey{}, sink)
}

// progressSinkFrom returns the engine-progress sink attached to ctx, or nil. The
// wasm backend reads it to decide whether to serve /dev/afmpeg-progress.
func progressSinkFrom(ctx context.Context) func([]byte) {
	sink, _ := ctx.Value(progressSinkKey{}).(func([]byte))

	return sink
}

// startProgress wires up live progress reporting for an invocation whose ctx
// carries a WithProgress channel: it wraps fs so the engine's reads and writes
// are observed (phase A) and attaches an engine-record sink to the returned ctx
// so a v9+ engine's /dev/afmpeg-progress stream refines the samples (phase B).
// When no channel is attached it returns ctx and fs unchanged and a no-op stop.
// The returned stop must be called when the invocation ends; it emits a final
// sample and releases the reporter's goroutine.
func (r *Runtime) startProgress(ctx context.Context, fs afero.Fs) (context.Context, afero.Fs, func()) {
	ch := progressFrom(ctx)
	if ch == nil {
		return ctx, fs, func() {}
	}

	rep := newProgressReporter(ch)
	rep.start()

	ctx = withProgressSink(ctx, rep.engineSink())

	return ctx, newObserveFs(fs, rep), rep.stop
}

// pathIO accumulates the bytes seen for one guest path.
type pathIO struct {
	read    int64
	written int64
	total   int64 // input size captured at open (Stat), 0 when unknown
}

// progressReporter aggregates observed I/O and forwards samples to the caller's
// channel from a single goroutine. The I/O accounting (addRead/addWrite) runs on
// the engine's syscall path and must stay cheap and non-blocking; the goroutine
// owns the (also non-blocking) sends to the caller.
type progressReporter struct {
	ch     chan<- Progress
	origin time.Time

	mu      sync.Mutex
	paths   map[string]*pathIO
	maxFrac float64

	// eng holds the latest phase-B engine record (spec 0032); haveEng is set once
	// the first well-formed record arrives, after which it supersedes the
	// byte-observed Frame/OutTime/Speed/OutputBytes and (when the engine reports a
	// duration) the Fraction.
	haveEng bool
	eng     engineSample

	pending chan struct{}
	done    chan struct{}
	wg      sync.WaitGroup
}

// engineSample is the accumulated state from the engine's progress records.
type engineSample struct {
	frame      int64
	outTimeUs  int64
	durationUs int64 // 0 until an engine reports it; enables out_time/duration Fraction
}

// engineRecord is one NDJSON line the v9+ engine writes to /dev/afmpeg-progress
// (spec 0032, D-B5). Every field is a pointer so an absent field leaves the
// accumulated state untouched rather than resetting it to zero. duration_us is
// forward-compatible: the n8.1.2-9 engine does not emit it (Fraction then falls
// back to the byte-observed value); a later engine that does lights up
// out_time/duration Fraction, including for generative inputs (R-PROGRESS-B2).
//
// TotalSize (the engine's encoded-payload sum) is parsed for schema completeness
// but not surfaced: Progress.OutputBytes stays the observed-fs file size, which
// is the true output size (total_size excludes container structure).
type engineRecord struct {
	Frame      *int64 `json:"frame"`
	OutTimeUs  *int64 `json:"out_time_us"`
	TotalSize  *int64 `json:"total_size"`
	DurationUs *int64 `json:"duration_us"`
}

func newProgressReporter(ch chan<- Progress) *progressReporter {
	return &progressReporter{
		ch:      ch,
		origin:  time.Now(),
		paths:   make(map[string]*pathIO),
		maxFrac: -1,
		pending: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
}

// ioLocked returns the counters for path; caller holds p.mu.
func (p *progressReporter) ioLocked(path string) *pathIO {
	c := p.paths[path]
	if c == nil {
		c = &pathIO{}
		p.paths[path] = c
	}

	return c
}

func (p *progressReporter) addRead(path string, n int, total int64) {
	if n <= 0 {
		return
	}

	p.mu.Lock()
	c := p.ioLocked(path)
	c.read += int64(n)

	if c.total == 0 && total > 0 {
		c.total = total
	}

	p.mu.Unlock()

	p.nudge()
}

func (p *progressReporter) addWrite(path string, n int) {
	if n <= 0 {
		return
	}

	p.mu.Lock()
	p.ioLocked(path).written += int64(n)
	p.mu.Unlock()

	p.nudge()
}

// engineSink returns the callback the vfs progress device feeds each complete
// NDJSON line to (spec 0032). It parses the line and folds it into the reporter;
// a malformed or empty line is dropped (R-PROGRESS-B4). The callback runs on the
// engine's write path, so it must stay cheap and non-blocking — like addRead/
// addWrite it only takes the mutex and nudges the drain goroutine.
func (p *progressReporter) engineSink() func([]byte) {
	return func(line []byte) {
		var rec engineRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return
		}

		p.noteEngine(rec)
	}
}

// noteEngine folds one engine record into the accumulated phase-B state. Absent
// fields (nil pointers) leave prior values untouched; out_time only advances
// (never regresses on a demuxer seek). It marks haveEng so snapshot switches to
// engine-sourced values.
func (p *progressReporter) noteEngine(rec engineRecord) {
	p.mu.Lock()

	p.haveEng = true

	if rec.Frame != nil {
		p.eng.frame = *rec.Frame
	}

	if rec.OutTimeUs != nil && *rec.OutTimeUs > p.eng.outTimeUs {
		p.eng.outTimeUs = *rec.OutTimeUs
	}

	if rec.DurationUs != nil && *rec.DurationUs > 0 {
		p.eng.durationUs = *rec.DurationUs
	}

	p.mu.Unlock()

	p.nudge()
}

// nudge signals the reporter goroutine that state changed, coalescing bursts:
// the pending channel holds at most one outstanding signal.
func (p *progressReporter) nudge() {
	select {
	case p.pending <- struct{}{}:
	default:
	}
}

// clampMaxFrac raises the monotone Fraction ceiling to cand (capped at 1) and
// returns it. Caller holds p.mu. It is the single point that enforces D3
// (never-decreasing) across both the engine and byte-observed sources.
func (p *progressReporter) clampMaxFrac(cand float64) float64 {
	if cand > p.maxFrac {
		p.maxFrac = cand
	}

	if p.maxFrac > 1 {
		p.maxFrac = 1
	}

	return p.maxFrac
}

// fractionLocked returns the completion fraction from whichever source is
// available: the engine's out_time/duration when it reported both (spec 0032,
// accurate even for a generative input), else the byte-observed read/total (phase
// A), else -1 when neither is known (D5). Both feed the same monotone clamp.
// Caller holds p.mu.
func (p *progressReporter) fractionLocked(read, total int64) float64 {
	frac := -1.0

	if p.haveEng && p.eng.durationUs > 0 {
		frac = p.clampMaxFrac(float64(p.eng.outTimeUs) / float64(p.eng.durationUs))
	}

	if total > 0 {
		frac = p.clampMaxFrac(float64(read) / float64(total))
	}

	return frac
}

// applyEngineLocked fills the fields only the engine can see once a phase-B
// record has arrived (spec 0032, D-B4): frame count, media time reached, and the
// host-derived ×realtime speed. OutputBytes is left as the observed-fs value —
// the true bytes written to the output file, which the engine's total_size (an
// encoded-payload sum, exclusive of container structure) would only undercount.
// Caller holds p.mu.
func (p *progressReporter) applyEngineLocked(prog *Progress, elapsed time.Duration) {
	if !p.haveEng {
		return
	}

	prog.Frame = p.eng.frame
	prog.OutTime = time.Duration(p.eng.outTimeUs) * time.Microsecond

	if elapsed > 0 {
		prog.Speed = prog.OutTime.Seconds() / elapsed.Seconds()
	}
}

// snapshot builds a Progress from the current counters. Input accounting is
// always byte-observed (the engine reports no input bytes): InputBytes/InputTotal
// are size-weighted across every input read from (D4). Fraction is clamped
// monotone-nondecreasing (D3) and is -1 when it cannot be determined (D5).
//
// When a phase-B engine record has arrived (spec 0032), Frame/OutTime/Speed come
// from the engine (D-B4), and — if the engine reported a media duration —
// Fraction derives from out_time/duration; otherwise Fraction stays byte-observed.
func (p *progressReporter) snapshot() Progress {
	elapsed := time.Since(p.origin)

	p.mu.Lock()

	var read, total, written int64

	for _, c := range p.paths {
		written += c.written
		if c.read > 0 && c.total > 0 {
			read += c.read
			total += c.total
		}
	}

	prog := Progress{
		Fraction:    p.fractionLocked(read, total),
		Elapsed:     elapsed,
		InputBytes:  read,
		InputTotal:  total,
		OutputBytes: written,
	}

	p.applyEngineLocked(&prog, elapsed)

	p.mu.Unlock()

	return prog
}

// emit sends the current snapshot, dropping it if the caller's channel is full.
func (p *progressReporter) emit() {
	select {
	case p.ch <- p.snapshot():
	default:
	}
}

// start launches the single reporter goroutine.
func (p *progressReporter) start() {
	p.wg.Add(1)

	go func() {
		defer p.wg.Done()

		for {
			select {
			case <-p.pending:
				p.emit()
			case <-p.done:
				p.emit() // final sample reflecting the completed job

				return
			}
		}
	}()
}

// stop halts the reporter goroutine after a final emit and waits for it. Safe to
// call once; the caller guarantees no further I/O is observed after stop (the
// engine invocation has returned by then).
func (p *progressReporter) stop() {
	close(p.done)
	p.wg.Wait()
}

// observeFs decorates an afero.Fs so every read and write the engine performs is
// counted and reported (spec 0031 phase A). It sits between afmpeg and the
// caller's fs; the engine, internal/vfs, and the job spec are untouched.
type observeFs struct {
	afero.Fs

	rep *progressReporter
}

func newObserveFs(base afero.Fs, rep *progressReporter) *observeFs {
	return &observeFs{Fs: base, rep: rep}
}

func (o *observeFs) wrap(name string, af afero.File) afero.File {
	var total int64
	if fi, err := o.Stat(name); err == nil {
		total = fi.Size()
	}

	return &observeFile{File: af, rep: o.rep, name: name, total: total}
}

func (o *observeFs) Open(name string) (afero.File, error) {
	af, err := o.Fs.Open(name)
	if err != nil {
		return af, err
	}

	return o.wrap(name, af), nil
}

func (o *observeFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	af, err := o.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return af, err
	}

	return o.wrap(name, af), nil
}

func (o *observeFs) Create(name string) (afero.File, error) {
	af, err := o.Fs.Create(name)
	if err != nil {
		return af, err
	}

	return o.wrap(name, af), nil
}

// observeFile counts bytes crossing Read/ReadAt (input consumed) and Write/
// WriteAt (output produced) — exactly the methods internal/vfs invokes on the
// afero file (see internal/vfs/file.go), so nothing the engine does escapes them.
type observeFile struct {
	afero.File

	rep   *progressReporter
	name  string
	total int64
}

func (f *observeFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	f.rep.addRead(f.name, n, f.total)

	return n, err
}

func (f *observeFile) ReadAt(p []byte, off int64) (int, error) {
	n, err := f.File.ReadAt(p, off)
	f.rep.addRead(f.name, n, f.total)

	return n, err
}

func (f *observeFile) Write(p []byte) (int, error) {
	n, err := f.File.Write(p)
	f.rep.addWrite(f.name, n)

	return n, err
}

func (f *observeFile) WriteAt(p []byte, off int64) (int, error) {
	n, err := f.File.WriteAt(p, off)
	f.rep.addWrite(f.name, n)

	return n, err
}
