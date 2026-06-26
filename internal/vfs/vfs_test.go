package vfs_test

import (
	"io"
	"testing"

	"github.com/spf13/afero"
	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"

	"gitlab.com/phpboyscout/afmpeg/internal/vfs"
)

// TestSeekOnWrite_MoovRewrite exercises the mp4 +faststart shape: write a
// placeholder header, append the payload, then seek back and overwrite the
// placeholder (the moov-atom size patch the muxer performs). This is R-0003-1
// — the highest-risk behaviour and the whole-project de-risk gate (spec 0003
// §1, §7). If wazero's sys.FS write path can't seek-back-and-overwrite over an
// afero.Fs, the whole approach is in question — so this is the first test.
func TestSeekOnWrite_MoovRewrite(t *testing.T) {
	t.Parallel()

	v := vfs.New(afero.NewMemMapFs())

	f, errno := v.OpenFile("out.mp4", experimentalsys.O_RDWR|experimentalsys.O_CREAT|experimentalsys.O_TRUNC, 0o644)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}

	// Write a 4-byte placeholder followed by the payload.
	if n, errno := f.Write([]byte("....mdatPAYLOAD")); errno != 0 || n != 15 {
		t.Fatalf("Write: n=%d errno=%v", n, errno)
	}

	// Seek back to the start and overwrite the placeholder (the moov patch).
	if off, errno := f.Seek(0, io.SeekStart); errno != 0 || off != 0 {
		t.Fatalf("Seek: off=%d errno=%v", off, errno)
	}

	if n, errno := f.Write([]byte("SIZE")); errno != 0 || n != 4 {
		t.Fatalf("overwrite Write: n=%d errno=%v", n, errno)
	}

	// Rewind and read the whole file back; assert the patched content.
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

	// Stat reports the final size.
	st, errno := f.Stat()
	if errno != 0 {
		t.Fatalf("Stat: errno %v", errno)
	}

	if st.Size != 15 {
		t.Fatalf("Size = %d, want 15", st.Size)
	}

	if errno := f.Close(); errno != 0 {
		t.Fatalf("Close: errno %v", errno)
	}
}

// TestPwrite_PatchAtOffset covers the positional-write path (fd_pwrite): patch a
// region without moving the file offset, then confirm a sequential read still
// sees the surrounding bytes intact.
func TestPwrite_PatchAtOffset(t *testing.T) {
	t.Parallel()

	v := vfs.New(afero.NewMemMapFs())

	f, errno := v.OpenFile("out.mp4", experimentalsys.O_RDWR|experimentalsys.O_CREAT, 0o644)
	if errno != 0 {
		t.Fatalf("OpenFile: errno %v", errno)
	}

	if _, errno := f.Write([]byte("....mdat")); errno != 0 {
		t.Fatalf("Write: errno %v", errno)
	}

	// Patch the 4-byte placeholder at offset 0 without disturbing the offset.
	if n, errno := f.Pwrite([]byte("SIZE"), 0); errno != 0 || n != 4 {
		t.Fatalf("Pwrite: n=%d errno=%v", n, errno)
	}

	if _, errno := f.Seek(0, io.SeekStart); errno != 0 {
		t.Fatalf("rewind: errno %v", errno)
	}

	got := make([]byte, 8)
	if _, errno := f.Read(got); errno != 0 {
		t.Fatalf("Read: errno %v", errno)
	}

	if string(got) != "SIZEmdat" {
		t.Fatalf("content = %q, want %q", got, "SIZEmdat")
	}

	if errno := f.Close(); errno != 0 {
		t.Fatalf("Close: errno %v", errno)
	}
}
