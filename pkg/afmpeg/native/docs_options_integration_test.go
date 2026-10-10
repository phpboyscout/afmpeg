package native_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/internal/docsoptions"
	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg/native"
)

// TestIntegration_DocumentedOptionsAreAccepted offers every option key the docs
// teach to the component the docs set it on, through a real driver, and requires
// the job to succeed. Since engine n9.0.1-2 an option nothing consumes fails the
// job, so this catches a wrong-but-well-formed name the lexical check in
// internal/docsoptions cannot (afmpeg#11 B).
//
// Gated on the AFMPEG_TEST_NATIVE_DRIVER* variables like the rest of this
// package: a site whose encoder or muxer no supplied driver carries is skipped,
// naming the driver it wants.
func TestIntegration_DocumentedOptionsAreAccepted(t *testing.T) {
	t.Parallel()

	sites, err := docsoptions.Extract(os.DirFS("../../.."))
	if err != nil {
		t.Fatal(err)
	}

	for _, site := range sites {
		t.Run(fmt.Sprintf("%s:%d", site.File, site.Line), func(t *testing.T) {
			t.Parallel()

			job := docsJob(site)
			if job.skip != "" {
				t.Skip(job.skip)
			}

			driver := integrationDriver(t, job.profile, job.gpl)

			rt, err := afmpeg.New(context.Background(), afmpeg.WithBackend(native.New(native.WithNativeBinary(driver))))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rt.Close(context.Background()) }()

			fs := afero.NewMemMapFs()
			for name, data := range job.files {
				if err := afero.WriteFile(fs, name, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			res, err := rt.RunJob(context.Background(), fs, job.cmd)
			if err == nil && res.ExitCode == 0 {
				return
			}

			t.Errorf("documented at %s:%d, keys %v: err %v, exit %d\n%s",
				site.File, site.Line, keyNames(site.Keys), err, res.ExitCode, res.Stderr)
		})
	}
}

// Which driver carries a component, per ffmpeg-wasi's variants reference.
// Anything absent is in every lean build.
var componentNeeds = map[string]struct {
	profile afmpeg.Profile
	gpl     bool
}{
	"libx264":    {afmpeg.ProfileLean, true},
	"libx265":    {afmpeg.ProfileFull, true},
	"libsvtav1":  {afmpeg.ProfileFull, false},
	"libopus":    {afmpeg.ProfileIntermediate, false},
	"libmp3lame": {afmpeg.ProfileIntermediate, false},
	"libvorbis":  {afmpeg.ProfileIntermediate, false},
	"libvpx":     {afmpeg.ProfileIntermediate, false},
	"libvpx-vp9": {afmpeg.ProfileIntermediate, false},
	"libwebp":    {afmpeg.ProfileIntermediate, false},
	"hls":        {afmpeg.ProfileIntermediate, false},
	"dash":       {afmpeg.ProfileIntermediate, false},
	"segment":    {afmpeg.ProfileIntermediate, false},
	"mpegts":     {afmpeg.ProfileIntermediate, false},
	"flv":        {afmpeg.ProfileIntermediate, false},
}

var profileOrder = map[afmpeg.Profile]int{afmpeg.ProfileLean: 0, afmpeg.ProfileIntermediate: 1, afmpeg.ProfileFull: 2}

const (
	rawWidth, rawHeight = 320, 240
	rawFrames           = 25
	pcmRate             = 48000
	pcmInputBytes       = 1 << 16
	rawInputFrames      = 2
	// In every build, lgpl and gpl alike.
	anyH264Encoder = "libopenh264"
)

type docsJobSpec struct {
	cmd     afmpeg.Command
	files   map[string][]byte
	profile afmpeg.Profile
	gpl     bool
	skip    string
}

func (j *docsJobSpec) require(component string) {
	n, ok := componentNeeds[component]
	if !ok {
		return
	}

	if profileOrder[n.profile] > profileOrder[j.profile] {
		j.profile = n.profile
	}

	j.gpl = j.gpl || n.gpl
}

func docsJob(site docsoptions.Site) docsJobSpec {
	byDict := map[docsoptions.Dict]map[string]string{}
	for _, k := range site.Keys {
		if byDict[k.Dict] == nil {
			byDict[k.Dict] = map[string]string{}
		}

		byDict[k.Dict][k.Name] = k.Value
	}

	switch {
	case byDict[docsoptions.SubtitleDict] != nil:
		return docsJobSpec{skip: "subtitle encoder options need a subtitle input, which this test does not synthesise"}
	case site.Kind == docsoptions.InputSite:
		return demuxerJob(site, byDict[docsoptions.DemuxerDict])
	case byDict[docsoptions.DemuxerDict] != nil:
		return docsJobSpec{skip: "demuxer options documented outside an input name no demuxer to offer them to"}
	default:
		return outputJob(site, byDict)
	}
}

func demuxerJob(site docsoptions.Site, opts map[string]string) docsJobSpec {
	size := pcmInputBytes
	cmd := afmpeg.Command{Inputs: []afmpeg.Input{{Path: "in.raw", Format: site.Format, Options: opts}}}

	if site.Format == "rawvideo" {
		// The demuxer rejects a partial final frame, so the input is sized to the
		// geometry the docs declare.
		frame, ok := rawFrameBytes(opts)
		if !ok {
			return docsJobSpec{skip: fmt.Sprintf("cannot size a rawvideo frame from %v", opts)}
		}

		size = frame * rawInputFrames
		cmd.FilterComplex = "[0:v]null[v]"
		cmd.Outputs = []afmpeg.Output{{Path: "out.mp4", Map: []string{"[v]"}, VideoCodec: anyH264Encoder}}
	} else {
		cmd.Outputs = []afmpeg.Output{{Path: "out.wav", Map: []string{"0:a"}, AudioCodec: afmpeg.CodecCopy}}
	}

	return docsJobSpec{
		cmd:     cmd,
		files:   map[string][]byte{"in.raw": make([]byte, size)},
		profile: afmpeg.ProfileLean,
	}
}

func outputJob(site docsoptions.Site, byDict map[docsoptions.Dict]map[string]string) docsJobSpec {
	out := afmpeg.Output{
		Path:          path.Base(site.Path),
		Format:        site.Format,
		VideoCodec:    site.VideoCodec,
		AudioCodec:    site.AudioCodec,
		Options:       byDict[docsoptions.EncoderDict],
		VideoOptions:  byDict[docsoptions.VideoDict],
		AudioOptions:  byDict[docsoptions.AudioDict],
		FormatOptions: byDict[docsoptions.MuxerDict],
	}

	if site.Path == "" {
		out.Path = "out.mp4"
	}

	if out.AudioOptions != nil && out.AudioCodec == "" {
		out.AudioCodec = "aac"
	}

	// An unscoped key names no codec; x264 carries every encoder-only name the
	// lexical check knows.
	if out.VideoCodec == "" && (out.AudioCodec == "" || out.VideoOptions != nil) {
		out.VideoCodec = "libx264"
	}

	job := docsJobSpec{files: map[string][]byte{}, profile: afmpeg.ProfileLean}

	var graph []string

	// The engine encodes only through a graph pad, so each stream passes a no-op filter.
	if out.VideoCodec != "" {
		graph = append(graph, fmt.Sprintf("[%d:v]null[v]", len(job.cmd.Inputs)))
		out.Map = append(out.Map, "[v]")
		job.cmd.Inputs = append(job.cmd.Inputs, rawVideoInput(job.files))
	}

	if out.AudioCodec != "" {
		graph = append(graph, fmt.Sprintf("[%d:a]anull[a]", len(job.cmd.Inputs)))
		out.Map = append(out.Map, "[a]")
		job.cmd.Inputs = append(job.cmd.Inputs, rawAudioInput(job.files))
	}

	for _, c := range []string{out.VideoCodec, out.AudioCodec, out.Format} {
		job.require(c)
	}

	job.cmd.FilterComplex = strings.Join(graph, ";")
	job.cmd.Outputs = []afmpeg.Output{out}

	return job
}

func rawVideoInput(files map[string][]byte) afmpeg.Input {
	const yuv420pFrame = rawWidth * rawHeight * 3 / 2

	files["in.yuv"] = bytes.Repeat([]byte{0x80}, yuv420pFrame*rawFrames)

	return afmpeg.Input{Path: "in.yuv", Format: "rawvideo", Options: map[string]string{
		"video_size":   fmt.Sprintf("%dx%d", rawWidth, rawHeight),
		"pixel_format": "yuv420p",
		"framerate":    fmt.Sprint(rawFrames),
	}}
}

func rawAudioInput(files map[string][]byte) afmpeg.Input {
	const s16Bytes = 2

	files["in.pcm"] = make([]byte, pcmRate*s16Bytes)

	return afmpeg.Input{Path: "in.pcm", Format: "s16le", Options: map[string]string{
		"sample_rate": fmt.Sprint(pcmRate),
		"ch_layout":   "mono",
	}}
}

// Bytes per pixel, doubled so yuv420p's 1.5 stays an integer.
var doubledBytesPerPixel = map[string]int{"yuv420p": 3, "gray": 2, "rgb24": 6, "rgba": 8}

func rawFrameBytes(opts map[string]string) (int, bool) {
	var w, h int
	if _, err := fmt.Sscanf(opts["video_size"], "%dx%d", &w, &h); err != nil {
		return 0, false
	}

	bpp, ok := doubledBytesPerPixel[opts["pixel_format"]]

	return w * h * bpp / 2, ok
}

func keyNames(keys []docsoptions.Key) []string {
	names := make([]string, 0, len(keys))
	for _, k := range keys {
		names = append(names, fmt.Sprintf("%s %s=%s", k.Dict, k.Name, k.Value))
	}

	return names
}
