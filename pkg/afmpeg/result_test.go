package afmpeg

import "testing"

func TestParseResult_analysisAndOutputs(t *testing.T) {
	t.Parallel()

	// The engine's process result JSON (spec 0017 §Q analysis + outputs).
	stdout := `{"outputs":[{"path":"out.mp4","streams":[` +
		`{"type":"video","codec":"libx264"},` +
		`{"type":"audio","codec":"aac","disposition":"copy"}]}],` +
		`"analysis":[` +
		`{"t":0.08,"key":"cropdetect.w","value":"160"},` +
		`{"t":2.5,"key":"silence_start","value":"2.5"},` +
		`{"t":3.4,"key":"silence_end","value":"3.4"}]}`

	pr, err := ParseResult(Result{ExitCode: 0, Stdout: stdout})
	if err != nil {
		t.Fatalf("ParseResult: %v", err)
	}

	if len(pr.Outputs) != 1 || pr.Outputs[0].Path != "out.mp4" {
		t.Fatalf("outputs = %+v", pr.Outputs)
	}

	if len(pr.Outputs[0].Streams) != 2 || pr.Outputs[0].Streams[1].Disposition != "copy" {
		t.Fatalf("streams = %+v", pr.Outputs[0].Streams)
	}

	if len(pr.Analysis) != 3 {
		t.Fatalf("want 3 measurements, got %d", len(pr.Analysis))
	}

	if pr.Analysis[0].Key != "cropdetect.w" || pr.Analysis[0].Value != "160" || pr.Analysis[0].Time != 0.08 {
		t.Fatalf("measurement[0] = %+v", pr.Analysis[0])
	}

	if pr.Analysis[1].Key != "silence_start" || pr.Analysis[2].Key != "silence_end" {
		t.Fatalf("silence events = %+v", pr.Analysis[1:])
	}
}

func TestParseResult_empty(t *testing.T) {
	t.Parallel()

	// No stdout (e.g. a non-process op) → a zero result, not an error.
	pr, err := ParseResult(Result{})
	if err != nil {
		t.Fatalf("ParseResult(empty): %v", err)
	}

	if pr.Outputs != nil || pr.Analysis != nil {
		t.Fatalf("want zero ProcessResult, got %+v", pr)
	}
}

func TestParseResult_malformed(t *testing.T) {
	t.Parallel()

	if _, err := ParseResult(Result{Stdout: "{not json"}); err == nil {
		t.Fatal("want an error for malformed stdout")
	}
}
