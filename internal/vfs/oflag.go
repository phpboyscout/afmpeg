package vfs

import (
	"os"

	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
)

// accessModeMask isolates the access-mode bits of an Oflag. O_RDONLY, O_RDWR
// and O_WRONLY occupy the low bits as distinct values; every other flag is
// bit-packed above them.
const accessModeMask = experimentalsys.O_RDWR | experimentalsys.O_WRONLY

// oflagToOS pairs each wazero Oflag bit afmpeg honours with its os-package
// equivalent. Flags without an afero meaning (O_DIRECTORY, O_NOFOLLOW,
// O_NONBLOCK, O_DSYNC, O_RSYNC) are intentionally absent and dropped.
var oflagToOS = []struct {
	guest experimentalsys.Oflag
	host  int
}{
	{experimentalsys.O_APPEND, os.O_APPEND},
	{experimentalsys.O_CREAT, os.O_CREATE},
	{experimentalsys.O_EXCL, os.O_EXCL},
	{experimentalsys.O_TRUNC, os.O_TRUNC},
	{experimentalsys.O_SYNC, os.O_SYNC},
}

// toOSFlag translates a wazero Oflag into the int flag set afero.Fs.OpenFile
// expects (the os.O_* values).
func toOSFlag(flag experimentalsys.Oflag) int {
	var osFlag int

	switch flag & accessModeMask {
	case experimentalsys.O_RDWR:
		osFlag = os.O_RDWR
	case experimentalsys.O_WRONLY:
		osFlag = os.O_WRONLY
	default:
		osFlag = os.O_RDONLY
	}

	for _, m := range oflagToOS {
		if flag&m.guest != 0 {
			osFlag |= m.host
		}
	}

	return osFlag
}
