package afmpeg

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

// runtimeCoreFeatures are the WebAssembly features afmpeg's runtime enables. It
// is the stable V2 set (which the Go WASI test guest needs) plus the experimental
// features an FFmpeg build requires — extended-const, tail-call, and (modern,
// exnref-based) exception-handling. The last carries the SjLj lowering that
// libvpx's encoder uses: clang emits setjmp/longjmp as wasm EH via libsetjmp.a,
// whose tag section wazero only loads with this feature on. Without these a real
// ffmpeg.wasm fails to compile under wazero.
const runtimeCoreFeatures = api.CoreFeaturesV2 |
	experimental.CoreFeaturesExtendedConst |
	experimental.CoreFeaturesTailCall |
	experimental.CoreFeaturesExceptionHandling

// sjljKey identifies the per-invocation setjmp/longjmp store on the context.
type sjljKey struct{}

// sjljStore holds the snapshots taken by setjmp, keyed by the guest's jmp_buf
// pointer. Run serialises invocations and each gets a fresh store, so no locking
// is needed.
type sjljStore struct {
	snapshots map[uint32]experimental.Snapshot
}

func newSjljStore() *sjljStore {
	return &sjljStore{snapshots: map[uint32]experimental.Snapshot{}}
}

func (s *sjljStore) save(jmpBuf uint32, snap experimental.Snapshot) {
	s.snapshots[jmpBuf] = snap
}

func (s *sjljStore) load(jmpBuf uint32) (experimental.Snapshot, bool) {
	snap, ok := s.snapshots[jmpBuf]

	return snap, ok
}

func storeFrom(ctx context.Context) *sjljStore {
	store, _ := ctx.Value(sjljKey{}).(*sjljStore)

	return store
}

// withSetjmp returns a context carrying a fresh setjmp/longjmp store with
// wazero's snapshotter enabled, for one invocation.
func withSetjmp(ctx context.Context) context.Context {
	return experimental.WithSnapshotter(context.WithValue(ctx, sjljKey{}, newSjljStore()))
}

// FFmpeg is compiled with C setjmp/longjmp, which WebAssembly has no native
// equivalent for; the build lowers them to imported host functions
// (env.__wasm_setjmp / __wasm_longjmp). afmpeg implements them with wazero's
// experimental Snapshotter: setjmp records a snapshot of the guest's execution
// keyed by its jmp_buf, and longjmp restores it (transferring control back to
// the setjmp site with the given return value). Providing this env module is a
// hard requirement for loading any real ffmpeg.wasm.
func setjmp(ctx context.Context, _ api.Module, stack []uint64) {
	store := storeFrom(ctx)
	if store == nil {
		return
	}

	store.save(api.DecodeU32(stack[0]), experimental.GetSnapshotter(ctx).Snapshot())
}

func longjmp(ctx context.Context, _ api.Module, stack []uint64) {
	store := storeFrom(ctx)
	if store == nil {
		return
	}

	snap, ok := store.load(api.DecodeU32(stack[0]))
	if !ok {
		return
	}

	snap.Restore(stack[1:]) // does not return — transfers control to the setjmp site
}

// instantiateEnv registers the "env" host module providing setjmp/longjmp.
func instantiateEnv(ctx context.Context, rt wazero.Runtime) error {
	_, err := rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(setjmp),
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export("__wasm_setjmp").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(longjmp),
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export("__wasm_longjmp").
		Instantiate(ctx)

	return errors.Wrap(err, "afmpeg: instantiate env module")
}
