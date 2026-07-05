package afmpeg

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/afero"
)

// fakeBackend is a test double proving Runtime drives any Backend through the
// exported seam — no wasm module required. It records what it was invoked with
// and returns a canned Result, so the native backend (spec 0028) can slot in
// behind the identical contract.
type fakeBackend struct {
	res     Result
	err     error
	gotArgs []string
	closed  bool
}

func (f *fakeBackend) Invoke(_ context.Context, _ afero.Fs, args ...string) (Result, error) {
	f.gotArgs = args

	return f.res, f.err
}

func (f *fakeBackend) Close(context.Context) error {
	f.closed = true

	return nil
}

func TestRuntime_delegatesRunToBackend(t *testing.T) {
	t.Parallel()

	fb := &fakeBackend{res: Result{ExitCode: 2, Stdout: "OUT", Stderr: "ERR"}}
	rt := &Runtime{backend: fb, sem: make(chan struct{}, 1)}

	res, err := rt.Run(context.Background(), afero.NewMemMapFs(), "the-job-spec")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The backend's Result surfaces verbatim (byte-identical across backends).
	if res.ExitCode != 2 || res.Stdout != "OUT" || res.Stderr != "ERR" {
		t.Fatalf("Result not passed through: %+v", res)
	}

	// The job-spec argv reaches the backend unchanged.
	if len(fb.gotArgs) != 1 || fb.gotArgs[0] != "the-job-spec" {
		t.Fatalf("backend got args %v, want [the-job-spec]", fb.gotArgs)
	}
}

func TestRuntime_runSurfacesBackendError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("backend blew up")
	rt := &Runtime{backend: &fakeBackend{err: sentinel}, sem: make(chan struct{}, 1)}

	if _, err := rt.Run(context.Background(), afero.NewMemMapFs(), "x"); !errors.Is(err, sentinel) {
		t.Fatalf("want the backend error surfaced, got %v", err)
	}
}

func TestRuntime_closeDelegatesAndBlocksRun(t *testing.T) {
	t.Parallel()

	fb := &fakeBackend{}
	rt := &Runtime{backend: fb, sem: make(chan struct{}, 1)}

	if err := rt.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !fb.closed {
		t.Fatal("Close did not delegate to the backend")
	}

	// A Run after Close fails cleanly rather than touching the backend.
	if _, err := rt.Run(context.Background(), afero.NewMemMapFs(), "x"); err == nil {
		t.Fatal("want an error running a closed runtime")
	}
}

// TestWithBackend_injectsAndSkipsModule proves the public injection seam: New with
// WithBackend uses the supplied backend, needs no WithModule* option (does not
// return ErrNoModule), and preflightVocab tolerates a backend that returns no
// version reply. This is exactly how pkg/afmpeg/native will plug in.
func TestWithBackend_injectsAndSkipsModule(t *testing.T) {
	t.Parallel()

	fb := &fakeBackend{res: Result{ExitCode: 0, Stdout: ""}} // empty → non-gated, tolerated by preflight

	rt, err := New(context.Background(), WithBackend(fb))
	if err != nil {
		t.Fatalf("New(WithBackend): %v (must not require a module)", err)
	}

	defer func() { _ = rt.Close(context.Background()) }()

	// preflightVocab ran op:"version" against the backend during New.
	if len(fb.gotArgs) != 1 {
		t.Fatalf("preflight did not invoke the backend: args=%v", fb.gotArgs)
	}

	res, err := rt.Run(context.Background(), afero.NewMemMapFs(), "spec")
	if err != nil {
		t.Fatalf("Run over injected backend: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
}
