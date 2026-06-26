package vfs

import (
	"io/fs"

	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
	wsys "github.com/tetratelabs/wazero/sys"
)

// nullPerm is the permission bits reported for /dev/null.
const nullPerm = 0o666

// nullFile implements /dev/null: writes are discarded and reads report EOF. The
// guest ffmpeg opens /dev/null as a sink for output streams it does not need.
type nullFile struct {
	experimentalsys.UnimplementedFile
}

func newNullFile() *nullFile {
	return &nullFile{}
}

// Read always reports EOF.
func (*nullFile) Read([]byte) (int, experimentalsys.Errno) {
	return 0, 0
}

// Pread always reports EOF.
func (*nullFile) Pread([]byte, int64) (int, experimentalsys.Errno) {
	return 0, 0
}

// Write discards buf and reports it fully written.
func (*nullFile) Write(buf []byte) (int, experimentalsys.Errno) {
	return len(buf), 0
}

// Pwrite discards buf and reports it fully written.
func (*nullFile) Pwrite(buf []byte, _ int64) (int, experimentalsys.Errno) {
	return len(buf), 0
}

// Seek is a no-op that keeps the offset at zero.
func (*nullFile) Seek(int64, int) (int64, experimentalsys.Errno) {
	return 0, 0
}

// Truncate is a no-op.
func (*nullFile) Truncate(int64) experimentalsys.Errno {
	return 0
}

// Sync is a no-op.
func (*nullFile) Sync() experimentalsys.Errno {
	return 0
}

// Datasync is a no-op.
func (*nullFile) Datasync() experimentalsys.Errno {
	return 0
}

// IsDir reports that the null device is not a directory.
func (*nullFile) IsDir() (bool, experimentalsys.Errno) {
	return false, 0
}

// Stat reports the null device's status.
func (*nullFile) Stat() (wsys.Stat_t, experimentalsys.Errno) {
	return nullStat(), 0
}

// Close is a no-op.
func (*nullFile) Close() experimentalsys.Errno {
	return 0
}

// nullStat is the status shared by the null device's file and path stat.
func nullStat() wsys.Stat_t {
	return wsys.Stat_t{
		Mode:  fs.ModeCharDevice | nullPerm,
		Nlink: singleLink,
	}
}
