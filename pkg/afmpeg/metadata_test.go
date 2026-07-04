package afmpeg_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// TestJobSpec_Metadata proves the spec-0020 write vocabulary serialises: container
// tags, the chapters passthrough directive, and per-stream language/disposition/
// tags land in the output's job-spec JSON (chapter copy has no self-bootstrappable
// integration fixture, so its emission is asserted here).
func TestJobSpec_Metadata(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.Command{
		Inputs: []afmpeg.Input{{Path: "in.mp4"}},
		Outputs: []afmpeg.Output{{
			Path:       "out.mkv",
			Map:        []string{"[v]", "0:a"},
			VideoCodec: "libopenh264",
			AudioCodec: afmpeg.CodecCopy,
			Metadata:   map[string]string{"title": "T", "artist": "A"},
			Chapters:   "copy",
			StreamMetadata: map[string]afmpeg.StreamMeta{
				"0:a": {Language: "eng", Disposition: []string{"default", "forced"}, Tags: map[string]string{"role": "main"}},
			},
		}},
	}

	data, err := cmd.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	var spec struct {
		Version int `json:"version"`
		Outputs []struct {
			Metadata       map[string]string `json:"metadata"`
			Chapters       string            `json:"chapters"`
			StreamMetadata map[string]struct {
				Language    string            `json:"language"`
				Disposition []string          `json:"disposition"`
				Tags        map[string]string `json:"tags"`
			} `json:"stream_metadata"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if spec.Version < 7 {
		t.Errorf("version = %d, want >= 7", spec.Version)
	}

	out := spec.Outputs[0]
	if out.Metadata["title"] != "T" || out.Metadata["artist"] != "A" {
		t.Errorf("metadata = %v", out.Metadata)
	}
	if out.Chapters != "copy" {
		t.Errorf("chapters = %q, want copy", out.Chapters)
	}

	sm, ok := out.StreamMetadata["0:a"]
	if !ok {
		t.Fatalf("stream_metadata missing 0:a: %v", out.StreamMetadata)
	}
	if sm.Language != "eng" || len(sm.Disposition) != 2 || sm.Tags["role"] != "main" {
		t.Errorf("stream_metadata[0:a] = %+v", sm)
	}
}

// TestJobSpec_Metadata_Omitted proves the metadata fields are omitempty — a plain
// output carries none of them, so v6-shaped specs are unchanged by the v7 fields.
func TestJobSpec_Metadata_Omitted(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.Command{
		Inputs:  []afmpeg.Input{{Path: "in.mp4"}},
		Outputs: []afmpeg.Output{{Path: "out.mp4", AudioCodec: "aac"}},
	}

	data, err := cmd.JobSpec()
	if err != nil {
		t.Fatalf("JobSpec: %v", err)
	}

	for _, key := range []string{"metadata", "chapters", "stream_metadata"} {
		if got := string(data); strings.Contains(got, `"`+key+`"`) {
			t.Errorf("plain output should omit %q, got %s", key, got)
		}
	}
}
