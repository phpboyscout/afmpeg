package native

import (
	"encoding/binary"
	"io"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
)

// serveConn services one file session on conn against fs: it validates the
// protocol version, reads the Open frame, opens the afero file, then serves
// Read/Write/Seek/Size/Close until the driver closes the file or the connection.
// One connection == one open file (the driver dials once per file its custom AVIO
// opens).
func serveConn(conn io.ReadWriteCloser, fs afero.Fs) (err error) {
	defer func() {
		if cerr := conn.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	var ver [1]byte
	if _, e := io.ReadFull(conn, ver[:]); e != nil {
		return errors.Wrap(e, "native: read protocol version")
	}

	if ver[0] != protocolVersion {
		return errors.Newf("native: unsupported protocol version %d (want %d)", ver[0], protocolVersion)
	}

	name, mode, err := readOpen(conn)
	if err != nil {
		return err
	}

	file, err := openFile(fs, name, mode)
	if err != nil {
		_, _ = conn.Write([]byte{statusErr})

		return errors.Wrapf(err, "native: open %q", name)
	}

	defer func() { _ = file.Close() }()

	if _, e := conn.Write([]byte{statusOK}); e != nil {
		return errors.Wrap(e, "native: write open status")
	}

	return serveFile(conn, file)
}

// readOpen reads the Open frame: 'O', mode, nameLen(u32), name.
func readOpen(conn io.Reader) (name string, mode byte, err error) {
	hdr := make([]byte, 6) //nolint:mnd // 'O' + mode + u32 nameLen
	if _, e := io.ReadFull(conn, hdr); e != nil {
		return "", 0, errors.Wrap(e, "native: read open header")
	}

	if hdr[0] != opOpen {
		return "", 0, errors.Newf("native: expected open op, got %q", hdr[0])
	}

	n := binary.LittleEndian.Uint32(hdr[2:])
	if n > maxFrameBytes {
		return "", 0, errors.Newf("native: open name length %d exceeds cap", n)
	}

	nameBuf := make([]byte, n)
	if _, e := io.ReadFull(conn, nameBuf); e != nil {
		return "", 0, errors.Wrap(e, "native: read open name")
	}

	return string(nameBuf), hdr[1], nil
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
var fileOps = map[byte]func(io.ReadWriter, afero.File) error{
	opRead:  serveRead,
	opWrite: serveWrite,
	opSeek:  serveSeek,
	opSize:  serveSize,
}

// serveFile runs the per-file op loop until Close or a clean connection EOF.
func serveFile(conn io.ReadWriter, file afero.File) error {
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

		if err := handler(conn, file); err != nil {
			return err
		}
	}
}

func serveRead(conn io.ReadWriter, file afero.File) error {
	n, err := readCount(conn)
	if err != nil {
		return err
	}

	buf := make([]byte, n)

	rn, rerr := file.Read(buf)
	if rerr != nil && !errors.Is(rerr, io.EOF) {
		return errors.Wrap(rerr, "native: read file")
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
