package afmpeg

// CommandOption configures a Command during NewCommand.
type CommandOption func(*Command)

// InputOption configures an Input added via WithInput / WithConcatInput.
type InputOption func(*Input)

// OutputOption configures an Output added via WithOutput.
type OutputOption func(*Output)

// NewCommand builds a Command from functional options — sugar for filling the
// struct. A zero-value Command{} works just as well for fully-explicit control.
func NewCommand(opts ...CommandOption) Command {
	var c Command

	for _, opt := range opts {
		opt(&c)
	}

	return c
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

// WithConcatInput adds an input that joins like-codec segments into one
// continuous input via the concat demuxer (a stream-copy join, spec 0013) —
// distinct from the concat filter, which decodes and re-encodes.
func WithConcatInput(paths ...string) CommandOption {
	return func(c *Command) { c.Inputs = append(c.Inputs, Input{Concat: paths}) }
}

// SeekTo starts the input at start seconds via a fast (keyframe) seek — the
// demuxer jumps to the keyframe at-or-before start, so nothing before it is
// read or decoded (spec 0014).
func SeekTo(start float64) InputOption {
	return func(in *Input) { in.Seek = &Seek{Start: start, Mode: SeekFast} }
}

// SeekAccurateTo starts the input at exactly start seconds: a fast seek plus
// decode-and-discard up to the precise frame. Costs a fraction-of-a-GOP decode;
// cannot feed a copied stream (a copy can only cut on keyframes).
func SeekAccurateTo(start float64) InputOption {
	return func(in *Input) { in.Seek = &Seek{Start: start, Mode: SeekAccurate} }
}

// InputFormat forces the input's demuxer by name (e.g. "rawvideo", "s16le",
// "mp4") instead of auto-probing — required for headerless/raw inputs (spec 0024).
func InputFormat(name string) InputOption {
	return func(in *Input) { in.Format = name }
}

// DemuxerOption sets one demuxer option on an input (spec 0024) — raw geometry
// rides here, e.g. DemuxerOption("video_size", "1280x720").
func DemuxerOption(key, value string) InputOption {
	return func(in *Input) {
		if in.Options == nil {
			in.Options = make(map[string]string)
		}

		in.Options[key] = value
	}
}

// WithFilterComplex sets the filtergraph (the engine parses it with libav's
// avfilter_graph_parse2).
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

// Map adds a graph output pad / stream specifier to mux (e.g. Map("[vout]")).
func Map(label string) OutputOption {
	return func(out *Output) { out.Map = append(out.Map, label) }
}

// VideoCodec sets an output's video encoder (e.g. "libx264").
func VideoCodec(codec string) OutputOption {
	return func(out *Output) { out.VideoCodec = codec }
}

// AudioCodec sets an output's audio encoder (e.g. "aac").
func AudioCodec(codec string) OutputOption {
	return func(out *Output) { out.AudioCodec = codec }
}

// WithOption sets one encoder option on an output (e.g. WithOption("crf", "23")).
func WithOption(key, value string) OutputOption {
	return func(out *Output) {
		if out.Options == nil {
			out.Options = make(map[string]string)
		}

		out.Options[key] = value
	}
}

// BitstreamFilter overrides the bitstream filter for a copied stream, keyed by
// its Map entry (e.g. BitstreamFilter("0:v", "h264_mp4toannexb")); "none"
// force-disables. Absent → the muxer auto-inserts any container-required filter
// on a remux (spec 0013).
func BitstreamFilter(mapKey, filter string) OutputOption {
	return func(out *Output) {
		if out.BitstreamFilters == nil {
			out.BitstreamFilters = make(map[string]string)
		}

		out.BitstreamFilters[mapKey] = filter
	}
}

// Duration stops the output after sec seconds (-t). Mutually exclusive with End.
func Duration(sec float64) OutputOption {
	return func(out *Output) { out.Duration = sec }
}

// End stops the output at position sec (-to). Mutually exclusive with Duration;
// an absolute source position under CopyTS, an output position otherwise.
func End(sec float64) OutputOption {
	return func(out *Output) { out.End = sec }
}

// CopyTS preserves source timestamps instead of zero-basing the output
// (spec 0014) — for when timelines must stay aligned across outputs.
func CopyTS() OutputOption {
	return func(out *Output) { out.CopyTS = true }
}
