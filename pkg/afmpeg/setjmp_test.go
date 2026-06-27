package afmpeg

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
)

func TestSjljStore(t *testing.T) {
	t.Parallel()

	s := newSjljStore()
	if _, ok := s.load(7); ok {
		t.Fatal("empty store should miss")
	}

	s.save(7, nil) // experimental.Snapshot is an interface; nil exercises the map
	if _, ok := s.load(7); !ok {
		t.Fatal("saved jmp_buf should hit")
	}
}

func TestStoreFrom(t *testing.T) {
	t.Parallel()

	if storeFrom(context.Background()) != nil {
		t.Fatal("bare context should carry no store")
	}

	if storeFrom(withSetjmp(context.Background())) == nil {
		t.Fatal("withSetjmp should attach a store")
	}
}

// TestSetjmpLongjmp_NoStore covers the safe no-op paths when no store is present
// (the snapshotter-backed paths are exercised by the real-ffmpeg integration
// test, which needs an actual ffmpeg.wasm).
func TestSetjmpLongjmp_NoStore(t *testing.T) {
	t.Parallel()

	setjmp(context.Background(), nil, []uint64{0, 0, 0})
	longjmp(context.Background(), nil, []uint64{0, 0})
}

// TestLongjmp_MissingSnapshot covers longjmp's miss path (a jmp_buf with no saved
// snapshot is a no-op rather than a panic).
func TestLongjmp_MissingSnapshot(t *testing.T) {
	t.Parallel()

	longjmp(withSetjmp(context.Background()), nil, []uint64{99, 0})
}

func TestInstantiateEnv(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)

	t.Cleanup(func() { _ = rt.Close(ctx) })

	if err := instantiateEnv(ctx, rt); err != nil {
		t.Fatalf("instantiateEnv: %v", err)
	}
}
