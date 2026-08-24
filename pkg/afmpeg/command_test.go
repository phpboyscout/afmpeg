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
		afmpeg.WithOutput("out.mp4", afmpeg.Map("[v]"), afmpeg.VideoCodec("libx264"), afmpeg.VideoOption("crf", "23")),
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

// TestCommand_JobSpec_Copy renders the spec-0013 copy vocabulary: the "copy"
// codec sentinel, unbracketed input-stream map specifiers, per-stream bitstream
// filters, and a concat-demuxer input.
func TestCommand_JobSpec_Copy(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.NewCommand(
		afmpeg.WithConcatInput("a.ts", "b.ts"),
		afmpeg.WithOutput("out.ts",
			afmpeg.Map("0:v"), afmpeg.Map("0:a"),
			afmpeg.VideoCodec(afmpeg.CodecCopy), afmpeg.AudioCodec(afmpeg.CodecCopy),
			afmpeg.BitstreamFilter("0:v", "h264_mp4toannexb")),
	)

	data, err := cmd.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var got struct {
		Inputs []struct {
			Path   string   `json:"path"`
			Concat []string `json:"concat"`
		} `json:"inputs"`
		Outputs []struct {
			Map              []string          `json:"map"`
			VideoCodec       string            `json:"video_codec"`
			AudioCodec       string            `json:"audio_codec"`
			BitstreamFilters map[string]string `json:"bitstream_filters"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}

	if len(got.Inputs) != 1 || len(got.Inputs[0].Concat) != 2 || got.Inputs[0].Concat[0] != "a.ts" {
		t.Fatalf("concat input = %+v", got.Inputs)
	}

	if got.Inputs[0].Path != "" {
		t.Errorf("concat input emitted a path %q, want none", got.Inputs[0].Path)
	}

	o := got.Outputs[0]
	if o.VideoCodec != "copy" || o.AudioCodec != "copy" {
		t.Errorf("codecs = %q/%q, want copy/copy", o.VideoCodec, o.AudioCodec)
	}

	if len(o.Map) != 2 || o.Map[0] != "0:v" || o.Map[1] != "0:a" {
		t.Errorf("map = %v, want [0:v 0:a]", o.Map)
	}

	if o.BitstreamFilters["0:v"] != "h264_mp4toannexb" {
		t.Errorf("bitstream_filters = %v", o.BitstreamFilters)
	}
}

// TestCommand_JobSpec_Seek renders the spec-0014 vocabulary: input seek
// (fast/accurate), the output window (duration/end), and copy_ts.
func TestCommand_JobSpec_Seek(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("in.mp4", afmpeg.SeekAccurateTo(12.5)),
		afmpeg.WithFilterComplex("[0:v]null[v]"),
		afmpeg.WithOutput("clip.mp4",
			afmpeg.Map("[v]"), afmpeg.VideoCodec("libopenh264"),
			afmpeg.Duration(5), afmpeg.CopyTS()),
	)

	data, err := cmd.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var got struct {
		Version int `json:"version"`
		Inputs  []struct {
			Seek *struct {
				Start float64 `json:"start"`
				Mode  string  `json:"mode"`
			} `json:"seek"`
		} `json:"inputs"`
		Outputs []struct {
			Duration float64 `json:"duration"`
			End      float64 `json:"end"`
			CopyTS   bool    `json:"copy_ts"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}

	if got.Version < 3 {
		t.Errorf("version = %d, want >= 3 (the 0014 vocabulary)", got.Version)
	}

	if s := got.Inputs[0].Seek; s == nil || s.Start != 12.5 || s.Mode != "accurate" {
		t.Errorf("seek = %+v, want start 12.5 mode accurate", got.Inputs[0].Seek)
	}

	o := got.Outputs[0]
	if o.Duration != 5 || o.End != 0 || !o.CopyTS {
		t.Errorf("window = %+v, want duration 5 / no end / copy_ts", o)
	}
}

// TestCommand_JobSpec_Validation covers the spec-0014 contract: the
// duration+end conflict and accurate-seek-on-copy are rejected, while graph
// pads, fast seeks, and entries left to the engine (unparseable / out-of-range)
// pass afmpeg's validation.
func TestCommand_JobSpec_Validation(t *testing.T) {
	t.Parallel()

	accurateIn := []afmpeg.Input{{Path: "in.mp4", Seek: &afmpeg.Seek{Start: 1, Mode: afmpeg.SeekAccurate}}}

	tests := []struct {
		name    string
		cmd     afmpeg.Command
		wantErr bool
	}{
		{
			"duration and end are mutually exclusive",
			afmpeg.Command{
				Inputs:  []afmpeg.Input{{Path: "in.mp4"}},
				Outputs: []afmpeg.Output{{Path: "o.mp4", AudioCodec: "aac", Duration: 5, End: 7}},
			},
			true,
		},
		{
			"accurate seek cannot feed a copied stream",
			afmpeg.Command{
				Inputs:  accurateIn,
				Outputs: []afmpeg.Output{{Path: "o.mp4", Map: []string{"0:v"}, VideoCodec: afmpeg.CodecCopy}},
			},
			true,
		},
		{
			"accurate seek into the graph is fine",
			afmpeg.Command{
				Inputs:  accurateIn,
				Outputs: []afmpeg.Output{{Path: "o.mp4", Map: []string{"[v]"}, VideoCodec: "libopenh264"}},
			},
			false,
		},
		{
			"fast seek may feed a copy",
			afmpeg.Command{
				Inputs:  []afmpeg.Input{{Path: "in.mp4", Seek: &afmpeg.Seek{Start: 1}}},
				Outputs: []afmpeg.Output{{Path: "o.mp4", Map: []string{"0:v"}, VideoCodec: afmpeg.CodecCopy}},
			},
			false,
		},
		{
			"unparseable and out-of-range map entries are the engine's to reject",
			afmpeg.Command{
				Inputs:  accurateIn,
				Outputs: []afmpeg.Output{{Path: "o.mp4", Map: []string{"nonsense", "x:v", "9:v"}, VideoCodec: afmpeg.CodecCopy}},
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tt.cmd.JobSpec()
			if (err != nil) != tt.wantErr {
				t.Fatalf("JobSpec: err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestCommand_JobSpec_InputOptions renders the spec-0024 vocabulary: a forced
// demuxer and a demuxer-options dict on an input.
func TestCommand_JobSpec_InputOptions(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("frames.yuv", afmpeg.InputFormat("rawvideo"),
			afmpeg.DemuxerOption("video_size", "1280x720"),
			afmpeg.DemuxerOption("framerate", "25")),
		afmpeg.WithFilterComplex("[0:v]null[v]"),
		afmpeg.WithOutput("out.mp4", afmpeg.Map("[v]"), afmpeg.VideoCodec("libopenh264")),
	)

	data, err := cmd.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var got struct {
		Version int `json:"version"`
		Inputs  []struct {
			Format  string            `json:"format"`
			Options map[string]string `json:"options"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}

	if got.Version < 4 {
		t.Errorf("version = %d, want >= 4 (the 0024 vocabulary)", got.Version)
	}

	in := got.Inputs[0]
	if in.Format != "rawvideo" {
		t.Errorf("format = %q, want rawvideo", in.Format)
	}

	if in.Options["video_size"] != "1280x720" || in.Options["framerate"] != "25" {
		t.Errorf("options = %v", in.Options)
	}
}

// TestCommand_JobSpec_OutputFormat renders the spec-0015 muxer vocabulary: a
// forced muxer and its format-options dict.
func TestCommand_JobSpec_OutputFormat(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("in.mp4"),
		afmpeg.WithFilterComplex("[0:v]null[v]"),
		afmpeg.WithOutput("stream.m3u8", afmpeg.Map("[v]"), afmpeg.VideoCodec("libopenh264"),
			afmpeg.OutputFormat("hls"),
			afmpeg.FormatOption("hls_time", "4"),
			afmpeg.FormatOption("hls_segment_filename", "seg_%03d.ts")),
	)

	data, err := cmd.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var got struct {
		Version int `json:"version"`
		Outputs []struct {
			Format        string            `json:"format"`
			FormatOptions map[string]string `json:"format_options"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}

	if got.Version < 5 {
		t.Errorf("version = %d, want >= 5 (the 0015 vocabulary)", got.Version)
	}

	o := got.Outputs[0]
	if o.Format != "hls" {
		t.Errorf("format = %q, want hls", o.Format)
	}

	if o.FormatOptions["hls_time"] != "4" || o.FormatOptions["hls_segment_filename"] != "seg_%03d.ts" {
		t.Errorf("format_options = %v", o.FormatOptions)
	}
}

// TestSeekAndWindowOptions exercises the 0014 builder options.
func TestSeekAndWindowOptions(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("in.mp4", afmpeg.SeekTo(3)),
		afmpeg.WithOutput("o.mp4", afmpeg.AudioCodec("aac"), afmpeg.End(9), afmpeg.CopyTS()),
	)

	if s := cmd.Inputs[0].Seek; s == nil || s.Start != 3 || s.Mode != afmpeg.SeekFast {
		t.Errorf("SeekTo: seek = %+v, want fast @3s", cmd.Inputs[0].Seek)
	}

	o := cmd.Outputs[0]
	if o.End != 9 || o.Duration != 0 || !o.CopyTS {
		t.Errorf("output window = %+v, want End 9 / CopyTS", o)
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
			afmpeg.AudioCodec("aac"), afmpeg.VideoOption("crf", "23")),
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
		len(o.Map) != 1 || o.Map[0] != "[v]" || o.VideoOptions["crf"] != "23" {
		t.Errorf("output = %+v", o)
	}

	// crf must NOT have landed in the common map: this output opens an aac
	// encoder too, and aac has no crf — so a common crf fails the whole job
	// (spec 0045). The point of VideoOption is that it does not go there.
	if _, ok := o.Options["crf"]; ok {
		t.Errorf("VideoOption put crf in Options, where it would reach the aac encoder: %+v", o.Options)
	}
}
