package afmpeg

import (
	"context"

	"github.com/spf13/afero"
)

// backend runs a single engine invocation behind the Runtime's public API. The
// sandboxed wasm backend (wazero) is the default and the only one core afmpeg
// imports; an opt-in native subprocess backend (spec 0028) implements the same
// two methods in a separate package, so RunJob/Probe/Frames never change.
//
// invoke consumes the same job-spec argv and the same afero.Fs the wasm path
// does and returns the engine's outcome; close releases the backend's resources.
type backend interface {
	invoke(ctx context.Context, fs afero.Fs, args ...string) (invocation, error)
	close(ctx context.Context) error
}

// invocation is the backend-neutral outcome of running the engine once: the exit
// code and the captured stdout/stderr. Both backends produce it, so Result and all
// stdout-JSON parsing (Probe/Frames) are identical regardless of backend.
type invocation struct {
	exitCode int
	stdout   string
	stderr   string
}
