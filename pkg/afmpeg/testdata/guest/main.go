// Command guest is a tiny WASI program compiled to wasm32-wasi by the afmpeg
// runtime tests. It stands in for ffmpeg.wasm: its argument protocol lets a test
// drive exit codes, stdout/stderr, file I/O over the mounted vfs, the
// seek-on-write (moov) round-trip, an ffprobe-shaped duration response, and a
// cancellable busy loop.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func main() {
	args := os.Args[1:] // drop the program name ("ffmpeg")

	if len(args) > 0 && strings.HasPrefix(args[0], "{") {
		os.Exit(jobMode(args[0]))
	}

	if len(args) == 0 {
		os.Exit(0)
	}

	os.Exit(dispatch(args))
}

// jobMode handles a JSON job spec (the ffmpeg-wasi vocabulary). For "op":"probe"
// it emits a probe response on stdout — the input path selects the response so
// tests can drive success, an input error, and malformed output. Any other op
// just succeeds.
func jobMode(spec string) int {
	var job struct {
		Op     string `json:"op"`
		Inputs []struct {
			Path string `json:"path"`
		} `json:"inputs"`
	}

	if err := json.Unmarshal([]byte(spec), &job); err != nil {
		fmt.Fprintln(os.Stderr, "bad job spec")
		return 2
	}

	if job.Op != "probe" {
		return 0
	}

	path := ""
	if len(job.Inputs) > 0 {
		path = job.Inputs[0].Path
	}

	switch path {
	case "fail-probe":
		fmt.Printf("{\"inputs\":[{\"path\":%q,\"error\":\"could not open input\"}]}\n", path)
	case "bad-json":
		fmt.Print("not json\n")
	default:
		fmt.Printf("{\"inputs\":[{\"path\":%q,\"format\":\"mov\",\"duration_sec\":12.34,"+
			"\"streams\":[{\"index\":0,\"type\":\"video\",\"codec\":\"h264\",\"width\":640,\"height\":480}]}]}\n", path)
	}

	return 0
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
