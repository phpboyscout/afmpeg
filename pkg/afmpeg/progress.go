package afmpeg

import (
	"context"
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
// closely for linear demuxers. The Frame/OutTime/Speed fields are reserved for
// phase B (an engine-side progress side-channel) and are zero under phase A.
type Progress struct {
	// Fraction is completion in [0,1], or -1 when it cannot be determined
	// (e.g. a purely generative input with no source file to consume). It never
	// decreases across an invocation, even when a demuxer seeks backwards.
	Fraction float64

	Elapsed     time.Duration // since the invocation began
	InputBytes  int64         // bytes read from inputs so far
	InputTotal  int64         // total size of the inputs being read, 0 if unknown
	OutputBytes int64         // bytes written to outputs so far

	// Populated only by phase B (zero under phase A):
	Frame   int64         // frames processed
	OutTime time.Duration // media timestamp reached
	Speed   float64       // ×realtime encode speed
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

// startProgress wraps fs so the engine's reads and writes are observed and
// reported on the channel WithProgress attached to ctx. When no channel is
// attached it returns fs unchanged and a no-op stop. The returned stop must be
// called when the invocation ends; it emits a final sample and releases the
// reporter's goroutine.
func (r *Runtime) startProgress(ctx context.Context, fs afero.Fs) (afero.Fs, func()) {
	ch := progressFrom(ctx)
	if ch == nil {
		return fs, func() {}
	}

	rep := newProgressReporter(ch)
	rep.start()

	return newObserveFs(fs, rep), rep.stop
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

	pending chan struct{}
	done    chan struct{}
	wg      sync.WaitGroup
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

// nudge signals the reporter goroutine that state changed, coalescing bursts:
// the pending channel holds at most one outstanding signal.
func (p *progressReporter) nudge() {
	select {
	case p.pending <- struct{}{}:
	default:
	}
}

// snapshot builds a Progress from the current counters. Fraction is size-weighted
// across every input read from (D4) and clamped monotone-nondecreasing (D3);
// it is -1 when nothing with a known size has been read (D5).
func (p *progressReporter) snapshot() Progress {
	p.mu.Lock()

	var read, total, written int64

	for _, c := range p.paths {
		written += c.written
		if c.read > 0 && c.total > 0 {
			read += c.read
			total += c.total
		}
	}

	frac := -1.0
	if total > 0 {
		frac = float64(read) / float64(total)
		if frac > 1 {
			frac = 1
		}

		if frac > p.maxFrac {
			p.maxFrac = frac
		}

		frac = p.maxFrac
	}

	p.mu.Unlock()

	return Progress{
		Fraction:    frac,
		Elapsed:     time.Since(p.origin),
		InputBytes:  read,
		InputTotal:  total,
		OutputBytes: written,
	}
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
