// Command guest is a tiny WASI program compiled to wasm32-wasi by the afmpeg
// runtime tests. It stands in for ffmpeg.wasm: its argument protocol lets a test
// drive exit codes, stdout/stderr, file I/O over the mounted vfs, the
// seek-on-write (moov) round-trip, an ffprobe-shaped duration response, and a
// cancellable busy loop.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func main() {
	args := os.Args[1:] // drop the program name ("ffmpeg")

	if code, ok := probeMode(args); ok {
		os.Exit(code)
	}

	if len(args) == 0 {
		os.Exit(0)
	}

	os.Exit(dispatch(args))
}

// probeMode handles ffprobe-shaped invocations. The probed path selects the
// response so tests can drive the duration, a probe failure, and an unparseable
// duration.
func probeMode(args []string) (int, bool) {
	for _, a := range args {
		if a != "-show_entries" {
			continue
		}

		switch args[len(args)-1] {
		case "fail-probe":
			fmt.Fprint(os.Stderr, "probe boom")
			return 1, true
		case "bad-duration":
			fmt.Fprint(os.Stdout, "notanumber\n")
			return 0, true
		default:
			fmt.Fprint(os.Stdout, "12.340\n")
			return 0, true
		}
	}

	return 0, false
}

func dispatch(args []string) int {
	cmd := args[0]

	switch {
	case strings.HasPrefix(cmd, "exit:"):
		n, _ := strconv.Atoi(strings.TrimPrefix(cmd, "exit:"))
		return n
	case strings.HasPrefix(cmd, "stderr:"):
		fmt.Fprint(os.Stderr, strings.TrimPrefix(cmd, "stderr:"))
		return 1
	case strings.HasPrefix(cmd, "stdout:"):
		fmt.Fprint(os.Stdout, strings.TrimPrefix(cmd, "stdout:"))
		return 0
	case cmd == "copy":
		return copyFile(args[1], args[2])
	case cmd == "moov":
		return moov(args[1])
	case cmd == "sleep":
		for {
			// Busy loop until the host cancels the context.
		}
	}

	return 0
}

func copyFile(src, dst string) int {
	in, err := os.Open(src)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 4
	}

	return 0
}

// moov reproduces the mp4 +faststart shape: write a placeholder + payload, seek
// back, overwrite the placeholder. This exercises the vfs bridge's seek-on-write
// through a real WASI host.
func moov(path string) int {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer f.Close()

	if _, err := f.Write([]byte("....mdatPAYLOAD")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 4
	}

	if _, err := f.Write([]byte("SIZE")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 5
	}

	return 0
}
