package afmpeg_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// guestModule is the compiled WASI test guest, built once in TestMain and reused
// as a stand-in for ffmpeg.wasm across the runtime tests.
var guestModule []byte

func TestMain(m *testing.M) {
	module, err := buildGuest()
	if err != nil {
		fmt.Fprintln(os.Stderr, "afmpeg test setup:", err)
		os.Exit(1)
	}

	guestModule = module

	os.Exit(m.Run())
}

// buildGuest compiles testdata/guest to wasm32-wasi and returns the module bytes.
func buildGuest() ([]byte, error) {
	dir, err := os.MkdirTemp("", "afmpeg-guest-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	out := filepath.Join(dir, "guest.wasm")

	cmd := exec.Command("go", "build", "-o", out, ".") //nolint:gosec // fixed in-repo test guest, static command line
	cmd.Dir = "testdata/guest"
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")

	if combined, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("build guest: %w: %s", err, combined)
	}

	return os.ReadFile(out) //nolint:gosec // reading the wasm just built into a temp dir
}

func newTestRuntime(t *testing.T) *afmpeg.Runtime {
	t.Helper()

	rt, err := afmpeg.New(context.Background(), afmpeg.WithModuleBytes(guestModule))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = rt.Close(context.Background()) })

	return rt
}

func TestNew_RequiresModule(t *testing.T) {
	t.Parallel()

	_, err := afmpeg.New(context.Background())
	if !errors.Is(err, afmpeg.ErrNoModule) {
		t.Fatalf("New without module: err = %v, want ErrNoModule", err)
	}
}

func TestNew_InvalidModule(t *testing.T) {
	t.Parallel()

	_, err := afmpeg.New(context.Background(), afmpeg.WithModuleBytes([]byte("not a wasm module")))
	if err == nil {
		t.Fatal("New with invalid module: want a compile error")
	}
}

func TestRun_ExitCode(t *testing.T) {
	t.Parallel()

	res, err := newTestRuntime(t).Run(context.Background(), afero.NewMemMapFs(), "exit:7")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", res.ExitCode)
	}
}

func TestRun_StderrCaptured(t *testing.T) {
	t.Parallel()

	res, err := newTestRuntime(t).Run(context.Background(), afero.NewMemMapFs(), "stderr:kaboom")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.ExitCode != 1 || !strings.Contains(res.Stderr, "kaboom") {
		t.Fatalf("Result = %+v, want exit 1 with stderr containing kaboom", res)
	}
}

// TestRun_InMemoryFileIO proves a guest's file I/O resolves against the supplied
// in-memory afero.Fs through a real WASI host, with no host disk access.
func TestRun_InMemoryFileIO(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.txt", []byte("hello afmpeg"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := newTestRuntime(t).Run(context.Background(), fs, "copy", "in.txt", "out.txt")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("Run copy: res=%+v err=%v", res, err)
	}

	got, err := afero.ReadFile(fs, "out.txt")
	if err != nil || string(got) != "hello afmpeg" {
		t.Fatalf("out.txt = %q err=%v, want %q", got, err, "hello afmpeg")
	}
}

// TestRun_SeekOnWriteThroughWASIHost is the spec-0003 end-to-end deferred from
// that thread: a guest performs the mp4 +faststart seek-on-write over the mounted
// vfs, through a real WASI host, and the patched bytes land in the in-memory fs.
func TestRun_SeekOnWriteThroughWASIHost(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	res, err := newTestRuntime(t).Run(context.Background(), fs, "moov", "out.mp4")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("Run moov: res=%+v err=%v", res, err)
	}

	got, err := afero.ReadFile(fs, "out.mp4")
	if err != nil || string(got) != "SIZEmdatPAYLOAD" {
		t.Fatalf("out.mp4 = %q err=%v, want %q", got, err, "SIZEmdatPAYLOAD")
	}
}

func TestRun_ContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := newTestRuntime(t).Run(ctx, afero.NewMemMapFs(), "sleep")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run sleep cancelled: err = %v, want context.Canceled", err)
	}
}

// TestRun_Serialized exercises the one-invocation-at-a-time guard under the race
// detector (spec 0004 D-0004-B).
func TestRun_Serialized(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(t)

	var wg sync.WaitGroup

	const workers = 8

	for range workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			if _, err := rt.Run(context.Background(), afero.NewMemMapFs(), "exit:0"); err != nil {
				t.Errorf("concurrent Run: %v", err)
			}
		}()
	}

	wg.Wait()
}

func TestProbe(t *testing.T) {
	t.Parallel()

	p, err := newTestRuntime(t).Probe(context.Background(), afero.NewMemMapFs(), "clip.mp4")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	if p.DurationSec != 12.34 {
		t.Fatalf("DurationSec = %v, want 12.34", p.DurationSec)
	}
}

func TestProbe_Failure(t *testing.T) {
	t.Parallel()

	_, err := newTestRuntime(t).Probe(context.Background(), afero.NewMemMapFs(), "fail-probe")
	if err == nil {
		t.Fatal("Probe of failing input: want an error")
	}
}

func TestProbe_UnparseableDuration(t *testing.T) {
	t.Parallel()

	_, err := newTestRuntime(t).Probe(context.Background(), afero.NewMemMapFs(), "bad-duration")
	if err == nil {
		t.Fatal("Probe of unparseable duration: want an error")
	}
}

func TestWithModuleFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "m.wasm")
	if err := os.WriteFile(path, guestModule, 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}

	rt, err := afmpeg.New(context.Background(), afmpeg.WithModuleFile(path))
	if err != nil {
		t.Fatalf("New WithModuleFile: %v", err)
	}

	_ = rt.Close(context.Background())
}

func TestWithModuleFile_Missing(t *testing.T) {
	t.Parallel()

	_, err := afmpeg.New(context.Background(), afmpeg.WithModuleFile(filepath.Join(t.TempDir(), "nope.wasm")))
	if err == nil {
		t.Fatal("WithModuleFile missing path: want an error")
	}
}

func TestWithModuleFS(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "m.wasm", guestModule, 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}

	rt, err := afmpeg.New(context.Background(), afmpeg.WithModuleFS(fs, "m.wasm"))
	if err != nil {
		t.Fatalf("New WithModuleFS: %v", err)
	}

	_ = rt.Close(context.Background())
}

func TestWithModuleFS_Missing(t *testing.T) {
	t.Parallel()

	_, err := afmpeg.New(context.Background(), afmpeg.WithModuleFS(afero.NewMemMapFs(), "missing.wasm"))
	if err == nil {
		t.Fatal("WithModuleFS missing path: want an error")
	}
}
