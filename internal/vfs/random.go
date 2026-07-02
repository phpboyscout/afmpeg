package vfs

import (
	"crypto/rand"
	"io/fs"

	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
	wsys "github.com/tetratelabs/wazero/sys"
)

// randPerm is the permission bits reported for the random devices.
const randPerm = 0o444

// randFile implements /dev/urandom and /dev/random: reads return
// cryptographically-random bytes from the host (crypto/rand). The guest ffmpeg
// reads these for entropy — libavutil's av_get_random_seed opens /dev/urandom
// (its only compiled entropy source here), and *without it* falls back to
// get_generic_seed, a loop that harvests clock() jitter and never terminates
// under wasi, where clock() does not advance. That call itself hangs, so any
// format seeding a PRNG stalls in its muxer init before writing a byte — the
// Matroska muxer seeds track UIDs this way and never even reaches the loop that
// consumes them. Serving the device lets av_get_random_seed return at once,
// which is what lets afmpeg produce mkv/webm (and any random-seeded format).
type randFile struct {
	experimentalsys.UnimplementedFile
}

func newRandFile() *randFile {
	return &randFile{}
}

// Read fills buf with random bytes; a short read never happens (crypto/rand
// fills the whole slice or errors), matching /dev/urandom's contract.
func (*randFile) Read(buf []byte) (int, experimentalsys.Errno) {
	if _, err := rand.Read(buf); err != nil {
		return 0, experimentalsys.EIO
	}

	return len(buf), 0
}

// Pread reads random bytes ignoring the offset — the device has no position.
func (r *randFile) Pread(buf []byte, _ int64) (int, experimentalsys.Errno) {
	return r.Read(buf)
}

// Write discards buf (writing to the random device is a harmless no-op).
func (*randFile) Write(buf []byte) (int, experimentalsys.Errno) {
	return len(buf), 0
}

// Pwrite discards buf.
func (*randFile) Pwrite(buf []byte, _ int64) (int, experimentalsys.Errno) {
	return len(buf), 0
}

// Seek is a no-op; the device has no position.
func (*randFile) Seek(int64, int) (int64, experimentalsys.Errno) {
	return 0, 0
}

// Sync is a no-op.
func (*randFile) Sync() experimentalsys.Errno {
	return 0
}

// Datasync is a no-op.
func (*randFile) Datasync() experimentalsys.Errno {
	return 0
}

// IsDir reports that the random device is not a directory.
func (*randFile) IsDir() (bool, experimentalsys.Errno) {
	return false, 0
}

// Stat reports the random device's status.
func (*randFile) Stat() (wsys.Stat_t, experimentalsys.Errno) {
	return randStat(), 0
}

// Close is a no-op.
func (*randFile) Close() experimentalsys.Errno {
	return 0
}

// randStat is the status shared by the random devices' file and path stat.
func randStat() wsys.Stat_t {
	return wsys.Stat_t{
		Mode:  fs.ModeCharDevice | randPerm,
		Nlink: singleLink,
	}
}
