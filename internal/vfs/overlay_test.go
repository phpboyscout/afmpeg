package vfs_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"testing"

	"github.com/spf13/afero"
	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"

	"gitlab.com/phpboyscout/afmpeg/internal/vfs"
)

var errInjected = errors.New("injected stat failure")

// statErrFs returns files whose Stat fails, to exercise the bridge's file-level
// Stat/IsDir error branches (MemMapFs never fails Stat on an open handle).
type statErrFs struct {
	afero.Fs
}

func (s statErrFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	f, err := s.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}

	return statErrFile{File: f}, nil
}

type statErrFile struct {
	afero.File
}

func (statErrFile) Stat() (os.FileInfo, error) {
	return nil, errInjected
}

// TestTmpIsolation is R-0003-3: writes under /tmp land in an isolated scratch fs
// and never touch the caller's root filesystem.
func TestTmpIsolation(t *testing.T) {
	t.Parallel()

	root := afero.NewMemMapFs()
	scratch := afero.NewMemMapFs()
	v := vfs.New(root, vfs.WithTmpFs(scratch))

	f, errno := v.OpenFile("/tmp/work.tmp", experimentalsys.O_RDWR|experimentalsys.O_CREAT, 0o644)
	if errno != 0 {
		t.Fatalf("OpenFile /tmp: errno %v", errno)
	}

	if _, errno := f.Write([]byte("scratch")); errno != 0 {
		t.Fatalf("Write: errno %v", errno)
	}

	if errno := f.Close(); errno != 0 {
		t.Fatalf("Close: errno %v", errno)
	}

	// Present in the scratch fs...
	if ok, _ := afero.Exists(scratch, "/tmp/work.tmp"); !ok {
		t.Fatalf("scratch write not found in the /tmp backend")
	}

	// ...and absent from the caller's root fs.
	if ok, _ := afero.Exists(root, "/tmp/work.tmp"); ok {
		t.Fatalf("scratch write leaked into the caller's root fs")
	}

	// Stat over the bridge resolves the scratch file.
	if st, errno := v.Stat("/tmp/work.tmp"); errno != 0 || st.Size != 7 {
		t.Fatalf("Stat /tmp = %d errno=%v, want size 7", st.Size, errno)
	}
}

// TestDevNull is R-0003-3: /dev/null accepts and discards writes, reports EOF on
// read, and stats as a character device.
func TestDevNull(t *testing.T) {
	t.Parallel()

	v := vfs.New(afero.NewMemMapFs())

	f, errno := v.OpenFile("/dev/null", experimentalsys.O_RDWR, 0)
	if errno != 0 {
		t.Fatalf("OpenFile /dev/null: errno %v", errno)
	}

	if n, errno := f.Write([]byte("discarded")); errno != 0 || n != 9 {
		t.Fatalf("Write = %d errno=%v, want 9", n, errno)
	}

	if n, errno := f.Pwrite([]byte("xy"), 3); errno != 0 || n != 2 {
		t.Fatalf("Pwrite = %d errno=%v, want 2", n, errno)
	}

	if n, errno := f.Read(make([]byte, 4)); errno != 0 || n != 0 {
		t.Fatalf("Read = %d errno=%v, want EOF (0)", n, errno)
	}

	if n, errno := f.Pread(make([]byte, 4), 0); errno != 0 || n != 0 {
		t.Fatalf("Pread = %d errno=%v, want EOF (0)", n, errno)
	}

	if off, errno := f.Seek(10, io.SeekStart); errno != 0 || off != 0 {
		t.Fatalf("Seek = %d errno=%v, want 0", off, errno)
	}

	if errno := f.Truncate(0); errno != 0 {
		t.Fatalf("Truncate: errno %v", errno)
	}

	if errno := f.Sync(); errno != 0 {
		t.Fatalf("Sync: errno %v", errno)
	}

	if errno := f.Datasync(); errno != 0 {
		t.Fatalf("Datasync: errno %v", errno)
	}

	if dir, errno := f.IsDir(); errno != 0 || dir {
		t.Fatalf("IsDir = %v errno=%v, want false", dir, errno)
	}

	if st, errno := f.Stat(); errno != 0 || st.Mode&fs.ModeCharDevice == 0 {
		t.Fatalf("file Stat mode = %v errno=%v, want char device", st.Mode, errno)
	}

	if errno := f.Close(); errno != 0 {
		t.Fatalf("Close: errno %v", errno)
	}

	// Path-level stat reports a writable character device.
	st, errno := v.Stat("/dev/null")
	if errno != 0 || st.Mode&fs.ModeCharDevice == 0 {
		t.Fatalf("Stat /dev/null mode = %v errno=%v, want char device", st.Mode, errno)
	}
}

// TestZeroLengthOps covers the short-circuit paths fd_read/fd_write take for
// empty buffers.
func TestZeroLengthOps(t *testing.T) {
	t.Parallel()

	v := vfs.New(afero.NewMemMapFs())

	f, errno := v.OpenFile("f.bin", experimentalsys.O_RDWR|experimentalsys.O_CREAT, 0o644)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}

	empty := []byte{}

	if n, errno := f.Write(empty); errno != 0 || n != 0 {
		t.Fatalf("Write empty = %d errno=%v", n, errno)
	}

	if n, errno := f.Pwrite(empty, 0); errno != 0 || n != 0 {
		t.Fatalf("Pwrite empty = %d errno=%v", n, errno)
	}

	if n, errno := f.Read(empty); errno != 0 || n != 0 {
		t.Fatalf("Read empty = %d errno=%v", n, errno)
	}

	if n, errno := f.Pread(empty, 0); errno != 0 || n != 0 {
		t.Fatalf("Pread empty = %d errno=%v", n, errno)
	}

	if errno := f.Close(); errno != 0 {
		t.Fatalf("Close: errno %v", errno)
	}
}

// TestFileStatError covers the error branches of the file-level Stat and IsDir
// via an injected Stat failure.
func TestFileStatError(t *testing.T) {
	t.Parallel()

	v := vfs.New(statErrFs{Fs: afero.NewMemMapFs()})

	f, errno := v.OpenFile("f.bin", experimentalsys.O_RDWR|experimentalsys.O_CREAT, 0o644)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}

	if _, errno := f.Stat(); errno == 0 {
		t.Fatalf("Stat: errno = 0, want non-zero")
	}

	if _, errno := f.IsDir(); errno == 0 {
		t.Fatalf("IsDir: errno = 0, want non-zero")
	}
}

// TestMissingPathErrors covers the stat-error branches of Unlink and Rmdir.
func TestMissingPathErrors(t *testing.T) {
	t.Parallel()

	v := vfs.New(afero.NewMemMapFs())

	if errno := v.Unlink("nope"); errno != experimentalsys.ENOENT {
		t.Fatalf("Unlink missing: errno = %v, want ENOENT", errno)
	}

	if errno := v.Rmdir("nope"); errno != experimentalsys.ENOENT {
		t.Fatalf("Rmdir missing: errno = %v, want ENOENT", errno)
	}
}
