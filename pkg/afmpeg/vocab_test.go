package afmpeg

import (
	"fmt"
	"testing"
)

// TestEvaluateVocab covers the version-gate decision: a gated engine older than
// what afmpeg emits is rejected; an equal-or-newer gated engine is accepted; a
// non-gated module (pre-gate or generic) is always tolerated.
func TestEvaluateVocab(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		engineVer int
		gated     bool
		wantErr   bool
	}{
		{"gated too old", vocabVersion - 1, true, true},
		{"gated exact", vocabVersion, true, false},
		{"gated newer", vocabVersion + 1, true, false},
		{"not gated (pre-gate/generic)", 0, false, false},
		{"not gated ignores version", vocabVersion - 1, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := evaluateVocab(tt.engineVer, tt.gated)
			if (err != nil) != tt.wantErr {
				t.Fatalf("evaluateVocab(%d, %v) err = %v, wantErr %v", tt.engineVer, tt.gated, err, tt.wantErr)
			}
		})
	}
}

// TestMemoryLimitPages covers the byte→page conversion: non-positive means no
// cap (0), bytes round up to whole 64 KiB pages, and an over-large limit clamps
// to the wasm32 maximum (so wazero's WithMemoryLimitPages never panics).
func TestMemoryLimitPages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		bytes int
		want  uint32
	}{
		{0, 0},
		{-1, 0},
		{1, 1},
		{wasmPageSize, 1},
		{wasmPageSize + 1, 2},
		{512 << 20, 8192},
		{5 << 30, maxMemoryLimitPages}, // 5 GiB clamps to the 4 GiB ceiling
	}

	for _, tt := range tests {
		if got := memoryLimitPages(tt.bytes); got != tt.want {
			t.Errorf("memoryLimitPages(%d) = %d, want %d", tt.bytes, got, tt.want)
		}
	}
}

// TestInterpretVocabReply covers the op:"version" reply → gate decision: a
// non-zero exit or an unparseable/version-less reply is tolerated; a parseable
// gated version is evaluated (too-old rejected, equal/newer accepted).
func TestInterpretVocabReply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stdout   string
		exitCode int
		wantErr  bool
	}{
		{"non-zero exit tolerated", `anything`, 2, false},
		{"unparseable reply tolerated", `not json`, 0, false},
		{"version-less reply tolerated", `{"ffmpeg_version":"n8"}`, 0, false},
		{"explicit zero version tolerated", `{"vocab_version":0}`, 0, false},
		{"current version accepted", fmt.Sprintf(`{"vocab_version":%d}`, vocabVersion), 0, false},
		{"newer version accepted", fmt.Sprintf(`{"vocab_version":%d}`, vocabVersion+5), 0, false},
		{"too-old gated version rejected", `{"vocab_version":1}`, 0, vocabVersion > 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := interpretVocabReply(tt.stdout, tt.exitCode)
			if (err != nil) != tt.wantErr {
				t.Fatalf("interpretVocabReply(%q, %d) err = %v, wantErr %v", tt.stdout, tt.exitCode, err, tt.wantErr)
			}
		})
	}
}
