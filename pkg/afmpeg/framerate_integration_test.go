package afmpeg_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg/native"
)

// The VFR fixture: video at 10 fps, then 30 fps between 1s and 2s, then 10 fps
// again, over 3s; audio with a hole from 1.0s to 1.5s. Its container still
// advertises 30 fps, which is what makes VFR easy to miss.
const (
	vfrSpanSec   = 3.0
	vfrTargetFPS = 25
)

// TestIntegration_VFRToCFR proves the spec 0025 filter pattern on a real
// variable-frame-rate source through the real engine, on both backends: `fps`
// gives every video frame the same duration, and `aresample=async=1` fills the
// audio hole so the audio stays on the video's timeline.
func TestIntegration_VFRToCFR(t *testing.T) {
	t.Parallel()

	src := synthesiseVFR(t)

	for _, b := range []struct {
		name string
		opt  func(t *testing.T) afmpeg.Option
	}{
		{"wasm", func(t *testing.T) afmpeg.Option {
			t.Helper()

			return afmpeg.WithModuleFile(integrationModule(t, afmpeg.ProfileLean))
		}},
		{"native", func(t *testing.T) afmpeg.Option {
			t.Helper()

			return afmpeg.WithBackend(native.New(native.WithNativeBinary(anyNativeDriver(t))))
		}},
	} {
		t.Run(b.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			rt, err := afmpeg.New(ctx, b.opt(t))
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			t.Cleanup(func() { _ = rt.Close(ctx) })

			t.Run("the fixture is VFR with an audio hole", func(t *testing.T) {
				v, a := transcodeTracks(ctx, t, rt, src, "[0:v]null[v];[0:a]anull[a]")

				if distinct(v.deltas) < 2 {
					t.Fatalf("passthrough video has uniform frame durations %v; the fixture is not VFR", v.deltas)
				}

				if a.longest() < 3*a.deltas[0] {
					t.Fatalf("passthrough audio has no hole (longest sample %d, first %d); the fixture is wrong",
						a.longest(), a.deltas[0])
				}
			})

			t.Run("fps and aresample=async give CFR in sync", func(t *testing.T) {
				v, a := transcodeTracks(ctx, t, rt, src, "[0:v]fps=25[v];[0:a]aresample=async=1[a]")

				want := v.timescale / vfrTargetFPS
				for i, d := range v.deltas {
					if d != want {
						t.Fatalf("video frame %d lasts %d/%d s, want %d (every frame 1/%d s)",
							i, d, v.timescale, want, vfrTargetFPS)
					}
				}

				frames := len(v.deltas)
				if lo, hi := vfrSpanSec*vfrTargetFPS-3, vfrSpanSec*vfrTargetFPS+1; float64(frames) < lo || float64(frames) > hi {
					t.Fatalf("got %d video frames, want %v to %v for %vs at %d fps", frames, lo, hi, vfrSpanSec, vfrTargetFPS)
				}

				if a.longest() > a.deltas[0] {
					t.Fatalf("audio still has a sample of %d against a frame of %d; the hole was not filled",
						a.longest(), a.deltas[0])
				}

				audioFrame := a.seconds(a.deltas[0])
				if gap := math.Abs(a.duration() - v.duration()); gap > audioFrame+v.seconds(want) {
					t.Fatalf("audio runs %.3fs and video %.3fs; %.3fs apart is more than one frame of each",
						a.duration(), v.duration(), gap)
				}
			})
		})
	}
}

// synthesiseVFR builds the fixture with the host ffmpeg. `-fps_mode vfr` keeps
// the selected frames' own timestamps rather than duplicating to a constant rate.
func synthesiseVFR(t *testing.T) []byte {
	t.Helper()

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no host ffmpeg to synthesise the VFR fixture")
	}

	out := filepath.Join(t.TempDir(), "vfr.mkv")

	gen := exec.CommandContext(context.Background(), ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=64x64:rate=30:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=8000:duration=3",
		"-filter_complex",
		"[0:v]select='not(mod(n\\,3))+between(n\\,30\\,59)',format=yuv420p[v];"+
			"[1:a]atrim=0:1,asetpts=PTS-STARTPTS[a1];"+
			"[1:a]atrim=1.5:3,asetpts=PTS-STARTPTS+0.5/TB[a2];"+
			"[a1][a2]concat=n=2:v=0:a=1[a]",
		"-map", "[v]", "-map", "[a]", "-fps_mode", "vfr",
		"-c:v", "libx264", "-preset", "veryfast", "-bf", "0", "-c:a", "pcm_s16le", out)
	if msg, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("synthesise VFR fixture: %v: %s", err, msg)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

// anyNativeDriver returns the first native driver supplied. The job needs only
// lean-profile components, which every profile and licence variant carries.
func anyNativeDriver(t *testing.T) string {
	t.Helper()

	for _, env := range []string{
		"AFMPEG_TEST_NATIVE_DRIVER", "AFMPEG_TEST_NATIVE_DRIVER_GPL",
		"AFMPEG_TEST_NATIVE_DRIVER_INTERMEDIATE", "AFMPEG_TEST_NATIVE_DRIVER_INTERMEDIATE_GPL",
		"AFMPEG_TEST_NATIVE_DRIVER_FULL", "AFMPEG_TEST_NATIVE_DRIVER_FULL_GPL",
	} {
		if path := os.Getenv(env); path != "" {
			return path
		}
	}

	t.Skip("set AFMPEG_TEST_NATIVE_DRIVER to a built native driver to run this test")

	return ""
}

// transcodeTracks runs the fixture through graph into an mp4 and returns its
// video and audio sample timing.
func transcodeTracks(ctx context.Context, t *testing.T, rt *afmpeg.Runtime, src []byte, graph string) (video, audio mp4Track) {
	t.Helper()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.mkv", src, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := rt.RunJob(ctx, fs, afmpeg.Command{
		Inputs:        []afmpeg.Input{{Path: "in.mkv"}},
		FilterComplex: graph,
		Outputs: []afmpeg.Output{{
			Path: "out.mp4", Map: []string{"[v]", "[a]"},
			VideoCodec: "libopenh264", AudioCodec: "aac",
		}},
	})
	if err != nil {
		t.Fatalf("RunJob: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("exit %d:\n%s", res.ExitCode, res.Stderr)
	}

	out, err := afero.ReadFile(fs, "out.mp4")
	if err != nil {
		t.Fatal(err)
	}

	tracks := readMP4Tracks(t, out)

	video, audio = tracks["vide"], tracks["soun"]
	if len(video.deltas) == 0 || len(audio.deltas) == 0 {
		t.Fatalf("out.mp4 lacks a video or audio track: %+v", tracks)
	}

	return video, audio
}

// mp4Track is one track's media timescale and per-sample durations, from its
// `stts` box. The muxer writes each sample's duration from the packet timestamps
// it was given, so a VFR stream shows up here as unequal deltas and an audio
// hole as one long sample. skip is the edit list's media_time: the encoder
// priming (AAC's 1024 samples) that is muxed but never presented.
type mp4Track struct {
	timescale uint32
	deltas    []uint32
	skip      uint32
}

func (tr mp4Track) seconds(ticks uint32) float64 { return float64(ticks) / float64(tr.timescale) }

func (tr mp4Track) duration() float64 {
	var sum uint64
	for _, d := range tr.deltas {
		sum += uint64(d)
	}

	return float64(sum-uint64(tr.skip)) / float64(tr.timescale)
}

// longest ignores the final sample, which a muxer may trim to the stream's end.
func (tr mp4Track) longest() uint32 {
	var m uint32
	for _, d := range tr.deltas[:len(tr.deltas)-1] {
		m = max(m, d)
	}

	return m
}

func distinct(xs []uint32) int {
	seen := map[uint32]bool{}
	for _, x := range xs {
		seen[x] = true
	}

	return len(seen)
}

// readMP4Tracks walks moov/trak/mdia for each track's handler type, `mdhd`
// timescale and expanded `stts` run table, keyed by handler ("vide", "soun").
func readMP4Tracks(t *testing.T, data []byte) map[string]mp4Track {
	t.Helper()

	moov, ok := mp4Child(data, "moov")
	if !ok {
		t.Fatal("no moov box")
	}

	tracks := map[string]mp4Track{}

	for _, trak := range mp4Children(moov, "trak") {
		mdia, _ := mp4Child(trak, "mdia")
		hdlr, _ := mp4Child(mdia, "hdlr")
		mdhd, _ := mp4Child(mdia, "mdhd")
		minf, _ := mp4Child(mdia, "minf")
		stbl, _ := mp4Child(minf, "stbl")
		stts, _ := mp4Child(stbl, "stts")

		if len(hdlr) < 12 || len(mdhd) < 24 || len(stts) < 8 {
			t.Fatalf("track lacks hdlr/mdhd/stts")
		}

		var tr mp4Track

		// mdhd: version 0 carries 32-bit times before the timescale, version 1 64-bit.
		if mdhd[0] == 1 {
			tr.timescale = binary.BigEndian.Uint32(mdhd[20:])
		} else {
			tr.timescale = binary.BigEndian.Uint32(mdhd[12:])
		}

		entries := binary.BigEndian.Uint32(stts[4:])
		for i := range entries {
			off := 8 + 8*int(i)
			count, delta := binary.BigEndian.Uint32(stts[off:]), binary.BigEndian.Uint32(stts[off+4:])

			for range count {
				tr.deltas = append(tr.deltas, delta)
			}
		}

		tr.skip = mediaTimeSkip(trak)
		tracks[string(hdlr[8:12])] = tr
	}

	return tracks
}

// mediaTimeSkip reads the first edit list entry's media_time, 0 when the track
// has no edit list or the entry is an empty edit (-1).
func mediaTimeSkip(trak []byte) uint32 {
	edts, _ := mp4Child(trak, "edts")
	elst, _ := mp4Child(edts, "elst")

	if len(elst) < 8 || binary.BigEndian.Uint32(elst[4:]) == 0 {
		return 0
	}

	var mediaTime int64
	if elst[0] == 1 {
		if len(elst) < 24 {
			return 0
		}

		mediaTime = int64(binary.BigEndian.Uint64(elst[16:])) //nolint:gosec // signed field by definition
	} else {
		if len(elst) < 16 {
			return 0
		}

		mediaTime = int64(int32(binary.BigEndian.Uint32(elst[12:]))) //nolint:gosec // signed field by definition
	}

	if mediaTime < 0 {
		return 0
	}

	return uint32(mediaTime) //nolint:gosec // a priming offset, far below 2^32
}

// mp4Children returns the payloads of every direct child box of the given type.
func mp4Children(data []byte, typ string) [][]byte {
	var out [][]byte

	for len(data) >= 8 {
		size := int(binary.BigEndian.Uint32(data))
		if size < 8 || size > len(data) {
			break
		}

		if bytes.Equal(data[4:8], []byte(typ)) {
			out = append(out, data[8:size])
		}

		data = data[size:]
	}

	return out
}

func mp4Child(data []byte, typ string) ([]byte, bool) {
	if c := mp4Children(data, typ); len(c) > 0 {
		return c[0], true
	}

	return nil, false
}
