package afmpeg_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// TestRunJob runs a built command end-to-end over a MemMapFs (composing
// 0003/0004), proving the builder → JobSpec → runtime path.
func TestRunJob(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("in.mp4"),
		afmpeg.WithFilterComplex("[0:v]null[v]"),
		afmpeg.WithOutput("out.mp4", afmpeg.Map("[v]"), afmpeg.VideoCodec("libx264"), afmpeg.WithOption("crf", "23")),
	)

	res, err := newTestRuntime(t).RunJob(context.Background(), afero.NewMemMapFs(), cmd)
	if err != nil {
		t.Fatalf("RunJob: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0:\n%s", res.ExitCode, res.Stderr)
	}
}

// TestCommand_JobSpec renders a Command to the ffmpeg-wasi job spec and checks
// the structured mapping (inputs / filtergraph / outputs / codecs / options).
func TestCommand_JobSpec(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.Command{
		Inputs:        []afmpeg.Input{{Path: "a.png"}, {Path: "music.mp3"}},
		FilterComplex: "[0:v]scale=1280:-2[vout];[1:a]anull[aout]",
		Outputs: []afmpeg.Output{{
			Path:       "out.mp4",
			Map:        []string{"[vout]", "[aout]"},
			VideoCodec: "libx264",
			AudioCodec: "aac",
			Options:    map[string]string{"crf": "20", "movflags": "+faststart"},
		}},
	}

	data, err := cmd.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var got struct {
		Op      string `json:"op"`
		Version int    `json:"version"`
		Inputs  []struct {
			Path string `json:"path"`
		} `json:"inputs"`
		Filter  string `json:"filter"`
		Outputs []struct {
			Path       string            `json:"path"`
			Map        []string          `json:"map"`
			VideoCodec string            `json:"video_codec"`
			AudioCodec string            `json:"audio_codec"`
			Options    map[string]string `json:"options"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}

	if got.Op != "process" {
		t.Errorf("op = %q, want process", got.Op)
	}

	if got.Version < 1 {
		t.Errorf("version = %d, want the current vocabulary version (>= 1)", got.Version)
	}

	if len(got.Inputs) != 2 || got.Inputs[0].Path != "a.png" || got.Inputs[1].Path != "music.mp3" {
		t.Errorf("inputs = %+v", got.Inputs)
	}

	if got.Filter != cmd.FilterComplex {
		t.Errorf("filter = %q, want %q", got.Filter, cmd.FilterComplex)
	}

	if len(got.Outputs) != 1 {
		t.Fatalf("outputs = %+v", got.Outputs)
	}

	o := got.Outputs[0]
	if o.Path != "out.mp4" || o.VideoCodec != "libx264" || o.AudioCodec != "aac" {
		t.Errorf("output = %+v", o)
	}

	if len(o.Map) != 2 || o.Map[0] != "[vout]" || o.Map[1] != "[aout]" {
		t.Errorf("map = %v", o.Map)
	}

	if o.Options["crf"] != "20" || o.Options["movflags"] != "+faststart" {
		t.Errorf("options = %v", o.Options)
	}
}

// TestNewCommand_Options exercises the functional-options builder.
func TestNewCommand_Options(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("a.png"),
		afmpeg.WithInput("b.mp3"),
		afmpeg.WithFilterComplex("[0:v]null[v]"),
		afmpeg.WithOutput("out.mp4",
			afmpeg.Map("[v]"), afmpeg.VideoCodec("libx264"),
			afmpeg.AudioCodec("aac"), afmpeg.WithOption("crf", "23")),
	)

	if len(cmd.Inputs) != 2 || cmd.Inputs[1].Path != "b.mp3" {
		t.Errorf("inputs = %+v", cmd.Inputs)
	}

	if cmd.FilterComplex != "[0:v]null[v]" {
		t.Errorf("filter = %q", cmd.FilterComplex)
	}

	if len(cmd.Outputs) != 1 {
		t.Fatalf("outputs = %+v", cmd.Outputs)
	}

	o := cmd.Outputs[0]
	if o.Path != "out.mp4" || o.VideoCodec != "libx264" || o.AudioCodec != "aac" ||
		len(o.Map) != 1 || o.Map[0] != "[v]" || o.Options["crf"] != "23" {
		t.Errorf("output = %+v", o)
	}
}
