package afmpeg

import (
	"context"
	"encoding/json"
	"os"
	"path"
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
//
// Spec 0034 settles which source wins when both are available: the engine's is
// authoritative, and Fraction reports -1 rather than a byte ratio the engine is
// about to contradict. Source says which one produced the value.
type Progress struct {
	// Fraction is completion in [0,1], or -1 when it cannot be determined — a
	// purely generative input with no source file and no engine duration, the
	// startup window before the engine's first record (spec 0034, D1), or inputs
	// fully consumed while the job is still encoding (D3). It never decreases
	// across an invocation, even when a demuxer seeks backwards.
	//
	// Fraction == 1 is not a completion signal: use the invocation's return. A
	// job whose inputs are small relative to its output exhausts them long before
	// it finishes, and reports -1 for that tail rather than a premature 1.
	Fraction float64

	// Source says how Fraction was derived, so a caller can decide how far to
	// trust it (spec 0034, D4). SourceUnknown exactly when Fraction is -1.
	Source FractionSource

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

// FractionSource identifies how Progress.Fraction was derived (spec 0034, D4).
type FractionSource uint8

const (
	// SourceUnknown means Fraction could not be determined; it is -1.
	SourceUnknown FractionSource = iota
	// SourceBytes means Fraction is byte-observed at the fs bridge: input bytes
	// read over the total size of the declared inputs (spec 0031 phase A). It
	// tracks completion well when the inputs dominate the work, and poorly when
	// the encode does.
	SourceBytes
	// SourceEngine means Fraction is engine-derived from out_time/duration (spec
	// 0032 phase B) — accurate regardless of input size, including for generative
	// inputs. Preferred whenever available.
	SourceEngine
)

func (s FractionSource) String() string {
	switch s {
	case SourceBytes:
		return "bytes"
	case SourceEngine:
		return "engine"
	case SourceUnknown:
		return "unknown"
	default:
		return "unknown"
	}
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
func (r *Runtime) startProgress(
	ctx context.Context, fs afero.Fs, plan progressPlan,
) (context.Context, afero.Fs, func()) {
	ch := progressFrom(ctx)
	if ch == nil {
		return ctx, fs, func() {}
	}

	rep := newProgressReporter(ch)
	rep.expectEngine = plan.engineProgress
	rep.declare(fs, plan.inputs)
	rep.start()

	ctx = withProgressSink(ctx, rep.engineSink())

	return ctx, newObserveFs(fs, rep), rep.stop
}

// progressPlan is what Run learns from the job spec before the invocation starts
// (spec 0034): which inputs are declared — so the byte denominator is fixed
// upfront rather than discovered as inputs open (D2) — and whether to expect
// engine records at all (D1).
type progressPlan struct {
	// engineProgress is set for a process job on a backend that can deliver
	// phase-B records. Only then does Fraction hold at -1 waiting for the engine.
	engineProgress bool

	// inputs are the guest paths declared in the spec, including concat members.
	inputs []string
}

// planProgress reads the job spec to build the progress plan. Inputs are
// collected for any op that declares them (frames and probe get phase-A byte
// progress too), while engineProgress is reserved for a process job — probe/
// frames carry no engine progress (0031 D6). Anything unparseable yields a zero
// plan, under which the reporter falls back to the lazily-discovered denominator.
func planProgress(args []string, engineCapable bool) progressPlan {
	if len(args) != 1 {
		return progressPlan{}
	}

	var spec jobSpec
	if err := json.Unmarshal([]byte(args[0]), &spec); err != nil {
		return progressPlan{}
	}

	plan := progressPlan{engineProgress: engineCapable && spec.Op == "process"}

	for _, in := range spec.Inputs {
		if in.Path != "" {
			plan.inputs = append(plan.inputs, in.Path)
		}

		plan.inputs = append(plan.inputs, in.Concat...)
	}

	return plan
}

// engineProgressCapable is implemented by a Backend that can deliver phase-B
// engine records (spec 0032). The wasm backend serves /dev/afmpeg-progress and
// so implements it; the native backend does not until 0033 is revived, and an
// injected third-party backend is assumed not to. A backend that cannot deliver
// records must not make Fraction wait for them (spec 0034, D1).
type engineProgressCapable interface {
	supportsEngineProgress() bool
}

func backendSupportsEngineProgress(b Backend) bool {
	c, ok := b.(engineProgressCapable)

	return ok && c.supportsEngineProgress()
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

	// expectEngine is set when phase-B records are expected, so Fraction holds at
	// -1 rather than reporting a byte ratio the engine will contradict (0034 D1).
	expectEngine bool

	mu    sync.Mutex
	paths map[string]*pathIO

	// declared is the byte denominator fixed at invocation start from the job
	// spec's inputs (0034 D2), keyed by normalised guest path with the value the
	// input's size. declaredTotal is their sum; both are zero when the spec named
	// no statable input, and the reporter then falls back to lazy discovery.
	declared      map[string]int64
	declaredTotal int64

	// Each source carries its own monotone clamp — they are never max-ed together,
	// which is what let a saturated byte ratio pin the engine's value at 1.0
	// (0034 §1). lastFrac floors whatever is reported, so a source switch cannot
	// regress (D5).
	maxFracEng   float64
	maxFracBytes float64
	lastFrac     float64

	// lastTotal is the largest byte denominator seen, so the lazy-discovery
	// fallback can tell a genuine saturation from one measured against a
	// denominator that had not finished growing.
	lastTotal int64

	// final is set by stop before the closing snapshot, which reports the true
	// end-of-job value rather than the in-flight "cannot say" of D3.
	final bool

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

// engineGrace bounds how long Fraction waits for the engine's first record
// before falling back to the byte-observed source (spec 0034, D1). A v9+ engine
// emits its first record well inside it — sub-second in practice — so the wait
// only plays out for an engine that never reports, where giving up and showing
// byte progress beats reporting -1 for the whole job.
const engineGrace = 2 * time.Second

func newProgressReporter(ch chan<- Progress) *progressReporter {
	return &progressReporter{
		ch:       ch,
		origin:   time.Now(),
		paths:    make(map[string]*pathIO),
		declared: make(map[string]int64),
		pending:  make(chan struct{}, 1),
		done:     make(chan struct{}),
	}
}

// declare stats the job spec's declared inputs against fs to fix the byte
// denominator before the invocation starts (spec 0034, D2), replacing the lazy
// accumulation that made the ratio ≈1 throughout for sequentially-opened inputs.
// An input that cannot be statted — missing, or a generative/lavfi pseudo-input —
// contributes nothing and is excluded from both numerator and denominator.
func (p *progressReporter) declare(fs afero.Fs, inputs []string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, in := range inputs {
		fi, err := fs.Stat(in)
		if err != nil || fi.IsDir() || fi.Size() <= 0 {
			continue
		}

		key := normalizeProgressPath(in)
		if _, seen := p.declared[key]; seen {
			continue
		}

		p.declared[key] = fi.Size()
		p.declaredTotal += fi.Size()
	}
}

// normalizeProgressPath collapses a guest path to a clean absolute form so a
// declared input matches the path the engine actually opens, which may or may not
// carry a leading slash (mirrors internal/vfs.normalize).
func normalizeProgressPath(name string) string {
	return path.Clean("/" + name)
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

// clampFrac raises a single source's monotone ceiling to cand (capped at 1) and
// returns it. Each source owns its own ceiling — sharing one is what let a
// saturated byte ratio pin the engine's value at 1.0 (spec 0034 §1).
func clampFrac(ceiling *float64, cand float64) float64 {
	if cand > *ceiling {
		*ceiling = cand
	}

	if *ceiling > 1 {
		*ceiling = 1
	}

	return *ceiling
}

// fractionLocked returns the completion fraction and the source it came from,
// applying spec 0034's precedence: the engine's out_time/duration is
// authoritative (D1); while an engine duration is still plausible the byte ratio
// is withheld rather than reported and later contradicted (D1); a byte ratio that
// has saturated mid-job reports -1 rather than a false 1.0 (D3). The result is
// floored at the last value reported so a source switch never regresses (D5).
// Caller holds p.mu.
func (p *progressReporter) fractionLocked(read, total int64, elapsed time.Duration) (float64, FractionSource) {
	frac, src := p.deriveFracLocked(read, total, elapsed)
	if frac < 0 {
		return -1, SourceUnknown
	}

	if frac < p.lastFrac {
		frac = p.lastFrac
	}

	p.lastFrac = frac

	return frac, src
}

// deriveFracLocked picks the source and computes its raw (per-source clamped)
// value, before the cross-source floor. Caller holds p.mu.
func (p *progressReporter) deriveFracLocked(read, total int64, elapsed time.Duration) (float64, FractionSource) {
	// D1 — the engine's number is authoritative once it reports a duration, and
	// is accurate even when the inputs are trivial next to the encode.
	if p.haveEng && p.eng.durationUs > 0 {
		return p.engineFracLocked()
	}

	// D1 — an engine record is still expected: decline rather than answer with a
	// byte ratio that is about to be contradicted downward.
	if p.awaitingEngineLocked(elapsed) {
		return -1, SourceUnknown
	}

	return p.byteFracLocked(read, total)
}

// engineOverrunTolerance is how far out_time may pass the reported duration
// before the duration is treated as wrong rather than merely reached (spec 0034,
// D6). A correct duration is met, not exceeded; a little slack absorbs the last
// frame's timestamp rounding past the end.
const engineOverrunTolerance = 1.02

// engineFracLocked derives Fraction from the engine's out_time/duration. When
// out_time overruns the duration the job is demonstrably longer than the engine
// predicted — a looped or filter-extended output, or an estimate that was simply
// wrong — so the ratio reports -1 rather than the confident 1.0 that would claim
// a still-running job had finished (spec 0034, D6: the same honesty D3 applies to
// the byte source). Caller holds p.mu.
func (p *progressReporter) engineFracLocked() (float64, FractionSource) {
	ratio := float64(p.eng.outTimeUs) / float64(p.eng.durationUs)

	if ratio > engineOverrunTolerance && !p.final {
		return -1, SourceUnknown
	}

	return clampFrac(&p.maxFracEng, ratio), SourceEngine
}

// awaitingEngineLocked reports whether Fraction should withhold a byte-observed
// answer because the engine's authoritative one is still coming (spec 0034, D1).
// Once the engine has spoken — with or without a duration — it has said all it
// will, and once engineGrace has elapsed in silence it is assumed never to speak.
// Caller holds p.mu.
func (p *progressReporter) awaitingEngineLocked(elapsed time.Duration) bool {
	return p.expectEngine && !p.final && !p.haveEng && elapsed < engineGrace
}

// byteFracLocked computes the byte-observed fraction (spec 0031 phase A) under
// spec 0034's D2/D3. Caller holds p.mu.
func (p *progressReporter) byteFracLocked(read, total int64) (float64, FractionSource) {
	if total <= 0 {
		return -1, SourceUnknown
	}

	// On the lazy-discovery fallback the denominator can grow as inputs open. A
	// ceiling derived from the smaller denominator was measuring a different
	// quantity, so growth retires it rather than pinning the ratio at a stale 1.0
	// for the rest of a concat job. Reported values stay monotone via lastFrac —
	// and a saturated ratio was withheld under D3, so nothing was promised.
	if total > p.lastTotal {
		p.lastTotal = total
		p.maxFracBytes = 0
	}

	frac := clampFrac(&p.maxFracBytes, float64(read)/float64(total))

	// D3 — every declared input byte is consumed but the job is still running, so
	// the remaining work is encode this source cannot see. "Cannot say" beats the
	// "done" a caller would otherwise render for the whole tail.
	if frac >= 1 && !p.final {
		return -1, SourceUnknown
	}

	return frac, SourceBytes
}

// byteProgressLocked sums the byte numerator and denominator. When the job spec
// declared statable inputs the denominator is that fixed total (spec 0034, D2)
// and only declared inputs count toward the numerator, each capped at its own
// size so a seeking or re-reading demuxer cannot push the ratio past 1. With no
// declared inputs (an unparseable spec) it falls back to 0031's lazy discovery,
// which D3's saturation guard still protects. Caller holds p.mu.
func (p *progressReporter) byteProgressLocked() (read, total int64) {
	if p.declaredTotal > 0 {
		for name, c := range p.paths {
			size, ok := p.declared[normalizeProgressPath(name)]
			if !ok {
				continue
			}

			read += min(c.read, size)
		}

		return read, p.declaredTotal
	}

	for _, c := range p.paths {
		if c.read > 0 && c.total > 0 {
			read += c.read
			total += c.total
		}
	}

	return read, total
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

	var written int64

	for _, c := range p.paths {
		written += c.written
	}

	read, total := p.byteProgressLocked()
	frac, src := p.fractionLocked(read, total, elapsed)

	prog := Progress{
		Fraction:    frac,
		Source:      src,
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
	// Mark the job finished before the closing emit so the final snapshot reports
	// the true end-of-job fraction rather than the in-flight "cannot say" that D3
	// and D1 return while work remains (spec 0034).
	p.mu.Lock()
	p.final = true
	p.mu.Unlock()

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
