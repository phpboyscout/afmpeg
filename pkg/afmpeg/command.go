package afmpeg

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
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

// jobSpec is the JSON the ffmpeg-wasi libav-direct engine consumes (its
// "process" operation); see ffmpeg-wasi's job-spec reference.
type jobSpec struct {
	Op      string      `json:"op"`
	Inputs  []jobInput  `json:"inputs"`
	Filter  string      `json:"filter,omitempty"`
	Outputs []jobOutput `json:"outputs"`
}

type jobInput struct {
	Path string `json:"path"`
}

type jobOutput struct {
	Path       string            `json:"path"`
	Map        []string          `json:"map,omitempty"`
	VideoCodec string            `json:"video_codec,omitempty"`
	AudioCodec string            `json:"audio_codec,omitempty"`
	Options    map[string]string `json:"options,omitempty"`
}

// JobSpec renders the command as the ffmpeg-wasi engine's JSON job spec — the
// alternative to Args() for the libav-direct driver. It is use-case-agnostic: it
// serialises the inputs, the filtergraph, and each output's codecs/maps plus its
// Raw "-flag value" pairs as encoder options. The engine derives the container
// from the output path and the pixel/sample format from the filtergraph and
// encoder, so any pixel format / output framerate / duration belongs in the
// FilterComplex for this backend.
func (c Command) JobSpec() ([]byte, error) {
	spec := jobSpec{Op: "process", Filter: c.FilterComplex}

	for _, in := range c.Inputs {
		spec.Inputs = append(spec.Inputs, jobInput{Path: in.Path})
	}

	for _, out := range c.Outputs {
		spec.Outputs = append(spec.Outputs, jobOutput{
			Path:       out.Path,
			Map:        out.Map,
			VideoCodec: out.VideoCodec,
			AudioCodec: out.AudioCodec,
			Options:    rawToOptions(out.Raw),
		})
	}

	data, err := json.Marshal(spec)

	return data, errors.Wrap(err, "afmpeg: marshal job spec")
}

// rawToOptions interprets a slice of "-flag value" pairs (the CLI escape hatch)
// as the engine's key/value encoder options: ["-crf","23"] → {"crf":"23"}, and a
// lone "-flag" → {"flag":""}. Returns nil when there are none.
func rawToOptions(raw []string) map[string]string {
	if len(raw) == 0 {
		return nil
	}

	opts := make(map[string]string)

	for i := 0; i < len(raw); i++ {
		key := strings.TrimPrefix(raw[i], "-")
		if key == "" {
			continue
		}

		if i+1 < len(raw) && !strings.HasPrefix(raw[i+1], "-") {
			opts[key] = raw[i+1]
			i++
		} else {
			opts[key] = ""
		}
	}

	if len(opts) == 0 {
		return nil
	}

	return opts
}

// RunJob renders c as a job spec and runs it over the fs bridge — sugar for
// Run with the ffmpeg-wasi driver. Structured results come back on Result.Stdout.
func (r *Runtime) RunJob(ctx context.Context, fs afero.Fs, c Command) (Result, error) {
	spec, err := c.JobSpec()
	if err != nil {
		return Result{}, err
	}

	return r.Run(ctx, fs, string(spec))
}
