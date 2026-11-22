package afmpeg_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// Output.FrameRate, spec 0025 §4.2.

func renderedOutput(t *testing.T, cmd afmpeg.Command) map[string]any {
	t.Helper()

	data, err := cmd.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var m struct {
		Outputs []map[string]any `json:"outputs"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	return m.Outputs[0]
}

func TestFrameRate_RendersUnderItsKeyAndIsOmittedWhenUnset(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("in.mkv"),
		afmpeg.WithOutput("out.mp4", afmpeg.VideoCodec("libopenh264"), afmpeg.FrameRate("30000/1001")),
	)
	if got := renderedOutput(t, cmd)["frame_rate"]; got != "30000/1001" {
		t.Errorf("frame_rate = %v, want 30000/1001", got)
	}

	cmd.Outputs[0].FrameRate = ""
	if got, ok := renderedOutput(t, cmd)["frame_rate"]; ok {
		t.Errorf("frame_rate = %v on an unset FrameRate, want the key omitted so a v10 reading stays unchanged", got)
	}
}

// A v10 engine ignores frame_rate and writes variable-rate output, so the
// version stamp is the only thing that makes an older engine refuse the job
// (spec 0025 §5).
func TestFrameRate_IsStampedWithAVocabularyThatCarriesIt(t *testing.T) {
	t.Parallel()

	data, err := afmpeg.NewCommand(
		afmpeg.WithInput("in.mkv"),
		afmpeg.WithOutput("out.mp4", afmpeg.VideoCodec("libopenh264"), afmpeg.FrameRate("25")),
	).JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var spec struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if spec.Version < 11 {
		t.Errorf("version = %d on a spec using frame_rate, want 11 or later so a v10 engine refuses it", spec.Version)
	}
}

func TestFrameRate_IsRefusedWhereItCannotApply(t *testing.T) {
	t.Parallel()

	in := []afmpeg.Input{{Path: "in.mkv"}}

	tests := []struct {
		name string
		out  afmpeg.Output
		want string // "" means accepted
	}{
		{"a copied video stream",
			afmpeg.Output{Path: "o.mkv", Map: []string{"0:v"}, VideoCodec: afmpeg.CodecCopy, FrameRate: "25"},
			"copied video"},
		{"an output with no video codec",
			afmpeg.Output{Path: "o.mkv", Map: []string{"[a]"}, AudioCodec: "aac", FrameRate: "25"},
			"no encoded video"},
		{"a map naming only copied streams",
			afmpeg.Output{Path: "o.mkv", Map: []string{"0:v", "0:a"}, VideoCodec: "libopenh264", FrameRate: "25"},
			"no encoded video"},
		{"an encoded graph pad", afmpeg.Output{
			Path: "o.mkv", Map: []string{"[v]", "0:a"}, VideoCodec: "libopenh264", AudioCodec: afmpeg.CodecCopy, FrameRate: "25",
		}, ""},
		{"the default graph", afmpeg.Output{Path: "o.mkv", VideoCodec: "libopenh264", FrameRate: "ntsc"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := afmpeg.Command{Inputs: in, Outputs: []afmpeg.Output{tt.out}}.JobSpec()

			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("JobSpec refused it: %v", err)
			case tt.want != "" && err == nil:
				t.Fatalf("JobSpec accepted it, want a refusal mentioning %q", tt.want)
			case tt.want != "" && !strings.Contains(err.Error(), tt.want):
				t.Fatalf("JobSpec: %v, want it to mention %q", err, tt.want)
			}
		})
	}
}
