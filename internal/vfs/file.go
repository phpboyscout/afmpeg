package vfs

import (
	"io"

	"github.com/spf13/afero"
	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
	wsys "github.com/tetratelabs/wazero/sys"
)

// file adapts an afero.File to experimental/sys.File. Embedding
// UnimplementedFile supplies ENOSYS defaults for operations afmpeg's guest does
// not exercise (e.g. SetAppend, Utimens), keeping the type forward-compatible.
type file struct {
	experimentalsys.UnimplementedFile

	af afero.File
}

func newFile(af afero.File) *file {
	return &file{af: af}
}

// Read fills buf from the current offset. A zero Errno with n == 0 signals EOF,
// matching wazero's file contract.
func (f *file) Read(buf []byte) (int, experimentalsys.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}

	n, err := f.af.Read(buf)

	return n, experimentalsys.UnwrapOSError(err)
}

// Pread reads at an absolute offset without moving the file offset (fd_pread).
func (f *file) Pread(buf []byte, off int64) (int, experimentalsys.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}

	n, err := f.af.ReadAt(buf, off)

	return n, experimentalsys.UnwrapOSError(err)
}

// Write writes buf at the current offset.
func (f *file) Write(buf []byte) (int, experimentalsys.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}

	n, err := f.af.Write(buf)

	return n, experimentalsys.UnwrapOSError(err)
}

// Pwrite writes at an absolute offset without moving the file offset
// (fd_pwrite) — the path the mp4 muxer uses to patch the moov atom under
// +faststart.
func (f *file) Pwrite(buf []byte, off int64) (int, experimentalsys.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}

	n, err := f.af.WriteAt(buf, off)

	return n, experimentalsys.UnwrapOSError(err)
}

// Seek moves the file offset and returns the new absolute offset.
func (f *file) Seek(offset int64, whence int) (int64, experimentalsys.Errno) {
	if uint(whence) > io.SeekEnd {
		return 0, experimentalsys.EINVAL
	}

	newOffset, err := f.af.Seek(offset, whence)

	return newOffset, experimentalsys.UnwrapOSError(err)
}

// Truncate resizes the file to size bytes.
func (f *file) Truncate(size int64) experimentalsys.Errno {
	return experimentalsys.UnwrapOSError(f.af.Truncate(size))
}

// Sync flushes the file to its backing store.
func (f *file) Sync() experimentalsys.Errno {
	return experimentalsys.UnwrapOSError(f.af.Sync())
}

// Datasync flushes file data. afero has no data-only sync, so it maps to Sync.
func (f *file) Datasync() experimentalsys.Errno {
	return f.Sync()
}

// Stat returns the file's status.
func (f *file) Stat() (wsys.Stat_t, experimentalsys.Errno) {
	info, err := f.af.Stat()
	if err != nil {
		return wsys.Stat_t{}, experimentalsys.UnwrapOSError(err)
	}

	return statFromInfo(info), 0
}

// IsDir reports whether the file is a directory.
func (f *file) IsDir() (bool, experimentalsys.Errno) {
	info, err := f.af.Stat()
	if err != nil {
		return false, experimentalsys.UnwrapOSError(err)
	}

	return info.IsDir(), 0
}

// Close releases the file.
func (f *file) Close() experimentalsys.Errno {
	return experimentalsys.UnwrapOSError(f.af.Close())
}
