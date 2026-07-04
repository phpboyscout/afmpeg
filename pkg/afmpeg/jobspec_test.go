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
			Options:          map[string]string{"crf": "23"},
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
	for _, k := range []string{"map", "options", "bitstream_filters", "format_options", "metadata", "stream_metadata"} {
		if _, ok := out[k]; !ok {
			t.Errorf("output missing key %q", k)
		}
	}

	// omitempty: End was 0, so it must not appear.
	if _, ok := out["end"]; ok {
		t.Errorf("end should be omitted when zero")
	}
}
