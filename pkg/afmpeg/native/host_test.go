package native

import (
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/spf13/afero"
)

// wireClient speaks the driver side of the IPC protocol over a net.Conn, with a
// sticky error so a test (or a fake driver) can script a session linearly. It is
// the test-side mirror of the custom AVIO callbacks the native driver installs.
type wireClient struct {
	c   net.Conn
	err error
}

func (w *wireClient) put(b []byte) {
	if w.err == nil {
		_, w.err = w.c.Write(b)
	}
}

func (w *wireClient) get(n int) []byte {
	if w.err != nil {
		return nil
	}

	b := make([]byte, n)
	if _, e := io.ReadFull(w.c, b); e != nil {
		w.err = e

		return nil
	}

	return b
}

func (w *wireClient) version() { w.put([]byte{protocolVersion}) }

func (w *wireClient) open(name string, mode byte) byte {
	hdr := make([]byte, 6)
	hdr[0] = opOpen
	hdr[1] = mode
	binary.LittleEndian.PutUint32(hdr[2:], uint32(len(name)))
	w.put(hdr)
	w.put([]byte(name))

	st := w.get(1)
	if st == nil {
		return statusErr
	}

	return st[0]
}

func (w *wireClient) read(size uint32) []byte {
	b := make([]byte, 5)
	b[0] = opRead
	binary.LittleEndian.PutUint32(b[1:], size)
	w.put(b)

	cnt := w.get(4)
	if cnt == nil {
		return nil
	}

	return w.get(int(binary.LittleEndian.Uint32(cnt)))
}

func (w *wireClient) write(data []byte) uint32 {
	b := make([]byte, 5)
	b[0] = opWrite
	binary.LittleEndian.PutUint32(b[1:], uint32(len(data)))
	w.put(b)
	w.put(data)

	n := w.get(4)
	if n == nil {
		return 0
	}

	return binary.LittleEndian.Uint32(n)
}

func (w *wireClient) seek(off int64, whence byte) int64 {
	b := make([]byte, 10)
	b[0] = opSeek
	binary.LittleEndian.PutUint64(b[1:], uint64(off))
	b[9] = whence
	w.put(b)

	r := w.get(8)
	if r == nil {
		return -1
	}

	return int64(binary.LittleEndian.Uint64(r))
}

func (w *wireClient) size() int64 {
	w.put([]byte{opSize})

	r := w.get(8)
	if r == nil {
		return -1
	}

	return int64(binary.LittleEndian.Uint64(r))
}

func (w *wireClient) close() { w.put([]byte{opClose}) }

// serveOver runs serveConn against fs on one end of a net.Pipe and returns the
// driver-side wire client plus a channel carrying serveConn's error.
func serveOver(fs afero.Fs) (*wireClient, <-chan error) {
	host, drv := net.Pipe()
	done := make(chan error, 1)

	go func() { done <- serveConn(host, fs) }()

	return &wireClient{c: drv}, done
}

func TestServeConn_readsAFile(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.mp4", []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, done := serveOver(fs)

	w.version()

	if st := w.open("in.mp4", modeRead); st != statusOK {
		t.Fatalf("open status = %d, want ok", st)
	}

	if got := string(w.read(1024)); got != "hello world" {
		t.Fatalf("read = %q, want %q", got, "hello world")
	}

	if sz := w.size(); sz != 11 {
		t.Fatalf("size = %d, want 11", sz)
	}

	w.close()
	_ = w.c.Close()

	if err := <-done; err != nil {
		t.Fatalf("serveConn: %v", err)
	}

	if w.err != nil {
		t.Fatalf("wire client: %v", w.err)
	}
}

// TestServeConn_writeThenSeekBackOverwrites is the load-bearing one: it proves the
// backward-seek overwrite the non-fragmented MP4 moov/mdat patch needs (spec 0028
// §5.2) works against afero — the exact case the HTTP-PUT bridge could not do.
func TestServeConn_writeThenSeekBackOverwrites(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	w, done := serveOver(fs)

	w.version()

	if st := w.open("out.mp4", modeWrite); st != statusOK {
		t.Fatalf("open status = %d, want ok", st)
	}

	if n := w.write([]byte("AAAAA")); n != 5 {
		t.Fatalf("write n = %d, want 5", n)
	}

	if off := w.seek(0, 0); off != 0 { // seek back to the start
		t.Fatalf("seek offset = %d, want 0", off)
	}

	w.write([]byte("BB")) // overwrite the first two bytes
	w.close()
	_ = w.c.Close()

	if err := <-done; err != nil {
		t.Fatalf("serveConn: %v", err)
	}

	out, err := afero.ReadFile(fs, "out.mp4")
	if err != nil {
		t.Fatal(err)
	}

	if string(out) != "BBAAA" {
		t.Fatalf("out = %q, want %q (seek-back overwrite)", out, "BBAAA")
	}
}

func TestServeConn_rejectsBadVersion(t *testing.T) {
	t.Parallel()

	w, done := serveOver(afero.NewMemMapFs())
	w.put([]byte{99}) // wrong protocol version
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want an error on a bad protocol version")
	}
}

func TestServeConn_openMissingReadFails(t *testing.T) {
	t.Parallel()

	w, done := serveOver(afero.NewMemMapFs())

	w.version()

	if st := w.open("nope.mp4", modeRead); st != statusErr {
		t.Fatalf("open status = %d, want err", st)
	}

	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want an error opening a missing file for read")
	}
}

func TestServeConn_rejectsBadMode(t *testing.T) {
	t.Parallel()

	w, done := serveOver(afero.NewMemMapFs())

	w.version()

	if st := w.open("x", 'q'); st != statusErr { // 'q' is neither read nor write
		t.Fatalf("open status = %d, want err", st)
	}

	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want an error on a bad open mode")
	}
}

func TestServeConn_noVersionByte(t *testing.T) {
	t.Parallel()

	w, done := serveOver(afero.NewMemMapFs())
	_ = w.c.Close() // drop the connection before announcing a version

	if err := <-done; err == nil {
		t.Fatal("want a version-read error")
	}
}

func TestServeConn_expectsOpenFirst(t *testing.T) {
	t.Parallel()

	w, done := serveOver(afero.NewMemMapFs())
	w.version()

	bad := make([]byte, 6)
	bad[0] = opRead // a 6-byte frame that is not an Open
	w.put(bad)
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want an expected-open error")
	}
}

// openedRead sets up a read session on a one-byte "in.mp4" and returns the wire
// client (post-open) plus serveConn's error channel, for the malformed-frame tests.
func openedRead(t *testing.T) (*wireClient, <-chan error) {
	t.Helper()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.mp4", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, done := serveOver(fs)
	w.version()
	w.open("in.mp4", modeRead)

	return w, done
}

func TestServeConn_cleanEOFWithoutClose(t *testing.T) {
	t.Parallel()

	w, done := openedRead(t)
	_ = w.c.Close() // driver drops the connection without a Close op

	if err := <-done; err != nil {
		t.Fatalf("a clean connection EOF should not be an error: %v", err)
	}
}

func TestServeConn_unknownOp(t *testing.T) {
	t.Parallel()

	w, done := openedRead(t)
	w.put([]byte{'X'}) // not a known opcode
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want an unknown-op error")
	}
}

func TestServeConn_truncatedReadFrame(t *testing.T) {
	t.Parallel()

	w, done := openedRead(t)
	w.put([]byte{opRead}) // opcode with no size following
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want a truncated-frame error")
	}
}

func TestServeConn_readSizeCapExceeded(t *testing.T) {
	t.Parallel()

	w, done := openedRead(t)

	frame := make([]byte, 5)
	frame[0] = opRead
	binary.LittleEndian.PutUint32(frame[1:], maxFrameBytes+1)
	w.put(frame)
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want a frame-size-cap error")
	}
}

func TestServeConn_openNameTooLong(t *testing.T) {
	t.Parallel()

	w, done := serveOver(afero.NewMemMapFs())
	w.version()

	hdr := make([]byte, 6)
	hdr[0] = opOpen
	hdr[1] = modeRead
	binary.LittleEndian.PutUint32(hdr[2:], maxFrameBytes+1)
	w.put(hdr)
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want an open-name-length cap error")
	}
}

func TestServeConn_truncatedWriteData(t *testing.T) {
	t.Parallel()

	w, done := serveOver(afero.NewMemMapFs())
	w.version()
	w.open("out.mp4", modeWrite)

	frame := make([]byte, 5)
	frame[0] = opWrite
	binary.LittleEndian.PutUint32(frame[1:], 10) // claims 10 bytes...
	w.put(frame)
	w.put([]byte("XY")) // ...but sends 2
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want a truncated write-data error")
	}
}

func TestServeConn_truncatedSeekFrame(t *testing.T) {
	t.Parallel()

	w, done := serveOver(afero.NewMemMapFs())
	w.version()
	w.open("out.mp4", modeWrite)
	w.put([]byte{opSeek, 0x01}) // opcode + 1 byte, short of the 9-byte body
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want a truncated seek-frame error")
	}
}

func TestServeConn_truncatedOpenName(t *testing.T) {
	t.Parallel()

	w, done := serveOver(afero.NewMemMapFs())
	w.version()

	hdr := make([]byte, 6)
	hdr[0] = opOpen
	hdr[1] = modeRead
	binary.LittleEndian.PutUint32(hdr[2:], 5) // claims a 5-byte name...
	w.put(hdr)
	w.put([]byte("ab")) // ...but sends 2
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want a truncated open-name error")
	}
}

// The following four close the connection after sending a valid request but before
// reading the reply, so the host's reply Write fails — covering the mid-reply error
// branches (writeU32 / writeU64 on a dead connection).

func TestServeConn_sizeReplyWriteFails(t *testing.T) {
	t.Parallel()

	w, done := openedRead(t)
	w.put([]byte{opSize})
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want a size reply-write error")
	}
}

func TestServeConn_readReplyWriteFails(t *testing.T) {
	t.Parallel()

	w, done := openedRead(t)

	frame := make([]byte, 5)
	frame[0] = opRead
	binary.LittleEndian.PutUint32(frame[1:], 1)
	w.put(frame)
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want a read reply-write error")
	}
}

func TestServeConn_seekReplyWriteFails(t *testing.T) {
	t.Parallel()

	w, done := openedRead(t)

	frame := make([]byte, 10)
	frame[0] = opSeek // offset 0, whence 0
	w.put(frame)
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want a seek reply-write error")
	}
}

func TestServeConn_writeReplyFails(t *testing.T) {
	t.Parallel()

	w, done := serveOver(afero.NewMemMapFs())
	w.version()
	w.open("out.mp4", modeWrite)

	frame := make([]byte, 5)
	frame[0] = opWrite
	binary.LittleEndian.PutUint32(frame[1:], 2)
	w.put(frame)
	w.put([]byte("hi"))
	_ = w.c.Close()

	if err := <-done; err == nil {
		t.Fatal("want a write reply-write error")
	}
}
