package native

import (
	"encoding/binary"
	"io"
	"os"

	"github.com/spf13/afero"
	"gitlab.com/phpboyscout/go/errors"
)

// serveConn services one session on conn against fs: it negotiates the protocol
// version, then dispatches the one operation the connection carries.
//
// One connection == one operation. For Open that means one file, served until the
// driver closes it (the driver dials once per file its custom AVIO opens); Move
// and Exists answer and end.
func serveConn(conn io.ReadWriteCloser, fs afero.Fs) (err error) {
	defer func() {
		if cerr := conn.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	agreed, err := negotiate(conn)
	if err != nil {
		return err
	}

	var tag [1]byte
	if _, e := io.ReadFull(conn, tag[:]); e != nil {
		return errors.Wrap(e, "native: read session opcode")
	}

	return dispatchSession(conn, fs, tag[0], agreed)
}

// negotiate reads the driver's version preamble and answers it.
//
// The driver announces the highest version it speaks; the host replies with the
// version it will actually use, so a driver built against a newer contract can
// degrade rather than guess (spec 0041 D1). A v1 driver expects no answer and
// would read one as its Open status, so the reply goes only from v2 up — which is
// what makes the negotiation additive rather than a break.
func negotiate(conn io.ReadWriter) (byte, error) {
	var ver [1]byte
	if _, err := io.ReadFull(conn, ver[:]); err != nil {
		return 0, errors.Wrap(err, "native: read protocol version")
	}

	if ver[0] < protocolVersionMin || ver[0] > protocolVersion {
		return 0, errors.Newf("native: unsupported protocol version %d (want %d..%d)",
			ver[0], protocolVersionMin, protocolVersion)
	}

	if ver[0] >= protocolVersionNegotiated {
		if _, err := conn.Write(ver[:]); err != nil {
			return 0, errors.Wrap(err, "native: write negotiated version")
		}
	}

	return ver[0], nil
}

// dispatchSession routes the one operation this connection carries. Move and
// Exists arrived with v2, so a v1 session that sends them is not speaking the
// contract it announced.
func dispatchSession(conn io.ReadWriter, fs afero.Fs, tag, agreed byte) error {
	switch tag {
	case opOpen:
		return serveOpenSession(conn, fs, agreed)
	case opMove, opExists:
		if agreed < protocolVersionNegotiated {
			return errors.Newf("native: %q needs protocol v%d, this session agreed v%d",
				tag, protocolVersionNegotiated, agreed)
		}

		if tag == opMove {
			return serveMove(conn, fs)
		}

		return serveExists(conn, fs)
	default:
		return errors.Newf("native: expected Open, Move or Exists, got %q", tag)
	}
}

// serveOpenSession opens the named file and serves frames against it until the
// driver closes it.
func serveOpenSession(conn io.ReadWriter, fs afero.Fs, agreed byte) error {
	name, mode, err := readOpen(conn)
	if err != nil {
		return err
	}

	file, err := openFile(fs, name, mode)
	if err != nil {
		// Always reply, including on failure: a driver waiting for a status byte
		// that never comes would hang rather than report.
		_, _ = conn.Write([]byte{statusErr})

		return errors.Wrapf(err, "native: open %q", name)
	}

	defer func() { _ = file.Close() }()

	if _, e := conn.Write([]byte{statusOK}); e != nil {
		return errors.Wrap(e, "native: write open status")
	}

	return serveFile(conn, file, agreed)
}

// readOpen reads the rest of an Open frame: mode, nameLen(u32), name. The 'O'
// tag has already been consumed by serveConn, which needs it to tell Open from
// the session-level Move and Exists.
func readOpen(conn io.Reader) (name string, mode byte, err error) {
	hdr := make([]byte, 5) //nolint:mnd // mode + u32 nameLen
	if _, e := io.ReadFull(conn, hdr); e != nil {
		return "", 0, errors.Wrap(e, "native: read open header")
	}

	n := binary.LittleEndian.Uint32(hdr[1:])
	if n > maxFrameBytes {
		return "", 0, errors.Newf("native: open name length %d exceeds cap", n)
	}

	nameBuf := make([]byte, n)
	if _, e := io.ReadFull(conn, nameBuf); e != nil {
		return "", 0, errors.Wrap(e, "native: read open name")
	}

	return string(nameBuf), hdr[0], nil
}

// readName reads a length-prefixed name: nameLen(u32), name[nameLen].
func readName(conn io.Reader, what string) (string, error) {
	n, err := readCount(conn)
	if err != nil {
		return "", err
	}

	buf := make([]byte, n)
	if _, e := io.ReadFull(conn, buf); e != nil {
		return "", errors.Wrapf(e, "native: read %s name", what)
	}

	return string(buf), nil
}

// serveMove renames from to to, so a muxer can replace a file atomically.
//
// HLS writes stream.m3u8.tmp and renames, so anything reading the playlist
// concurrently sees a whole file or the previous one. Copy-then-delete would
// satisfy the file layout and lose the property the muxer wanted, which is why
// this is a frame rather than something the engine emulates (spec 0041 D3).
//
// A host whose filesystem cannot rename atomically answers with a failure status
// and the job fails by name — never a silent fallback to the weaker guarantee.
func serveMove(conn io.ReadWriter, fs afero.Fs) error {
	from, err := readName(conn, "move source")
	if err != nil {
		return err
	}

	to, err := readName(conn, "move target")
	if err != nil {
		return err
	}

	if rerr := fs.Rename(from, to); rerr != nil {
		if _, e := conn.Write([]byte{statusErr}); e != nil {
			return errors.Wrap(e, "native: write move status")
		}

		return errors.Wrapf(rerr, "native: move %q to %q", from, to)
	}

	if _, e := conn.Write([]byte{statusOK}); e != nil {
		return errors.Wrap(e, "native: write move status")
	}

	return nil
}

// serveExists answers whether one exact name is present, and how big it is.
//
// Narrow deliberately: not a directory listing. An afero.Fs over object storage
// may have no cheap listing at all, and the surface this serves is one demuxer —
// avio_check has three call sites in libavformat and libavfilter and all three
// are in img2dec.c. What it buys is that a probe and an open resolve against the
// SAME filesystem, which is exactly what #36 was (spec 0041 D4).
//
// A missing name is an ordinary answer, not a host fault: the engine probes for
// files that may not exist.
func serveExists(conn io.ReadWriter, fs afero.Fs) error {
	name, err := readName(conn, "exists")
	if err != nil {
		return err
	}

	reply := make([]byte, 9) //nolint:mnd // status + u64 size

	info, serr := fs.Stat(name)
	switch {
	case serr != nil:
		reply[0] = statusErr
	default:
		reply[0] = statusOK

		size := info.Size()
		if size < 0 {
			size = 0
		}

		binary.LittleEndian.PutUint64(reply[1:], uint64(size))
	}

	if _, e := conn.Write(reply); e != nil {
		return errors.Wrap(e, "native: write exists reply")
	}

	return nil
}

// openFile maps an Open mode to an afero file. Write mode is O_RDWR (not
// append/write-only) so the muxer's backward seeks — e.g. the non-fragmented MP4
// moov/mdat patch on av_write_trailer — can overwrite earlier bytes (spec 0028 §5.2).
func openFile(fs afero.Fs, name string, mode byte) (afero.File, error) {
	switch mode {
	case modeRead:
		return fs.Open(name) //nolint:wrapcheck // wrapped by the caller with the name
	case modeWrite:
		return fs.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644) //nolint:wrapcheck,mnd
	default:
		return nil, errors.Newf("native: bad open mode %q", mode)
	}
}

// fileOps dispatches a per-file opcode to its handler. Close and a clean EOF are
// handled inline in serveFile (they end the loop rather than serve a frame).
//
// Read takes the agreed version because its reply shape depends on it: from v2 it
// can report a failure, and under v1 it cannot.
var fileOps = map[byte]func(io.ReadWriter, afero.File, byte) error{
	opRead:  serveRead,
	opWrite: func(c io.ReadWriter, f afero.File, _ byte) error { return serveWrite(c, f) },
	opSeek:  func(c io.ReadWriter, f afero.File, _ byte) error { return serveSeek(c, f) },
	opSize:  func(c io.ReadWriter, f afero.File, _ byte) error { return serveSize(c, f) },
}

// serveFile runs the per-file op loop until Close or a clean connection EOF.
func serveFile(conn io.ReadWriter, file afero.File, agreed byte) error {
	op := make([]byte, 1)

	for {
		if _, e := io.ReadFull(conn, op); e != nil {
			if errors.Is(e, io.EOF) {
				return nil // driver closed the connection — a clean end
			}

			return errors.Wrap(e, "native: read op")
		}

		if op[0] == opClose {
			return nil
		}

		handler, ok := fileOps[op[0]]
		if !ok {
			return errors.Newf("native: unknown op %q", op[0])
		}

		if err := handler(conn, file, agreed); err != nil {
			return err
		}
	}
}

// serveRead answers a Read frame. From v2 a read that fails is REPORTED rather
// than dropped: the count is signed, and a negative value tells the driver the
// host could not serve the read (spec 0041 D2).
//
// Under v1 there is no way to say it. The reply is a count where zero means end
// of file, so a host that cannot serve a read can only lie — answer zero, and the
// driver treats a failure as a clean end of stream — or hang. Dropping the
// connection, which is what this did, is the least-bad v1 answer and stays the v1
// answer.
func serveRead(conn io.ReadWriter, file afero.File, agreed byte) error {
	n, err := readCount(conn)
	if err != nil {
		return err
	}

	buf := make([]byte, n)

	rn, rerr := file.Read(buf)
	if rerr != nil && !errors.Is(rerr, io.EOF) {
		if agreed < protocolVersionNegotiated {
			return errors.Wrap(rerr, "native: read file")
		}

		if e := writeI32(conn, readFailed); e != nil {
			return e
		}

		// The session survives: the driver now knows this read failed and can fail
		// the job with a real error instead of a truncated output and exit 0, which
		// is what #20 was.
		return nil
	}

	if err := writeU32(conn, uint32(rn)); err != nil { //nolint:gosec // rn ∈ [0,n], n ≤ maxFrameBytes
		return err
	}

	if rn > 0 {
		if _, err := conn.Write(buf[:rn]); err != nil {
			return errors.Wrap(err, "native: write read data")
		}
	}

	return nil
}

func serveWrite(conn io.ReadWriter, file afero.File) error {
	n, err := readCount(conn)
	if err != nil {
		return err
	}

	buf := make([]byte, n)
	if _, e := io.ReadFull(conn, buf); e != nil {
		return errors.Wrap(e, "native: read write data")
	}

	wn, werr := file.Write(buf)
	if werr != nil {
		return errors.Wrap(werr, "native: write file")
	}

	return writeU32(conn, uint32(wn)) //nolint:gosec // wn ∈ [0,n]
}

func serveSeek(conn io.ReadWriter, file afero.File) error {
	var b [9]byte //nolint:mnd // u64 offset + 1 whence
	if _, e := io.ReadFull(conn, b[:]); e != nil {
		return errors.Wrap(e, "native: read seek")
	}

	off := int64(binary.LittleEndian.Uint64(b[:8])) //nolint:gosec // AVIO offsets are file positions

	newOff, serr := file.Seek(off, int(b[8]))
	if serr != nil {
		return errors.Wrap(serr, "native: seek file")
	}

	return writeU64(conn, uint64(newOff)) //nolint:gosec // a file position is non-negative
}

func serveSize(conn io.ReadWriter, file afero.File) error {
	st, err := file.Stat()
	if err != nil {
		return errors.Wrap(err, "native: stat file")
	}

	return writeU64(conn, uint64(st.Size())) //nolint:gosec // a file size is non-negative
}

// readCount reads a u32 frame length and enforces the cap.
func readCount(conn io.Reader) (uint32, error) {
	var b [4]byte
	if _, e := io.ReadFull(conn, b[:]); e != nil {
		return 0, errors.Wrap(e, "native: read count")
	}

	n := binary.LittleEndian.Uint32(b[:])
	if n > maxFrameBytes {
		return 0, errors.Newf("native: frame size %d exceeds cap %d", n, maxFrameBytes)
	}

	return n, nil
}

// writeI32 writes a signed frame count. Only the failure form is negative, and
// only from v2.
func writeI32(conn io.Writer, v int32) error {
	return writeU32(conn, uint32(v)) //nolint:gosec // the wire field is 32 bits either way
}

func writeU32(conn io.Writer, v uint32) error {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)

	_, err := conn.Write(b[:])

	return errors.Wrap(err, "native: write u32")
}

func writeU64(conn io.Writer, v uint64) error {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)

	_, err := conn.Write(b[:])

	return errors.Wrap(err, "native: write u64")
}
