package native

import (
	"encoding/binary"
	"errors"
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

// version announces the highest version this client speaks and consumes the
// host's answer, returning the version actually agreed.
//
// From v2 the host replies with the version it will use, so a client that did not
// read it would take that byte as the reply to its next frame and desynchronise
// everything after. A v1 client expects no answer and gets none, which is what
// makes the negotiation additive.
func (w *wireClient) version() byte { return w.versionAs(protocolVersion) }

func (w *wireClient) versionAs(v byte) byte {
	w.put([]byte{v})

	if v < protocolVersionNegotiated {
		return v
	}

	b := w.get(1)
	if b == nil {
		return 0
	}

	return b[0]
}

// move sends a Move frame and returns the status byte.
func (w *wireClient) move(from, to string) byte {
	b := []byte{opMove}
	b = binary.LittleEndian.AppendUint32(b, uint32(len(from)))
	b = append(b, from...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(to)))
	b = append(b, to...)
	w.put(b)

	st := w.get(1)
	if st == nil {
		return statusErr
	}

	return st[0]
}

// exists sends an Exists frame and returns the status byte and the size.
func (w *wireClient) exists(name string) (byte, uint64) {
	b := []byte{opExists}
	b = binary.LittleEndian.AppendUint32(b, uint32(len(name)))
	b = append(b, name...)
	w.put(b)

	r := w.get(9)
	if r == nil {
		return statusErr, 0
	}

	return r[0], binary.LittleEndian.Uint64(r[1:])
}

// readRaw sends a Read frame and returns the raw signed count without consuming
// any payload, so a test can observe the failure form.
func (w *wireClient) readRaw(size uint32) int32 {
	b := make([]byte, 5)
	b[0] = opRead
	binary.LittleEndian.PutUint32(b[1:], size)
	w.put(b)

	cnt := w.get(4)
	if cnt == nil {
		return 0
	}

	return int32(binary.LittleEndian.Uint32(cnt))
}

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

// --- protocol v2 (afmpeg spec 0041) ---------------------------------------

// failingReadFs serves a file whose Read always fails, which afero has no
// built-in way to express. A host-side read failure is the whole subject of D2,
// and it cannot be provoked with MemMapFs alone.
type failingReadFs struct{ afero.Fs }

type failingReadFile struct{ afero.File }

func (failingReadFile) Read([]byte) (int, error) { return 0, errors.New("device on fire") }

func (f failingReadFs) Open(name string) (afero.File, error) {
	inner, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}

	return failingReadFile{inner}, nil
}

func TestServeConn_negotiatesTheVersionItWillSpeak(t *testing.T) {
	t.Parallel()

	w, done := serveOver(afero.NewMemMapFs())

	if got := w.version(); got != protocolVersion {
		t.Fatalf("negotiated version = %d, want %d", got, protocolVersion)
	}

	_ = w.c.Close()
	<-done
}

// A v1 driver predates the negotiation and expects no answer to its preamble.
// Serving it costs nothing and keeps a released driver working against a newer
// host, which is the direction compatibility has to hold: the host ships first
// (spec 0041 D5).
func TestServeConn_stillServesAVersionOneDriver(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.mp4", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, done := serveOver(fs)

	if got := w.versionAs(1); got != 1 {
		t.Fatalf("v1 preamble returned %d — a v1 driver must get no answer at all", got)
	}

	if st := w.open("in.mp4", modeRead); st != statusOK {
		t.Fatalf("open status = %d, want ok", st)
	}

	if got := string(w.read(64)); got != "hello" {
		t.Fatalf("read = %q, want %q", got, "hello")
	}

	w.close()
	_ = w.c.Close()

	if err := <-done; err != nil {
		t.Fatalf("serveConn: %v", err)
	}
}

// D3 — Move, so a muxer can replace a file atomically. HLS writes a .tmp and
// renames; copy-then-delete satisfies the layout and loses the property.
func TestServeConn_moveReplacesAtomically(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "stream.m3u8", []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := afero.WriteFile(fs, "stream.m3u8.tmp", []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, done := serveOver(fs)
	w.version()

	if st := w.move("stream.m3u8.tmp", "stream.m3u8"); st != statusOK {
		t.Fatalf("move status = %d, want ok", st)
	}

	_ = w.c.Close()
	<-done

	// The point of the frame is that the destination holds the new bytes and the
	// source is gone — a copy that left the .tmp behind would satisfy neither.
	got, err := afero.ReadFile(fs, "stream.m3u8")
	if err != nil || string(got) != "new" {
		t.Fatalf("after move, stream.m3u8 = %q (err %v), want %q", got, err, "new")
	}

	if _, err := fs.Stat("stream.m3u8.tmp"); err == nil {
		t.Error("the source still exists after a move — that is a copy, not a rename")
	}
}

// A host whose filesystem cannot rename says so, and the job fails by name. The
// rule this protects is that the weaker guarantee is never taken silently.
func TestServeConn_moveFailureIsReported(t *testing.T) {
	t.Parallel()

	w, done := serveOver(afero.NewMemMapFs())
	w.version()

	if st := w.move("no-such-file", "wherever"); st != statusErr {
		t.Fatalf("move of a missing file returned status %d, want an error", st)
	}

	_ = w.c.Close()
	<-done
}

// D4 — Exists, so a probe and an open resolve against the SAME filesystem.
func TestServeConn_existsAnswersForOneName(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "f_000.png", []byte("1234567890"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, done := serveOver(fs)
	w.version()

	st, size := w.exists("f_000.png")
	if st != statusOK || size != 10 {
		t.Fatalf("exists(present) = status %d size %d, want ok and 10", st, size)
	}

	_ = w.c.Close()
	<-done

	// A missing name is an ordinary answer, not a host fault: the engine probes
	// for files that may not exist, which is how image2 finds a sequence's end.
	w2, done2 := serveOver(fs)
	w2.version()

	if st, _ := w2.exists("f_999.png"); st != statusErr {
		t.Fatalf("exists(absent) = status %d, want the not-found answer", st)
	}

	_ = w2.c.Close()
	<-done2
}

// D2 — a Read reply can report a failure.
//
// Under v1 the reply was a count where zero meant end of file, so a host that
// could not serve a read could only lie or hang, and the fix for ffmpeg-wasi#20
// shipped with no regression test because the fault could not be delivered.
func TestServeConn_readFailureIsReportedNotSwallowed(t *testing.T) {
	t.Parallel()

	base := afero.NewMemMapFs()
	if err := afero.WriteFile(base, "in.mp4", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, done := serveOver(failingReadFs{base})
	w.version()

	if st := w.open("in.mp4", modeRead); st != statusOK {
		t.Fatalf("open status = %d, want ok", st)
	}

	// Zero would be indistinguishable from a clean end of file — which is exactly
	// the ambiguity that made #20 untestable.
	if n := w.readRaw(64); n >= 0 {
		t.Fatalf("read count = %d, want a negative failure form; 0 would read as EOF "+
			"and any positive value as data", n)
	}

	// The session survives the failure, so the driver can fail the job with a real
	// error rather than losing the connection and having to guess why.
	if sz := w.size(); sz != 5 {
		t.Errorf("after a reported read failure the session is unusable (size = %d, want 5)", sz)
	}

	w.close()
	_ = w.c.Close()

	if err := <-done; err != nil {
		t.Fatalf("serveConn: %v", err)
	}
}
