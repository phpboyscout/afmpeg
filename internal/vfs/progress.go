package vfs

import (
	"bytes"
	"io/fs"

	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
	wsys "github.com/tetratelabs/wazero/sys"
)

const (
	// devProgress is the write-only guest path the v9+ engine emits its NDJSON
	// progress records to (spec 0032, D-B1). afmpeg serves it only when a caller
	// attached a sink (WithProgressSink) — i.e. only when WithProgress is active —
	// so an older host never has this path routed to its real backing fs (D-B3).
	devProgress = "/dev/afmpeg-progress"

	// progressPerm is the permission bits reported for /dev/afmpeg-progress; the
	// device is write-only from the guest's side.
	progressPerm = 0o222

	// maxProgressBuffer caps the partial-line buffer so a malformed engine stream
	// that never emits a newline cannot grow it without bound. A single NDJSON
	// progress record is a few dozen bytes; 64 KiB is a generous ceiling past which
	// the buffer is dropped (the side-channel is best-effort — 0031 D2, R-PROGRESS-B4).
	maxProgressBuffer = 64 << 10
)

// progressFile implements /dev/afmpeg-progress (spec 0032): the engine opens it
// write-only and streams newline-delimited JSON progress records to it. Write
// splits the byte stream on '\n' and hands each complete line to sink; a trailing
// partial line buffers until its newline arrives in a later write. Everything is
// best-effort — a slow or absent consumer never blocks the engine, and the device
// itself never fails a write (R-PROGRESS-B4).
type progressFile struct {
	experimentalsys.UnimplementedFile

	// sink receives one complete NDJSON line (without its trailing newline) per
	// call, from the single guest write path. It must not block.
	sink func([]byte)

	// buf holds bytes received since the last newline.
	buf []byte
}

func newProgressFile(sink func([]byte)) *progressFile {
	return &progressFile{sink: sink}
}

// consume appends p to the line buffer and dispatches every complete line to the
// sink. A copy of each line is handed to the sink so a later append (which may
// reuse the backing array) can never mutate a line the sink still holds.
func (f *progressFile) consume(p []byte) {
	f.buf = append(f.buf, p...)

	for {
		i := bytes.IndexByte(f.buf, '\n')
		if i < 0 {
			break
		}

		line := bytes.TrimSpace(f.buf[:i])
		if len(line) > 0 && f.sink != nil {
			cp := make([]byte, len(line))
			copy(cp, line)
			f.sink(cp)
		}

		f.buf = f.buf[i+1:]
	}

	// Drop a runaway partial line rather than buffer it unboundedly.
	if len(f.buf) > maxProgressBuffer {
		f.buf = f.buf[:0]
	}
}

// Write consumes buf as progress bytes and reports it fully written.
func (f *progressFile) Write(buf []byte) (int, experimentalsys.Errno) {
	f.consume(buf)

	return len(buf), 0
}

// Pwrite consumes buf ignoring the offset — the device is an append-only stream.
func (f *progressFile) Pwrite(buf []byte, _ int64) (int, experimentalsys.Errno) {
	f.consume(buf)

	return len(buf), 0
}

// Read always reports EOF; the device is write-only.
func (*progressFile) Read([]byte) (int, experimentalsys.Errno) {
	return 0, 0
}

// Pread always reports EOF.
func (*progressFile) Pread([]byte, int64) (int, experimentalsys.Errno) {
	return 0, 0
}

// Seek is a no-op; the device has no position.
func (*progressFile) Seek(int64, int) (int64, experimentalsys.Errno) {
	return 0, 0
}

// Truncate is a no-op.
func (*progressFile) Truncate(int64) experimentalsys.Errno {
	return 0
}

// Sync is a no-op.
func (*progressFile) Sync() experimentalsys.Errno {
	return 0
}

// Datasync is a no-op.
func (*progressFile) Datasync() experimentalsys.Errno {
	return 0
}

// IsDir reports that the progress device is not a directory.
func (*progressFile) IsDir() (bool, experimentalsys.Errno) {
	return false, 0
}

// Stat reports the progress device's status.
func (*progressFile) Stat() (wsys.Stat_t, experimentalsys.Errno) {
	return progressStat(), 0
}

// Close is a no-op; the reporter's lifecycle is owned host-side.
func (*progressFile) Close() experimentalsys.Errno {
	return 0
}

// progressStat is the status shared by the progress device's file and path stat.
func progressStat() wsys.Stat_t {
	return wsys.Stat_t{
		Mode:  fs.ModeCharDevice | progressPerm,
		Nlink: singleLink,
	}
}

// isDevProgress reports whether name addresses the progress device.
func isDevProgress(name string) bool {
	return normalize(name) == devProgress
}
