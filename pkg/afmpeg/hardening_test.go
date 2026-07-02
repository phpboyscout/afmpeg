package afmpeg_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// newTestRuntimeWith builds a Runtime from the shared test guest with extra
// options (memory ceiling, timeout) layered on top of the module source.
func newTestRuntimeWith(t *testing.T, opts ...afmpeg.Option) *afmpeg.Runtime {
	t.Helper()

	all := append([]afmpeg.Option{afmpeg.WithModuleBytes(guestModule)}, opts...)

	rt, err := afmpeg.New(context.Background(), all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(context.Background()) })

	return rt
}

// TestMemoryLimit_OverAllocationFailsCleanly is the load-bearing finding-1
// acceptance test: a guest that over-allocates under a low ceiling fails cleanly
// (non-zero exit or bounded error) instead of OOM-killing the host, while the
// same allocation under a generous ceiling succeeds. The host process surviving
// is implicit — a wazero-capped guest can never grow past the ceiling, so this
// test itself would not run to completion if the cap were not enforced.
func TestMemoryLimit_OverAllocationFailsCleanly(t *testing.T) {
	t.Parallel()

	const (
		lowCeiling  = 64 << 20  // 64 MB — room for the Go guest baseline, not the alloc
		highCeiling = 512 << 20 // generous headroom
	)

	// Negative control: under a generous ceiling a 128 MB allocation succeeds.
	res, err := newTestRuntimeWith(t, afmpeg.WithMemoryLimit(highCeiling)).
		Run(context.Background(), afero.NewMemMapFs(), "grow:128")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("grow under generous ceiling: res=%+v err=%v, want clean success", res, err)
	}

	// Under the low ceiling a 256 MB allocation must fail — not take down the host.
	res, err = newTestRuntimeWith(t, afmpeg.WithMemoryLimit(lowCeiling)).
		Run(context.Background(), afero.NewMemMapFs(), "grow:256")
	if err == nil && res.ExitCode == 0 {
		t.Fatalf("grow past low ceiling: res=%+v err=%v, want a bounded failure", res, err)
	}
}

// TestMemoryLimit_Unbounded proves WithMemoryLimit(0) removes the cap: an
// allocation that fails under a default runtime succeeds when the cap is off.
func TestMemoryLimit_Unbounded(t *testing.T) {
	t.Parallel()

	res, err := newTestRuntimeWith(t, afmpeg.WithMemoryLimit(0)).
		Run(context.Background(), afero.NewMemMapFs(), "grow:256")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("grow with cap removed: res=%+v err=%v, want clean success", res, err)
	}
}

// TestTimeout_ImposedDefault is the load-bearing finding-2 acceptance test: an
// invocation with a context that has no deadline, against a non-terminating job,
// returns a bounded "aborted" error within ~the configured timeout AND the
// Runtime stays usable afterwards (proving r.mu was released, not wedged).
func TestTimeout_ImposedDefault(t *testing.T) {
	t.Parallel()

	rt := newTestRuntimeWith(t, afmpeg.WithTimeout(150*time.Millisecond))

	start := time.Now()

	_, err := rt.Run(context.Background(), afero.NewMemMapFs(), "sleep")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sleep under imposed timeout: err = %v, want context.DeadlineExceeded", err)
	}

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took %v, want it bounded near 150ms", elapsed)
	}

	// The Runtime must still work — the hung invocation released the mutex.
	res, err := rt.Run(context.Background(), afero.NewMemMapFs(), "exit:0")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("Run after a timed-out invocation: res=%+v err=%v, want usable Runtime", res, err)
	}
}

// TestTimeout_CallerDeadlineHonoured proves a caller deadline earlier than the
// default is honoured (min, never extended): a 100ms caller deadline against a
// runtime with a 1h default aborts in ~100ms.
func TestTimeout_CallerDeadlineHonoured(t *testing.T) {
	t.Parallel()

	rt := newTestRuntimeWith(t, afmpeg.WithTimeout(1*time.Hour))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()

	_, err := rt.Run(ctx, afero.NewMemMapFs(), "sleep")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sleep under caller deadline: err = %v, want context.DeadlineExceeded", err)
	}

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("caller deadline took %v, want it bounded near 100ms", elapsed)
	}
}
