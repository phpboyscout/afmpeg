// Package afmpeg is a pure-Go FFmpeg binding whose filesystem I/O runs on an
// afero.Fs — including a fully in-memory filesystem — with no CGO, no host FFmpeg
// install, and no temp files.
//
// FFmpeg is embedded as a WebAssembly (WASI) module and executed via wazero (a
// zero-dependency, pure-Go WASM runtime). Its filesystem syscalls are bridged to
// the afero.Fs the caller supplies (see internal/vfs), so inputs and outputs can
// stay entirely in RAM and the whole thing cross-compiles to a single static
// binary.
//
// Build a Runtime once (compiling the module is the expensive step) and reuse it;
// Run serialises invocations, one at a time per Runtime. The GPL ffmpeg.wasm is
// never embedded — supply it with one of WithModuleFile, WithModuleBytes, or
// WithModuleFS (spec 0004 D-C):
//
//	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile("ffmpeg.wasm"))
//	if err != nil {
//		return err
//	}
//	defer rt.Close(ctx)
//
//	fs := afero.NewMemMapFs() // or any afero backend
//	// ... write inputs into fs ...
//	res, err := rt.Run(ctx, fs, "-i", "in/clip.mp4", /* … */, "out/reel.mp4")
//	// res.ExitCode / res.Stderr report ffmpeg's outcome; a non-zero exit is not
//	// a Go error — only host-side failures are.
//	out, err := afero.ReadFile(fs, "out/reel.mp4") // the result, in memory
//
// Run returns the exit code and captured stderr; Probe returns a media file's
// duration over the same bridge. The higher-level timeline render helper layers
// on top (spec 0005).
package afmpeg
