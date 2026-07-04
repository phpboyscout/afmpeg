// Command afmpeg-bench is the spec-0008 performance measurement rig: it runs a
// handful of representative media workloads through afmpeg's WASM runtime and
// against the host's native ffmpeg, and emits a markdown report with the
// per-workload timings and the native multiple (the honest wasm-vs-native ratio).
//
// It is an investigation tool, not shipped library surface (hence cmd/, which the
// coverage policy does not measure). Run it with a built module and a native
// ffmpeg on PATH:
//
//	go run ./cmd/afmpeg-bench \
//	  -lgpl  ../ffmpeg-wasi/dist/ffmpeg-wasi-lgpl.wasm \
//	  -gpl   ../ffmpeg-wasi/dist/ffmpeg-wasi-gpl.wasm \
//	  -runs 3 -out docs/development/spikes/0008-perf/REPORT.md
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

func main() {
	opts := parseFlags()

	if err := run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "afmpeg-bench: %v\n", err)
		os.Exit(1)
	}
}

// options are the harness knobs.
type options struct {
	lgplModule string // openh264 (LGPL) module
	gplModule  string // libx264 (GPL) module
	nativeBin  string // native ffmpeg on PATH (or an explicit path)
	runs       int    // timed repetitions per measurement (median reported)
	batch      int    // jobs in the fleet-throughput experiment
	out        string // report path ("" → stdout only)
}

func parseFlags() options {
	var o options

	flag.StringVar(&o.lgplModule, "lgpl", "", "path to the LGPL (openh264) ffmpeg-wasi module")
	flag.StringVar(&o.gplModule, "gpl", "", "path to the GPL (libx264) ffmpeg-wasi module")
	flag.StringVar(&o.nativeBin, "native", "ffmpeg", "native ffmpeg binary (name on PATH or a path)")
	flag.IntVar(&o.runs, "runs", 3, "timed repetitions per measurement (median reported)")
	flag.IntVar(&o.batch, "batch", 16, "number of jobs in the fleet-throughput experiment")
	flag.StringVar(&o.out, "out", "", "write the markdown report here (default: stdout only)")
	flag.Parse()

	return o
}

// env captures the host context that frames every number in the report.
type env struct {
	when         string
	goVersion    string
	goos, goarch string
	cpus         int
	ffmpeg       string
}

func gatherEnv(ctx context.Context, nativeBin string) env {
	return env{
		when:      time.Now().UTC().Format(time.RFC3339),
		goVersion: runtime.Version(),
		goos:      runtime.GOOS,
		goarch:    runtime.GOARCH,
		cpus:      runtime.NumCPU(),
		ffmpeg:    nativeVersion(ctx, nativeBin),
	}
}

func nativeVersion(ctx context.Context, bin string) string {
	out, err := exec.CommandContext(ctx, bin, "-hide_banner", "-version").Output()
	if err != nil {
		return "unavailable"
	}

	// The first line is "ffmpeg version <v> …".
	for i, b := range out {
		if b == '\n' {
			return string(out[:i])
		}
	}

	return string(out)
}
