// Package afmpeg is a pure-Go FFmpeg binding whose filesystem I/O runs on an
// afero.Fs — including a fully in-memory filesystem — with no CGO, no host FFmpeg
// install, and no temp files.
//
// FFmpeg is embedded as a WebAssembly (WASI) module and executed via wazero (a
// zero-dependency, pure-Go WASM runtime). Its filesystem syscalls are bridged to the
// afero.Fs the caller supplies, so inputs and outputs can stay entirely in RAM and
// the whole thing cross-compiles to a single static binary.
//
// STATUS: scaffold / intent. No implementation yet — see the design + requirements
// in docs/development/specs/0001-afmpeg.md before building. The intended public shape
// (subject to the spec's §10 decisions) is roughly:
//
//	rt, err := afmpeg.New(ctx)            // compile the embedded module once, reuse
//	defer rt.Close(ctx)
//	fs := afero.NewMemMapFs()             // or any afero backend
//	// ... write inputs into fs ...
//	res, err := rt.Run(ctx, fs, "-i", "in/clip.mp4", /* … */, "out/reel.mp4")
//	out, err := afero.ReadFile(fs, "out/reel.mp4")   // result, in memory
//
// The novel core is internal/vfs: an afero.Fs → wazero experimental/sys.FS adapter,
// so the guest ffmpeg's reads/writes hit the caller's (in-memory) afero filesystem
// rather than the host disk.
package afmpeg
