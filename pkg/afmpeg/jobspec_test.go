package afmpeg_test

import (
	"encoding/json"
	"testing"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// TestJobOutput_FieldsEmittedUnderRightKeys guards the Output→jobOutput struct
// conversion (command.go): it renders a fully-populated Output and asserts each
// field lands under its expected JSON key. Go's compiler already rejects a
// field add/remove/reorder (the conversion stops compiling), so the only residual
// risk is a hand-mistyped json tag on jobOutput — which would silently emit a
// value under the wrong key. This round-trip catches exactly that.
func TestJobOutput_FieldsEmittedUnderRightKeys(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.Command{
		Inputs: []afmpeg.Input{{Path: "in.mp4"}},
		Outputs: []afmpeg.Output{{
			Path:             "out.mp4",
			Map:              []string{"0:v"},
			VideoCodec:       "libx264",
			AudioCodec:       "aac",
			Options:          map[string]string{"threads": "2"},
			VideoOptions:     map[string]string{"crf": "23"},
			AudioOptions:     map[string]string{"b": "128000"},
			SubtitleOptions:  map[string]string{"height": "480"},
			SubtitleCodec:    "srt",
			BitstreamFilters: map[string]string{"0:v": "h264_mp4toannexb"},
			Duration:         5, // End left 0 (mutually exclusive)
			CopyTS:           true,
			Format:           "mp4",
			FormatOptions:    map[string]string{"movflags": "faststart"},
			Metadata:         map[string]string{"title": "T"},
			Chapters:         "copy",
			StreamMetadata:   map[string]afmpeg.StreamMeta{"0:v": {Language: "eng"}},
		}},
	}

	data, err := cmd.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	out := m["outputs"].([]any)[0].(map[string]any)

	// Scalars land under the right key with the right value — a swapped/mistyped
	// tag (e.g. two fields sharing "format") would fail here.
	scalars := map[string]any{
		"path": "out.mp4", "video_codec": "libx264", "audio_codec": "aac",
		"subtitle_codec": "srt", "duration": float64(5), "copy_ts": true,
		"format": "mp4", "chapters": "copy",
	}
	for k, want := range scalars {
		if out[k] != want {
			t.Errorf("output[%q] = %v (%T), want %v", k, out[k], out[k], want)
		}
	}

	// The map/slice fields are present under their keys.
	for _, k := range []string{
		"map", "options", "video_options", "audio_options", "subtitle_options",
		"bitstream_filters", "format_options", "metadata", "stream_metadata",
	} {
		if _, ok := out[k]; !ok {
			t.Errorf("output missing key %q", k)
		}
	}

	// omitempty: End was 0, so it must not appear.
	if _, ok := out["end"]; ok {
		t.Errorf("end should be omitted when zero")
	}

	// The four option maps must be four distinct keys carrying four distinct
	// values. A copy-paste slip in the struct tags would put two of them under
	// one key, and the presence check above cannot see that.
	for k, want := range map[string]string{
		"options": "threads", "video_options": "crf",
		"audio_options": "b", "subtitle_options": "height",
	} {
		got, ok := out[k].(map[string]any)
		if !ok {
			t.Errorf("output[%q] is %T, want an object", k, out[k])
			continue
		}

		if len(got) != 1 {
			t.Errorf("output[%q] has %d keys, want 1 — the option maps are being merged", k, len(got))
		}

		if _, ok := got[want]; !ok {
			t.Errorf("output[%q] does not carry %q; it holds %v", k, want, got)
		}
	}
}

// TestPerKindOptionBuilders_FillTheirOwnMap guards the four builder options
// against each other (spec 0045 D6). They differ only in which field they write,
// so the failure mode is one of them writing another's map — which no compiler
// check and no round-trip through JSON would catch, because every map has the
// same type and every key would still be emitted somewhere.
func TestPerKindOptionBuilders_FillTheirOwnMap(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("in.mp4"),
		afmpeg.WithOutput("out.mp4",
			afmpeg.EncoderOption("threads", "2"),
			afmpeg.VideoOption("crf", "23"),
			afmpeg.AudioOption("b", "128000"),
			afmpeg.SubtitleOption("height", "480"),
		),
	)

	out := cmd.Outputs[0]
	for _, tc := range []struct {
		name string
		got  map[string]string
		want string
	}{
		{"Options", out.Options, "threads"},
		{"VideoOptions", out.VideoOptions, "crf"},
		{"AudioOptions", out.AudioOptions, "b"},
		{"SubtitleOptions", out.SubtitleOptions, "height"},
	} {
		if len(tc.got) != 1 {
			t.Errorf("%s has %d entries, want exactly 1: %v", tc.name, len(tc.got), tc.got)
			continue
		}

		if _, ok := tc.got[tc.want]; !ok {
			t.Errorf("%s does not carry %q; it holds %v — a builder wrote the wrong map",
				tc.name, tc.want, tc.got)
		}
	}
}

// TestVocabularyIsStampedOnEveryProcessSpec pins the gate 0045 D4 rests on. A
// v10 afmpeg must never emit an unstamped spec, because an older engine would
// then run it and silently drop the per-kind maps — the exact failure the
// vocabulary exists to prevent.
func TestVocabularyIsStampedOnEveryProcessSpec(t *testing.T) {
	t.Parallel()

	data, err := afmpeg.Command{
		Inputs:  []afmpeg.Input{{Path: "in.mp4"}},
		Outputs: []afmpeg.Output{{Path: "out.mp4", VideoCodec: "libx264"}},
	}.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got, ok := m["version"].(float64); !ok || int(got) < 10 {
		t.Errorf("process spec carries version %v, want >= 10 — an unstamped or stale spec "+
			"lets an older engine drop the per-kind option maps without saying so", m["version"])
	}
}
