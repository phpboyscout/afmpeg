package vfs

import (
	"io/fs"
	"path"
	"strings"

	"github.com/spf13/afero"
	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
	wsys "github.com/tetratelabs/wazero/sys"
)

const (
	// singleLink is the hard-link count reported for every node; afero backends
	// do not track links, so afmpeg presents the POSIX minimum of one.
	singleLink = 1

	// devNull is the guest path discarded by the null device.
	devNull = "/dev/null"

	// devURandom and devRandom are the guest paths served by the random device —
	// the host entropy source libav needs to seed formats like Matroska (see
	// randFile). Both map to the same non-blocking crypto/rand source.
	devURandom = "/dev/urandom"
	devRandom  = "/dev/random"

	// defaultTmpPrefix is the guest directory routed to the scratch fs.
	defaultTmpPrefix = "/tmp"
)

// Compile-time guarantee that the adapters satisfy the wazero contracts.
var (
	_ experimentalsys.FS   = (*FS)(nil)
	_ experimentalsys.File = (*file)(nil)
	_ experimentalsys.File = (*nullFile)(nil)
	_ experimentalsys.File = (*progressFile)(nil)
)

// FS adapts an afero.Fs to experimental/sys.FS. Construct it with New; the zero
// value is not usable. Embedding UnimplementedFS keeps the implementation
// forward-compatible: operations afmpeg does not provide return ENOSYS.
type FS struct {
	experimentalsys.UnimplementedFS

	root      afero.Fs
	tmp       afero.Fs
	tmpPrefix string

	// progressSink, when non-nil, makes /dev/afmpeg-progress a live write-device
	// that streams the engine's NDJSON progress records to it (spec 0032). It is
	// set only when the caller attached progress reporting (WithProgress → the
	// wasm backend threads it here); otherwise the device is not served and the
	// path resolves against the backing fs like any other (D-B3).
	progressSink func([]byte)
}

// Option configures an FS.
type Option func(*FS)

// WithTmpFs overrides the in-memory filesystem backing the overlaid /tmp. The
// default is a fresh afero.NewMemMapFs; supplying one lets a caller (or a test)
// inspect what the guest wrote to scratch.
func WithTmpFs(tmp afero.Fs) Option {
	return func(f *FS) {
		f.tmp = tmp
	}
}

// WithProgressSink serves /dev/afmpeg-progress as a write-only device that hands
// each complete NDJSON line the engine writes to it to sink (spec 0032, D-B1).
// Absent this option the device is not overlaid, so the path behaves like any
// other backing-fs path (D-B3). A nil sink is a no-op.
func WithProgressSink(sink func([]byte)) Option {
	return func(f *FS) {
		f.progressSink = sink
	}
}

// New returns a sys.FS backed by afs. Every guest read and write resolves
// against afs, so handing it an afero.NewMemMapFs keeps the whole pipeline in
// memory with no host filesystem access (R-AF-2). /tmp and /dev/null are
// overlaid automatically.
func New(afs afero.Fs, opts ...Option) *FS {
	f := &FS{
		root:      afs,
		tmp:       afero.NewMemMapFs(),
		tmpPrefix: defaultTmpPrefix,
	}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

// backendFor returns the afero.Fs a guest path resolves against: the isolated
// scratch fs for paths under /tmp, otherwise the caller's filesystem.
func (f *FS) backendFor(name string) afero.Fs {
	clean := normalize(name)
	if clean == f.tmpPrefix || strings.HasPrefix(clean, f.tmpPrefix+"/") {
		return f.tmp
	}

	return f.root
}

// OpenFile opens path relative to the resolved backend, or returns the null
// device for /dev/null.
func (f *FS) OpenFile(
	name string, flag experimentalsys.Oflag, perm fs.FileMode,
) (experimentalsys.File, experimentalsys.Errno) {
	if isDevNull(name) {
		return newNullFile(), 0
	}

	if isDevRandom(name) {
		return newRandFile(), 0
	}

	if f.progressSink != nil && isDevProgress(name) {
		return newProgressFile(f.progressSink), 0
	}

	af, err := f.backendFor(name).OpenFile(name, toOSFlag(flag), perm)
	if err != nil {
		return nil, experimentalsys.UnwrapOSError(err)
	}

	return newFile(af), 0
}

// Stat returns file status for path.
func (f *FS) Stat(name string) (wsys.Stat_t, experimentalsys.Errno) {
	if isDevNull(name) {
		return nullStat(), 0
	}

	if isDevRandom(name) {
		return randStat(), 0
	}

	if f.progressSink != nil && isDevProgress(name) {
		return progressStat(), 0
	}

	info, err := f.backendFor(name).Stat(name)
	if err != nil {
		return wsys.Stat_t{}, experimentalsys.UnwrapOSError(err)
	}

	return statFromInfo(info), 0
}

// Lstat returns file status for path without following symlinks. The afero
// backends afmpeg targets (MemMapFs in particular) do not model symlinks, so
// this is equivalent to Stat.
func (f *FS) Lstat(name string) (wsys.Stat_t, experimentalsys.Errno) {
	return f.Stat(name)
}

// Mkdir creates a directory at path.
func (f *FS) Mkdir(name string, perm fs.FileMode) experimentalsys.Errno {
	return experimentalsys.UnwrapOSError(f.backendFor(name).Mkdir(name, perm))
}

// Rename moves a file or directory from one path to another within the same
// backend.
func (f *FS) Rename(from, to string) experimentalsys.Errno {
	return experimentalsys.UnwrapOSError(f.backendFor(from).Rename(from, to))
}

// Unlink removes a non-directory file; POSIX unlink rejects directories.
func (f *FS) Unlink(name string) experimentalsys.Errno {
	backend := f.backendFor(name)

	info, err := backend.Stat(name)
	if err != nil {
		return experimentalsys.UnwrapOSError(err)
	}

	if info.IsDir() {
		return experimentalsys.EISDIR
	}

	return experimentalsys.UnwrapOSError(backend.Remove(name))
}

// Rmdir removes an empty directory; POSIX rmdir rejects non-directories.
func (f *FS) Rmdir(name string) experimentalsys.Errno {
	backend := f.backendFor(name)

	info, err := backend.Stat(name)
	if err != nil {
		return experimentalsys.UnwrapOSError(err)
	}

	if !info.IsDir() {
		return experimentalsys.ENOTDIR
	}

	return experimentalsys.UnwrapOSError(backend.Remove(name))
}

// normalize collapses a guest path to a clean absolute form so /tmp routing and
// the /dev/null match are insensitive to a leading slash or "." / ".." noise.
func normalize(name string) string {
	return path.Clean("/" + name)
}

// isDevNull reports whether name addresses the overlaid null device.
func isDevNull(name string) bool {
	return normalize(name) == devNull
}

// isDevRandom reports whether name addresses an overlaid random device.
func isDevRandom(name string) bool {
	clean := normalize(name)

	return clean == devURandom || clean == devRandom
}

// statFromInfo builds a wazero Stat_t from an fs.FileInfo. afero backends do
// not expose a serial number or distinct access/change times, so Ino is left
// zero and all three timestamps mirror the modification time (the documented
// behaviour for fs.FileInfo-backed implementations).
func statFromInfo(info fs.FileInfo) wsys.Stat_t {
	mtim := info.ModTime().UnixNano()

	return wsys.Stat_t{
		Mode:  info.Mode(),
		Size:  info.Size(),
		Nlink: singleLink,
		Atim:  mtim,
		Mtim:  mtim,
		Ctim:  mtim,
	}
}
