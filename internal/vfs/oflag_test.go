package vfs

import (
	"os"
	"testing"

	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
)

func TestToOSFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   experimentalsys.Oflag
		want int
	}{
		{"rdonly", experimentalsys.O_RDONLY, os.O_RDONLY},
		{"rdwr", experimentalsys.O_RDWR, os.O_RDWR},
		{"wronly", experimentalsys.O_WRONLY, os.O_WRONLY},
		{
			"wronly+create+trunc",
			experimentalsys.O_WRONLY | experimentalsys.O_CREAT | experimentalsys.O_TRUNC,
			os.O_WRONLY | os.O_CREATE | os.O_TRUNC,
		},
		{"rdwr+append", experimentalsys.O_RDWR | experimentalsys.O_APPEND, os.O_RDWR | os.O_APPEND},
		{"create+excl", experimentalsys.O_CREAT | experimentalsys.O_EXCL, os.O_CREATE | os.O_EXCL},
		{"wronly+sync", experimentalsys.O_WRONLY | experimentalsys.O_SYNC, os.O_WRONLY | os.O_SYNC},
		{"directory-flag-dropped", experimentalsys.O_RDONLY | experimentalsys.O_DIRECTORY, os.O_RDONLY},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := toOSFlag(tt.in); got != tt.want {
				t.Fatalf("toOSFlag(%v) = %#x, want %#x", tt.in, got, tt.want)
			}
		})
	}
}
