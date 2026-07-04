package afmpeg

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

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
// files joined via the concat demuxer (spec 0013), optionally windowed by Seek.
type Input struct {
	Path string

	// Concat lists like-codec files joined into one continuous input via the
	// concat *demuxer* (a stream-copy join, distinct from the concat filter which
	// re-encodes). When set, Path is ignored.
	Concat []string

	// Seek starts the input at a point instead of decoding from the beginning
	// (spec 0014) — the engine seeks the demuxer to the keyframe at-or-before
	// Start, so the packets before it are never read.
	Seek *Seek

	// Format forces the demuxer by name (e.g. "rawvideo", "s16le", "mp4") instead
	// of auto-probing — required for headerless/raw inputs (spec 0024).
	Format string

	// Options are demuxer options passed as an AVDictionary (spec 0024); raw
	// geometry rides here (e.g. {"video_size": "1280x720", "pixel_format":
	// "yuv420p", "framerate": "25"} for rawvideo, {"sample_rate": "48000"} for
	// PCM). An unconsumed key is a typed error.
	Options map[string]string
}

// Seek is an input's start window (mirrors ffmpeg's -ss).
type Seek struct {
	Start float64 // seconds
	Mode  string  // SeekFast (default when empty) or SeekAccurate
}

// Seek modes: fast lands on the keyframe at-or-before Start (cheap, the
// default); accurate additionally decodes and discards up to the exact frame —
// paying a fraction-of-a-GOP decode for frame accuracy. Accurate cannot apply to
// a copied stream (a copy can only cut on keyframes).
const (
	SeekFast     = "fast"
	SeekAccurate = "accurate"
)

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

	// Duration stops the output after this many seconds (-t); End stops it at a
	// position (-to). Mutually exclusive (spec 0014). On the default zero-based
	// timeline the two coincide; under CopyTS, End is an absolute source position.
	Duration float64
	End      float64

	// CopyTS preserves the source timestamps instead of zero-basing the output —
	// a clip starting at t=0 is the default; set CopyTS when timelines must stay
	// aligned across outputs (spec 0014).
	CopyTS bool

	// Format forces the muxer by name (e.g. "hls", "dash", "segment", "mpegts")
	// instead of guessing from the path extension — needed where the extension
	// doesn't imply the muxer (spec 0015).
	Format string

	// FormatOptions are muxer options passed to write_header (spec 0015) — segment
	// timing/naming (hls_time, hls_segment_filename, …), fragmentation flags
	// (movflags), etc. Distinct from Options, which reach the encoder.
	FormatOptions map[string]string

	// Metadata sets container-level tags on the output (spec 0020) — e.g.
	// {"title": "…", "artist": "…"} → the muxer's metadata.
	Metadata map[string]string

	// Chapters is a chapter passthrough directive (spec 0020): "copy" carries the
	// first input's chapters onto the output, an input index ("1") picks another,
	// and "" / "none" drops them. Authoring chapters inline is not supported.
	Chapters string

	// StreamMetadata sets per-stream language / disposition / tags, keyed by the
	// Map entry the stream comes from (a graph pad "[vout]" or a copied "0:a").
	StreamMetadata map[string]StreamMeta
}

// StreamMeta is per-output-stream metadata (spec 0020): a language tag, a set of
// disposition flag names (e.g. "default", "forced", "attached_pic"), and
// arbitrary tags. Applied to the output stream before the header is written.
type StreamMeta struct {
	Language    string            `json:"language,omitempty"`
	Disposition []string          `json:"disposition,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
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
	Path    string            `json:"path,omitempty"`
	Concat  []string          `json:"concat,omitempty"`
	Seek    *jobSeek          `json:"seek,omitempty"`
	Format  string            `json:"format,omitempty"`
	Options map[string]string `json:"options,omitempty"`
}

type jobSeek struct {
	Start float64 `json:"start"`
	Mode  string  `json:"mode,omitempty"`
}

type jobOutput struct {
	Path             string                `json:"path"`
	Map              []string              `json:"map,omitempty"`
	VideoCodec       string                `json:"video_codec,omitempty"`
	AudioCodec       string                `json:"audio_codec,omitempty"`
	Options          map[string]string     `json:"options,omitempty"`
	BitstreamFilters map[string]string     `json:"bitstream_filters,omitempty"`
	Duration         float64               `json:"duration,omitempty"`
	End              float64               `json:"end,omitempty"`
	CopyTS           bool                  `json:"copy_ts,omitempty"`
	Format           string                `json:"format,omitempty"`
	FormatOptions    map[string]string     `json:"format_options,omitempty"`
	Metadata         map[string]string     `json:"metadata,omitempty"`
	Chapters         string                `json:"chapters,omitempty"`
	StreamMetadata   map[string]StreamMeta `json:"stream_metadata,omitempty"`
}

// CodecCopy is the codec sentinel that remuxes a mapped stream — passed through
// packet-for-packet with no decode/encode (mirrors ffmpeg's `-c copy`, spec 0013).
// Set it on Output.VideoCodec / Output.AudioCodec, with the copied input streams
// named in Output.Map as "in:type[:idx]" specifiers (e.g. "0:v", "0:a:0").
const CodecCopy = "copy"

// JobSpec renders the command to the ffmpeg-wasi engine's "process" job spec —
// inputs, the filtergraph, and each output's codecs / maps / windows / encoder
// options — after validating the seek/window contract (spec 0014). It is pure —
// no I/O, safe to inspect or log.
func (c Command) JobSpec() ([]byte, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}

	spec := jobSpec{Op: "process", Version: vocabVersion, Filter: c.FilterComplex}

	for _, in := range c.Inputs {
		ji := jobInput{Path: in.Path, Concat: in.Concat, Format: in.Format, Options: in.Options}
		if in.Seek != nil {
			ji.Seek = &jobSeek{Start: in.Seek.Start, Mode: in.Seek.Mode}
		}

		spec.Inputs = append(spec.Inputs, ji)
	}

	for _, out := range c.Outputs {
		spec.Outputs = append(spec.Outputs, jobOutput(out))
	}

	data, err := json.Marshal(spec)

	return data, errors.Wrap(err, "afmpeg: marshal job spec")
}

// validate enforces the spec-0014 contract before the engine sees it: duration
// and end are mutually exclusive, and an accurate seek cannot feed a copied
// stream (a copy can only cut on keyframes; the engine enforces this too, but
// failing here is cheaper and clearer).
func (c Command) validate() error {
	for _, out := range c.Outputs {
		if out.Duration > 0 && out.End > 0 {
			return errors.Newf("afmpeg: output %q: Duration and End are mutually exclusive", out.Path)
		}

		for _, m := range out.Map {
			if err := c.validateCopyEntry(out.Path, m); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateCopyEntry rejects a copied map entry ("0:v") whose input carries an
// accurate seek; graph pads and entries the engine will report itself pass.
func (c Command) validateCopyEntry(outPath, m string) error {
	if strings.HasPrefix(m, "[") {
		return nil // graph pad (encoded), not a copy
	}

	// An unparseable or out-of-range entry is left for the engine to report.
	idx, _, ok := strings.Cut(m, ":")
	if !ok {
		return nil
	}

	in, convErr := strconv.Atoi(idx)
	if convErr != nil || in < 0 || in >= len(c.Inputs) {
		return nil //nolint:nilerr // not this check's concern — the engine rejects bad map entries
	}

	if s := c.Inputs[in].Seek; s != nil && s.Mode == SeekAccurate {
		return errors.Newf(
			"afmpeg: output %q maps copied stream %q from an accurate-seek input (copy cuts on keyframes; use SeekFast)",
			outPath, m)
	}

	return nil
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
