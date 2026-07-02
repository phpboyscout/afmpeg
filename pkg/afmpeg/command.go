package afmpeg

import (
	"context"
	"encoding/json"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
)

// Command is a declarative description of a media job for the ffmpeg-wasi engine:
// a sequence of inputs, an optional filtergraph, and a sequence of outputs. It is
// plain, comparable-ish, copyable data — fill it directly for full control, or
// build it with NewCommand. JobSpec() renders it to the engine's JSON "process"
// job spec; it is use-case-agnostic (a consumer's reel/timeline is composed on
// top of it, in the consumer's code).
type Command struct {
	Inputs        []Input
	FilterComplex string
	Outputs       []Output
}

// Input is one media input: a single Path, or a Concat playlist of like-codec
// files joined via the concat demuxer (spec 0013).
type Input struct {
	Path string

	// Concat lists like-codec files joined into one continuous input via the
	// concat *demuxer* (a stream-copy join, distinct from the concat filter which
	// re-encodes). When set, Path is ignored.
	Concat []string
}

// Output is one muxed output: its path, the graph output pads / stream specifiers
// to mux, the encoders, and their options.
type Output struct {
	Path       string
	Map        []string          // graph pads ("[vout]") and/or copied input streams ("0:v")
	VideoCodec string            // the video encoder (e.g. "libx264"), or CodecCopy to remux
	AudioCodec string            // the audio encoder (e.g. "aac"), or CodecCopy to remux
	Options    map[string]string // encoder options (e.g. {"crf": "23"})

	// BitstreamFilters overrides the bitstream filter for a copied stream, keyed
	// by its Map entry (e.g. {"0:v": "h264_mp4toannexb"}); "none" force-disables.
	// Absent → the muxer auto-inserts any container-required filter (spec 0013).
	BitstreamFilters map[string]string
}

// jobSpec is the JSON the ffmpeg-wasi engine consumes (the process / probe ops).
// Version stamps the job-spec vocabulary the spec is written in; the engine
// rejects a spec whose Version exceeds what it supports (spec 0007 §4 contract).
type jobSpec struct {
	Op      string      `json:"op"`
	Version int         `json:"version"`
	Inputs  []jobInput  `json:"inputs"`
	Filter  string      `json:"filter,omitempty"`
	Outputs []jobOutput `json:"outputs,omitempty"`
}

type jobInput struct {
	Path   string   `json:"path,omitempty"`
	Concat []string `json:"concat,omitempty"`
}

type jobOutput struct {
	Path             string            `json:"path"`
	Map              []string          `json:"map,omitempty"`
	VideoCodec       string            `json:"video_codec,omitempty"`
	AudioCodec       string            `json:"audio_codec,omitempty"`
	Options          map[string]string `json:"options,omitempty"`
	BitstreamFilters map[string]string `json:"bitstream_filters,omitempty"`
}

// CodecCopy is the codec sentinel that remuxes a mapped stream — passed through
// packet-for-packet with no decode/encode (mirrors ffmpeg's `-c copy`, spec 0013).
// Set it on Output.VideoCodec / Output.AudioCodec, with the copied input streams
// named in Output.Map as "in:type[:idx]" specifiers (e.g. "0:v", "0:a:0").
const CodecCopy = "copy"

// JobSpec renders the command to the ffmpeg-wasi engine's "process" job spec —
// inputs, the filtergraph, and each output's codecs / maps / encoder options. It
// is pure — no I/O, safe to inspect or log.
func (c Command) JobSpec() ([]byte, error) {
	spec := jobSpec{Op: "process", Version: vocabVersion, Filter: c.FilterComplex}

	for _, in := range c.Inputs {
		spec.Inputs = append(spec.Inputs, jobInput(in))
	}

	for _, out := range c.Outputs {
		spec.Outputs = append(spec.Outputs, jobOutput(out))
	}

	data, err := json.Marshal(spec)

	return data, errors.Wrap(err, "afmpeg: marshal job spec")
}

// RunJob renders c as a job spec and runs it over the fs bridge — sugar for Run
// with the rendered spec. Structured results (process status, probe info) come
// back on Result.Stdout.
func (r *Runtime) RunJob(ctx context.Context, fs afero.Fs, c Command) (Result, error) {
	spec, err := c.JobSpec()
	if err != nil {
		return Result{}, err
	}

	return r.Run(ctx, fs, string(spec))
}
