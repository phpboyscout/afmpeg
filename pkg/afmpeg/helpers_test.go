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
