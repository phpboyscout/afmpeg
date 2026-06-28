package afmpeg_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// TestIntegration_FFmpegWasiDriver drives the ffmpeg-wasi libav-direct engine —
// the structured job-spec vocabulary, not ffmpeg CLI args — over the in-memory
// bridge. It proves the afmpeg ↔ ffmpeg-wasi seam end-to-end: a probe whose JSON
// comes back on stdout, and a real transcode (WAV → AAC/mp4) written entirely in
// memory. Gated on AFMPEG_TEST_FFMPEG_WASI (a path to a built ffmpeg-wasi-*.wasm).
func TestIntegration_FFmpegWasiDriver(t *testing.T) {
	t.Parallel()

	module := os.Getenv("AFMPEG_TEST_FFMPEG_WASI")
	if module == "" {
		t.Skip("set AFMPEG_TEST_FFMPEG_WASI to a built ffmpeg-wasi driver to run this test")
	}

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

	module := os.Getenv("AFMPEG_TEST_FFMPEG_WASI")
	if module == "" {
		t.Skip("set AFMPEG_TEST_FFMPEG_WASI to a built ffmpeg-wasi driver to run this test")
	}

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

// TestIntegration_Probe_FFmpegWasiDriver is the regression test for
// BUG-REPORT-probe: Runtime.Probe drives the ffmpeg-wasi engine's probe op (a JSON
// job spec) and parses the structured result, instead of the old CLI `ffmpeg -i`
// + stderr transport the libav-direct engine rejects. Gated on AFMPEG_TEST_FFMPEG_WASI.
func TestIntegration_Probe_FFmpegWasiDriver(t *testing.T) {
	t.Parallel()

	module := os.Getenv("AFMPEG_TEST_FFMPEG_WASI")
	if module == "" {
		t.Skip("set AFMPEG_TEST_FFMPEG_WASI to a built ffmpeg-wasi driver to run this test")
	}

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
