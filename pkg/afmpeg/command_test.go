package afmpeg_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// TestRunCommand runs a built command end-to-end over a MemMapFs (composing
// 0003/0004), proving the builder → runtime path.
func TestRunCommand(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.NewCommand(afmpeg.WithInput("in"), afmpeg.WithOutput("out"))

	res, err := newTestRuntime(t).RunCommand(context.Background(), afero.NewMemMapFs(), cmd)
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
}

// TestRunCommand_PassesArgs proves RunCommand forwards the rendered Args() to the
// guest: a global-raw "exit:4" lands as argv[0] and the guest exits 4.
func TestRunCommand_PassesArgs(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.Command{Global: afmpeg.Global{Raw: []string{"exit:4"}}}

	res, err := newTestRuntime(t).RunCommand(context.Background(), afero.NewMemMapFs(), cmd)
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}

	if res.ExitCode != 4 {
		t.Fatalf("ExitCode = %d, want 4 (args not forwarded?)", res.ExitCode)
	}
}

// TestCommand_JobSpec renders a Command to the ffmpeg-wasi job spec and checks
// the structured mapping (inputs / filtergraph / outputs / codecs, and each
// Output.Raw "-flag value" pair as an encoder option).
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
			Raw:        []string{"-crf", "20", "-movflags", "+faststart"},
		}},
	}

	data, err := cmd.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var got struct {
		Op     string `json:"op"`
		Inputs []struct {
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
