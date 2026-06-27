package afmpeg

import (
	"slices"
	"strings"
	"testing"
)

// TestNewCommand_AllOptions exercises every functional option and every Args
// branch, asserting the exact rendered slice and the canonical ordering
// (globals → inputs → filtergraph → outputs).
func TestNewCommand_AllOptions(t *testing.T) {
	t.Parallel()

	cmd := NewCommand(
		OverwriteOutput(true),
		LogLevel("error"),
		GlobalRaw("-stats"),
		WithInput("logo.png", Loop(), Duration(5), InputFormat("image2"), InputRaw("-ss", "1")),
		WithInput("in.mkv"),
		WithFilterComplex("[1:v][0:v]overlay=10:10[v]"),
		WithOutput("out.mp4",
			Map("[v]"), VideoCodec("libx264"), AudioCodec("aac"),
			PixelFormat("yuv420p"), OutputFormat("mp4"), OutputRaw("-crf", "23")),
	)

	want := []string{
		"-y", "-loglevel", "error", "-stats",
		"-loop", "1", "-t", "5", "-f", "image2", "-ss", "1", "-i", "logo.png",
		"-i", "in.mkv",
		"-filter_complex", "[1:v][0:v]overlay=10:10[v]",
		"-map", "[v]", "-c:v", "libx264", "-c:a", "aac",
		"-pix_fmt", "yuv420p", "-f", "mp4", "-crf", "23", "out.mp4",
	}

	if got := cmd.Args(); !slices.Equal(got, want) {
		t.Fatalf("Args() =\n  %q\nwant\n  %q", got, want)
	}
}

// TestNewCommand_Defaults verifies the baked sane defaults (D-0005-A).
func TestNewCommand_Defaults(t *testing.T) {
	t.Parallel()

	got := NewCommand(WithInput("a"), WithOutput("b")).Args()
	want := []string{"-y", "-loglevel", "error", "-i", "a", "b"}

	if !slices.Equal(got, want) {
		t.Fatalf("Args() = %q, want %q", got, want)
	}
}

// TestZeroCommand_NoDefaults verifies the struct form bakes nothing.
func TestZeroCommand_NoDefaults(t *testing.T) {
	t.Parallel()

	cmd := Command{Inputs: []Input{{Path: "a"}}, Outputs: []Output{{Path: "b"}}}
	got := cmd.Args()
	want := []string{"-i", "a", "b"}

	if !slices.Equal(got, want) {
		t.Fatalf("Args() = %q, want %q", got, want)
	}
}

// TestOverwriteOutput_Disable covers turning the default off.
func TestOverwriteOutput_Disable(t *testing.T) {
	t.Parallel()

	if slices.Contains(NewCommand(OverwriteOutput(false)).Args(), "-y") {
		t.Fatal("OverwriteOutput(false) should drop -y")
	}
}

// TestStructAndConstructorAgree proves both entry points render identical args.
func TestStructAndConstructorAgree(t *testing.T) {
	t.Parallel()

	built := NewCommand(
		WithInput("in.mkv"),
		WithOutput("out.mp4", VideoCodec("libx264")),
	)

	// The struct can express exactly what NewCommand produces, defaults included.
	declared := Command{
		Global:  Global{OverwriteOutput: true, LogLevel: "error"},
		Inputs:  []Input{{Path: "in.mkv"}},
		Outputs: []Output{{Path: "out.mp4", VideoCodec: "libx264"}},
	}

	if !slices.Equal(built.Args(), declared.Args()) {
		t.Fatalf("constructor %q != struct %q", built.Args(), declared.Args())
	}
}

// TestWorkflows is the generality bar (R-0005-C): the builder expresses a spread
// of unrelated ffmpeg workflows, not a reel.
func TestWorkflows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cmd          Command
		wantContains []string
	}{
		{
			"transcode",
			Command{
				Inputs:  []Input{{Path: "in.mkv"}},
				Outputs: []Output{{Path: "out.mp4", VideoCodec: "libx264", AudioCodec: "aac", Raw: []string{"-crf", "23"}}},
			},
			[]string{"-i in.mkv", "-c:v libx264", "-c:a aac", "-crf 23", "out.mp4"},
		},
		{
			"scale",
			Command{Inputs: []Input{{Path: "in.mp4"}}, FilterComplex: "scale=1280:-2", Outputs: []Output{{Path: "out.mp4"}}},
			[]string{"-filter_complex scale=1280:-2"},
		},
		{
			"overlay",
			Command{
				Inputs:        []Input{{Path: "bg.mp4"}, {Path: "logo.png"}},
				FilterComplex: "[0:v][1:v]overlay=10:10[v]",
				Outputs:       []Output{{Path: "out.mp4", Map: []string{"[v]"}}},
			},
			[]string{"-i bg.mp4", "-i logo.png", "overlay=10:10", "-map [v]"},
		},
		{
			"concat",
			Command{
				Inputs:        []Input{{Path: "a.mp4"}, {Path: "b.mp4"}},
				FilterComplex: "[0:v][1:v]concat=n=2:v=1[v]",
				Outputs:       []Output{{Path: "out.mp4", Map: []string{"[v]"}}},
			},
			[]string{"-i a.mp4", "-i b.mp4", "concat=n=2", "-map [v]"},
		},
		{
			"thumbnail",
			Command{Inputs: []Input{{Path: "in.mp4"}}, Outputs: []Output{{Path: "thumb.png", Raw: []string{"-frames:v", "1"}}}},
			[]string{"-frames:v 1", "thumb.png"},
		},
		{
			"audio-extract",
			Command{Inputs: []Input{{Path: "in.mp4"}}, Outputs: []Output{{Path: "out.m4a", AudioCodec: "aac", Raw: []string{"-vn"}}}},
			[]string{"-vn", "-c:a aac", "out.m4a"},
		},
		{
			"reel-as-one-example",
			reelCommand(),
			[]string{"-loop 1", "xfade", "amix", "alimiter", "-c:v libx264", "out/reel.mp4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := strings.Join(tt.cmd.Args(), " ")
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("%s: args missing %q\nin: %s", tt.name, want, got)
				}
			}
		})
	}
}

// reelCommand expresses keryx's crossfade reel via the general builder — proof
// the original use case is reachable without being privileged in the API.
func reelCommand() Command {
	return NewCommand(
		WithInput("cards/01.png", Loop(), Duration(2)),
		WithInput("cards/02.png", Loop(), Duration(3)),
		WithInput("music.mp3"),
		WithFilterComplex(
			"[0:v]scale=1080:1920,fps=30,setsar=1[v0];"+
				"[1:v]scale=1080:1920,fps=30,setsar=1[v1];"+
				"[v0][v1]xfade=transition=fade:duration=0.4:offset=1.6[vout];"+
				"[2:a]volume=0.16[aout];"+
				"[aout]amix=inputs=1:normalize=0,alimiter=limit=0.95[a]"),
		WithOutput("out/reel.mp4",
			Map("[vout]"), Map("[a]"),
			VideoCodec("libx264"), AudioCodec("aac"), PixelFormat("yuv420p"),
			OutputRaw("-crf", "20", "-movflags", "+faststart")),
	)
}
