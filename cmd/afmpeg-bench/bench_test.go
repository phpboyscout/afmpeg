package main

import (
	"testing"
	"time"
)

func TestStatMedianAndMin(t *testing.T) {
	t.Parallel()

	s := stat{runs: []time.Duration{30, 10, 20}}

	if got := s.median(); got != 20 {
		t.Errorf("median = %v, want 20", got)
	}

	if got := s.min(); got != 10 {
		t.Errorf("min = %v, want 10", got)
	}

	var empty stat
	if got := empty.median(); got != 0 {
		t.Errorf("empty median = %v, want 0", got)
	}
}

func TestRatioAndPerSec(t *testing.T) {
	t.Parallel()

	if got := ratio(10, 5); got != 2 {
		t.Errorf("ratio = %v, want 2", got)
	}

	if got := ratio(1, 0); got != 0 {
		t.Errorf("ratio by zero = %v, want 0", got)
	}

	if got := perSec(10, 2*time.Second); got != 5 {
		t.Errorf("perSec = %v, want 5", got)
	}
}

func TestLastLine(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a\nb\nc":      "c",
		"a\nb\n":       "b",
		"single":       "single",
		"":             "",
		"x\n\n\ny\n\n": "y",
	}

	for in, want := range cases {
		if got := lastLine(in); got != want {
			t.Errorf("lastLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDur(t *testing.T) {
	t.Parallel()

	if got := dur(0); got != "—" {
		t.Errorf("dur(0) = %q", got)
	}

	if got := dur(250 * time.Millisecond); got != "250 ms" {
		t.Errorf("dur(250ms) = %q", got)
	}

	if got := dur(1500 * time.Millisecond); got != "1.50 s" {
		t.Errorf("dur(1.5s) = %q", got)
	}
}
