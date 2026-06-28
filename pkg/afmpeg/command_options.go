package afmpeg

// CommandOption configures a Command during NewCommand.
type CommandOption func(*Command)

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

// WithInput adds an input at path.
func WithInput(path string) CommandOption {
	return func(c *Command) { c.Inputs = append(c.Inputs, Input{Path: path}) }
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
