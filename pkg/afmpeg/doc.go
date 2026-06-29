// Package afmpeg is a pure-Go FFmpeg binding whose filesystem I/O runs on an
// afero.Fs — including a fully in-memory filesystem — with no CGO, no host FFmpeg
// install, and no temp files.
//
// FFmpeg is supplied as a WebAssembly (WASI) module — the companion
// [ffmpeg-wasi] engine — and executed via wazero (a zero-dependency, pure-Go WASM
// runtime). Its filesystem syscalls are bridged to the afero.Fs the caller
// supplies (see internal/vfs), so inputs and outputs can stay entirely in RAM and
// the whole thing cross-compiles to a single static binary.
//
// Build a Runtime once (compiling the module is the expensive step) and reuse it;
// Run serialises invocations, one at a time per Runtime. The module is never
// embedded (spec 0004 D-C) — supply it. For the project's own releases prefer
// WithModuleRelease, which fetches and signature-verifies a published ffmpeg-wasi
// artifact against a pinned key before it runs; otherwise use WithModuleURL (pin a
// URL + SHA-256) or one of WithModuleFile / WithModuleBytes / WithModuleFS:
//
//	rt, err := afmpeg.New(ctx,
//		afmpeg.WithModuleRelease("n8.1.2-3", afmpeg.VariantLGPL))
//	if err != nil {
//		return err
//	}
//	defer rt.Close(ctx)
//
//	// Build a media job and run it over an in-memory filesystem.
//	fs := afero.NewMemMapFs() // or any afero backend
//	// ... write inputs into fs ...
//	cmd := afmpeg.NewCommand(
//		afmpeg.WithInput("in.mp4"),
//		afmpeg.WithFilterComplex("[0:v]scale=1280:-2[v]"),
//		afmpeg.WithOutput("out.mp4", afmpeg.Map("[v]"), afmpeg.VideoCodec("libx264")),
//	)
//	res, err := rt.RunJob(ctx, fs, cmd)
//	// res.ExitCode / res.Stderr report the outcome; a non-zero exit is not a Go
//	// error — only host-side failures are. The engine's structured results come
//	// back on res.Stdout.
//	out, err := afero.ReadFile(fs, "out.mp4") // the result, in memory
//
// The Command builder renders to the engine's JSON job spec (Command.JobSpec);
// it is use-case-agnostic — a consumer's reel/timeline is composed on top, in the
// consumer's code (spec 0005). Probe reports a media file's container, duration,
// and streams over the same bridge.
//
// [ffmpeg-wasi]: https://ffmpeg-wasi.phpboyscout.uk
package afmpeg
