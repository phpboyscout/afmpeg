// Package native runs media jobs through a native ffmpeg-wasi driver subprocess
// (spec 0028 Backend B) instead of the sandboxed wasm module — the opt-in escape
// hatch for hardware acceleration and native-speed encode. It serves the caller's
// afero.Fs to the driver over a custom seekable IPC channel, so all media I/O
// stays in memory (nothing touches host disk) exactly as the wasm backend does.
//
// It implements afmpeg.Backend; wire it in with:
//
//	rt, err := afmpeg.New(ctx, afmpeg.WithBackend(native.New(native.WithNativeBinary(path))))
//
// The wasm backend remains afmpeg's default (spec 0028 D-0028-C); importing this
// package is the deliberate opt-in that lets a Runtime spawn a subprocess. During
// development it lives here as a subpackage; it promotes to its own module at the
// first native release, when the exported afmpeg.Backend seam makes the move
// mechanical.
package native

// The framed IPC protocol between this host and the native driver. One connection
// serves one opened file: the driver dials, announces the protocol version, sends
// an Open frame, then issues Read/Write/Seek/Size/Close against that file, which
// the host services against the caller's afero.Fs. The custom seekable AVIO the
// driver installs turns these callbacks into this wire traffic.
//
// Wire format — a 1-byte version preamble, then framed ops (all integers
// little-endian), matching the 0028 AVIO bridge spike:
//
//	preamble → version(1)
//	Open     → 'O', mode(1: 'r'|'w'), nameLen(u32), name[nameLen]  ; ← status(1: 0 ok, non-zero err)
//	Read     → 'R', size(u32)                                      ; ← n(u32), data[n]  (n==0 → EOF)
//	Write    → 'W', size(u32), data[size]                          ; ← n(u32)
//	Seek     → 'S', offset(u64), whence(1: 0 set|1 cur|2 end)      ; ← newOffset(u64)
//	Size     → 'Z'                                                 ; ← size(u64)
//	Close    → 'C'                                                 ; (no reply)
const (
	// protocolVersion is the framed-IPC contract version. The host and the native
	// driver are version-locked via the paired afmpeg/ffmpeg-wasi release; a
	// connection announcing a different version is rejected.
	protocolVersion = 1

	opOpen  = 'O'
	opRead  = 'R'
	opWrite = 'W'
	opSeek  = 'S'
	opSize  = 'Z'
	opClose = 'C'

	modeRead  = 'r'
	modeWrite = 'w'

	statusOK  = 0
	statusErr = 1

	// maxFrameBytes caps a single Read/Write frame so a buggy or hostile driver
	// cannot make the host allocate unboundedly. AVIO buffers are KBs; 64 MiB is
	// generous headroom.
	maxFrameBytes = 64 << 20
)
