package afmpeg

import (
	"context"
	"encoding/json"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
)

// FrameJob is a declarative request to pull still frames from one video input to
// templated image files — the engine's "frames" op (spec 0021). It is the typed
// alternative to hand-tuning a `process` job's fps/select graph into an image2
// muxer: the timestamps, count, and naming live in fields, not a graph string.
// Render it with JobSpec(), or run it with Runtime.Frames.
type FrameJob struct {
	// Input is the video path. Format/Options force/parameterise the demuxer for
	// headerless or raw inputs (spec 0024), same as Input.Format/Options.
	Input   string
	Format  string
	Options map[string]string

	// Select chooses which frames — exactly one selector (see FrameSelect).
	Select FrameSelect

	// Path is the output template with an optional integer token, e.g.
	// "out/frame_%03d.png". Zero tokens is valid only for a single frame.
	Path string

	// Codec is the image encoder: "png" (default), "mjpeg", "webp" (0018), …
	Codec string

	// Scale is optional ffmpeg scale args applied to each frame, e.g. "320:-2".
	Scale string

	// Count caps the number of frames emitted (0 = the engine default).
	Count int
}

// FrameSelect chooses which frames a FrameJob extracts. Exactly one field must be
// set (validated) — the selectors are mutually exclusive (spec 0021 D-0021-B).
type FrameSelect struct {
	// Timestamp extracts the single frame at this many seconds (pointer so 0 —
	// the first frame — is distinct from unset).
	Timestamp *float64

	// Timestamps extracts one frame at each listed second.
	Timestamps []float64

	// Interval extracts a frame every N seconds across the input (N > 0).
	Interval float64

	// SceneThreshold extracts scene-change frames whose score exceeds this
	// (select='gt(scene,T)'); a pointer so 0 is distinct from unset.
	SceneThreshold *float64

	// Thumbnail extracts representative frames via the thumbnail filter.
	Thumbnail bool
}

// ExtractedFrame is one frame the engine wrote, from its result JSON.
type ExtractedFrame struct {
	Path      string  `json:"path"`
	Index     int     `json:"index"`
	Timestamp float64 `json:"timestamp"`
}

// FramesResult is the outcome of a frames job: the written frames and their count.
type FramesResult struct {
	Frames []ExtractedFrame
	Count  int
}

// jobFrames is the JSON the engine's "frames" op consumes.
type jobFrames struct {
	Op      string         `json:"op"`
	Version int            `json:"version"`
	Inputs  []jobInput     `json:"inputs"`
	Select  jobFrameSelect `json:"select"`
	Path    string         `json:"path"`
	Codec   string         `json:"codec,omitempty"`
	Scale   string         `json:"scale,omitempty"`
	Count   int            `json:"count,omitempty"`
}

// jobFrameSelect mirrors the engine's one-of selector. Scene is `any` because the
// engine takes either a number (threshold) or the string "thumbnail".
type jobFrameSelect struct {
	Timestamp  *float64  `json:"timestamp,omitempty"`
	Timestamps []float64 `json:"timestamps,omitempty"`
	Interval   float64   `json:"interval,omitempty"`
	Scene      any       `json:"scene,omitempty"`
}

// JobSpec renders the frames job to the engine's "frames" job spec after
// validating the one-of selector. Pure — no I/O.
func (j FrameJob) JobSpec() ([]byte, error) {
	if j.Path == "" {
		return nil, errors.New("afmpeg: FrameJob.Path (output template) is required")
	}

	sel, err := j.Select.wire()
	if err != nil {
		return nil, err
	}

	spec := jobFrames{
		Op:      "frames",
		Version: vocabVersion,
		Inputs:  []jobInput{{Path: j.Input, Format: j.Format, Options: j.Options}},
		Select:  sel,
		Path:    j.Path,
		Codec:   j.Codec,
		Scale:   j.Scale,
		Count:   j.Count,
	}

	data, err := json.Marshal(spec)

	return data, errors.Wrap(err, "afmpeg: marshal frames spec")
}

// wire validates the one-of selector and renders it to the engine's shape.
func (s FrameSelect) wire() (jobFrameSelect, error) {
	set := 0

	for _, on := range []bool{
		s.Timestamp != nil, len(s.Timestamps) > 0, s.Interval > 0, s.SceneThreshold != nil, s.Thumbnail,
	} {
		if on {
			set++
		}
	}

	if set != 1 {
		return jobFrameSelect{}, errors.New(
			"afmpeg: FrameSelect must set exactly one of Timestamp/Timestamps/Interval/SceneThreshold/Thumbnail")
	}

	w := jobFrameSelect{Timestamp: s.Timestamp, Timestamps: s.Timestamps, Interval: s.Interval}
	switch {
	case s.SceneThreshold != nil:
		w.Scene = *s.SceneThreshold
	case s.Thumbnail:
		w.Scene = "thumbnail"
	}

	return w, nil
}

// Frames runs a frame-extraction job over the fs bridge and parses the engine's
// per-frame result JSON — the third op beside Probe and RunJob (spec 0021).
func (r *Runtime) Frames(ctx context.Context, fs afero.Fs, job FrameJob) (FramesResult, error) {
	spec, err := job.JobSpec()
	if err != nil {
		return FramesResult{}, err
	}

	res, err := r.Run(ctx, fs, string(spec))
	if err != nil {
		return FramesResult{}, err
	}

	if res.ExitCode != 0 {
		return FramesResult{}, errors.Newf("afmpeg: frames: %s", tail(res.Stderr))
	}

	var out struct {
		Frames []ExtractedFrame `json:"frames"`
		Count  int              `json:"count"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		return FramesResult{}, errors.Wrap(err, "afmpeg: frames: parse engine output")
	}

	return FramesResult{Frames: out.Frames, Count: out.Count}, nil
}
