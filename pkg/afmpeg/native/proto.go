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
//	preamble → version(1)                                          ; ← agreed(1)   [v2+ only]
//	Open     → 'O', mode(1: 'r'|'w'), nameLen(u32), name[nameLen]  ; ← status(1: 0 ok, non-zero err)
//	Read     → 'R', size(u32)                                      ; ← n(i32), data[n]  (0 → EOF, <0 → failed)
//	Write    → 'W', size(u32), data[size]                          ; ← n(u32)
//	Seek     → 'S', offset(u64), whence(1: 0 set|1 cur|2 end)      ; ← newOffset(u64)
//	Size     → 'Z'                                                 ; ← size(u64)
//	Close    → 'C'                                                 ; (no reply)
//	Move     → 'M', fromLen(u32), from, toLen(u32), to             ; ← status(1)   [v2]
//	Exists   → 'E', nameLen(u32), name                             ; ← status(1), size(u64)  [v2]
//
// Move and Exists are session-level like Open: a connection carries one of the
// three, and Move/Exists end the session with their reply rather than opening a
// file. See afmpeg spec 0041.
const (
	// protocolVersion is the highest framed-IPC contract version this host speaks.
	//
	// v2 adds three things v1 could not express (spec 0041): a Read reply that can
	// report a FAILURE rather than only a count or EOF, a Move so a muxer can
	// replace a file atomically, and an Exists so a probe and an open resolve
	// against the same filesystem.
	protocolVersion = 2

	// protocolVersionMin is the oldest version still served. A v1 driver predates
	// the new frames and never sends them, so serving it costs nothing and keeps a
	// released driver working against a newer host — which is the direction
	// compatibility has to hold, since the host ships first (spec 0041 D5).
	protocolVersionMin = 1

	// protocolVersionNegotiated is the first version where the host answers the
	// preamble with the version it will speak. A v1 driver does not expect a reply
	// and would read it as its Open status, so the answer is sent only from here up.
	protocolVersionNegotiated = 2

	opOpen   = 'O'
	opRead   = 'R'
	opWrite  = 'W'
	opSeek   = 'S'
	opSize   = 'Z'
	opClose  = 'C'
	opMove   = 'M'
	opExists = 'E'

	// readFailed is the Read reply that says the host could not serve the read.
	//
	// Under v1 a Read reply was a count where zero meant end of file, with no way
	// to say "I failed" — so a host that could not serve a read could only lie or
	// hang, and the fix for #20 shipped with no regression test because the fault
	// could not be delivered. The count is signed from v2 (spec 0041 D2).
	//
	// A v1 driver would read this as 0xFFFFFFFF and refuse it against its frame
	// cap, so it fails safe rather than reading wild memory — but it is only ever
	// sent once v2 is agreed.
	readFailed int32 = -1

	modeRead  = 'r'
	modeWrite = 'w'

	statusOK  = 0
	statusErr = 1

	// maxFrameBytes caps a single Read/Write frame so a buggy or hostile driver
	// cannot make the host allocate unboundedly. AVIO buffers are KBs; 64 MiB is
	// generous headroom.
	maxFrameBytes = 64 << 20
)
