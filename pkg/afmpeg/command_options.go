package afmpeg

// CommandOption configures a Command during NewCommand.
type CommandOption func(*Command)

// InputOption configures an Input added via WithInput.
type InputOption func(*Input)

// OutputOption configures an Output added via WithOutput.
type OutputOption func(*Output)

// NewCommand builds a Command from sane defaults plus functional options. The
// defaults (D-0005-A): overwrite the output (-y) and quiet logging
// (-loglevel error) — programmatic conveniences, with no codec/quality opinion.
// A zero-value Command{} bakes none of these; use it for fully-explicit control.
func NewCommand(opts ...CommandOption) Command {
	c := Command{
		Global: Global{
			OverwriteOutput: true,
			LogLevel:        "error",
		},
	}

	for _, opt := range opts {
		opt(&c)
	}

	return c
}

// OverwriteOutput sets whether ffmpeg overwrites existing outputs (-y).
func OverwriteOutput(enable bool) CommandOption {
	return func(c *Command) { c.Global.OverwriteOutput = enable }
}

// LogLevel sets ffmpeg's -loglevel (e.g. "error", "info", "verbose").
func LogLevel(level string) CommandOption {
	return func(c *Command) { c.Global.LogLevel = level }
}

// GlobalRaw appends arbitrary global flags.
func GlobalRaw(args ...string) CommandOption {
	return func(c *Command) { c.Global.Raw = append(c.Global.Raw, args...) }
}

// WithInput adds an input at path, configured by the given input options.
func WithInput(path string, opts ...InputOption) CommandOption {
	return func(c *Command) {
		in := Input{Path: path}
		for _, opt := range opts {
			opt(&in)
		}

		c.Inputs = append(c.Inputs, in)
	}
}

// WithFilterComplex sets the -filter_complex graph.
func WithFilterComplex(graph string) CommandOption {
	return func(c *Command) { c.FilterComplex = graph }
}

// WithOutput adds an output at path, configured by the given output options.
func WithOutput(path string, opts ...OutputOption) CommandOption {
	return func(c *Command) {
		out := Output{Path: path}
		for _, opt := range opts {
			opt(&out)
		}

		c.Outputs = append(c.Outputs, out)
	}
}

// Loop loops a still input (-loop 1).
func Loop() InputOption {
	return func(in *Input) { in.Loop = true }
}

// Duration limits an input to d seconds (-t, pre-input).
func Duration(d float64) InputOption {
	return func(in *Input) { in.Duration = d }
}

// InputFormat forces an input's demuxer/format (-f).
func InputFormat(format string) InputOption {
	return func(in *Input) { in.Format = format }
}

// InputRaw appends arbitrary pre-input flags.
func InputRaw(args ...string) InputOption {
	return func(in *Input) { in.Raw = append(in.Raw, args...) }
}

// Map adds an output stream map (-map label).
func Map(label string) OutputOption {
	return func(out *Output) { out.Map = append(out.Map, label) }
}

// VideoCodec sets an output's video codec (-c:v).
func VideoCodec(codec string) OutputOption {
	return func(out *Output) { out.VideoCodec = codec }
}

// AudioCodec sets an output's audio codec (-c:a).
func AudioCodec(codec string) OutputOption {
	return func(out *Output) { out.AudioCodec = codec }
}

// PixelFormat sets an output's pixel format (-pix_fmt).
func PixelFormat(format string) OutputOption {
	return func(out *Output) { out.PixelFormat = format }
}

// OutputFormat forces an output's muxer/container (-f).
func OutputFormat(format string) OutputOption {
	return func(out *Output) { out.Format = format }
}

// OutputRaw appends arbitrary per-output flags.
func OutputRaw(args ...string) OutputOption {
	return func(out *Output) { out.Raw = append(out.Raw, args...) }
}
