package afmpeg

import (
	"encoding/json"

	"github.com/cockroachdb/errors"
)

// ProcessResult is the structured result of a process job (Run / RunJob), parsed
// from Result.Stdout. Outputs describes each muxed output the engine produced;
// Analysis carries any measurements analysis filters emitted (spec 0017 §Q).
type ProcessResult struct {
	Outputs  []OutputResult `json:"outputs"`
	Analysis []Measurement  `json:"analysis,omitempty"`
}

// OutputResult is one muxed output: its Path, whether it Segmented into a set of
// files (an hls/dash/segment muxer — Path is then the playlist/manifest), and the
// streams the engine wrote.
type OutputResult struct {
	Path      string         `json:"path"`
	Segmented bool           `json:"segmented,omitempty"`
	Streams   []OutputStream `json:"streams"`
}

// OutputStream is one written stream: its media Type and Codec, and Disposition
// "copy" for a stream-copied (remuxed) stream.
type OutputStream struct {
	Type        string `json:"type"`
	Codec       string `json:"codec"`
	Disposition string `json:"disposition,omitempty"`
}

// Measurement is one analysis-filter data point (spec 0017 §Q): the source Time in
// seconds, the metadata Key with the "lavfi." prefix dropped (e.g. "cropdetect.w",
// "r128.I", "silence_start", "black_start"), and its Value. Values are the filters'
// own strings — a consumer parses the numeric ones as needed. The series is
// consecutive-deduplicated per key, so a stable measurement (cropdetect) appears
// once while discrete events (silence_start/silence_end) each appear.
//
// Analysis filters that only log by default (ebur128, astats) need their
// metadata=1 option to populate this — e.g. filter "ebur128=metadata=1".
type Measurement struct {
	Time  float64 `json:"t"`
	Key   string  `json:"key"`
	Value string  `json:"value"`
}

// ParseResult decodes the structured process result from a Run / RunJob Result. An
// empty Stdout (or a non-process op) yields a zero ProcessResult, not an error.
// (A non-zero Result.ExitCode means the engine failed — check that first; the
// engine emits this result JSON only on success.)
func ParseResult(res Result) (ProcessResult, error) {
	var pr ProcessResult
	if res.Stdout == "" {
		return pr, nil
	}

	if err := json.Unmarshal([]byte(res.Stdout), &pr); err != nil {
		return pr, errors.Wrap(err, "afmpeg: parse process result")
	}

	return pr, nil
}
