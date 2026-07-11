//go:build spike

package main

import (
	"os"
	"sync"

	"github.com/spf13/afero"
)

// Event is one observed I/O delta from the engine, as seen by the host at the
// afero.Fs boundary. The engine never emits these — the host synthesises them by
// watching the bytes flow through the filesystem it already implements.
type Event struct {
	ElapsedMS int64  // ms since the run started
	Path      string // guest path, e.g. "in.wav" / "out.m4a"
	Op        string // "read" | "write"
	Cum       int64  // cumulative bytes read-from / written-to this path
	Total     int64  // known size for inputs (via Stat); 0 when unknown (outputs)
}

// observeFs decorates an afero.Fs so every Read/Write the engine performs is
// reported on a channel. It is a pure host-side wrapper around the SAME afero.Fs
// the caller already hands to Runtime.Run — no afmpeg or engine change is
// involved. This is the whole thesis of the spike: afmpeg *is* the filesystem,
// so progress is observable at the I/O boundary for free.
type observeFs struct {
	afero.Fs
	sink  func(Event)
	start func() int64 // elapsed-ms clock
	mu    sync.Mutex
	cum   map[string]*counters
}

type counters struct{ read, written int64 }

func newObserveFs(base afero.Fs, clock func() int64, sink func(Event)) *observeFs {
	return &observeFs{Fs: base, sink: sink, start: clock, cum: map[string]*counters{}}
}

func (o *observeFs) countersFor(name string) *counters {
	o.mu.Lock()
	defer o.mu.Unlock()

	c := o.cum[name]
	if c == nil {
		c = &counters{}
		o.cum[name] = c
	}

	return c
}

func (o *observeFs) wrap(name string, af afero.File) afero.File {
	total := int64(0)
	if fi, err := o.Fs.Stat(name); err == nil {
		total = fi.Size()
	}

	return &observeFile{File: af, fs: o, name: name, total: total}
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

// observeFile counts bytes crossing Read/ReadAt/Write/WriteAt and emits an Event
// per operation. These are exactly the methods internal/vfs calls on the afero
// file (see internal/vfs/file.go), so nothing the engine does escapes them.
type observeFile struct {
	afero.File
	fs    *observeFs
	name  string
	total int64
}

func (f *observeFile) emit(op string, n int, cum int64) {
	if n <= 0 {
		return
	}

	f.fs.sink(Event{
		ElapsedMS: f.fs.start(),
		Path:      f.name,
		Op:        op,
		Cum:       cum,
		Total:     f.total,
	})
}

func (f *observeFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	c := f.fs.countersFor(f.name)
	f.fs.mu.Lock()
	c.read += int64(n)
	cum := c.read
	f.fs.mu.Unlock()
	f.emit("read", n, cum)

	return n, err
}

func (f *observeFile) ReadAt(p []byte, off int64) (int, error) {
	n, err := f.File.ReadAt(p, off)
	c := f.fs.countersFor(f.name)
	f.fs.mu.Lock()
	c.read += int64(n)
	cum := c.read
	f.fs.mu.Unlock()
	f.emit("read", n, cum)

	return n, err
}

func (f *observeFile) Write(p []byte) (int, error) {
	n, err := f.File.Write(p)
	c := f.fs.countersFor(f.name)
	f.fs.mu.Lock()
	c.written += int64(n)
	cum := c.written
	f.fs.mu.Unlock()
	f.emit("write", n, cum)

	return n, err
}

func (f *observeFile) WriteAt(p []byte, off int64) (int, error) {
	n, err := f.File.WriteAt(p, off)
	c := f.fs.countersFor(f.name)
	f.fs.mu.Lock()
	c.written += int64(n)
	cum := c.written
	f.fs.mu.Unlock()
	f.emit("write", n, cum)

	return n, err
}
