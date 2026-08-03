package afmpeg

import (
	"bytes"
	"context"

	"github.com/spf13/afero"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/experimental/sysfs"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	wsys "github.com/tetratelabs/wazero/sys"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/afmpeg/internal/vfs"
)

// wasmBackend runs the engine as a sandboxed wasm module via wazero — the default
// backend (spec 0028 D-0028-C). The module is compiled once (newWASMBackend) and
// re-instantiated per invocation.
type wasmBackend struct {
	rt       wazero.Runtime
	compiled wazero.CompiledModule
}

// wasmBackend is the default backend implementation.
var _ Backend = (*wasmBackend)(nil)

// newWASMBackend builds the wazero runtime — the memory cap (spec 0027 §4A), the
// setjmp/longjmp env module, and WASI — and compiles cfg.module. The compile is
// the expensive step; it happens once per Runtime.
func newWASMBackend(ctx context.Context, cfg *config) (*wasmBackend, error) {
	rtCfg := wazero.NewRuntimeConfig().
		WithCoreFeatures(runtimeCoreFeatures).
		WithCloseOnContextDone(true)

	if pages := memoryLimitPages(cfg.memoryLimitBytes); pages > 0 {
		rtCfg = rtCfg.WithMemoryLimitPages(pages)
	}

	rt := wazero.NewRuntimeWithConfig(ctx, rtCfg)

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

	return &wasmBackend{rt: rt, compiled: compiled}, nil
}

// Close releases the wazero runtime (and with it the compiled module).
// supportsEngineProgress reports that this backend can deliver phase-B engine
// records: it serves /dev/afmpeg-progress from the vfs bridge (spec 0032), so
// Fraction may wait for the engine's first record (spec 0034, D1).
func (*wasmBackend) supportsEngineProgress() bool { return true }

func (b *wasmBackend) Close(ctx context.Context) error {
	return errors.Wrap(b.rt.Close(ctx), "afmpeg: close runtime")
}

// Invoke instantiates the module once with fs mounted and args applied, capturing
// stdout and stderr into a Result. The module name is cleared so the compiled
// module can be instantiated repeatedly across calls.
func (b *wasmBackend) Invoke(ctx context.Context, fs afero.Fs, args ...string) (Result, error) {
	mount, err := mountConfig(fs, progressSinkFrom(ctx))
	if err != nil {
		return Result{}, err
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
	mod, instErr := b.rt.InstantiateModule(withSetjmp(ctx), b.compiled, cfg)
	if mod != nil {
		_ = mod.Close(ctx)
	}

	exitCode, err := exitCodeFrom(ctx, instErr)
	if err != nil {
		return Result{}, err
	}

	return Result{ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

// mountConfig builds a wazero FSConfig that mounts the vfs bridge over fs at the
// guest root. When sink is non-nil (a progress-reporting invocation, spec 0032),
// the bridge also serves /dev/afmpeg-progress as a write-device feeding sink.
func mountConfig(fs afero.Fs, sink func([]byte)) (wazero.FSConfig, error) {
	sysCfg, ok := wazero.NewFSConfig().(sysfs.FSConfig)
	if !ok {
		return nil, errors.New("afmpeg: wazero FSConfig does not support sys.FS mounts")
	}

	var opts []vfs.Option
	if sink != nil {
		opts = append(opts, vfs.WithProgressSink(sink))
	}

	return sysCfg.WithSysFSMount(vfs.New(fs, opts...), "/"), nil
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
