package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// results is everything the report renders.
type results struct {
	env        env
	lgplModule string
	gplModule  string
	runs       int
	workloads  []workloadResult
	presets    []presetRow
	fleet      *fleetResult
	frames     *framesResult
	notes      []string // measurements that errored (recorded, not fatal)
}

// note records a measurement failure without aborting the run — a measurement rig
// must survive one bad workload and still report the rest.
func (r *results) note(format string, a ...any) {
	r.notes = append(r.notes, fmt.Sprintf(format, a...))
}

type workloadResult struct {
	name, desc                         string
	openh264, x264, native             stat
	haveOpenh264, haveX264, haveNative bool
}

type presetRow struct {
	preset       string
	x264, native stat
}

type fleetResult struct {
	jobs, workers    int
	serial, parallel time.Duration
}

type framesResult struct {
	x264, native stat
}

// presets is the x264 speed/quality sweep (GPL module + native baseline). Kept to
// the extremes + one middle so the WASM sweep stays within a sane wall-clock.
var presets = []string{"ultrafast", "veryfast", "medium"}

// fleetPreset keeps the throughput experiment's per-job cost bounded (x264 medium
// single-threaded in WASM is slow); ultrafast makes the batch tractable while
// still exercising the full decode→scale→encode→mux path per job.
const fleetPreset = "ultrafast"

func newRuntime(ctx context.Context, module string) (*afmpeg.Runtime, error) {
	return afmpeg.New(ctx, afmpeg.WithModuleFile(module), afmpeg.WithTimeout(5*time.Minute))
}

func run(ctx context.Context, o options) error {
	if o.lgplModule == "" && o.gplModule == "" {
		return fmt.Errorf("need at least one of -lgpl / -gpl (a built ffmpeg-wasi module)")
	}

	res := results{env: gatherEnv(ctx, o.nativeBin), lgplModule: o.lgplModule, gplModule: o.gplModule, runs: o.runs}

	dir, err := os.MkdirTemp("", "afmpeg-bench-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	fmt.Fprintln(os.Stderr, "synthesising fixture (testsrc2 640x480 4s + sine → H.264/AAC)…")

	fixture, nativeIn, err := makeFixture(ctx, o.nativeBin, dir)
	if err != nil {
		return err
	}

	nativeOut := func(ext string) string { return dir + "/out." + ext }

	// Build one runtime per available variant (compile once; reused across runs).
	var lgpl, gpl *afmpeg.Runtime

	if o.lgplModule != "" {
		if lgpl, err = newRuntime(ctx, o.lgplModule); err != nil {
			return fmt.Errorf("compile LGPL module: %w", err)
		}
		defer func() { _ = lgpl.Close(ctx) }()
	}

	if o.gplModule != "" {
		if gpl, err = newRuntime(ctx, o.gplModule); err != nil {
			return fmt.Errorf("compile GPL module: %w", err)
		}
		defer func() { _ = gpl.Close(ctx) }()
	}

	// 1. Per-workload timings across the encoder axis.
	for _, w := range workloads {
		fmt.Fprintf(os.Stderr, "workload %-10s …\n", w.name)

		wr := workloadResult{name: w.name, desc: w.desc}

		if lgpl != nil {
			if s, err := measureWASM(ctx, lgpl, fixture, w.command("libopenh264", nil), o.runs); err != nil {
				res.note("wasm openh264 %s: %v", w.name, err)
			} else {
				wr.openh264, wr.haveOpenh264 = s, true
			}
		}

		if gpl != nil {
			if s, err := measureWASM(ctx, gpl, fixture, w.command("libx264", nil), o.runs); err != nil {
				res.note("wasm x264 %s: %v", w.name, err)
			} else {
				wr.x264, wr.haveX264 = s, true
			}
		}

		if s, err := measure(o.runs, func() error {
			return runNative(ctx, o.nativeBin, w.nativeArgs("libx264", nativeIn), nativeOut("mp4"))
		}); err != nil {
			res.note("native %s: %v", w.name, err)
		} else {
			wr.native, wr.haveNative = s, true
		}

		res.workloads = append(res.workloads, wr)
	}

	// 2. x264 preset sweep on the transcode workload (GPL module vs native).
	if gpl != nil {
		tc := workloads[0] // transcode
		fmt.Fprintln(os.Stderr, "x264 preset sweep …")

		for _, p := range presets {
			opts := map[string]string{"preset": p}

			w, err := measureWASM(ctx, gpl, fixture, tc.command("libx264", opts), o.runs)
			if err != nil {
				res.note("wasm preset %s: %v", p, err)

				continue
			}

			args := append(tc.nativeArgs("libx264", nativeIn), "-preset", p)

			n, err := measure(o.runs, func() error { return runNative(ctx, o.nativeBin, args, nativeOut("mp4")) })
			if err != nil {
				res.note("native preset %s: %v", p, err)

				continue
			}

			res.presets = append(res.presets, presetRow{preset: p, x264: w, native: n})
		}
	}

	// 3. Fleet throughput (instance-level parallelism) on the transcode workload.
	if fleetRT := gpl; fleetRT != nil || lgpl != nil {
		module := o.gplModule
		enc := "libx264"
		fleetOpts := map[string]string{"preset": fleetPreset}

		if module == "" {
			module, enc, fleetOpts = o.lgplModule, "libopenh264", nil // openh264 has no x264 preset
		}

		fmt.Fprintf(os.Stderr, "fleet throughput (%d jobs, %d workers) …\n", o.batch, res.env.cpus)

		serial, parallel, err := fleetThroughput(ctx,
			func() (*afmpeg.Runtime, error) { return newRuntime(ctx, module) },
			fixture, workloads[0].command(enc, fleetOpts), o.batch, res.env.cpus)
		if err != nil {
			res.note("fleet: %v", err)
		} else {
			res.fleet = &fleetResult{jobs: o.batch, workers: res.env.cpus, serial: serial, parallel: parallel}
		}
	}

	// 4. Thumbnail (frames op) — decode + scale, minimal encode.
	if gpl != nil || lgpl != nil {
		rt := gpl
		if rt == nil {
			rt = lgpl
		}

		fmt.Fprintln(os.Stderr, "thumbnail (frames op) …")

		fr, ferr := measureFrames(ctx, rt, fixture, o.runs)

		nargs := []string{"-ss", "2", "-i", nativeIn, "-frames:v", "1", "-vf", "scale=160:-2"}
		nf, nerr := measure(o.runs, func() error { return runNative(ctx, o.nativeBin, nargs, nativeOut("png")) })

		switch {
		case ferr != nil:
			res.note("wasm frames: %v", ferr)
		case nerr != nil:
			res.note("native frames: %v", nerr)
		default:
			res.frames = &framesResult{x264: fr, native: nf}
		}
	}

	return emitReport(res, o.out)
}

// measureWASM times a process workload against a fresh in-memory fs each run.
func measureWASM(ctx context.Context, rt *afmpeg.Runtime, fixture []byte, cmd afmpeg.Command, runs int) (stat, error) {
	fs, err := inputFS(fixture)
	if err != nil {
		return stat{}, err
	}

	return measure(runs, func() error { return runWASM(ctx, rt, fs, cmd) })
}

// measureFrames times the frames op (single frame at t=2s, scaled to 160).
func measureFrames(ctx context.Context, rt *afmpeg.Runtime, fixture []byte, runs int) (stat, error) {
	fs, err := inputFS(fixture)
	if err != nil {
		return stat{}, err
	}

	at := 2.0
	job := afmpeg.FrameJob{
		Input:  inName,
		Select: afmpeg.FrameSelect{Timestamp: &at},
		Path:   "thumb.png",
		Codec:  "png",
		Scale:  "160:-2",
	}

	return measure(runs, func() error {
		_ = fs.Remove("thumb.png")

		fr, err := rt.Frames(ctx, fs, job)
		if err != nil {
			return err
		}

		if fr.Count == 0 {
			return fmt.Errorf("frames op produced no frame")
		}

		return nil
	})
}
