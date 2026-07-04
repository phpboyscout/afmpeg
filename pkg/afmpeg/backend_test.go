package afmpeg

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/afero"
)

// fakeBackend is a test double proving Runtime drives any backend through the
// interface seam — no wasm module required. It records what it was invoked with
// and returns a canned invocation, so the native backend (spec 0028) can slot in
// behind the identical contract.
type fakeBackend struct {
	inv     invocation
	err     error
	gotArgs []string
	closed  bool
}

func (f *fakeBackend) invoke(_ context.Context, _ afero.Fs, args ...string) (invocation, error) {
	f.gotArgs = args

	return f.inv, f.err
}

func (f *fakeBackend) close(context.Context) error {
	f.closed = true

	return nil
}

func TestRuntime_delegatesRunToBackend(t *testing.T) {
	t.Parallel()

	fb := &fakeBackend{inv: invocation{exitCode: 2, stdout: "OUT", stderr: "ERR"}}
	rt := &Runtime{backend: fb, sem: make(chan struct{}, 1)}

	res, err := rt.Run(context.Background(), afero.NewMemMapFs(), "the-job-spec")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The invocation's fields surface verbatim as the Result (byte-identical across backends).
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
