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

func TestParseDurationFromStderr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stderr  string
		want    float64
		wantErr bool
	}{
		{"simple", "  Duration: 00:00:12.34, start: 0.0", 12.34, false},
		{"hours-minutes", "Duration: 01:23:45.67, bitrate: 1", 1*3600 + 23*60 + 45.67, false},
		{"na", "  Duration: N/A, start: 0.0", 0, true},
		{"missing", "No such file or directory", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseDurationFromStderr(tt.stderr)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %v", got)
				}

				return
			}

			if err != nil || got != tt.want {
				t.Fatalf("= %v err=%v, want %v", got, err, tt.want)
			}
		})
	}
}
