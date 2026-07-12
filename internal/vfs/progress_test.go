package vfs_test

import (
	"bytes"
	"io/fs"
	"sync"
	"testing"

	"github.com/spf13/afero"
	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"

	"gitlab.com/phpboyscout/afmpeg/internal/vfs"
)

const devProgress = "/dev/afmpeg-progress"

// collectSink returns a sink that records each line it is handed (copied) and a
// getter for the collected lines. It is safe for the single guest write path.
func collectSink() (func([]byte), func() [][]byte) {
	var (
		mu    sync.Mutex
		lines [][]byte
	)

	sink := func(line []byte) {
		mu.Lock()
		defer mu.Unlock()

		cp := make([]byte, len(line))
		copy(cp, line)
		lines = append(lines, cp)
	}

	get := func() [][]byte {
		mu.Lock()
		defer mu.Unlock()

		return lines
	}

	return sink, get
}

// With a sink attached, /dev/afmpeg-progress is a device whose writes are split
// into complete NDJSON lines and handed to the sink, with partial lines buffered
// across writes and blank lines dropped.
func TestProgressDevice_splitsLinesAcrossWrites(t *testing.T) {
	t.Parallel()

	sink, lines := collectSink()
	v := vfs.New(afero.NewMemMapFs(), vfs.WithProgressSink(sink))

	f, errno := v.OpenFile(devProgress, experimentalsys.O_WRONLY|experimentalsys.O_CREAT, 0o222)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}

	// A record split across two writes, a whole record, then a trailing partial.
	writes := [][]byte{
		[]byte(`{"frame":1,`),
		[]byte(`"out_time_us":100}` + "\n" + `{"frame":2,"out_time_us":200}` + "\n"),
		[]byte("\n"), // blank line dropped
		[]byte(`{"frame":3`),
	}
	for _, w := range writes {
		n, werr := f.Write(w)
		if werr != 0 || n != len(w) {
			t.Fatalf("Write: n=%d errno=%v", n, werr)
		}
	}

	got := lines()
	if len(got) != 2 {
		t.Fatalf("want 2 complete lines, got %d: %q", len(got), got)
	}
	if string(got[0]) != `{"frame":1,"out_time_us":100}` {
		t.Fatalf("line 0 = %q", got[0])
	}
	if string(got[1]) != `{"frame":2,"out_time_us":200}` {
		t.Fatalf("line 1 = %q", got[1])
	}

	// The device stats as a write-only char device, and never touches the fs.
	st, errno := f.Stat()
	if errno != 0 || st.Mode&fs.ModeCharDevice == 0 {
		t.Fatalf("Stat mode=%v errno=%v, want char device", st.Mode, errno)
	}
	if errno := f.Close(); errno != 0 {
		t.Fatalf("Close: errno %v", errno)
	}
}

// Pwrite feeds the same line splitter as Write (the engine may use either).
func TestProgressDevice_pwriteFeedsSink(t *testing.T) {
	t.Parallel()

	sink, lines := collectSink()
	v := vfs.New(afero.NewMemMapFs(), vfs.WithProgressSink(sink))

	f, errno := v.OpenFile(devProgress, experimentalsys.O_WRONLY, 0o222)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}

	rec := []byte(`{"frame":9,"out_time_us":42}` + "\n")
	if _, werr := f.Pwrite(rec, 0); werr != 0 {
		t.Fatalf("Pwrite: errno %v", werr)
	}

	if got := lines(); len(got) != 1 || string(got[0]) != `{"frame":9,"out_time_us":42}` {
		t.Fatalf("Pwrite did not feed the sink: %q", got)
	}

	// The device reads as EOF (write-only).
	if n, rerr := f.Read(make([]byte, 8)); n != 0 || rerr != 0 {
		t.Fatalf("Read on write-only device: n=%d errno=%v", n, rerr)
	}
}

// Without a sink, /dev/afmpeg-progress is NOT overlaid: the path resolves against
// the backing fs like any other (spec 0032 D-B3 — no spurious device for a host
// that did not opt in). afmpeg never sends progress:true in that case, so the
// engine never writes here; this proves the guard, not a real code path.
func TestProgressDevice_notServedWithoutSink(t *testing.T) {
	t.Parallel()

	mem := afero.NewMemMapFs()
	v := vfs.New(mem) // no WithProgressSink

	f, errno := v.OpenFile(devProgress, experimentalsys.O_WRONLY|experimentalsys.O_CREAT, 0o644)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}
	if _, werr := f.Write([]byte("data\n")); werr != 0 {
		t.Fatalf("Write: errno %v", werr)
	}
	_ = f.Close()

	// It landed on the backing fs as a regular file, not the device.
	b, err := afero.ReadFile(mem, devProgress)
	if err != nil {
		t.Fatalf("expected a backing-fs file without a sink: %v", err)
	}
	if !bytes.Equal(b, []byte("data\n")) {
		t.Fatalf("backing file = %q, want the raw bytes", b)
	}
}

// The device's remaining File methods are inert no-ops (the engine only opens it
// write-only): Pread reports EOF, and Seek/Truncate/Sync/Datasync/IsDir/Close all
// succeed without side effects.
func TestProgressDevice_noopMethods(t *testing.T) {
	t.Parallel()

	v := vfs.New(afero.NewMemMapFs(), vfs.WithProgressSink(func([]byte) {}))

	f, errno := v.OpenFile(devProgress, experimentalsys.O_WRONLY, 0o222)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}

	if n, e := f.Pread(make([]byte, 4), 0); n != 0 || e != 0 {
		t.Fatalf("Pread: n=%d errno=%v, want EOF", n, e)
	}
	if off, e := f.Seek(10, 0); off != 0 || e != 0 {
		t.Fatalf("Seek: off=%d errno=%v", off, e)
	}
	if e := f.Truncate(0); e != 0 {
		t.Fatalf("Truncate: errno %v", e)
	}
	if e := f.Sync(); e != 0 {
		t.Fatalf("Sync: errno %v", e)
	}
	if e := f.Datasync(); e != 0 {
		t.Fatalf("Datasync: errno %v", e)
	}
	if dir, e := f.IsDir(); dir || e != 0 {
		t.Fatalf("IsDir: %v errno=%v, want false", dir, e)
	}
	if e := f.Close(); e != 0 {
		t.Fatalf("Close: errno %v", e)
	}
}

// A malformed stream that never emits a newline must not grow the buffer without
// bound: past the cap the partial buffer is dropped (best-effort, R-PROGRESS-B4).
func TestProgressDevice_dropsRunawayPartialLine(t *testing.T) {
	t.Parallel()

	var calls int
	sink := func([]byte) { calls++ }
	v := vfs.New(afero.NewMemMapFs(), vfs.WithProgressSink(sink))

	f, errno := v.OpenFile(devProgress, experimentalsys.O_WRONLY, 0o222)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}

	// 256 KiB with no newline — well past the 64 KiB cap; nothing is emitted and
	// the write never fails.
	blob := bytes.Repeat([]byte("x"), 256<<10)
	if n, werr := f.Write(blob); werr != 0 || n != len(blob) {
		t.Fatalf("Write: n=%d errno=%v", n, werr)
	}
	if calls != 0 {
		t.Fatalf("no complete line was written, sink called %d times", calls)
	}
}
