package vfs_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"

	"gitlab.com/phpboyscout/afmpeg/internal/vfs"
)

// recordingFs wraps an afero.Fs and records the paths opened, so a test can
// prove the adapter routes operations through the injected filesystem rather
// than the host (R-0003-2).
type recordingFs struct {
	afero.Fs

	opened []string
}

func (r *recordingFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	r.opened = append(r.opened, name)

	return r.Fs.OpenFile(name, flag, perm)
}

// TestNoHostFilesystemAccess is R-0003-2: with a MemMapFs and no host preopens,
// a guest write resolves entirely in memory and never reaches the host disk.
func TestNoHostFilesystemAccess(t *testing.T) {
	t.Parallel()

	mem := afero.NewMemMapFs()
	spy := &recordingFs{Fs: mem}
	v := vfs.New(spy)

	const canary = "/afmpeg-novfs-canary.bin"

	f, errno := v.OpenFile(canary, experimentalsys.O_RDWR|experimentalsys.O_CREAT, 0o644)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}

	if _, errno := f.Write([]byte("in-memory only")); errno != 0 {
		t.Fatalf("Write: errno %v", errno)
	}

	if errno := f.Close(); errno != 0 {
		t.Fatalf("Close: errno %v", errno)
	}

	// It was routed through the injected fs...
	if len(spy.opened) == 0 || spy.opened[0] != canary {
		t.Fatalf("OpenFile not routed to the injected fs; opened=%v", spy.opened)
	}

	// ...it landed in the in-memory fs...
	if ok, _ := afero.Exists(mem, canary); !ok {
		t.Fatalf("canary not present in the in-memory fs")
	}

	// ...and it never touched the host disk.
	if _, err := os.Stat(canary); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("canary leaked to the host fs: err=%v", err)
	}
}

func TestOpenFile_Errors(t *testing.T) {
	t.Parallel()

	v := vfs.New(afero.NewMemMapFs())

	// Missing file without O_CREAT → ENOENT.
	if _, errno := v.OpenFile("missing", experimentalsys.O_RDONLY, 0); errno != experimentalsys.ENOENT {
		t.Fatalf("open missing: errno = %v, want ENOENT", errno)
	}

	// Create then re-create with O_EXCL → EEXIST.
	if _, errno := v.OpenFile("dup", experimentalsys.O_WRONLY|experimentalsys.O_CREAT, 0o644); errno != 0 {
		t.Fatalf("create dup: errno %v", errno)
	}

	_, errno := v.OpenFile("dup", experimentalsys.O_WRONLY|experimentalsys.O_CREAT|experimentalsys.O_EXCL, 0o644)
	if errno != experimentalsys.EEXIST {
		t.Fatalf("exclusive re-create: errno = %v, want EEXIST", errno)
	}
}

func TestStatAndLstat(t *testing.T) {
	t.Parallel()

	mem := afero.NewMemMapFs()
	if err := afero.WriteFile(mem, "f.bin", []byte("12345"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	v := vfs.New(mem)

	st, errno := v.Stat("f.bin")
	if errno != 0 {
		t.Fatalf("Stat: errno %v", errno)
	}

	if st.Size != 5 {
		t.Fatalf("Stat size = %d, want 5", st.Size)
	}

	if st.Nlink != 1 {
		t.Fatalf("Stat nlink = %d, want 1", st.Nlink)
	}

	// Lstat mirrors Stat for the symlink-free backends afmpeg targets.
	lst, errno := v.Lstat("f.bin")
	if errno != 0 || lst.Size != st.Size {
		t.Fatalf("Lstat = %+v errno=%v, want size %d", lst, errno, st.Size)
	}

	if _, errno := v.Stat("nope"); errno != experimentalsys.ENOENT {
		t.Fatalf("Stat missing: errno = %v, want ENOENT", errno)
	}
}

func TestDirectoryOps(t *testing.T) {
	t.Parallel()

	mem := afero.NewMemMapFs()
	v := vfs.New(mem)

	if errno := v.Mkdir("d", 0o755); errno != 0 {
		t.Fatalf("Mkdir: errno %v", errno)
	}

	if err := afero.WriteFile(mem, "d/f.bin", []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Unlink rejects directories; Rmdir rejects non-directories.
	if errno := v.Unlink("d"); errno != experimentalsys.EISDIR {
		t.Fatalf("Unlink dir: errno = %v, want EISDIR", errno)
	}

	if errno := v.Rmdir("d/f.bin"); errno != experimentalsys.ENOTDIR {
		t.Fatalf("Rmdir file: errno = %v, want ENOTDIR", errno)
	}

	// Rename then unlink the file; rmdir the now-empty directory.
	if errno := v.Rename("d/f.bin", "d/g.bin"); errno != 0 {
		t.Fatalf("Rename: errno %v", errno)
	}

	if errno := v.Unlink("d/g.bin"); errno != 0 {
		t.Fatalf("Unlink: errno %v", errno)
	}

	if errno := v.Rmdir("d"); errno != 0 {
		t.Fatalf("Rmdir: errno %v", errno)
	}

	if _, errno := v.Stat("d"); errno != experimentalsys.ENOENT {
		t.Fatalf("dir still present after Rmdir: errno %v", errno)
	}
}

func TestFileOps(t *testing.T) {
	t.Parallel()

	v := vfs.New(afero.NewMemMapFs())

	f, errno := v.OpenFile("f.bin", experimentalsys.O_RDWR|experimentalsys.O_CREAT, 0o644)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}

	if _, errno := f.Write([]byte("0123456789")); errno != 0 {
		t.Fatalf("Write: errno %v", errno)
	}

	// Pread reads at an offset without moving the cursor.
	got := make([]byte, 3)
	if n, errno := f.Pread(got, 4); errno != 0 || string(got[:n]) != "456" {
		t.Fatalf("Pread = %q errno=%v, want \"456\"", got[:n], errno)
	}

	// Sync / Datasync are no-op-safe on MemMapFs.
	if errno := f.Sync(); errno != 0 {
		t.Fatalf("Sync: errno %v", errno)
	}

	if errno := f.Datasync(); errno != 0 {
		t.Fatalf("Datasync: errno %v", errno)
	}

	// Truncate shrinks the file.
	if errno := f.Truncate(4); errno != 0 {
		t.Fatalf("Truncate: errno %v", errno)
	}

	st, errno := f.Stat()
	if errno != 0 || st.Size != 4 {
		t.Fatalf("Stat after truncate = %d errno=%v, want 4", st.Size, errno)
	}

	if dir, errno := f.IsDir(); errno != 0 || dir {
		t.Fatalf("IsDir = %v errno=%v, want false", dir, errno)
	}

	if errno := f.Close(); errno != 0 {
		t.Fatalf("Close: errno %v", errno)
	}
}

func TestSeek_InvalidWhence(t *testing.T) {
	t.Parallel()

	v := vfs.New(afero.NewMemMapFs())

	f, errno := v.OpenFile("f.bin", experimentalsys.O_RDWR|experimentalsys.O_CREAT, 0o644)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}

	const badWhence = 9
	if _, errno := f.Seek(0, badWhence); errno != experimentalsys.EINVAL {
		t.Fatalf("Seek bad whence: errno = %v, want EINVAL", errno)
	}
}

// TestMultiBackend is R-0003-5: the same seek-on-write round-trip succeeds
// against any afero backend, not just MemMapFs.
func TestMultiBackend(t *testing.T) {
	t.Parallel()

	t.Run("memmapfs", func(t *testing.T) {
		t.Parallel()
		runSeekOnWrite(t, afero.NewMemMapFs(), "out.bin")
	})

	t.Run("basepathfs-over-mem", func(t *testing.T) {
		t.Parallel()

		base := afero.NewMemMapFs()
		if err := base.MkdirAll("/work", 0o755); err != nil {
			t.Fatalf("mkdir base: %v", err)
		}

		runSeekOnWrite(t, afero.NewBasePathFs(base, "/work"), "out.bin")
	})

	t.Run("osfs-tempdir", func(t *testing.T) {
		t.Parallel()
		runSeekOnWrite(t, afero.NewOsFs(), filepath.Join(t.TempDir(), "out.bin"))
	})
}

// runSeekOnWrite performs the write → seek-back → overwrite → read round-trip
// against the given backend and asserts the patched content.
func runSeekOnWrite(t *testing.T, afs afero.Fs, name string) {
	t.Helper()

	v := vfs.New(afs)

	f, errno := v.OpenFile(name, experimentalsys.O_RDWR|experimentalsys.O_CREAT|experimentalsys.O_TRUNC, 0o644)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}

	if _, errno := f.Write([]byte("....mdatPAYLOAD")); errno != 0 {
		t.Fatalf("Write: errno %v", errno)
	}

	if _, errno := f.Seek(0, io.SeekStart); errno != 0 {
		t.Fatalf("Seek: errno %v", errno)
	}

	if _, errno := f.Write([]byte("SIZE")); errno != 0 {
		t.Fatalf("overwrite: errno %v", errno)
	}

	if _, errno := f.Seek(0, io.SeekStart); errno != 0 {
		t.Fatalf("rewind: errno %v", errno)
	}

	got := make([]byte, 32)

	n, errno := f.Read(got)
	if errno != 0 {
		t.Fatalf("Read: errno %v", errno)
	}

	if want := "SIZEmdatPAYLOAD"; string(got[:n]) != want {
		t.Fatalf("content = %q, want %q", got[:n], want)
	}

	if errno := f.Close(); errno != 0 {
		t.Fatalf("Close: errno %v", errno)
	}
}
