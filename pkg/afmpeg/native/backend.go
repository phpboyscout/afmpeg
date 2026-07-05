package native

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// socketEnv names the Unix-domain socket the driver connects to for its custom
// AVIO I/O. The host sets it per invocation; the native driver reads it.
const socketEnv = "AFMPEG_NATIVE_SOCKET"

// Backend runs each job by spawning the native driver and serving the caller's
// afero.Fs to it over a Unix socket (spec 0028 Backend B). It satisfies
// afmpeg.Backend, so a Runtime built with afmpeg.WithBackend(native.New(...))
// drives it behind the unchanged RunJob/Probe/Frames API.
type Backend struct {
	binary string
	spawn  spawnFunc // seam for tests; defaults to execDriver
}

var _ afmpeg.Backend = (*Backend)(nil)

// Option configures a native Backend.
type Option func(*Backend)

// WithNativeBinary sets the path to a native ffmpeg-wasi driver binary you already
// have (the bring-your-own path — you supply the bytes, unverified). To fetch and
// signature-verify the driver from a published release the same way
// afmpeg.WithModuleRelease does the .wasm, use [NewFromRelease] instead (spec 0028
// D-0028-D).
func WithNativeBinary(path string) Option {
	return func(b *Backend) { b.binary = path }
}

// New builds a native Backend from the given options.
func New(opts ...Option) *Backend {
	b := &Backend{}
	for _, o := range opts {
		o(b)
	}

	if b.spawn == nil {
		b.spawn = b.execDriver
	}

	return b
}

// Close releases backend-level resources. The driver is spawned per invocation
// (like the wasm backend instantiates per call), so there is nothing persistent
// to tear down.
func (b *Backend) Close(context.Context) error { return nil }

// Invoke runs one job: it opens a Unix socket, spawns the driver with the job-spec
// argv and the socket in its environment, serves fs to the driver over that socket
// until the process exits, and returns the driver's Result. All media I/O flows
// through the socket into fs — no host-disk file for inputs or outputs.
func (b *Backend) Invoke(ctx context.Context, fs afero.Fs, args ...string) (afmpeg.Result, error) {
	dir, err := os.MkdirTemp("", "afmpeg-native-")
	if err != nil {
		return afmpeg.Result{}, errors.Wrap(err, "native: temp dir for socket")
	}

	defer func() { _ = os.RemoveAll(dir) }()

	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "unix", filepath.Join(dir, "io.sock"))
	if err != nil {
		return afmpeg.Result{}, errors.Wrap(err, "native: listen")
	}

	sockPath := ln.Addr().String()

	// Accept connections and serve each against fs until the listener closes. The
	// WaitGroup lives entirely inside this goroutine (Add before spawning a server,
	// Wait after the accept loop ends) so there is no Add/Wait race with Invoke.
	served := make(chan struct{})

	go func() {
		var wg sync.WaitGroup

		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				break // listener closed
			}

			wg.Add(1)

			go func() {
				defer wg.Done()

				_ = serveConn(conn, fs)
			}()
		}

		wg.Wait()
		close(served)
	}()

	drv, err := b.spawn(ctx, sockPath, args)
	if err != nil {
		_ = ln.Close()

		<-served

		return afmpeg.Result{}, err
	}

	exitCode, stdout, stderr, werr := drv.wait()

	_ = ln.Close() // stop accepting; unblocks the accept loop

	<-served // let in-flight file sessions drain

	if werr != nil {
		return afmpeg.Result{}, werr
	}

	return afmpeg.Result{ExitCode: exitCode, Stdout: stdout, Stderr: stderr}, nil
}

// driver is a running driver process the host waits on.
type driver interface {
	// wait blocks until the driver exits and returns its exit code plus captured
	// stdout/stderr. A non-zero exit is reported in exitCode with a nil error; only
	// a host-side failure (a non-exit wait error) returns a non-nil error.
	wait() (exitCode int, stdout, stderr string, err error)
}

// spawnFunc starts the driver for one invocation. Injectable so tests can stand in
// a fake driver without a real binary.
type spawnFunc func(ctx context.Context, sockPath string, args []string) (driver, error)

// execDriver is the production spawn: exec the configured binary with the job-spec
// argv and the socket path in the environment, capturing stdout/stderr.
func (b *Backend) execDriver(ctx context.Context, sockPath string, args []string) (driver, error) {
	if b.binary == "" {
		return nil, errors.New("native: no driver binary configured (use WithNativeBinary)")
	}

	// Launching the configured driver with the job-spec argv is the whole point of
	// this backend (spec 0028); the binary is the consumer's chosen, signed artifact.
	cmd := exec.CommandContext(ctx, b.binary, args...) //nolint:gosec // G204: intended subprocess

	cmd.Env = append(os.Environ(), socketEnv+"="+sockPath)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, errors.Wrap(err, "native: start driver")
	}

	return &execProc{cmd: cmd, stdout: &stdout, stderr: &stderr}, nil
}

// execProc adapts an exec.Cmd to the driver interface.
type execProc struct {
	cmd            *exec.Cmd
	stdout, stderr *bytes.Buffer
}

func (p *execProc) wait() (int, string, string, error) {
	err := p.cmd.Wait()

	// A non-zero exit is a normal Result, not a host error; only a non-exit failure
	// (couldn't reap, killed by our own context, …) is surfaced as an error.
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return 0, "", "", errors.Wrap(err, "native: wait for driver")
	}

	return p.cmd.ProcessState.ExitCode(), p.stdout.String(), p.stderr.String(), nil
}
