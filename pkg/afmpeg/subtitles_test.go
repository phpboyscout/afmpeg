package afmpeg_test

import (
	"encoding/json"
	"testing"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// TestJobSpec_SubtitleCodec proves the spec-0019 subtitle-stream vocabulary
// serialises: subtitle_codec on an output, including a subtitle-only sidecar
// (no video/audio codec) mapping an "N:s" specifier.
func TestJobSpec_SubtitleCodec(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.Command{
		Inputs:  []afmpeg.Input{{Path: "in.mkv"}},
		Outputs: []afmpeg.Output{{Path: "out.vtt", Map: []string{"0:s"}, SubtitleCodec: "webvtt"}},
	}

	data, err := cmd.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var spec struct {
		Version int `json:"version"`
		Outputs []struct {
			Map           []string `json:"map"`
			SubtitleCodec string   `json:"subtitle_codec"`
			VideoCodec    string   `json:"video_codec"`
			AudioCodec    string   `json:"audio_codec"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if spec.Version < 8 {
		t.Errorf("version = %d, want >= 8", spec.Version)
	}

	out := spec.Outputs[0]
	if out.SubtitleCodec != "webvtt" {
		t.Errorf("subtitle_codec = %q, want webvtt", out.SubtitleCodec)
	}
	if len(out.Map) != 1 || out.Map[0] != "0:s" {
		t.Errorf("map = %v, want [0:s]", out.Map)
	}
	if out.VideoCodec != "" || out.AudioCodec != "" {
		t.Errorf("sidecar output should carry no video/audio codec, got v=%q a=%q", out.VideoCodec, out.AudioCodec)
	}
}
