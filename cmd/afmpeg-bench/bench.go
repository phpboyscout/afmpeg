package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// A workload is one representative job, expressed twice: as an afmpeg Command
// (for the WASM runtime) and as native ffmpeg args (for the baseline). Both sides
// take the H.264 encoder name so we can sweep openh264/libx264.
type workload struct {
	name string
	desc string
	// twoInputs feeds the input file to the command twice (for xfade/amix).
	twoInputs bool
	filter    string
	maps      []string
	// native returns the ffmpeg args (without the trailing output path).
	nativeArgs func(enc, in string) []string
}

// workloads is the process-op set. Each runs on every variant + native.
var workloads = []workload{
	{
		name:   "transcode",
		desc:   "decode → scale 160 → H.264/AAC mp4 (encode-dominated)",
		filter: "[0:v]scale=160:-2[v]",
		maps:   []string{"[v]", "0:a"},
		nativeArgs: func(enc, in string) []string {
			return []string{"-i", in, "-filter_complex", "[0:v]scale=160:-2[v]", "-map", "[v]", "-map", "0:a", "-c:v", enc, "-c:a", "aac"}
		},
	},
	{
		name:      "reel",
		desc:      "two inputs → xfade + amix → H.264/AAC mp4 (filter + encode, keyrx-shaped)",
		twoInputs: true,
		filter:    "[0:v][1:v]xfade=transition=fade:duration=1:offset=2[v];[0:a][1:a]amix=inputs=2[a]",
		maps:      []string{"[v]", "[a]"},
		nativeArgs: func(enc, in string) []string {
			return []string{"-i", in, "-i", in, "-filter_complex", "[0:v][1:v]xfade=transition=fade:duration=1:offset=2[v];[0:a][1:a]amix=inputs=2[a]", "-map", "[v]", "-map", "[a]", "-c:v", enc, "-c:a", "aac"}
		},
	},
}

// command renders a workload into an afmpeg Command for the given encoder,
// optionally overlaying extra output options (e.g. an x264 preset).
func (w workload) command(enc string, opts map[string]string) afmpeg.Command {
	inputs := []afmpeg.Input{{Path: inName}}
	if w.twoInputs {
		inputs = append(inputs, afmpeg.Input{Path: inName})
	}

	return afmpeg.Command{
		Inputs:        inputs,
		FilterComplex: w.filter,
		Outputs: []afmpeg.Output{{
			Path:       outName,
			Map:        w.maps,
			VideoCodec: enc,
			AudioCodec: "aac",
			Options:    opts,
		}},
	}
}

const (
	inName  = "in.mp4"
	outName = "out.mp4"
)

// stat is a set of timed runs.
type stat struct {
	runs []time.Duration
}

func (s stat) median() time.Duration {
	if len(s.runs) == 0 {
		return 0
	}

	d := append([]time.Duration(nil), s.runs...)
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })

	return d[len(d)/2]
}

func (s stat) min() time.Duration {
	if len(s.runs) == 0 {
		return 0
	}

	m := s.runs[0]
	for _, d := range s.runs[1:] {
		if d < m {
			m = d
		}
	}

	return m
}

// measure times fn `runs` times, aborting on the first error.
func measure(runs int, fn func() error) (stat, error) {
	var s stat

	for i := 0; i < runs; i++ {
		start := time.Now()
		if err := fn(); err != nil {
			return s, err
		}

		s.runs = append(s.runs, time.Since(start))
	}

	return s, nil
}

// inputFS returns a fresh in-memory filesystem preloaded with the fixture — the
// afero model the WASM backend runs against (no host disk touched).
func inputFS(fixture []byte) (afero.Fs, error) {
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, inName, fixture, 0o644); err != nil {
		return nil, err
	}

	return fs, nil
}

// runWASM runs one process job through the afmpeg runtime over an in-memory fs.
// A non-zero engine exit is surfaced as an error so a failing workload never
// pollutes the timings.
func runWASM(ctx context.Context, rt *afmpeg.Runtime, fs afero.Fs, cmd afmpeg.Command) error {
	_ = fs.Remove(outName) // avoid measuring an overwrite-vs-create difference

	res, err := rt.RunJob(ctx, fs, cmd)
	if err != nil {
		return err
	}

	if res.ExitCode != 0 {
		return fmt.Errorf("engine exit %d: %s", res.ExitCode, lastLine(res.Stderr))
	}

	return nil
}

// runNative runs the equivalent job through the host ffmpeg to a temp output.
func runNative(ctx context.Context, bin string, args []string, out string) error {
	full := append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)
	full = append(full, out)

	cmd := exec.CommandContext(ctx, bin, full...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("native ffmpeg: %w: %s", err, lastLine(stderr.String()))
	}

	return nil
}

// makeFixture synthesises the source clip with native ffmpeg (testsrc2 + sine →
// H.264/AAC mp4) and returns both its bytes (for afero) and a temp path (for the
// native baseline). Caller removes the temp dir.
func makeFixture(ctx context.Context, bin, dir string) (bytes []byte, path string, err error) {
	path = filepath.Join(dir, "in.mp4")
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", path,
	}

	if out, e := exec.CommandContext(ctx, bin, args...).CombinedOutput(); e != nil {
		return nil, "", fmt.Errorf("make fixture: %w: %s", e, lastLine(string(out)))
	}

	b, e := os.ReadFile(path) //nolint:gosec // path is harness-controlled
	if e != nil {
		return nil, "", e
	}

	return b, path, nil
}

// fleetThroughput measures batch throughput: N jobs run serially on one Runtime
// versus across a pool of `workers` Runtimes (the instance-level parallelism lever
// from spec 0008 §4.1). Returns (serial, parallel) wall-clock for the whole batch.
func fleetThroughput(ctx context.Context, newRT func() (*afmpeg.Runtime, error), fixture []byte, cmd afmpeg.Command, jobs, workers int) (serial, parallel time.Duration, err error) {
	one, err := newRT()
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = one.Close(ctx) }()

	job := func(rt *afmpeg.Runtime) error {
		fs, e := inputFS(fixture)
		if e != nil {
			return e
		}

		return runWASM(ctx, rt, fs, cmd)
	}

	start := time.Now()
	for i := 0; i < jobs; i++ {
		if e := job(one); e != nil {
			return 0, 0, e
		}
	}
	serial = time.Since(start)

	pool := make([]*afmpeg.Runtime, workers)
	for i := range pool {
		if pool[i], err = newRT(); err != nil {
			return 0, 0, err
		}
	}
	defer func() {
		for _, rt := range pool {
			_ = rt.Close(ctx)
		}
	}()

	start = time.Now()
	queue := make(chan int, jobs)
	for i := 0; i < jobs; i++ {
		queue <- i
	}
	close(queue)

	var wg sync.WaitGroup

	errs := make(chan error, workers)

	for w := 0; w < workers; w++ {
		wg.Add(1)

		go func(rt *afmpeg.Runtime) {
			defer wg.Done()

			for range queue {
				if e := job(rt); e != nil {
					errs <- e

					return
				}
			}
		}(pool[w])
	}

	wg.Wait()
	close(errs)

	if e := <-errs; e != nil {
		return 0, 0, e
	}

	parallel = time.Since(start)

	return serial, parallel, nil
}

// lastLine returns the final non-empty line of s (for terse error context).
func lastLine(s string) string {
	var last string

	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			if line := s[start:i]; line != "" {
				last = line
			}

			start = i + 1
		}
	}

	return last
}
