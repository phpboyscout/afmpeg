package docsoptions_test

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gitlab.com/phpboyscout/afmpeg/internal/docsoptions"
)

// TestDocumentedOptionNamesAreWellFormed is the check afmpeg#11 asked for: every
// option key the docs teach must pass the shape rules, with no engine needed.
func TestDocumentedOptionNamesAreWellFormed(t *testing.T) {
	t.Parallel()

	for _, v := range docsoptions.Check(extractRepo(t)) {
		t.Error(v)
	}
}

// TestExtractionStillSeesTheDocs guards the check above against passing because
// the extraction silently stopped matching anything.
func TestExtractionStillSeesTheDocs(t *testing.T) {
	t.Parallel()

	found := map[string]docsoptions.Dict{}
	total := 0

	for _, s := range extractRepo(t) {
		for _, k := range s.Keys {
			found[s.File+" "+k.Name] = k.Dict
			total++
		}
	}

	const floor = 20
	if total < floor {
		t.Errorf("extracted %d documented keys, far fewer than the %d the docs held when this was written", total, floor)
	}

	for key, want := range map[string]docsoptions.Dict{
		"docs/how-to/package-for-streaming.md hls_time":         docsoptions.MuxerDict,
		"docs/how-to/compose-a-command.md movflags":             docsoptions.MuxerDict,
		"docs/how-to/package-for-streaming.md g":                docsoptions.VideoDict,
		"docs/how-to/read-a-raw-input.md video_size":            docsoptions.DemuxerDict,
		"docs/how-to/use-the-native-backend.md preset":          docsoptions.EncoderDict,
		"docs/tutorials/first-in-memory-transcode.md b":         docsoptions.VideoDict,
		"docs/tutorials/first-in-memory-transcode.md framerate": docsoptions.DemuxerDict,
	} {
		got, ok := found[key]

		switch {
		case !ok:
			t.Errorf("%s not extracted", key)
		case got != want:
			t.Errorf("%s: dictionary %s, want %s", key, got, want)
		}
	}
}

func extractRepo(t *testing.T) []docsoptions.Site {
	t.Helper()

	sites, err := docsoptions.Extract(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}

	return sites
}

func keysOf(sites []docsoptions.Site) []docsoptions.Key {
	var keys []docsoptions.Key
	for _, s := range sites {
		keys = append(keys, s.Keys...)
	}

	return keys
}

func equal[T any](t *testing.T, what string, got, want T) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got  %+v\n want %+v", what, got, want)
	}
}

func TestExtractFile_StructLiteralWithElidedElementType(t *testing.T) {
	t.Parallel()

	sites := docsoptions.ExtractFile("doc.md", "intro\n\n```go\n"+`cmd := afmpeg.Command{
    Inputs:        []afmpeg.Input{{Path: "in.raw", Format: "rawvideo", Options: map[string]string{"video_size": "320x240"}}},
    FilterComplex: "[0:v]scale=1280:-2[vout]", // [unbalanced in a comment
    Outputs: []afmpeg.Output{{
        Path:          "out.mp4",
        VideoCodec:    "libx264",
        AudioCodec:    "aac",
        Options:       map[string]string{"threads": "2"},
        VideoOptions:  map[string]string{"crf": "23"},
        FormatOptions: map[string]string{"movflags": "+faststart"},
    }},
}`+"\n```\n")

	if len(sites) != 2 {
		t.Fatalf("got %d sites, want 2: %+v", len(sites), sites)
	}

	equal(t, "input site", sites[0], docsoptions.Site{
		File: "doc.md", Line: 5, Kind: docsoptions.InputSite, Path: "in.raw", Format: "rawvideo",
		Keys: []docsoptions.Key{{Dict: docsoptions.DemuxerDict, Name: "video_size", Value: "320x240", Line: 5}},
	})
	equal(t, "output site", sites[1], docsoptions.Site{
		File: "doc.md", Line: 7, Kind: docsoptions.OutputSite, Path: "out.mp4", VideoCodec: "libx264", AudioCodec: "aac",
		Keys: []docsoptions.Key{
			{Dict: docsoptions.EncoderDict, Name: "threads", Value: "2", Line: 11},
			{Dict: docsoptions.VideoDict, Name: "crf", Value: "23", Line: 12},
			{Dict: docsoptions.MuxerDict, Name: "movflags", Value: "+faststart", Line: 13},
		},
	})
}

func TestExtractFile_BuilderCalls(t *testing.T) {
	t.Parallel()

	sites := docsoptions.ExtractFile("doc.md", "```go\n"+`cmd := afmpeg.NewCommand(
    afmpeg.WithInput("tone.pcm", afmpeg.InputFormat("s16le"),
        afmpeg.DemuxerOption("sample_rate", "48000")),
    afmpeg.WithFilterComplex("[0:a]anull[a]"),
    afmpeg.WithOutput("stream.m3u8", afmpeg.Map("[a]"), afmpeg.OutputFormat("hls"),
        afmpeg.AudioCodec("aac"), afmpeg.AudioOption("b", "128000"),
        afmpeg.EncoderOption("threads", "2"), afmpeg.WithOption("g", "12"),
        afmpeg.SubtitleOption("x", "y"), afmpeg.VideoOption("bf", "0"),
        afmpeg.FormatOption("hls_time", "4")),
)`+"\n```\n")

	if len(sites) != 2 {
		t.Fatalf("got %d sites, want 2: %+v", len(sites), sites)
	}

	in, out := sites[0], sites[1]
	equal(t, "input", []any{in.Kind, in.Path, in.Format}, []any{docsoptions.InputSite, "tone.pcm", "s16le"})
	equal(t, "output", []any{out.Kind, out.Path, out.Format, out.AudioCodec},
		[]any{docsoptions.OutputSite, "stream.m3u8", "hls", "aac"})

	var dicts []docsoptions.Dict
	for _, k := range out.Keys {
		dicts = append(dicts, k.Dict)
	}

	equal(t, "dictionaries", dicts, []docsoptions.Dict{
		docsoptions.AudioDict, docsoptions.EncoderDict, docsoptions.EncoderDict,
		docsoptions.SubtitleDict, docsoptions.VideoDict, docsoptions.MuxerDict,
	})
}

func TestExtractFile_InlineSpansAndIgnoredText(t *testing.T) {
	t.Parallel()

	sites := docsoptions.ExtractFile("doc.md", strings.Join([]string{
		"| `-b:v 300k` | `Options{\"b\": \"300k\"}` |",
		"| `-movflags` | `FormatOptions{\"movflags\": \"+faststart\"}` and `Input.Options{\"probesize\": \"32\"}` |",
		"`WithOption` alone names nothing, and neither does a type:",
		"```go",
		"type Output struct {",
		"    Options       map[string]string",
		"    FormatOptions map[string]string",
		"}",
		`fmt.Println("afmpeg.VideoOption(\"crf\", \"23\")") // afmpeg.VideoOption("qp", "1")`,
		"```",
	}, "\n"))

	equal(t, "keys", keysOf(sites), []docsoptions.Key{
		{Dict: docsoptions.EncoderDict, Name: "b", Value: "300k", Line: 1},
		{Dict: docsoptions.MuxerDict, Name: "movflags", Value: "+faststart", Line: 2},
		{Dict: docsoptions.DemuxerDict, Name: "probesize", Value: "32", Line: 2},
	})

	for _, s := range sites {
		if s.Kind != docsoptions.Unscoped {
			t.Errorf("site at line %d is %v, want Unscoped", s.Line, s.Kind)
		}
	}
}

// The names afmpeg#10 removed from the docs, each in the shape it was written
// in, plus the reverse mistakes, must all be caught.
func TestCheck_CatchesWhatAfmpeg10Fixed(t *testing.T) {
	t.Parallel()

	sites := docsoptions.ExtractFile("doc.md", "```go\n"+`afmpeg.WithOutput("out/clip.mp4", afmpeg.VideoCodec("libopenh264"), afmpeg.WithOption("b:v", "300k"))
afmpeg.WithOutput("thumb.png", afmpeg.Map("[v]"), afmpeg.WithOption("frames:v", "1"))
afmpeg.Output{Path: "out.mp4", Options: map[string]string{"crf": "23", "movflags": "+faststart"}}
afmpeg.WithOutput("out.mp4", afmpeg.FormatOption("crf", "23"))
afmpeg.WithInput("in.mp4", afmpeg.DemuxerOption("hls_time", "4"))`+"\n```\n")

	var got []string

	for _, v := range docsoptions.Check(sites) {
		got = append(got, v.Key.Name)

		if !strings.HasPrefix(v.String(), "doc.md:") {
			t.Errorf("violation %q does not lead with its file", v)
		}
	}

	slices.Sort(got)
	equal(t, "caught", got, []string{"b:v", "crf", "frames:v", "hls_time", "movflags"})
}

func TestCheck_AcceptsWellPlacedNames(t *testing.T) {
	t.Parallel()

	sites := docsoptions.ExtractFile("doc.md", "```go\n"+`afmpeg.WithOutput("s.m3u8", afmpeg.VideoOption("g", "100"), afmpeg.EncoderOption("threads", "2"),
    afmpeg.FormatOption("hls_time", "4"), afmpeg.FormatOption("movflags", "+faststart"))
afmpeg.WithInput("f.yuv", afmpeg.DemuxerOption("video_size", "1280x720"))`+"\n```\n")

	if v := docsoptions.Check(sites); len(v) != 0 {
		t.Errorf("unexpected violations: %v", v)
	}
}

func TestDictString(t *testing.T) {
	t.Parallel()

	for d, want := range map[docsoptions.Dict]string{
		docsoptions.EncoderDict:  "encoder",
		docsoptions.VideoDict:    "video encoder",
		docsoptions.AudioDict:    "audio encoder",
		docsoptions.SubtitleDict: "subtitle encoder",
		docsoptions.MuxerDict:    "muxer",
		docsoptions.DemuxerDict:  "demuxer",
		docsoptions.Dict(99):     "Dict(99)",
	} {
		if got := d.String(); got != want {
			t.Errorf("Dict(%d).String() = %q, want %q", int(d), got, want)
		}
	}
}

func TestExtract_MissingSourceIsAnError(t *testing.T) {
	t.Parallel()

	if _, err := docsoptions.Extract(os.DirFS(t.TempDir())); err == nil {
		t.Error("want an error for a tree with no docs")
	}
}
