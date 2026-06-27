package afmpeg

import (
	"strings"
	"testing"
)

func TestTail(t *testing.T) {
	t.Parallel()

	if got := tail("short"); got != "short" {
		t.Fatalf("tail(short) = %q, want short", got)
	}

	long := strings.Repeat("x", stderrTailBytes+100)
	if got := tail(long); len(got) != stderrTailBytes {
		t.Fatalf("tail(long) length = %d, want %d", len(got), stderrTailBytes)
	}
}

func TestParseDuration(t *testing.T) {
	t.Parallel()

	dur, err := parseDuration("  12.34\n")
	if err != nil || dur != 12.34 {
		t.Fatalf("parseDuration = %v err=%v, want 12.34", dur, err)
	}

	if _, err := parseDuration("not-a-number"); err == nil {
		t.Fatal("parseDuration(non-numeric): want an error")
	}
}

func TestProbeArgs(t *testing.T) {
	t.Parallel()

	args := probeArgs("clip.mp4")

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-show_entries format=duration") {
		t.Fatalf("probeArgs missing duration query: %v", args)
	}

	if args[len(args)-1] != "clip.mp4" {
		t.Fatalf("probeArgs last arg = %q, want the path", args[len(args)-1])
	}
}
