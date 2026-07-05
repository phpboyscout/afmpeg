package afmpeg

import (
	"context"

	"github.com/spf13/afero"
)

// Backend runs a single engine invocation behind Runtime's stable API. The
// sandboxed wasm backend (wazero) is the default and the only one core afmpeg
// depends on; an opt-in native subprocess backend (spec 0028), constructed in a
// separate package (pkg/afmpeg/native) and injected with WithBackend, implements
// the same two methods — so RunJob/Probe/Frames and all result parsing never
// change with the backend.
//
// Invoke consumes the engine argv (the job-spec JSON is args[0]) and the afero.Fs
// to serve, and returns the engine's Result — the same {ExitCode, Stdout, Stderr}
// Run surfaces. A non-zero engine exit is a Result with a nil error; only
// host-side failures (spawn, the fs bridge, cancellation) return an error. Close
// releases the backend's resources and is called by Runtime.Close.
type Backend interface {
	Invoke(ctx context.Context, fs afero.Fs, args ...string) (Result, error)
	Close(ctx context.Context) error
}
