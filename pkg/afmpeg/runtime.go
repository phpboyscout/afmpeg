package afmpeg

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/experimental/sysfs"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	wsys "github.com/tetratelabs/wazero/sys"

	"gitlab.com/phpboyscout/afmpeg/internal/vfs"
)

const (
	// guestName is argv[0] presented to the guest (ffmpeg reads its own name).
	guestName = "ffmpeg"

	// stderrTailBytes bounds how much ffmpeg stderr is surfaced on a failure.
	stderrTailBytes = 1500

	// durationBits is the float precision for parsing a probed duration.
	durationBits = 64
)

// ErrNoModule is returned by New when no wasm module source is configured. The
// GPL ffmpeg.wasm is never embedded, so a WithModule* option is mandatory
// (spec 0004 D-C / D-0004-C).
var ErrNoModule = errors.New("afmpeg: no wasm module configured (use WithModuleFile, WithModuleBytes, or WithModuleFS)")

// Runtime holds the compiled wazero module and runtime. Build it once with New —
// compilation is the expensive step — and reuse it. Run serialises invocations:
// one at a time per Runtime (spec 0004 D-0004-B).
type Runtime struct {
	rt       wazero.Runtime
	compiled wazero.CompiledModule
	mu       sync.Mutex
}

// Result is the outcome of a Run: the ffmpeg exit code and its captured stderr.
// A non-zero ExitCode is reported here with a nil error; only host-side failures
// (module instantiation, the vfs bridge, context cancellation) return an error.
type Result struct {
	ExitCode int
	Stderr   string
}

// Probe is the outcome of a Probe: the input's duration in seconds.
type Probe struct {
	DurationSec float64
}

// config accumulates New's options.
type config struct {
	module []byte
}

// Option configures a Runtime at construction.
type Option func(*config) error

// WithModuleBytes supplies the ffmpeg.wasm module as raw bytes.
func WithModuleBytes(module []byte) Option {
	return func(c *config) error {
		c.module = module

		return nil
	}
}

// WithModuleFile loads the ffmpeg.wasm module from a host filesystem path.
func WithModuleFile(path string) Option {
	return func(c *config) error {
		module, err := os.ReadFile(path) //nolint:gosec // caller-supplied module path is intended (spec 0004 D-C)
		if err != nil {
			return errors.Wrap(err, "afmpeg: read module file")
		}

		c.module = module

		return nil
	}
}

// WithModuleFS loads the ffmpeg.wasm module from an afero filesystem.
func WithModuleFS(fs afero.Fs, path string) Option {
	return func(c *config) error {
		module, err := afero.ReadFile(fs, path)
		if err != nil {
			return errors.Wrap(err, "afmpeg: read module from fs")
		}

		c.module = module

		return nil
	}
}

// New compiles the configured wasm module once and returns a reusable Runtime.
// Exactly one WithModule* option is required.
func New(ctx context.Context, opts ...Option) (*Runtime, error) {
	cfg := &config{}

	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}

	if len(cfg.module) == 0 {
		return nil, ErrNoModule
	}

	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	compiled, err := rt.CompileModule(ctx, cfg.module)
	if err != nil {
		_ = rt.Close(ctx)

		return nil, errors.Wrap(err, "afmpeg: compile module")
	}

	return &Runtime{rt: rt, compiled: compiled}, nil
}

// Close releases the runtime's resources.
func (r *Runtime) Close(ctx context.Context) error {
	return errors.Wrap(r.rt.Close(ctx), "afmpeg: close runtime")
}

// Run executes one ffmpeg invocation with its filesystem bridged to fs (paths in
// args resolve against fs, e.g. "in/clip.mp4", "out/reel.mp4"). It returns the
// exit code and captured stderr; a non-zero exit is reported in Result with a
// nil error. Only host-side failures return a non-nil error.
func (r *Runtime) Run(ctx context.Context, fs afero.Fs, args ...string) (Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	inv, err := r.invoke(ctx, fs, args...)
	if err != nil {
		return Result{}, err
	}

	return Result{ExitCode: inv.exitCode, Stderr: inv.stderr}, nil
}

// Probe returns a media file's duration in seconds over the same fs bridge
// (R-AF-5). It runs the module with ffprobe-shaped arguments and parses the
// reported duration.
func (r *Runtime) Probe(ctx context.Context, fs afero.Fs, path string) (Probe, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	inv, err := r.invoke(ctx, fs, probeArgs(path)...)
	if err != nil {
		return Probe{}, err
	}

	if inv.exitCode != 0 {
		return Probe{}, errors.Newf("afmpeg: probe failed (exit %d): %s", inv.exitCode, tail(inv.stderr))
	}

	dur, err := parseDuration(inv.stdout)
	if err != nil {
		return Probe{}, err
	}

	return Probe{DurationSec: dur}, nil
}

// invocation is the internal outcome of running the module once.
type invocation struct {
	exitCode int
	stdout   string
	stderr   string
}

// invoke instantiates the module once with fs mounted and args applied, capturing
// stdout and stderr. The module name is cleared so the compiled module can be
// instantiated repeatedly across calls.
func (r *Runtime) invoke(ctx context.Context, fs afero.Fs, args ...string) (invocation, error) {
	mount, err := mountConfig(fs)
	if err != nil {
		return invocation{}, err
	}

	var stdout, stderr bytes.Buffer

	cfg := wazero.NewModuleConfig().
		WithName("").
		WithArgs(append([]string{guestName}, args...)...).
		WithStdout(&stdout).
		WithStderr(&stderr).
		WithFSConfig(mount)

	mod, instErr := r.rt.InstantiateModule(ctx, r.compiled, cfg)
	if mod != nil {
		_ = mod.Close(ctx)
	}

	exitCode, err := exitCodeFrom(ctx, instErr)
	if err != nil {
		return invocation{}, err
	}

	return invocation{exitCode: exitCode, stdout: stdout.String(), stderr: stderr.String()}, nil
}

// mountConfig builds a wazero FSConfig that mounts the vfs bridge over fs at the
// guest root.
func mountConfig(fs afero.Fs) (wazero.FSConfig, error) {
	sysCfg, ok := wazero.NewFSConfig().(sysfs.FSConfig)
	if !ok {
		return nil, errors.New("afmpeg: wazero FSConfig does not support sys.FS mounts")
	}

	return sysCfg.WithSysFSMount(vfs.New(fs), "/"), nil
}

// exitCodeFrom interprets an InstantiateModule error: a cancelled context is a
// host-side abort, a wazero ExitError carries the guest exit code, and anything
// else is a real failure.
func exitCodeFrom(ctx context.Context, err error) (int, error) {
	if err == nil {
		return 0, nil
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return 0, errors.Wrap(ctxErr, "afmpeg: invocation aborted")
	}

	var exitErr *wsys.ExitError
	if errors.As(err, &exitErr) {
		return int(exitErr.ExitCode()), nil
	}

	return 0, errors.Wrap(err, "afmpeg: invocation failed")
}

// probeArgs builds the ffprobe-shaped argument list for a duration probe,
// mirroring keryx's probe (format=duration, no wrappers, no key).
func probeArgs(path string) []string {
	return []string{
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	}
}

// parseDuration parses the numeric duration ffprobe prints on stdout.
func parseDuration(stdout string) (float64, error) {
	trimmed := strings.TrimSpace(stdout)

	dur, err := strconv.ParseFloat(trimmed, durationBits)
	if err != nil {
		return 0, errors.Wrapf(err, "afmpeg: parse duration %q", trimmed)
	}

	return dur, nil
}

// tail returns the last stderrTailBytes of s, for surfacing an error tail.
func tail(s string) string {
	if len(s) > stderrTailBytes {
		return s[len(s)-stderrTailBytes:]
	}

	return s
}
