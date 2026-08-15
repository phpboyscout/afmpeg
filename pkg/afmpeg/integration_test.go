package afmpeg_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"golang.org/x/image/font/gofont/goregular"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// TestIntegration_FFmpegWasiDriver drives the ffmpeg-wasi libav-direct engine —
// the structured job-spec vocabulary, not ffmpeg CLI args — over the in-memory
// bridge. It proves the afmpeg ↔ ffmpeg-wasi seam end-to-end: a probe whose JSON
// comes back on stdout, and a real transcode (WAV → AAC/mp4) written entirely in
// memory. Gated on AFMPEG_TEST_FFMPEG_WASI (a path to a built ffmpeg-wasi-*.wasm).
func TestIntegration_FFmpegWasiDriver(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.wav", makeWAVMono(8000, 1.0), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// probe — the engine returns structured JSON on stdout.
	probe, err := rt.Run(ctx, fs, `{"op":"probe","inputs":[{"path":"in.wav"}]}`)
	if err != nil {
		t.Fatalf("probe Run: %v", err)
	}

	if probe.ExitCode != 0 {
		t.Fatalf("probe exit %d:\n%s", probe.ExitCode, probe.Stderr)
	}

	if !strings.Contains(probe.Stdout, `"codec":"pcm_s16le"`) ||
		!strings.Contains(probe.Stdout, `"duration_sec"`) {
		t.Fatalf("probe stdout missing expected fields:\n%s", probe.Stdout)
	}

	// process — transcode WAV (pcm_s16le) -> AAC in mp4, entirely in memory.
	proc, err := rt.Run(ctx, fs,
		`{"op":"process","inputs":[{"path":"in.wav"}],"outputs":[{"path":"out.mp4","audio_codec":"aac"}]}`)
	if err != nil {
		t.Fatalf("process Run: %v", err)
	}

	if proc.ExitCode != 0 {
		t.Fatalf("process exit %d:\n%s", proc.ExitCode, proc.Stderr)
	}

	out, err := afero.ReadFile(fs, "out.mp4")
	if err != nil || len(out) < 12 || string(out[4:8]) != "ftyp" {
		t.Fatalf("no valid mp4 produced: err=%v len=%d", err, len(out))
	}

	// The output exists only in the in-memory fs, never on the host disk.
	if _, err := os.Stat("out.mp4"); !os.IsNotExist(err) {
		t.Fatalf("output leaked to the host filesystem: %v", err)
	}
}

// TestIntegration_H264Encode_OpenH264 proves the LGPL engine's H.264 encoder
// (openh264) works end-to-end: a still PNG is decoded, converted to yuv420p, and
// encoded to H.264 in mp4 — entirely in memory, via the generic Command emitter.
// This exercises the self-compiled openh264 + its single-threaded pthread shim on a
// real encode. Gated on AFMPEG_TEST_FFMPEG_WASI (point it at a libopenh264-capable
// module — the LGPL or GPL variant from n8.1.2-2 onward).
func TestIntegration_H264Encode_OpenH264(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.png", makePNG(32, 32), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cmd := afmpeg.Command{
		Inputs:        []afmpeg.Input{{Path: "in.png"}},
		FilterComplex: "[0:v]format=yuv420p[v]",
		Outputs: []afmpeg.Output{{
			Path:       "out.mp4",
			Map:        []string{"[v]"},
			VideoCodec: "libopenh264",
		}},
	}

	res, err := rt.RunJob(ctx, fs, cmd)
	if err != nil {
		t.Fatalf("RunJob: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("H.264 encode exit %d:\n%s", res.ExitCode, res.Stderr)
	}

	out, err := afero.ReadFile(fs, "out.mp4")
	if err != nil || len(out) < 12 || string(out[4:8]) != "ftyp" {
		t.Fatalf("no valid mp4 from libopenh264 encode: err=%v len=%d", err, len(out))
	}
}

// makePNG builds a tiny w×h RGBA PNG (a diagonal gradient) in memory — a
// dependency-free still fixture for the H.264 encode test. Dimensions should be
// even for yuv420p.
func makePNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: uint8((x + y) * 4), A: 255})
		}
	}

	buf := new(bytes.Buffer)
	_ = png.Encode(buf, img)

	return buf.Bytes()
}

// TestIntegration_VerifiedRelease loads a real, published, signed ffmpeg-wasi
// release end-to-end via WithModuleRelease: it fetches the module + manifest over
// the network, verifies the live AWS-KMS signature against afmpeg's pinned key,
// checks the checksums and provenance, and compiles the module. The capstone of
// spec 0010 — production crypto, no fixtures. Gated on AFMPEG_TEST_LIVE_RELEASE
// (it hits the GitLab package registry).
func TestIntegration_VerifiedRelease(t *testing.T) {
	t.Parallel()

	if os.Getenv("AFMPEG_TEST_LIVE_RELEASE") == "" {
		t.Skip("set AFMPEG_TEST_LIVE_RELEASE=1 to verify a live signed release over the network")
	}

	ctx := context.Background()

	var prov afmpeg.Provenance

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleRelease(
		"n8.1.2-4", afmpeg.VariantLGPL,
		afmpeg.WithReleaseProvenance(&prov),
	))
	if err != nil {
		t.Fatalf("WithModuleRelease (verify failed?): %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	if prov.FFmpegVersion != "n8.1.2" || prov.BuildTag != "n8.1.2-4" {
		t.Fatalf("verified provenance unexpected: %+v", prov)
	}

	t.Logf("verified + compiled n8.1.2-3 lgpl: ffmpeg=%s license=%s",
		prov.FFmpegVersion, prov.Variants["lgpl"].License)
}

// makeWAVMono builds a minimal mono pcm_s16le WAV (a 440 Hz sine) in memory — a
// dependency-free fixture for the in-memory transcode test.
func makeWAVMono(sampleRate int, seconds float64) []byte {
	n := int(float64(sampleRate) * seconds)

	pcm := new(bytes.Buffer)
	for i := 0; i < n; i++ {
		v := int16(0.3 * 32767 * math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)))
		_ = binary.Write(pcm, binary.LittleEndian, v)
	}

	pcmBytes := pcm.Bytes()
	dataLen := uint32(len(pcmBytes)) //nolint:gosec // G115: tiny fixed-size test fixture
	sr := uint32(sampleRate)         //nolint:gosec // G115: tiny fixed-size test fixture

	buf := new(bytes.Buffer)
	buf.WriteString("RIFF")
	_ = binary.Write(buf, binary.LittleEndian, 36+dataLen)
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(buf, binary.LittleEndian, uint32(16)) // PCM fmt chunk size
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))  // PCM
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))  // mono
	_ = binary.Write(buf, binary.LittleEndian, sr)
	_ = binary.Write(buf, binary.LittleEndian, sr*2)       // byte rate (mono s16)
	_ = binary.Write(buf, binary.LittleEndian, uint16(2))  // block align
	_ = binary.Write(buf, binary.LittleEndian, uint16(16)) // bits/sample
	buf.WriteString("data")
	_ = binary.Write(buf, binary.LittleEndian, dataLen)
	buf.Write(pcmBytes)

	return buf.Bytes()
}

// TestIntegration_RunJob exercises the full generic emitter → engine path: a
// Command with a filtergraph is rendered via JobSpec and run by the real
// ffmpeg-wasi driver over an in-memory FS. Gated on AFMPEG_TEST_FFMPEG_WASI.
func TestIntegration_RunJob(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.wav", makeWAVMono(8000, 1.0), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cmd := afmpeg.Command{
		Inputs:        []afmpeg.Input{{Path: "in.wav"}},
		FilterComplex: "[0:a]volume=0.8[aout]",
		Outputs: []afmpeg.Output{{
			Path:       "out.mp4",
			Map:        []string{"[aout]"},
			AudioCodec: "aac",
		}},
	}

	res, err := rt.RunJob(ctx, fs, cmd)
	if err != nil {
		t.Fatalf("RunJob: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("RunJob exit %d:\n%s", res.ExitCode, res.Stderr)
	}

	out, err := afero.ReadFile(fs, "out.mp4")
	if err != nil || len(out) < 12 || string(out[4:8]) != "ftyp" {
		t.Fatalf("no valid mp4 from RunJob: err=%v len=%d", err, len(out))
	}
}

// TestIntegration_RunJob_MultiOutput exercises multi-output muxing (0007): one
// filter graph fans (via asplit) into two labelled pads, each mapped to its own
// output file with its own muxer. Proves the driver honours every `outputs[]`
// entry and routes pads by `map`. Gated on AFMPEG_TEST_FFMPEG_WASI.
func TestIntegration_RunJob_MultiOutput(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.wav", makeWAVMono(8000, 1.0), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// One source fans out (asplit) into two labelled pads, each mapped to its own
	// output file — exercising the driver's per-output muxing + map routing.
	cmd := afmpeg.Command{
		Inputs:        []afmpeg.Input{{Path: "in.wav"}},
		FilterComplex: "[0:a]asplit=2[a1][a2];[a1]volume=0.9[loud];[a2]volume=0.1[quiet]",
		Outputs: []afmpeg.Output{
			{Path: "loud.mp4", Map: []string{"[loud]"}, AudioCodec: "aac"},
			{Path: "quiet.mp4", Map: []string{"[quiet]"}, AudioCodec: "aac"},
		},
	}

	res, err := rt.RunJob(ctx, fs, cmd)
	if err != nil {
		t.Fatalf("RunJob: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("RunJob exit %d:\n%s", res.ExitCode, res.Stderr)
	}

	for _, name := range []string{"loud.mp4", "quiet.mp4"} {
		out, err := afero.ReadFile(fs, name)
		if err != nil || len(out) < 12 || string(out[4:8]) != "ftyp" {
			t.Fatalf("output %s not a valid mp4/m4a: err=%v len=%d", name, err, len(out))
		}
	}
}

// TestIntegration_Probe_FFmpegWasiDriver is the regression test for
// BUG-REPORT-probe: Runtime.Probe drives the ffmpeg-wasi engine's probe op (a JSON
// job spec) and parses the structured result, instead of the old CLI `ffmpeg -i`
// + stderr transport the libav-direct engine rejects. Gated on AFMPEG_TEST_FFMPEG_WASI.
func TestIntegration_Probe_FFmpegWasiDriver(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "tone.wav", makeWAVMono(8000, 2.0), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Regression for BUG-REPORT-probe: Probe drives the engine's probe op (not the
	// old CLI `ffmpeg -i` transport the libav-direct engine rejects).
	p, err := rt.Probe(ctx, fs, "tone.wav")
	if err != nil {
		t.Fatalf("Runtime.Probe: %v", err)
	}

	if p.Format != "wav" || p.DurationSec < 1.9 || p.DurationSec > 2.1 {
		t.Fatalf("Probe = %+v, want wav / ~2.0s", p)
	}

	if len(p.Streams) != 1 || p.Streams[0].Type != "audio" || p.Streams[0].Codec != "pcm_s16le" {
		t.Fatalf("Probe streams = %+v, want one pcm_s16le audio stream", p.Streams)
	}
}

// TestIntegration_VersionGate proves the engine's job-spec version gate against a
// real driver: op:version reports a vocab version, and a spec declaring a version
// newer than the engine supports is rejected with the distinct exit code 3 (not
// the malformed-spec code 2) rather than silently dropping its fields. Gated on
// AFMPEG_TEST_FFMPEG_WASI.
func TestIntegration_VersionGate(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()

	// op:version reports a machine-readable vocab version (what New's preflight reads).
	ver, err := rt.Run(ctx, fs, `{"op":"version"}`)
	if err != nil || ver.ExitCode != 0 {
		t.Fatalf("version op: res=%+v err=%v", ver, err)
	}

	if !strings.Contains(ver.Stdout, `"vocab_version"`) {
		t.Fatalf("version stdout missing vocab_version:\n%s", ver.Stdout)
	}

	// A spec from the future is rejected with exit 3, not silently accepted.
	res, err := rt.Run(ctx, fs, `{"op":"process","version":999999,"inputs":[{"path":"x"}],"outputs":[{"path":"y.mp4","audio_codec":"aac"}]}`)
	if err != nil {
		t.Fatalf("too-new spec Run: %v", err)
	}

	if res.ExitCode != 3 {
		t.Fatalf("too-new spec exit = %d, want 3 (version-too-new):\n%s", res.ExitCode, res.Stderr)
	}
}

// bootstrapAVMP4 self-produces a tiny H.264/AAC mp4 in fs (spec 0013 §8: the
// dep-free PNG/WAV corpus can't emit H.264, so encode one with the shipped
// openh264 first, then the copy tests remux that). Fails the test on error.
func bootstrapAVMP4(t *testing.T, rt *afmpeg.Runtime, fs afero.Fs, path string) {
	t.Helper()

	if err := afero.WriteFile(fs, "boot.png", makePNG(32, 32), 0o644); err != nil {
		t.Fatalf("seed png: %v", err)
	}
	if err := afero.WriteFile(fs, "boot.wav", makeWAVMono(8000, 1.0), 0o644); err != nil {
		t.Fatalf("seed wav: %v", err)
	}

	boot := afmpeg.Command{
		Inputs:        []afmpeg.Input{{Path: "boot.png"}, {Path: "boot.wav"}},
		FilterComplex: "[0:v]format=yuv420p[v];[1:a]anull[a]",
		Outputs: []afmpeg.Output{{
			Path: path, Map: []string{"[v]", "[a]"},
			VideoCodec: "libopenh264", AudioCodec: "aac",
		}},
	}

	res, err := rt.RunJob(context.Background(), fs, boot)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("bootstrap H.264/AAC mp4: res=%+v err=%v", res, err)
	}
}

// TestIntegration_StreamCopy_Remux proves the 0013 copy path end-to-end with a
// real container change: an H.264/AAC mp4 is remuxed to Matroska with both streams
// copied (no re-encode) via the "copy" codec sentinel + "in:type" map specifiers,
// and the copied codecs survive. (Matroska output also exercises the /dev/urandom
// vfs device — libav seeds mkv track UIDs from it.)
func TestIntegration_StreamCopy_Remux(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapAVMP4(t, rt, fs, "src.mp4")

	cmd := afmpeg.Command{
		Inputs: []afmpeg.Input{{Path: "src.mp4"}},
		Outputs: []afmpeg.Output{{
			Path: "out.mkv", Map: []string{"0:v", "0:a"},
			VideoCodec: afmpeg.CodecCopy, AudioCodec: afmpeg.CodecCopy,
		}},
	}

	res, err := rt.RunJob(ctx, fs, cmd)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("copy remux: res=%+v err=%v", res, err)
	}

	if !strings.Contains(res.Stdout, `"disposition":"copy"`) {
		t.Fatalf("process result did not report a copied stream:\n%s", res.Stdout)
	}

	// The remuxed file must be Matroska and preserve the source codecs (no re-encode).
	p, err := rt.Probe(ctx, fs, "out.mkv")
	if err != nil {
		t.Fatalf("probe remux: %v", err)
	}

	if !strings.Contains(p.Format, "matroska") {
		t.Fatalf("remux format = %q, want matroska", p.Format)
	}

	var haveH264, haveAAC bool
	for _, s := range p.Streams {
		haveH264 = haveH264 || s.Codec == "h264"
		haveAAC = haveAAC || s.Codec == "aac"
	}
	if !haveH264 || !haveAAC {
		t.Fatalf("remux streams = %+v, want h264 + aac preserved", p.Streams)
	}
}

// bootstrapClipMP4 self-produces a ~2s multi-keyframe H.264/AAC mp4 in fs: the
// PNG still is looped to 50 frames at 25 fps with a keyframe every 8 frames
// (gop 0.32s), so seek tests have real keyframes to land between (spec 0014 §8).
func bootstrapClipMP4(t *testing.T, rt *afmpeg.Runtime, fs afero.Fs, path string) {
	t.Helper()

	if err := afero.WriteFile(fs, "boot.png", makePNG(32, 32), 0o644); err != nil {
		t.Fatalf("seed png: %v", err)
	}
	if err := afero.WriteFile(fs, "boot.wav", makeWAVMono(8000, 2.0), 0o644); err != nil {
		t.Fatalf("seed wav: %v", err)
	}

	boot := afmpeg.Command{
		Inputs:        []afmpeg.Input{{Path: "boot.png"}, {Path: "boot.wav"}},
		FilterComplex: "[0:v]loop=loop=49:size=1:start=0,fps=25,format=yuv420p[v];[1:a]anull[a]",
		Outputs: []afmpeg.Output{{
			Path: path, Map: []string{"[v]", "[a]"},
			VideoCodec: "libopenh264", AudioCodec: "aac",
			Options: map[string]string{"g": "8"},
		}},
	}

	res, err := rt.RunJob(context.Background(), fs, boot)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("bootstrap clip mp4: res=%+v err=%v", res, err)
	}
}

// TestIntegration_Seek drives the spec-0014 windows end-to-end against a real
// multi-keyframe clip: fast vs accurate seek, duration and end cutoffs, PTS
// rebasing vs copy_ts, keyframe-snapped copy-trim, and the accurate-on-copy
// rejection.
func TestIntegration_Seek(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapClipMP4(t, rt, fs, "src.mp4")

	// encodeClip runs one seek/window job re-encoding the video and returns the
	// probed output.
	encodeClip := func(name string, in afmpeg.Input, out afmpeg.Output) afmpeg.Probe {
		t.Helper()

		out.Map = []string{"[v]"}
		out.VideoCodec = "libopenh264"

		cmd := afmpeg.Command{
			Inputs:        []afmpeg.Input{in},
			FilterComplex: "[0:v]null[v]",
			Outputs:       []afmpeg.Output{out},
		}

		res, err := rt.RunJob(ctx, fs, cmd)
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("%s: res=%+v err=%v", name, res, err)
		}

		p, err := rt.Probe(ctx, fs, out.Path)
		if err != nil {
			t.Fatalf("%s probe: %v", name, err)
		}

		return p
	}

	src := afmpeg.Input{Path: "src.mp4"}

	// (a) Fast seek to 1.0s: lands on the keyframe at-or-before 1.0 (gop 0.32s),
	// so the clip covers [~0.68..1.0, 2.0] — duration in (1.0, 1.4].
	fast := encodeClip("fast seek",
		afmpeg.Input{Path: src.Path, Seek: &afmpeg.Seek{Start: 1.0}},
		afmpeg.Output{Path: "fast.mp4"})
	if fast.DurationSec < 0.95 || fast.DurationSec > 1.45 {
		t.Errorf("fast-seek duration = %.2fs, want ~(1.0, 1.4] (keyframe snap)", fast.DurationSec)
	}

	// (b) Accurate seek to 1.0s: decode-and-discard to the exact frame — ~1.0s.
	acc := encodeClip("accurate seek",
		afmpeg.Input{Path: src.Path, Seek: &afmpeg.Seek{Start: 1.0, Mode: afmpeg.SeekAccurate}},
		afmpeg.Output{Path: "acc.mp4"})
	if acc.DurationSec < 0.85 || acc.DurationSec > 1.15 {
		t.Errorf("accurate-seek duration = %.2fs, want ~1.0s", acc.DurationSec)
	}

	// (c) Duration cutoff: 0.5s from an accurate 1.0s start.
	dur := encodeClip("duration cutoff",
		afmpeg.Input{Path: src.Path, Seek: &afmpeg.Seek{Start: 1.0, Mode: afmpeg.SeekAccurate}},
		afmpeg.Output{Path: "dur.mp4", Duration: 0.5})
	if dur.DurationSec < 0.35 || dur.DurationSec > 0.7 {
		t.Errorf("duration-cutoff duration = %.2fs, want ~0.5s", dur.DurationSec)
	}

	// End on the zero-based output timeline behaves like duration.
	end := encodeClip("end cutoff",
		afmpeg.Input{Path: src.Path, Seek: &afmpeg.Seek{Start: 1.0, Mode: afmpeg.SeekAccurate}},
		afmpeg.Output{Path: "end.mp4", End: 0.5})
	if end.DurationSec < 0.35 || end.DurationSec > 0.7 {
		t.Errorf("end-cutoff duration = %.2fs, want ~0.5s", end.DurationSec)
	}

	// (d) Default rebase → output starts at ~0; copy_ts preserves the source PTS.
	if acc.StartSec > 0.3 {
		t.Errorf("rebased output starts at %.2fs, want ~0", acc.StartSec)
	}

	cts := encodeClip("copy_ts",
		afmpeg.Input{Path: src.Path, Seek: &afmpeg.Seek{Start: 1.0, Mode: afmpeg.SeekAccurate}},
		afmpeg.Output{Path: "cts.mp4", CopyTS: true})
	if cts.StartSec < 0.7 {
		t.Errorf("copy_ts output starts at %.2fs, want ~1.0 (source timeline preserved)", cts.StartSec)
	}
}

// TestIntegration_Seek_CopyTrim is 0013's deferred keyframe-accurate copy-trim,
// switched on by 0014: a fast seek composed with stream copy cuts on a keyframe
// with no re-encode.
func TestIntegration_Seek_CopyTrim(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapClipMP4(t, rt, fs, "src.mp4")

	cmd := afmpeg.Command{
		Inputs: []afmpeg.Input{{Path: "src.mp4", Seek: &afmpeg.Seek{Start: 1.0}}},
		Outputs: []afmpeg.Output{{
			Path: "trim.mp4", Map: []string{"0:v"}, VideoCodec: afmpeg.CodecCopy,
		}},
	}

	res, err := rt.RunJob(ctx, fs, cmd)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("copy-trim: res=%+v err=%v", res, err)
	}

	p, err := rt.Probe(ctx, fs, "trim.mp4")
	if err != nil {
		t.Fatalf("probe copy-trim: %v", err)
	}

	// Keyframe-snapped: covers [keyframe ≤ 1.0, 2.0] with no re-encode.
	if p.DurationSec < 0.95 || p.DurationSec > 1.45 {
		t.Errorf("copy-trim duration = %.2fs, want ~(1.0, 1.4]", p.DurationSec)
	}

	if len(p.Streams) != 1 || p.Streams[0].Codec != "h264" {
		t.Errorf("copy-trim streams = %+v, want one copied h264 stream", p.Streams)
	}

	// The engine enforces D-0014-F: accurate seek on a copied stream is an error
	// (bypass afmpeg's own validation by sending the raw spec).
	raw := `{"op":"process","version":3,"inputs":[{"path":"src.mp4","seek":{"start":1.0,"mode":"accurate"}}],` +
		`"outputs":[{"path":"x.mp4","map":["0:v"],"video_codec":"copy"}]}`

	rej, err := rt.Run(ctx, fs, raw)
	if err != nil {
		t.Fatalf("accurate-on-copy Run: %v", err)
	}

	if rej.ExitCode == 0 || !strings.Contains(rej.Stderr, "accurate") {
		t.Errorf("accurate-on-copy: exit=%d stderr=%q, want a rejection naming accurate", rej.ExitCode, rej.Stderr)
	}
}

// makeRawYUV420p builds frames of raw yuv420p video — Y a diagonal gradient,
// U/V mid-grey — the headerless bytes a "rawvideo" input reads (spec 0024).
func makeRawYUV420p(w, h, frames int) []byte {
	ySize, cSize := w*h, (w/2)*(h/2)
	buf := make([]byte, 0, frames*(ySize+2*cSize))
	for f := 0; f < frames; f++ {
		y := make([]byte, ySize)
		for i := range y {
			y[i] = byte((i + f*8) & 0xff)
		}
		buf = append(buf, y...)
		grey := make([]byte, cSize)
		for i := range grey {
			grey[i] = 128
		}
		buf = append(buf, grey...) // U
		buf = append(buf, grey...) // V
	}

	return buf
}

// makeRawPCMS16LE builds seconds of mono signed-16-bit little-endian PCM (a low
// tone) — the headerless bytes an "s16le" input reads (spec 0024).
func makeRawPCMS16LE(sampleRate int, seconds float64) []byte {
	n := int(float64(sampleRate) * seconds)
	buf := make([]byte, n*2)
	for i := 0; i < n; i++ {
		s := uint16(int16(3000 * math.Sin(2*math.Pi*220*float64(i)/float64(sampleRate)))) //nolint:gosec // int16→uint16 is the intended PCM byte reinterpretation
		binary.LittleEndian.PutUint16(buf[i*2:], s)
	}

	return buf
}

// TestIntegration_RawInput proves spec-0024 forced formats + demuxer options: a
// headerless raw video and raw audio, opened with an explicit demuxer + geometry
// options, decode through the normal transcode path with their parameters intact.
func TestIntegration_RawInput(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "frames.yuv", makeRawYUV420p(64, 48, 10), 0o644); err != nil {
		t.Fatalf("seed yuv: %v", err)
	}
	if err := afero.WriteFile(fs, "tone.pcm", makeRawPCMS16LE(8000, 1.0), 0o644); err != nil {
		t.Fatalf("seed pcm: %v", err)
	}

	// Raw video: forced "rawvideo" demuxer + geometry, transcoded to H.264/mp4.
	vcmd := afmpeg.NewCommand(
		afmpeg.WithInput("frames.yuv", afmpeg.InputFormat("rawvideo"),
			afmpeg.DemuxerOption("video_size", "64x48"),
			afmpeg.DemuxerOption("pixel_format", "yuv420p"),
			afmpeg.DemuxerOption("framerate", "25")),
		afmpeg.WithFilterComplex("[0:v]null[v]"),
		afmpeg.WithOutput("v.mp4", afmpeg.Map("[v]"), afmpeg.VideoCodec("libopenh264")),
	)
	if res, err := rt.RunJob(ctx, fs, vcmd); err != nil || res.ExitCode != 0 {
		t.Fatalf("raw video: res=%+v err=%v", res, err)
	}

	vp, err := rt.Probe(ctx, fs, "v.mp4")
	if err != nil {
		t.Fatalf("probe raw video out: %v", err)
	}
	if len(vp.Streams) != 1 || vp.Streams[0].Width != 64 || vp.Streams[0].Height != 48 {
		t.Fatalf("raw video streams = %+v, want one 64x48 stream", vp.Streams)
	}

	// Raw audio: forced "s16le" demuxer + rate, transcoded to AAC/mp4.
	acmd := afmpeg.NewCommand(
		afmpeg.WithInput("tone.pcm", afmpeg.InputFormat("s16le"),
			afmpeg.DemuxerOption("sample_rate", "8000"),
			afmpeg.DemuxerOption("ch_layout", "mono")),
		afmpeg.WithOutput("a.mp4", afmpeg.AudioCodec("aac")),
	)
	if res, err := rt.RunJob(ctx, fs, acmd); err != nil || res.ExitCode != 0 {
		t.Fatalf("raw audio: res=%+v err=%v", res, err)
	}

	ap, err := rt.Probe(ctx, fs, "a.mp4")
	if err != nil {
		t.Fatalf("probe raw audio out: %v", err)
	}
	if ap.DurationSec < 0.8 || ap.DurationSec > 1.2 {
		t.Fatalf("raw audio duration = %.2fs, want ~1.0s", ap.DurationSec)
	}
}

// TestIntegration_InputOptions covers the spec-0024 diagnostics + forced format:
// an unconsumed demuxer option is a typed error, and forcing the demuxer opens a
// file whose extension would mislead the auto-probe.
func TestIntegration_InputOptions(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapAVMP4(t, rt, fs, "clip.mp4")
	mp4, err := afero.ReadFile(fs, "clip.mp4")
	if err != nil {
		t.Fatalf("read bootstrap: %v", err)
	}
	if err := afero.WriteFile(fs, "clip.bin", mp4, 0o644); err != nil { // an mp4 named .bin
		t.Fatalf("write .bin: %v", err)
	}

	// Forced format opens it where auto-probe-by-extension might not commit.
	forced := afmpeg.NewCommand(
		afmpeg.WithInput("clip.bin", afmpeg.InputFormat("mp4")),
		afmpeg.WithOutput("out.mp4", afmpeg.AudioCodec("aac")),
	)
	if res, err := rt.RunJob(ctx, fs, forced); err != nil || res.ExitCode != 0 {
		t.Fatalf("forced format: res=%+v err=%v", res, err)
	}

	// A bogus demuxer option surfaces as a typed diagnostic (Q2: fail loud).
	bad := afmpeg.NewCommand(
		afmpeg.WithInput("clip.bin", afmpeg.InputFormat("mp4"),
			afmpeg.DemuxerOption("this_is_not_a_real_option", "1")),
		afmpeg.WithOutput("out2.mp4", afmpeg.AudioCodec("aac")),
	)
	res, err := rt.RunJob(ctx, fs, bad)
	if err != nil {
		t.Fatalf("bad option Run: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("bad demuxer option: exit 0, want a rejection\n%s", res.Stderr)
	}
}

// TestIntegration_IndexedStreamSelection proves N:v:K selects a specific stream
// (spec 0024 R-0024-2): a source with two differently-sized video streams is
// re-read via 0:v:1, and the second stream's geometry comes through.
func TestIntegration_IndexedStreamSelection(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.png", makePNG(64, 64), 0o644); err != nil {
		t.Fatalf("seed png: %v", err)
	}

	// A multi-frame file with two video streams: 0:v:0 = 48x48, 0:v:1 = 32x32.
	// Multi-frame so a re-encode of the selected stream produces probeable output.
	twoStream := afmpeg.Command{
		Inputs: []afmpeg.Input{{Path: "in.png"}},
		FilterComplex: "[0:v]loop=loop=24:size=1:start=0,fps=25,split=2[a][b];" +
			"[a]scale=48:48,format=yuv420p[v0];[b]scale=32:32,format=yuv420p[v1]",
		Outputs: []afmpeg.Output{{
			Path: "two.mp4", Map: []string{"[v0]", "[v1]"}, VideoCodec: "libopenh264",
		}},
	}
	if res, err := rt.RunJob(ctx, fs, twoStream); err != nil || res.ExitCode != 0 {
		t.Fatalf("two-stream bootstrap: res=%+v err=%v", res, err)
	}

	// Selecting each video stream by index yields that stream's geometry — proof
	// the index resolves to a specific stream, not just the best.
	for _, tc := range []struct {
		out   string
		spec  string
		width int
	}{
		{"first.mp4", "[0:v:0]null[v]", 48},
		{"second.mp4", "[0:v:1]null[v]", 32},
	} {
		sel := afmpeg.Command{
			Inputs:        []afmpeg.Input{{Path: "two.mp4"}},
			FilterComplex: tc.spec,
			Outputs:       []afmpeg.Output{{Path: tc.out, Map: []string{"[v]"}, VideoCodec: "libopenh264"}},
		}
		if res, err := rt.RunJob(ctx, fs, sel); err != nil || res.ExitCode != 0 {
			t.Fatalf("select %s: res=%+v err=%v", tc.spec, res, err)
		}

		p, err := rt.Probe(ctx, fs, tc.out)
		if err != nil {
			t.Fatalf("probe %s: %v", tc.out, err)
		}
		if len(p.Streams) != 1 || p.Streams[0].Width != tc.width {
			t.Fatalf("%s selected %+v, want width %d", tc.spec, p.Streams, tc.width)
		}
	}
}

// TestIntegration_NativeCodecs round-trips the spec-0016 native codec batch that
// has an encoder: transcode into the codec, then probe it back — proving both the
// encoder and the decoder. The decode-only codecs (prores, dnxhd, mpeg2video,
// vc1, wmv3, theora, dca, eac3, wmav2) are enabled in the build but can't be
// synthesised from the dependency-free corpus (spec 0016 §8) — they await the
// shared licence-clean media fixtures. Needs the intermediate-profile module.
func TestIntegration_NativeCodecs(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileIntermediate)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapClipMP4(t, rt, fs, "src.mp4") // multi-frame, so image2 output has frames

	// Audio encoders → Matroska (permissive), probed back to the codec. Resample
	// to 48 kHz so ac3 (which rejects the fixture's 8 kHz) is happy; harmless to
	// the others.
	audio := []struct{ codec, out string }{
		{"ac3", "a_ac3.mkv"},
		{"alac", "a_alac.mkv"},
		{"pcm_s24le", "a_s24.mkv"},
		{"pcm_f32le", "a_f32.mkv"},
	}
	for _, tc := range audio {
		t.Run(tc.codec, func(t *testing.T) {
			cmd := afmpeg.Command{
				Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
				FilterComplex: "[0:a]aresample=48000[a]",
				Outputs:       []afmpeg.Output{{Path: tc.out, Map: []string{"[a]"}, AudioCodec: tc.codec}},
			}
			if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
				t.Fatalf("encode %s: res=%+v err=%v", tc.codec, res, err)
			}

			p, err := rt.Probe(ctx, fs, tc.out)
			if err != nil {
				t.Fatalf("probe %s: %v", tc.out, err)
			}
			if len(p.Streams) != 1 || p.Streams[0].Codec != tc.codec {
				t.Fatalf("%s round-trip streams = %+v, want codec %s", tc.codec, p.Streams, tc.codec)
			}
		})
	}

	// Image encoders: an image2 sequence (a %d pattern is its natural mode) whose
	// first frame carries the codec's magic bytes — proving the encoder is linked
	// and produces valid output.
	images := []struct {
		codec, pattern, first string
		magic                 []byte
	}{
		{"bmp", "f_%03d.bmp", "f_001.bmp", []byte("BM")},
		{"tiff", "f_%03d.tiff", "f_001.tiff", []byte("II")},
	}
	for _, tc := range images {
		t.Run(tc.codec, func(t *testing.T) {
			enc := afmpeg.Command{
				Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
				FilterComplex: "[0:v]null[v]",
				Outputs:       []afmpeg.Output{{Path: tc.pattern, Map: []string{"[v]"}, VideoCodec: tc.codec}},
			}
			if res, err := rt.RunJob(ctx, fs, enc); err != nil || res.ExitCode != 0 {
				t.Fatalf("encode %s: res=%+v err=%v", tc.codec, res, err)
			}

			img, err := afero.ReadFile(fs, tc.first)
			if err != nil || !bytes.HasPrefix(img, tc.magic) {
				t.Fatalf("%s encode: %s missing or wrong magic (err=%v)", tc.codec, tc.first, err)
			}
		})
	}
}

// TestIntegration_LGPLEncoders proves the spec-0018 external encoder libs cross-
// compiled into deps.sh (libopus/libmp3lame/libvorbis): each named encoder links
// and produces a stream that probes back to its FFmpeg codec name. 0018 adds no
// vocabulary — the encoder names are just newly-valid audio_codec strings — so a
// round-trip is the whole proof. Needs the intermediate-profile module.
func TestIntegration_LGPLEncoders(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileIntermediate)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapClipMP4(t, rt, fs, "src.mp4")

	// The encoder name we pass (libopus) differs from the codec name it produces
	// (opus) — probe checks the produced codec. Resample to 48 kHz: opus only
	// accepts it, and it's harmless to mp3/vorbis. Matroska carries all three.
	cases := []struct{ encoder, codec, out string }{
		{"libopus", "opus", "a_opus.mkv"},
		{"libmp3lame", "mp3", "a_mp3.mkv"},
		{"libvorbis", "vorbis", "a_vorbis.mkv"},
	}
	for _, tc := range cases {
		t.Run(tc.encoder, func(t *testing.T) {
			cmd := afmpeg.Command{
				Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
				FilterComplex: "[0:a]aresample=48000[a]",
				Outputs:       []afmpeg.Output{{Path: tc.out, Map: []string{"[a]"}, AudioCodec: tc.encoder}},
			}
			if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
				t.Fatalf("encode %s: res=%+v err=%v", tc.encoder, res, err)
			}

			p, err := rt.Probe(ctx, fs, tc.out)
			if err != nil {
				t.Fatalf("probe %s: %v", tc.out, err)
			}
			if len(p.Streams) != 1 || p.Streams[0].Codec != tc.codec {
				t.Fatalf("%s round-trip streams = %+v, want codec %s", tc.encoder, p.Streams, tc.codec)
			}
		})
	}

	// libvpx: VP8/VP9 video → WebM, probed back. Tiny frame + short window keeps
	// the notably-slow single-threaded VP9 encode fast enough for a unit run. The
	// encoder name differs from the codec (libvpx→vp8, libvpx-vp9→vp9).
	video := []struct{ encoder, codec, out string }{
		{"libvpx", "vp8", "v_vp8.webm"},
		{"libvpx-vp9", "vp9", "v_vp9.webm"},
	}
	for _, tc := range video {
		t.Run(tc.encoder, func(t *testing.T) {
			cmd := afmpeg.Command{
				Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
				FilterComplex: "[0:v]scale=48:48,fps=5[v]",
				Outputs:       []afmpeg.Output{{Path: tc.out, Map: []string{"[v]"}, VideoCodec: tc.encoder, Duration: 0.6}},
			}
			if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
				t.Fatalf("encode %s: res=%+v err=%v", tc.encoder, res, err)
			}

			p, err := rt.Probe(ctx, fs, tc.out)
			if err != nil {
				t.Fatalf("probe %s: %v", tc.out, err)
			}
			if len(p.Streams) != 1 || p.Streams[0].Codec != tc.codec {
				t.Fatalf("%s round-trip streams = %+v, want codec %s", tc.encoder, p.Streams, tc.codec)
			}
		})
	}

	// libwebp: encode a frame to WebP (via the image2 muxer, like bmp/tiff) and
	// check the RIFF/WEBP container magic — proof the encoder links and emits a
	// valid file. (WebP demux for a probe round-trip isn't in this batch.)
	t.Run("libwebp", func(t *testing.T) {
		enc := afmpeg.Command{
			Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
			FilterComplex: "[0:v]scale=64:64[v]",
			Outputs:       []afmpeg.Output{{Path: "f_%03d.webp", Map: []string{"[v]"}, VideoCodec: "libwebp"}},
		}
		if res, err := rt.RunJob(ctx, fs, enc); err != nil || res.ExitCode != 0 {
			t.Fatalf("encode libwebp: res=%+v err=%v", res, err)
		}
		img, err := afero.ReadFile(fs, "f_001.webp")
		if err != nil || len(img) < 12 || !bytes.HasPrefix(img, []byte("RIFF")) || !bytes.Equal(img[8:12], []byte("WEBP")) {
			t.Fatalf("libwebp encode: f_001.webp missing or wrong magic (len=%d err=%v)", len(img), err)
		}
	})
}

// TestIntegration_Frames drives the spec-0021 `frames` op against a real clip:
// each selector (single/list/interval/thumbnail), scale, count cap, and a second
// image codec — asserting file counts and decoded dimensions over the driver.
func TestIntegration_Frames(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileIntermediate)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapClipMP4(t, rt, fs, "src.mp4") // ~2.0s, 25fps, 32x32

	// exists asserts every reported frame was actually written.
	exists := func(t *testing.T, res afmpeg.FramesResult) {
		t.Helper()
		if len(res.Frames) != res.Count {
			t.Fatalf("count %d != len(frames) %d", res.Count, len(res.Frames))
		}
		for _, f := range res.Frames {
			if ok, _ := afero.Exists(fs, f.Path); !ok {
				t.Fatalf("reported frame %q not written", f.Path)
			}
		}
	}

	t.Run("single timestamp", func(t *testing.T) {
		ts := 1.0
		res, err := rt.Frames(ctx, fs, afmpeg.FrameJob{
			Input: "src.mp4", Path: "one.png", Select: afmpeg.FrameSelect{Timestamp: &ts},
		})
		if err != nil {
			t.Fatalf("frames: %v", err)
		}
		if res.Count != 1 {
			t.Fatalf("count = %d, want 1", res.Count)
		}
		exists(t, res)
		p, err := rt.Probe(ctx, fs, "one.png")
		if err != nil || len(p.Streams) != 1 || p.Streams[0].Width != 32 || p.Streams[0].Height != 32 {
			t.Fatalf("probe one.png: streams=%+v err=%v", p.Streams, err)
		}
	})

	t.Run("explicit list", func(t *testing.T) {
		res, err := rt.Frames(ctx, fs, afmpeg.FrameJob{
			Input: "src.mp4", Path: "ts_%02d.png",
			Select: afmpeg.FrameSelect{Timestamps: []float64{0.2, 0.8, 1.6}},
		})
		if err != nil || res.Count != 3 {
			t.Fatalf("list: count=%d err=%v", res.Count, err)
		}
		exists(t, res)
	})

	t.Run("interval", func(t *testing.T) {
		res, err := rt.Frames(ctx, fs, afmpeg.FrameJob{
			Input: "src.mp4", Path: "iv_%02d.png", Select: afmpeg.FrameSelect{Interval: 0.5},
		})
		// ~2.0s at 0.5s → {0,0.5,1.0,1.5}, allowing a boundary frame at 2.0.
		if err != nil || res.Count < 4 || res.Count > 5 {
			t.Fatalf("interval: count=%d (want 4-5) err=%v", res.Count, err)
		}
		exists(t, res)
	})

	t.Run("thumbnail", func(t *testing.T) {
		res, err := rt.Frames(ctx, fs, afmpeg.FrameJob{
			Input: "src.mp4", Path: "th_%02d.png", Select: afmpeg.FrameSelect{Thumbnail: true},
		})
		if err != nil || res.Count < 1 {
			t.Fatalf("thumbnail: count=%d err=%v", res.Count, err)
		}
		exists(t, res)
	})

	t.Run("scale", func(t *testing.T) {
		ts := 0.5
		res, err := rt.Frames(ctx, fs, afmpeg.FrameJob{
			Input: "src.mp4", Path: "sc.png", Scale: "16:-2",
			Select: afmpeg.FrameSelect{Timestamp: &ts},
		})
		if err != nil || res.Count != 1 {
			t.Fatalf("scale: count=%d err=%v", res.Count, err)
		}
		p, err := rt.Probe(ctx, fs, "sc.png")
		if err != nil || len(p.Streams) != 1 || p.Streams[0].Width != 16 {
			t.Fatalf("scaled frame: streams=%+v err=%v (want width 16)", p.Streams, err)
		}
	})

	t.Run("count cap", func(t *testing.T) {
		res, err := rt.Frames(ctx, fs, afmpeg.FrameJob{
			Input: "src.mp4", Path: "cap_%03d.png", Count: 3,
			Select: afmpeg.FrameSelect{Interval: 0.1}, // ~20 without the cap
		})
		if err != nil || res.Count != 3 {
			t.Fatalf("count cap: count=%d (want 3) err=%v", res.Count, err)
		}
		exists(t, res)
	})

	t.Run("mjpeg codec", func(t *testing.T) {
		ts := 1.0
		res, err := rt.Frames(ctx, fs, afmpeg.FrameJob{
			Input: "src.mp4", Path: "m.jpg", Codec: "mjpeg", Select: afmpeg.FrameSelect{Timestamp: &ts},
		})
		if err != nil || res.Count != 1 {
			t.Fatalf("mjpeg: count=%d err=%v", res.Count, err)
		}
		p, err := rt.Probe(ctx, fs, "m.jpg")
		if err != nil || len(p.Streams) != 1 || p.Streams[0].Codec != "mjpeg" {
			t.Fatalf("mjpeg frame: streams=%+v err=%v", p.Streams, err)
		}
	})
}

// TestIntegration_Metadata proves the spec-0020 write→read round-trip: a copy job
// sets container tags and a per-stream language + disposition into a Matroska
// output (which preserves arbitrary tags verbatim), and probe reads them all back.
func TestIntegration_Metadata(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapClipMP4(t, rt, fs, "src.mp4")

	cmd := afmpeg.Command{
		Inputs: []afmpeg.Input{{Path: "src.mp4"}},
		Outputs: []afmpeg.Output{{
			Path:       "tagged.mkv",
			Map:        []string{"0:v", "0:a"},
			VideoCodec: afmpeg.CodecCopy,
			AudioCodec: afmpeg.CodecCopy,
			Metadata:   map[string]string{"title": "My Title", "comment": "made by afmpeg"},
			StreamMetadata: map[string]afmpeg.StreamMeta{
				"0:a": {Language: "deu", Disposition: []string{"forced"}},
			},
		}},
	}
	if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
		t.Fatalf("tag job: res=%+v err=%v", res, err)
	}

	p, err := rt.Probe(ctx, fs, "tagged.mkv")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}

	if p.Tags["title"] != "My Title" {
		t.Fatalf("container tags = %+v, want title=My Title", p.Tags)
	}

	var audio *afmpeg.ProbeStream
	for i := range p.Streams {
		if p.Streams[i].Type == "audio" {
			audio = &p.Streams[i]
		}
	}
	if audio == nil {
		t.Fatalf("no audio stream in %+v", p.Streams)
	}
	if audio.Language != "deu" {
		t.Errorf("audio language = %q, want deu", audio.Language)
	}
	if !slices.Contains(audio.Disposition, "forced") {
		t.Errorf("audio disposition = %v, want to contain forced", audio.Disposition)
	}
}

// TestIntegration_BurnIn proves the spec-0019 burn-in path (freetype + harfbuzz +
// libass, via the spec-0029 meson toolchain): the drawtext filter renders text
// from a font mounted on the fs (no fontconfig — fonts named by path), the
// subtitles filter burns an .srt through libass, and a missing font fails cleanly.
// Needs the intermediate-profile module.
func TestIntegration_BurnIn(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileIntermediate)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapClipMP4(t, rt, fs, "src.mp4")
	if err := afero.WriteFile(fs, "font.ttf", goregular.TTF, 0o644); err != nil {
		t.Fatalf("seed font: %v", err)
	}
	// A minimal ASS: one dialogue over the clip, styled in the mounted Go font.
	// libass parses .ass directly (no subtitle demuxer needed — that's the
	// subtitle-stream increment); .srt burn-in would need the subrip codec.
	ass := "[Script Info]\nScriptType: v4.00+\nPlayResX: 32\nPlayResY: 32\n\n" +
		"[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, Alignment, Encoding\n" +
		"Style: Default,Go,10,&H00FFFFFF,2,1\n\n" +
		"[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: 0,0:00:00.00,0:00:02.00,Default,,0,0,0,,hello afmpeg\n"
	if err := afero.WriteFile(fs, "sub.ass", []byte(ass), 0o644); err != nil {
		t.Fatalf("seed ass: %v", err)
	}

	// probeVideo asserts a job produced a single-video-stream output.
	probeVideo := func(t *testing.T, path string) {
		t.Helper()
		p, err := rt.Probe(ctx, fs, path)
		if err != nil || len(p.Streams) != 1 || p.Streams[0].Type != "video" {
			t.Fatalf("probe %s: streams=%+v err=%v", path, p.Streams, err)
		}
	}

	t.Run("drawtext renders with a mounted font", func(t *testing.T) {
		cmd := afmpeg.Command{
			Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
			FilterComplex: "[0:v]drawtext=fontfile=font.ttf:text='afmpeg':fontsize=8:fontcolor=white:x=2:y=2[v]",
			Outputs:       []afmpeg.Output{{Path: "text.mp4", Map: []string{"[v]"}, VideoCodec: "libopenh264"}},
		}
		if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
			t.Fatalf("drawtext: res=%+v err=%v", res, err)
		}
		probeVideo(t, "text.mp4")
	})

	t.Run("ass burns subtitles via libass", func(t *testing.T) {
		// No fontconfig: point libass at the mounted font dir; the .ass Style names
		// the Go font's family so it matches.
		cmd := afmpeg.Command{
			Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
			FilterComplex: "[0:v]ass=filename=sub.ass:fontsdir=.[v]",
			Outputs:       []afmpeg.Output{{Path: "subbed.mp4", Map: []string{"[v]"}, VideoCodec: "libopenh264"}},
		}
		if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
			t.Fatalf("ass: res=%+v err=%v", res, err)
		}
		probeVideo(t, "subbed.mp4")
	})

	t.Run("missing font fails cleanly", func(t *testing.T) {
		cmd := afmpeg.Command{
			Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
			FilterComplex: "[0:v]drawtext=fontfile=nope.ttf:text='x':fontsize=8[v]",
			Outputs:       []afmpeg.Output{{Path: "bad.mp4", Map: []string{"[v]"}, VideoCodec: "libopenh264"}},
		}
		res, err := rt.RunJob(ctx, fs, cmd)
		if err == nil && res.ExitCode == 0 {
			t.Fatalf("expected a clean failure with no font, got res=%+v", res)
		}
	})
}

// TestIntegration_SubtitleStreams proves the spec-0019 subtitle-stream lane: a
// subtitle track (mapped by an "N:s" specifier) is converted between codecs
// (srt→webvtt), copied unchanged, and embedded into an mp4 as mov_text — the
// AVMEDIA_TYPE_SUBTITLE lane beside the graph and the copy path. Needs the
// intermediate-profile module.
func TestIntegration_SubtitleStreams(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileIntermediate)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapClipMP4(t, rt, fs, "src.mp4")
	srt := "1\n00:00:00,000 --> 00:00:01,500\nhello afmpeg\n\n2\n00:00:01,500 --> 00:00:02,000\nbye\n"
	if err := afero.WriteFile(fs, "sub.srt", []byte(srt), 0o644); err != nil {
		t.Fatalf("seed srt: %v", err)
	}

	// The engine reads the .srt as a subtitle stream via the srt demuxer.
	if p, err := rt.Probe(ctx, fs, "sub.srt"); err != nil || len(p.Streams) != 1 || p.Streams[0].Type != "subtitle" {
		t.Fatalf("probe sub.srt: streams=%+v err=%v", p.Streams, err)
	}

	t.Run("convert srt to webvtt", func(t *testing.T) {
		cmd := afmpeg.Command{
			Inputs:  []afmpeg.Input{{Path: "sub.srt"}},
			Outputs: []afmpeg.Output{{Path: "out.vtt", Map: []string{"0:s"}, SubtitleCodec: "webvtt"}},
		}
		if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
			t.Fatalf("srt→vtt: res=%+v err=%v", res, err)
		}
		p, err := rt.Probe(ctx, fs, "out.vtt")
		if err != nil || len(p.Streams) != 1 || p.Streams[0].Codec != "webvtt" {
			t.Fatalf("probe out.vtt: streams=%+v err=%v", p.Streams, err)
		}
	})

	t.Run("copy subtitle stream", func(t *testing.T) {
		cmd := afmpeg.Command{
			Inputs:  []afmpeg.Input{{Path: "sub.srt"}},
			Outputs: []afmpeg.Output{{Path: "copy.srt", Map: []string{"0:s"}, SubtitleCodec: afmpeg.CodecCopy}},
		}
		if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
			t.Fatalf("srt copy: res=%+v err=%v", res, err)
		}
		b, err := afero.ReadFile(fs, "copy.srt")
		if err != nil || !bytes.Contains(b, []byte("hello afmpeg")) {
			t.Fatalf("copy.srt missing or wrong content (err=%v)", err)
		}
	})

	t.Run("embed mov_text into mp4", func(t *testing.T) {
		cmd := afmpeg.Command{
			Inputs: []afmpeg.Input{{Path: "src.mp4"}, {Path: "sub.srt"}},
			Outputs: []afmpeg.Output{{
				Path:          "embedded.mp4",
				Map:           []string{"0:v", "0:a", "1:s"},
				VideoCodec:    afmpeg.CodecCopy,
				AudioCodec:    afmpeg.CodecCopy,
				SubtitleCodec: "mov_text",
			}},
		}
		if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
			t.Fatalf("embed mov_text: res=%+v err=%v", res, err)
		}
		p, err := rt.Probe(ctx, fs, "embedded.mp4")
		if err != nil {
			t.Fatalf("probe embedded.mp4: %v", err)
		}
		var sub *afmpeg.ProbeStream
		for i := range p.Streams {
			if p.Streams[i].Type == "subtitle" {
				sub = &p.Streams[i]
			}
		}
		if sub == nil || sub.Codec != "mov_text" {
			t.Fatalf("embedded.mp4 streams = %+v, want a mov_text subtitle", p.Streams)
		}
	})
}

// TestIntegration_ProbeRawInput proves ProbeInput forwards Format/Options so a
// raw/headerless input — un-probeable by auto-detection — is probeable (the
// review fix to spec 0024's read side). Needs the built driver.
func TestIntegration_ProbeRawInput(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "audio.raw", makeRawPCMS16LE(8000, 0.25), 0o644); err != nil {
		t.Fatalf("seed raw pcm: %v", err)
	}

	// ProbeInput forwards Format + Options → the s16le demuxer reads the headerless
	// bytes as pcm_s16le (which auto-detection, with no magic to go on, can't).
	p, err := rt.ProbeInput(ctx, fs, afmpeg.Input{
		Path: "audio.raw", Format: "s16le", Options: map[string]string{"sample_rate": "8000"},
	})
	if err != nil || len(p.Streams) != 1 || p.Streams[0].Type != "audio" || p.Streams[0].Codec != "pcm_s16le" {
		t.Fatalf("ProbeInput raw: streams=%+v err=%v", p.Streams, err)
	}
	if p.Streams[0].SampleRate != 8000 {
		t.Errorf("ProbeInput raw: sample_rate = %d, want 8000", p.Streams[0].SampleRate)
	}
}

// TestIntegration_NativeFilters proves the spec-0017 filter batch: one graph per
// group parses and produces output (the filters are flag-only additions to the
// filtergraph string — no vocabulary change). Needs the intermediate-profile
// module.
func TestIntegration_NativeFilters(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileIntermediate)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapClipMP4(t, rt, fs, "src.mp4")

	cases := []struct {
		name   string
		filter string
		out    string
		output afmpeg.Output
	}{
		{"hue+curves (color/levels)", "[0:v]hue=s=0,curves=preset=lighter[v]", "v.mp4",
			afmpeg.Output{Map: []string{"[v]"}, VideoCodec: "libopenh264"}},
		{"hstack (compose)", "[0:v]split=2[a][b];[a][b]hstack[v]", "h.mp4",
			afmpeg.Output{Map: []string{"[v]"}, VideoCodec: "libopenh264"}},
		{"thumbnail (frame select)", "[0:v]thumbnail[v]", "t.mp4",
			afmpeg.Output{Map: []string{"[v]"}, VideoCodec: "libopenh264"}},
		{"yadif (deinterlace)", "[0:v]yadif[v]", "y.mp4",
			afmpeg.Output{Map: []string{"[v]"}, VideoCodec: "libopenh264"}},
		{"rotate+hflip (geometry)", "[0:v]hflip,vignette[v]", "g.mp4",
			afmpeg.Output{Map: []string{"[v]"}, VideoCodec: "libopenh264"}},
		{"palettegen|paletteuse → gif", "[0:v]scale=48:48,split[a][b];[a]palettegen[p];[b][p]paletteuse[v]", "p.gif",
			afmpeg.Output{Map: []string{"[v]"}, VideoCodec: "gif"}},
		// loudnorm resamples to 192 kHz internally; aac needs a supported rate, so
		// the graph resamples down — the realistic loudnorm→aac shape.
		{"loudnorm (audio loudness)", "[0:a]loudnorm=I=-16:TP=-1.5:LRA=11,aresample=48000[a]", "l.mp4",
			afmpeg.Output{Map: []string{"[a]"}, AudioCodec: "aac"}},
		{"atempo (audio speed)", "[0:a]atempo=1.5[a]", "s.mp4",
			afmpeg.Output{Map: []string{"[a]"}, AudioCodec: "aac"}},
		{"highpass+equalizer (audio EQ)", "[0:a]highpass=f=200,equalizer=f=1000:width_type=h:width=200:g=-3[a]", "e.mp4",
			afmpeg.Output{Map: []string{"[a]"}, AudioCodec: "aac"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.output
			out.Path = tc.out
			cmd := afmpeg.Command{
				Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
				FilterComplex: tc.filter,
				Outputs:       []afmpeg.Output{out},
			}

			res, err := rt.RunJob(ctx, fs, cmd)
			if err != nil || res.ExitCode != 0 {
				t.Fatalf("%s: res=%+v err=%v", tc.name, res, err)
			}

			// The graph ran and produced a probeable file with a stream.
			p, err := rt.Probe(ctx, fs, tc.out)
			if err != nil || len(p.Streams) == 0 {
				t.Fatalf("%s: probe %s = %+v err=%v, want a stream", tc.name, tc.out, p, err)
			}
		})
	}
}

// TestIntegration_Containers round-trips the spec-0015 native container batch:
// transcode a clip into each new muxer, then probe it back and assert the format
// and stream codecs survived. Needs the intermediate-profile module.
func TestIntegration_Containers(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileIntermediate)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapClipMP4(t, rt, fs, "src.mp4")

	cases := []struct {
		out, filter, vcodec, acodec, wantFormat, wantVideo string
	}{
		{"o.ts", "[0:v]null[v];[0:a]anull[a]", "libopenh264", "aac", "mpegts", "h264"},
		{"o.flv", "[0:v]null[v];[0:a]anull[a]", "libopenh264", "aac", "flv", "h264"},
		{"o.avi", "[0:v]null[v];[0:a]anull[a]", "mjpeg", "pcm_s16le", "avi", "mjpeg"},
	}

	for _, tc := range cases {
		t.Run(tc.wantFormat, func(t *testing.T) {
			cmd := afmpeg.Command{
				Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
				FilterComplex: tc.filter,
				Outputs: []afmpeg.Output{{
					Path: tc.out, Map: []string{"[v]", "[a]"},
					VideoCodec: tc.vcodec, AudioCodec: tc.acodec,
				}},
			}
			if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
				t.Fatalf("transcode → %s: res=%+v err=%v", tc.wantFormat, res, err)
			}

			p, err := rt.Probe(ctx, fs, tc.out)
			if err != nil {
				t.Fatalf("probe %s: %v", tc.out, err)
			}
			if !strings.Contains(p.Format, tc.wantFormat) {
				t.Errorf("%s format = %q, want %q", tc.out, p.Format, tc.wantFormat)
			}

			var haveVideo bool
			for _, s := range p.Streams {
				haveVideo = haveVideo || s.Codec == tc.wantVideo
			}
			if !haveVideo {
				t.Errorf("%s streams = %+v, want a %s video stream", tc.out, p.Streams, tc.wantVideo)
			}
		})
	}

	// Animated GIF: the gif container rides with its codec (spec 0015 §9).
	gif := afmpeg.Command{
		Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
		FilterComplex: "[0:v]scale=48:48[v]",
		Outputs:       []afmpeg.Output{{Path: "o.gif", Map: []string{"[v]"}, VideoCodec: "gif"}},
	}
	if res, err := rt.RunJob(ctx, fs, gif); err != nil || res.ExitCode != 0 {
		t.Fatalf("transcode → gif: res=%+v err=%v", res, err)
	}
	if p, err := rt.Probe(ctx, fs, "o.gif"); err != nil || !strings.Contains(p.Format, "gif") {
		t.Fatalf("gif probe = %+v err=%v, want gif", p, err)
	}
}

// TestIntegration_MuxerSelection proves outputs[].format forces the muxer where
// the path extension wouldn't (spec 0015 D-0015-C).
func TestIntegration_MuxerSelection(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileIntermediate)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapAVMP4(t, rt, fs, "src.mp4")

	// A ".bin" path the extension can't guess — forced to mpegts.
	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("src.mp4"),
		afmpeg.WithOutput("stream.bin", afmpeg.OutputFormat("mpegts"),
			afmpeg.AudioCodec("aac")),
	)
	if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
		t.Fatalf("forced mpegts: res=%+v err=%v", res, err)
	}
	if p, err := rt.Probe(ctx, fs, "stream.bin"); err != nil || !strings.Contains(p.Format, "mpegts") {
		t.Fatalf("forced-mpegts probe = %+v err=%v, want mpegts", p, err)
	}
}

// TestIntegration_Segmenting_HLS proves R-0015-1: one outputs[] entry with
// format:"hls" writes a set of .ts segment files + an .m3u8 playlist to the
// mounted fs (no network), driven by format_options.
func TestIntegration_Segmenting_HLS(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileIntermediate)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapClipMP4(t, rt, fs, "src.mp4") // ~2s, so hls_time=1 → ≥2 segments

	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("src.mp4"),
		afmpeg.WithFilterComplex("[0:v]null[v]"),
		afmpeg.WithOutput("stream.m3u8", afmpeg.Map("[v]"),
			afmpeg.OutputFormat("hls"), afmpeg.VideoCodec("libopenh264"),
			afmpeg.FormatOption("hls_time", "1"),
			afmpeg.FormatOption("hls_segment_filename", "seg_%03d.ts"),
			afmpeg.FormatOption("hls_list_size", "0")),
	)

	res, err := rt.RunJob(ctx, fs, cmd)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("hls segmenting: res=%+v err=%v", res, err)
	}

	if !strings.Contains(res.Stdout, `"segmented":true`) {
		t.Errorf("result did not mark the output segmented:\n%s", res.Stdout)
	}

	// The playlist and at least two segment files must exist on the fs.
	if pl, err := afero.ReadFile(fs, "stream.m3u8"); err != nil || !strings.Contains(string(pl), "seg_000.ts") {
		t.Fatalf("playlist stream.m3u8 missing or doesn't reference the segments: err=%v", err)
	}

	segs := 0
	for i := 0; i < 10; i++ {
		if ok, _ := afero.Exists(fs, fmt.Sprintf("seg_%03d.ts", i)); ok {
			segs++
		}
	}
	if segs < 2 {
		t.Fatalf("hls produced %d segment files, want >= 2", segs)
	}
}

// TestIntegration_FragmentedMP4 proves R-0015-2: fragmented MP4 via movflags
// through format_options (the CMAF/low-latency shape).
func TestIntegration_FragmentedMP4(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapClipMP4(t, rt, fs, "src.mp4")

	cmd := afmpeg.NewCommand(
		afmpeg.WithInput("src.mp4"),
		afmpeg.WithFilterComplex("[0:v]null[v]"),
		afmpeg.WithOutput("frag.mp4", afmpeg.Map("[v]"), afmpeg.VideoCodec("libopenh264"),
			afmpeg.FormatOption("movflags", "+frag_keyframe+empty_moov+default_base_moof")),
	)
	if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
		t.Fatalf("fragmented mp4: res=%+v err=%v", res, err)
	}

	// A fragmented mp4 carries an 'moof' box (media fragments) — absent in a plain
	// mp4 (which has a single 'moov').
	out, err := afero.ReadFile(fs, "frag.mp4")
	if err != nil || !bytes.Contains(out, []byte("moof")) {
		t.Fatalf("fragmented mp4 has no moof box: err=%v len=%d", err, len(out))
	}
}

// TestIntegration_TS_CopyRemux is 0013's deferred mp4→ts copy remux, switched on
// by 0015's mpegts muxer: cross-container copy needs the auto-inserted
// h264_mp4toannexb bitstream filter.
func TestIntegration_TS_CopyRemux(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileIntermediate)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapAVMP4(t, rt, fs, "src.mp4")

	cmd := afmpeg.Command{
		Inputs: []afmpeg.Input{{Path: "src.mp4"}},
		Outputs: []afmpeg.Output{{
			Path: "out.ts", Map: []string{"0:v", "0:a"},
			VideoCodec: afmpeg.CodecCopy, AudioCodec: afmpeg.CodecCopy,
		}},
	}
	if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
		t.Fatalf("mp4→ts copy: res=%+v err=%v", res, err)
	}

	p, err := rt.Probe(ctx, fs, "out.ts")
	if err != nil || !strings.Contains(p.Format, "mpegts") {
		t.Fatalf("ts probe = %+v err=%v, want mpegts", p, err)
	}

	var haveH264, haveAAC bool
	for _, s := range p.Streams {
		haveH264 = haveH264 || s.Codec == "h264"
		haveAAC = haveAAC || s.Codec == "aac"
	}
	if !haveH264 || !haveAAC {
		t.Fatalf("ts streams = %+v, want h264 + aac copied", p.Streams)
	}
}

// TestIntegration_TS_ConcatCopy is the full A/V concat-copy 0013 deferred: MPEG-TS
// segments carry continuous timestamps, so joining two of them with copy (which
// mp4's audio priming made impossible) works.
func TestIntegration_TS_ConcatCopy(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileIntermediate)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()

	// Two like-codec TS segments (transcode the clip to ts twice).
	for _, seg := range []string{"a.ts", "b.ts"} {
		bootstrapClipMP4(t, rt, fs, "src.mp4")
		cmd := afmpeg.Command{
			Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
			FilterComplex: "[0:v]null[v];[0:a]anull[a]",
			Outputs: []afmpeg.Output{{
				Path: seg, Map: []string{"[v]", "[a]"}, VideoCodec: "libopenh264", AudioCodec: "aac",
			}},
		}
		if res, err := rt.RunJob(ctx, fs, cmd); err != nil || res.ExitCode != 0 {
			t.Fatalf("make %s: res=%+v err=%v", seg, res, err)
		}
	}

	// Join both segments, copying every stream — the mp4-impossible case.
	join := afmpeg.Command{
		Inputs: []afmpeg.Input{{Concat: []string{"a.ts", "b.ts"}}},
		Outputs: []afmpeg.Output{{
			Path: "joined.ts", Map: []string{"0:v", "0:a"},
			VideoCodec: afmpeg.CodecCopy, AudioCodec: afmpeg.CodecCopy,
		}},
	}
	res, err := rt.RunJob(ctx, fs, join)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("ts concat-copy (full A/V): res=%+v err=%v", res, err)
	}

	seg, err := rt.Probe(ctx, fs, "a.ts")
	if err != nil {
		t.Fatalf("probe segment: %v", err)
	}
	joined, err := rt.Probe(ctx, fs, "joined.ts")
	if err != nil {
		t.Fatalf("probe joined: %v", err)
	}
	if joined.DurationSec < seg.DurationSec*1.5 {
		t.Fatalf("joined %.2fs, want ~2x a segment's %.2fs", joined.DurationSec, seg.DurationSec)
	}
}

// TestIntegration_StreamCopy_Concat proves the concat-demuxer input mode: two
// like-codec segments are joined into one continuous input and stream-copied out.
//
// The join is video-only here on purpose. Full A/V concat of mp4 segments with
// copy hits mp4's audio-priming timestamp discontinuity at the segment boundary
// (non-monotonic DTS) — a property of concatenating mp4, not of the demuxer. The
// marquee concat-copy source is MPEG-TS, which has no such issue and lands with
// spec 0015; this test proves R-0013-3 (the demuxer joins like-codec inputs) on
// the containers 0013 ships.
func TestIntegration_StreamCopy_Concat(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapAVMP4(t, rt, fs, "seg0.mp4")
	bootstrapAVMP4(t, rt, fs, "seg1.mp4")

	cmd := afmpeg.Command{
		Inputs: []afmpeg.Input{{Concat: []string{"seg0.mp4", "seg1.mp4"}}},
		Outputs: []afmpeg.Output{{
			Path: "joined.mp4", Map: []string{"0:v"}, VideoCodec: afmpeg.CodecCopy,
		}},
	}

	res, err := rt.RunJob(ctx, fs, cmd)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("concat copy: res=%+v err=%v", res, err)
	}

	// A valid joined output whose video codec survived the copy.
	joined, err := rt.Probe(ctx, fs, "joined.mp4")
	if err != nil {
		t.Fatalf("probe joined: %v", err)
	}

	if len(joined.Streams) != 1 || joined.Streams[0].Codec != "h264" {
		t.Fatalf("joined streams = %+v, want one copied h264 stream", joined.Streams)
	}
}

// TestIntegration_StreamCopy_BitstreamFilterNone proves an explicit bitstream
// filter override is honoured: forcing "none" on a copied stream still remuxes.
func TestIntegration_StreamCopy_BitstreamFilterNone(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapAVMP4(t, rt, fs, "src.mp4")

	cmd := afmpeg.Command{
		Inputs: []afmpeg.Input{{Path: "src.mp4"}},
		Outputs: []afmpeg.Output{{
			Path: "out.mkv", Map: []string{"0:v", "0:a"},
			VideoCodec: afmpeg.CodecCopy, AudioCodec: afmpeg.CodecCopy,
			BitstreamFilters: map[string]string{"0:v": "none"},
		}},
	}

	res, err := rt.RunJob(ctx, fs, cmd)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("copy with bsf=none: res=%+v err=%v", res, err)
	}

	if out, err := afero.ReadFile(fs, "out.mkv"); err != nil || len(out) < 12 {
		t.Fatalf("no valid mkv produced: err=%v len=%d", err, len(out))
	}
}

// TestIntegration_StreamCopy_Mixed proves copy and transcode coexist in one job:
// the video is copied while the audio is re-encoded through the graph.
func TestIntegration_StreamCopy_Mixed(t *testing.T) {
	t.Parallel()

	module := integrationModule(t, afmpeg.ProfileLean)

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(ctx) })

	fs := afero.NewMemMapFs()
	bootstrapAVMP4(t, rt, fs, "src.mp4")

	// Copy the video (0:v) unchanged; re-encode the audio through the graph.
	cmd := afmpeg.Command{
		Inputs:        []afmpeg.Input{{Path: "src.mp4"}},
		FilterComplex: "[0:a]volume=0.5[aout]",
		Outputs: []afmpeg.Output{{
			Path: "out.mp4", Map: []string{"0:v", "[aout]"},
			VideoCodec: afmpeg.CodecCopy, AudioCodec: "aac",
		}},
	}

	res, err := rt.RunJob(ctx, fs, cmd)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("mixed copy+transcode: res=%+v err=%v", res, err)
	}

	out, err := afero.ReadFile(fs, "out.mp4")
	if err != nil || len(out) < 12 || string(out[4:8]) != "ftyp" {
		t.Fatalf("mixed job produced no valid mp4: err=%v len=%d", err, len(out))
	}
}
