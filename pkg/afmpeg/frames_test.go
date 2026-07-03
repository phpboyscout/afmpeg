package afmpeg_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// decodeFrames unmarshals a FrameJob's rendered spec for structural assertions.
func decodeFrames(t *testing.T, j afmpeg.FrameJob) map[string]any {
	t.Helper()

	data, err := j.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	return m
}

func TestFrameJob_JobSpec_Shape(t *testing.T) {
	t.Parallel()

	ts := 12.5
	m := decodeFrames(t, afmpeg.FrameJob{
		Input:  "in.mp4",
		Select: afmpeg.FrameSelect{Timestamp: &ts},
		Path:   "out/frame_%03d.png",
		Codec:  "png",
		Scale:  "320:-2",
		Count:  25,
	})

	if m["op"] != "frames" {
		t.Errorf("op = %v, want frames", m["op"])
	}
	if m["version"].(float64) < 6 {
		t.Errorf("version = %v, want >= 6", m["version"])
	}
	sel := m["select"].(map[string]any)
	if sel["timestamp"].(float64) != 12.5 {
		t.Errorf("select.timestamp = %v, want 12.5", sel["timestamp"])
	}
	if m["path"] != "out/frame_%03d.png" || m["codec"] != "png" || m["scale"] != "320:-2" {
		t.Errorf("path/codec/scale mismatch: %v", m)
	}
	ins := m["inputs"].([]any)
	if len(ins) != 1 || ins[0].(map[string]any)["path"] != "in.mp4" {
		t.Errorf("inputs = %v, want one with path in.mp4", m["inputs"])
	}
}

func TestFrameJob_SelectorShapes(t *testing.T) {
	t.Parallel()

	thr := 0.4
	cases := []struct {
		name   string
		sel    afmpeg.FrameSelect
		assert func(t *testing.T, sel map[string]any)
	}{
		{"timestamps", afmpeg.FrameSelect{Timestamps: []float64{1, 5, 30}}, func(t *testing.T, s map[string]any) {
			if len(s["timestamps"].([]any)) != 3 {
				t.Errorf("timestamps = %v", s["timestamps"])
			}
		}},
		{"interval", afmpeg.FrameSelect{Interval: 10}, func(t *testing.T, s map[string]any) {
			if s["interval"].(float64) != 10 {
				t.Errorf("interval = %v", s["interval"])
			}
		}},
		{"sceneThreshold", afmpeg.FrameSelect{SceneThreshold: &thr}, func(t *testing.T, s map[string]any) {
			if s["scene"].(float64) != 0.4 {
				t.Errorf("scene = %v, want 0.4", s["scene"])
			}
		}},
		{"thumbnail", afmpeg.FrameSelect{Thumbnail: true}, func(t *testing.T, s map[string]any) {
			if s["scene"] != "thumbnail" {
				t.Errorf("scene = %v, want thumbnail", s["scene"])
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := decodeFrames(t, afmpeg.FrameJob{Input: "in.mp4", Path: "f_%d.png", Select: tc.sel})
			tc.assert(t, m["select"].(map[string]any))
		})
	}
}

// TestFrames_Runtime covers Runtime.Frames over the test guest: a canned success
// parses into FramesResult, a non-zero exit and malformed output both error, and
// an invalid job fails before the engine runs.
func TestFrames_Runtime(t *testing.T) {
	t.Parallel()

	ts := 1.0
	ok := afmpeg.FrameJob{Input: "clip.mp4", Path: "f_%03d.png", Select: afmpeg.FrameSelect{Timestamp: &ts}}

	t.Run("success parses", func(t *testing.T) {
		res, err := newTestRuntime(t).Frames(context.Background(), afero.NewMemMapFs(), ok)
		if err != nil {
			t.Fatalf("Frames: %v", err)
		}
		if res.Count != 1 || len(res.Frames) != 1 || res.Frames[0].Path != "f_000.png" || res.Frames[0].Timestamp != 1.5 {
			t.Fatalf("result = %+v", res)
		}
	})

	t.Run("non-zero exit errors", func(t *testing.T) {
		bad := ok
		bad.Input = "frames-fail"
		if _, err := newTestRuntime(t).Frames(context.Background(), afero.NewMemMapFs(), bad); err == nil {
			t.Fatal("want error on non-zero exit")
		}
	})

	t.Run("bad json errors", func(t *testing.T) {
		bad := ok
		bad.Input = "frames-badjson"
		if _, err := newTestRuntime(t).Frames(context.Background(), afero.NewMemMapFs(), bad); err == nil {
			t.Fatal("want error on malformed output")
		}
	})

	t.Run("invalid job fails before run", func(t *testing.T) {
		if _, err := newTestRuntime(t).Frames(context.Background(), afero.NewMemMapFs(), afmpeg.FrameJob{Input: "x"}); err == nil {
			t.Fatal("want error on job with no selector/path")
		}
	})
}

func TestFrameJob_Validation(t *testing.T) {
	t.Parallel()

	ts := 1.0
	thr := 0.3
	cases := []struct {
		name    string
		job     afmpeg.FrameJob
		wantErr bool
	}{
		{"no selector", afmpeg.FrameJob{Input: "in.mp4", Path: "f.png"}, true},
		{"two selectors", afmpeg.FrameJob{Input: "in.mp4", Path: "f_%d.png",
			Select: afmpeg.FrameSelect{Timestamp: &ts, SceneThreshold: &thr}}, true},
		{"missing path", afmpeg.FrameJob{Input: "in.mp4",
			Select: afmpeg.FrameSelect{Timestamp: &ts}}, true},
		{"ok single", afmpeg.FrameJob{Input: "in.mp4", Path: "f.png",
			Select: afmpeg.FrameSelect{Timestamp: &ts}}, false},
		{"zero timestamp is set", afmpeg.FrameJob{Input: "in.mp4", Path: "f.png",
			Select: afmpeg.FrameSelect{Timestamp: new(float64)}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.job.JobSpec()
			if (err != nil) != tc.wantErr {
				t.Errorf("JobSpec err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
