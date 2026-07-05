package native

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// fakeDriver stands in for a spawned driver process: a goroutine runs script
// against the socket, and wait blocks until it finishes, returning canned output.
type fakeDriver struct {
	done   chan struct{}
	exit   int
	stdout string
}

func (f *fakeDriver) wait() (int, string, string, error) {
	<-f.done

	return f.exit, f.stdout, "", nil
}

// fakeSpawn returns a spawnFunc that runs script (a driver session) against the
// socket in a goroutine — exercising the real listener, accept loop, and serveConn
// without a native binary.
func fakeSpawn(script func(sockPath string) (exit int, stdout string)) spawnFunc {
	return func(_ context.Context, sockPath string, _ []string) (driver, error) {
		fd := &fakeDriver{done: make(chan struct{})}

		go func() {
			defer close(fd.done)

			fd.exit, fd.stdout = script(sockPath)
		}()

		return fd, nil
	}
}

func TestBackend_Invoke_servesFsOverSocket(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.mp4", []byte("SOURCE"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The fake driver reads in.mp4 and writes a transformed out.mp4 — one
	// connection per file, exactly as the custom AVIO opens them.
	script := func(sock string) (int, string) {
		in, err := net.Dial("unix", sock)
		if err != nil {
			return 3, "dial in"
		}
		defer func() { _ = in.Close() }()

		ri := &wireClient{c: in}
		ri.version()
		ri.open("in.mp4", modeRead)
		data := ri.read(1024)
		ri.close()

		out, err := net.Dial("unix", sock)
		if err != nil {
			return 3, "dial out"
		}
		defer func() { _ = out.Close() }()

		wo := &wireClient{c: out}
		wo.version()
		wo.open("out.mp4", modeWrite)
		wo.write(append([]byte("XFORM:"), data...))
		wo.close()

		if ri.err != nil || wo.err != nil {
			return 4, "wire error"
		}

		return 0, "done"
	}

	b := New()
	b.spawn = fakeSpawn(script)

	res, err := b.Invoke(context.Background(), fs, `{"op":"process"}`)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if res.ExitCode != 0 || res.Stdout != "done" {
		t.Fatalf("result = %+v, want {0 done }", res)
	}

	got, err := afero.ReadFile(fs, "out.mp4")
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "XFORM:SOURCE" {
		t.Fatalf("out.mp4 = %q, want %q", got, "XFORM:SOURCE")
	}
}

func TestBackend_Invoke_returnsDriverExit(t *testing.T) {
	t.Parallel()

	// A driver that touches no files (like op:version) and exits non-zero.
	b := New()
	b.spawn = fakeSpawn(func(string) (int, string) { return 3, "boom" })

	res, err := b.Invoke(context.Background(), afero.NewMemMapFs(), `{"op":"version"}`)
	if err != nil {
		t.Fatalf("Invoke: %v (a non-zero exit is not a host error)", err)
	}

	if res.ExitCode != 3 || res.Stdout != "boom" {
		t.Fatalf("result = %+v, want exit 3", res)
	}
}

func TestBackend_Invoke_spawnErrorSurfaces(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("spawn failed")

	b := New()
	b.spawn = func(context.Context, string, []string) (driver, error) { return nil, sentinel }

	if _, err := b.Invoke(context.Background(), afero.NewMemMapFs(), "{}"); !errors.Is(err, sentinel) {
		t.Fatalf("want the spawn error surfaced, got %v", err)
	}
}

func TestBackend_noBinaryConfigured(t *testing.T) {
	t.Parallel()

	// New() with no WithNativeBinary → execDriver has nothing to run.
	if _, err := New().Invoke(context.Background(), afero.NewMemMapFs(), "{}"); err == nil {
		t.Fatal("want an error when no driver binary is configured")
	}
}

// TestBackend_execDriver_realProcess exercises the production spawn (execDriver +
// execProc.wait) with real subprocesses that ignore the socket — proving the
// process lifecycle and exit-code capture, which op:version-style jobs (no media
// I/O) also follow.
func TestBackend_execDriver_realProcess(t *testing.T) {
	t.Parallel()

	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no 'true' binary")
	}

	res, err := New(WithNativeBinary(truePath)).Invoke(context.Background(), afero.NewMemMapFs(), "{}")
	if err != nil {
		t.Fatalf("Invoke(true): %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("true exit = %d, want 0", res.ExitCode)
	}

	if falsePath, ferr := exec.LookPath("false"); ferr == nil {
		res, err := New(WithNativeBinary(falsePath)).Invoke(context.Background(), afero.NewMemMapFs(), "{}")
		if err != nil {
			t.Fatalf("Invoke(false): %v (non-zero exit is not a host error)", err)
		}

		if res.ExitCode == 0 {
			t.Fatal("false exit = 0, want non-zero")
		}
	}
}

// TestAfmpegNew_withNativeBackend proves the whole wiring: afmpeg.New with
// WithBackend(native.New(...)) constructs a Runtime (preflightVocab runs
// op:version against the driver; an empty reply is tolerated as a non-gated
// engine), needing no wasm module.
func TestAfmpegNew_withNativeBackend(t *testing.T) {
	t.Parallel()

	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no 'true' binary")
	}

	rt, err := afmpeg.New(context.Background(), afmpeg.WithBackend(New(WithNativeBinary(truePath))))
	if err != nil {
		t.Fatalf("afmpeg.New(WithBackend(native)): %v", err)
	}

	if err := rt.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// errDriver's wait fails host-side (not a normal non-zero exit), covering Invoke's
// error path after the listener is torn down.
type errDriver struct{}

func (errDriver) wait() (int, string, string, error) {
	return 0, "", "", errors.New("wait failed")
}

func TestBackend_Invoke_waitErrorSurfaces(t *testing.T) {
	t.Parallel()

	b := New()
	b.spawn = func(context.Context, string, []string) (driver, error) { return errDriver{}, nil }

	if _, err := b.Invoke(context.Background(), afero.NewMemMapFs(), "{}"); err == nil {
		t.Fatal("want the wait error surfaced")
	}
}

func TestWithNativeBinary(t *testing.T) {
	t.Parallel()

	b := New(WithNativeBinary("/opt/afmpeg/driver"))
	if b.binary != "/opt/afmpeg/driver" {
		t.Fatalf("binary = %q, want /opt/afmpeg/driver", b.binary)
	}
}
