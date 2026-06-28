package afmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
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
	// guestName is argv[0] presented to the guest module.
	guestName = "ffmpeg"

	// stderrTailBytes bounds how much module stderr is surfaced on a failure.
	stderrTailBytes = 1500
)

// ErrNoModule is returned by New when no wasm module source is configured. The
// module is never embedded, so a WithModule* option is mandatory (spec 0004 D-C).
var ErrNoModule = errors.New("afmpeg: no wasm module configured (use WithModuleFile, WithModuleBytes, or WithModuleFS)")

// Runtime holds the compiled wazero module and runtime. Build it once with New —
// compilation is the expensive step — and reuse it. Run serialises invocations:
// one at a time per Runtime (spec 0004 D-0004-B).
type Runtime struct {
	rt       wazero.Runtime
	compiled wazero.CompiledModule
	mu       sync.Mutex
}

// Result is the outcome of a Run: the exit code and the module's captured stdout
// and stderr. A non-zero ExitCode is reported here with a nil error; only
// host-side failures (module instantiation, the vfs bridge, context
// cancellation) return an error. The ffmpeg-wasi engine returns its structured
// results (process status, probe info) as JSON on Stdout.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Probe describes a media input: its container format, duration, and streams.
type Probe struct {
	Format      string
	DurationSec float64
	Streams     []ProbeStream
}

// ProbeStream is one stream within a probed input.
type ProbeStream struct {
	Index      int    `json:"index"`
	Type       string `json:"type"` // "video" or "audio"
	Codec      string `json:"codec"`
	Width      int    `json:"width"`       // video
	Height     int    `json:"height"`      // video
	SampleRate int    `json:"sample_rate"` // audio
	Channels   int    `json:"channels"`    // audio
}

// config accumulates New's options.
type config struct {
	module []byte
	fetch  func(context.Context) ([]byte, error)
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

	// A deferred source (e.g. WithModuleURL) is resolved here, with New's context,
	// unless module bytes were supplied directly.
	if len(cfg.module) == 0 && cfg.fetch != nil {
		module, err := cfg.fetch(ctx)
		if err != nil {
			return nil, err
		}

		cfg.module = module
	}

	if len(cfg.module) == 0 {
		return nil, ErrNoModule
	}

	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCoreFeatures(runtimeCoreFeatures).
		WithCloseOnContextDone(true))

	if err := instantiateEnv(ctx, rt); err != nil {
		_ = rt.Close(ctx)

		return nil, err
	}

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

	return Result{ExitCode: inv.exitCode, Stdout: inv.stdout, Stderr: inv.stderr}, nil
}

// Probe reports a media file's container/stream info over the fs bridge, via the
// ffmpeg-wasi engine's probe op (`{"op":"probe"}`), reading the structured JSON it
// returns on stdout. It needs the ffmpeg-wasi engine (the structured module).
func (r *Runtime) Probe(ctx context.Context, fs afero.Fs, path string) (Probe, error) {
	spec, err := json.Marshal(jobSpec{Op: "probe", Inputs: []jobInput{{Path: path}}})
	if err != nil {
		return Probe{}, errors.Wrap(err, "afmpeg: marshal probe spec")
	}

	res, err := r.Run(ctx, fs, string(spec))
	if err != nil {
		return Probe{}, err
	}

	if res.ExitCode != 0 {
		return Probe{}, errors.Newf("afmpeg: probe %q: %s", path, tail(res.Stderr))
	}

	var out struct {
		Inputs []struct {
			Error       string        `json:"error"`
			Format      string        `json:"format"`
			DurationSec float64       `json:"duration_sec"`
			Streams     []ProbeStream `json:"streams"`
		} `json:"inputs"`
	}

	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		return Probe{}, errors.Wrapf(err, "afmpeg: probe %q: parse engine output", path)
	}

	if len(out.Inputs) == 0 {
		return Probe{}, errors.Newf("afmpeg: probe %q: no input in engine output", path)
	}

	in := out.Inputs[0]
	if in.Error != "" {
		return Probe{}, errors.Newf("afmpeg: probe %q: %s", path, in.Error)
	}

	return Probe{Format: in.Format, DurationSec: in.DurationSec, Streams: in.Streams}, nil
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

	// withSetjmp enables the setjmp/longjmp snapshotter for this invocation; the
	// guest ffmpeg's setjmp/longjmp lower to the env host module (setjmp.go).
	mod, instErr := r.rt.InstantiateModule(withSetjmp(ctx), r.compiled, cfg)
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

// tail returns the last stderrTailBytes of s, for surfacing an error tail.
func tail(s string) string {
	if len(s) > stderrTailBytes {
		return s[len(s)-stderrTailBytes:]
	}

	return s
}
