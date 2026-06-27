package afmpeg

import (
	"context"
	"strconv"

	"github.com/spf13/afero"
)

// shortestFloat is strconv's shortest-representation precision, so a Duration of
// 5 renders as "5" rather than "5.000000".
const shortestFloat = -1

// Command is a declarative description of an ffmpeg invocation: global options, a
// sequence of inputs, an optional complex filtergraph, and a sequence of outputs.
// It is plain, comparable, copyable data — fill it directly for full control, or
// build it with NewCommand for sane defaults plus functional options. Args()
// renders it to the argument slice Run executes; it is use-case-agnostic (a
// consumer's reel/timeline is composed on top of it, in the consumer's code).
type Command struct {
	Global        Global
	Inputs        []Input
	FilterComplex string
	Outputs       []Output
}

// Global holds invocation-wide options.
type Global struct {
	OverwriteOutput bool     // -y
	LogLevel        string   // -loglevel (e.g. "error"); "" leaves ffmpeg's default
	Raw             []string // arbitrary global flags, in order
}

// Input is one ffmpeg input with its pre-input options.
type Input struct {
	Path     string
	Loop     bool     // -loop 1
	Duration float64  // -t (pre-input); <= 0 omits it
	Format   string   // -f
	Raw      []string // arbitrary pre-input flags (e.g. -ss 3)
}

// Output is one ffmpeg output with its options.
type Output struct {
	Path        string
	Map         []string // -map … (one per entry)
	VideoCodec  string   // -c:v
	AudioCodec  string   // -c:a
	PixelFormat string   // -pix_fmt
	Format      string   // -f (container)
	Raw         []string // arbitrary per-output flags (e.g. -crf 23, -frames:v 1, -movflags +faststart)
}

// Args renders the command to the ffmpeg argument slice (everything after the
// program name), in the order ffmpeg requires: globals, then each input, then the
// filtergraph, then each output. It is pure — no I/O, safe to inspect or log.
func (c Command) Args() []string {
	var args []string

	if c.Global.OverwriteOutput {
		args = append(args, "-y")
	}

	if c.Global.LogLevel != "" {
		args = append(args, "-loglevel", c.Global.LogLevel)
	}

	args = append(args, c.Global.Raw...)

	for _, in := range c.Inputs {
		args = append(args, in.args()...)
	}

	if c.FilterComplex != "" {
		args = append(args, "-filter_complex", c.FilterComplex)
	}

	for _, out := range c.Outputs {
		args = append(args, out.args()...)
	}

	return args
}

func (in Input) args() []string {
	var args []string

	if in.Loop {
		args = append(args, "-loop", "1")
	}

	if in.Duration > 0 {
		args = append(args, "-t", strconv.FormatFloat(in.Duration, 'f', shortestFloat, durationBits))
	}

	if in.Format != "" {
		args = append(args, "-f", in.Format)
	}

	args = append(args, in.Raw...)
	args = append(args, "-i", in.Path)

	return args
}

func (out Output) args() []string {
	var args []string

	for _, m := range out.Map {
		args = append(args, "-map", m)
	}

	if out.VideoCodec != "" {
		args = append(args, "-c:v", out.VideoCodec)
	}

	if out.AudioCodec != "" {
		args = append(args, "-c:a", out.AudioCodec)
	}

	if out.PixelFormat != "" {
		args = append(args, "-pix_fmt", out.PixelFormat)
	}

	if out.Format != "" {
		args = append(args, "-f", out.Format)
	}

	args = append(args, out.Raw...)
	args = append(args, out.Path)

	return args
}

// RunCommand builds c's arguments and runs them over the fs bridge — sugar for
// Run(ctx, fs, c.Args()...).
func (r *Runtime) RunCommand(ctx context.Context, fs afero.Fs, c Command) (Result, error) {
	return r.Run(ctx, fs, c.Args()...)
}
