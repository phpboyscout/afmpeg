package native

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// stubDriver answers --capabilities with caps and any job with job, counting how
// often it was asked for its capabilities.
type stubDriver struct {
	caps, job string
	capsAsked atomic.Int32

	// capsFails makes the first capsFails --capabilities spawns fail with
	// capsErr, or exit capsExit when capsErr is nil.
	capsFails atomic.Int32
	capsErr   error
	capsExit  int
}

func (s *stubDriver) spawn(ctx context.Context, _ string, args []string) (driver, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out, exit := s.job, 0
	if len(args) > 0 && args[0] == "--capabilities" {
		s.capsAsked.Add(1)
		out = s.caps
		if s.capsFails.Add(-1) >= 0 {
			if s.capsErr != nil {
				return nil, s.capsErr
			}
			exit = s.capsExit
		}
	}

	fd := &fakeDriver{done: make(chan struct{}), stdout: out, exit: exit}
	close(fd.done)

	return fd, nil
}

// syncBuffer lets parallel Invokes share one log sink under the race detector.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.buf.String()
}

func stubBackend(d *stubDriver) (*Backend, *syncBuffer) {
	logs := &syncBuffer{}
	b := New(WithLogger(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	b.spawn = d.spawn

	return b, logs
}

func TestBackend_Invoke_reportsTheJobsSandbox(t *testing.T) {
	t.Parallel()

	cases := []struct {
		state    afmpeg.Sandbox
		wantWarn bool
		wantInfo bool
	}{
		{afmpeg.SandboxLandlock, false, false},
		{afmpeg.SandboxUnavailable, true, false},
		{afmpeg.SandboxDisabled, false, true},
	}
	for _, c := range cases {
		t.Run(string(c.state), func(t *testing.T) {
			t.Parallel()

			b, logs := stubBackend(&stubDriver{job: `{"sandbox":"` + string(c.state) + `","outputs":[]}`})

			for range 3 {
				res, err := b.Invoke(context.Background(), afero.NewMemMapFs(), `{"op":"process"}`)
				if err != nil {
					t.Fatalf("Invoke: %v", err)
				}
				if res.Sandbox != c.state {
					t.Fatalf("Sandbox = %q, want %q", res.Sandbox, c.state)
				}
			}

			if got := strings.Count(logs.String(), "level=WARN"); got != boolCount(c.wantWarn) {
				t.Errorf("warnings over three jobs = %d, want %d\n%s", got, boolCount(c.wantWarn), logs)
			}
			if got := strings.Count(logs.String(), "level=INFO"); got != boolCount(c.wantInfo) {
				t.Errorf("info lines over three jobs = %d, want %d\n%s", got, boolCount(c.wantInfo), logs)
			}
		})
	}
}

// A probe or frames result carries no sandbox field, so the state comes from
// --capabilities, asked once however many jobs need it.
func TestBackend_Invoke_fallsBackToCapabilitiesOnce(t *testing.T) {
	t.Parallel()

	d := &stubDriver{caps: `{"sandbox":"unavailable"}`, job: `{"format":"mp4"}`}
	b, logs := stubBackend(d)

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			res, err := b.Invoke(context.Background(), afero.NewMemMapFs(), `{"op":"probe"}`)
			if err != nil {
				t.Errorf("Invoke: %v", err)
				return
			}
			if res.Sandbox != afmpeg.SandboxUnavailable {
				t.Errorf("Sandbox = %q, want unavailable", res.Sandbox)
			}
		})
	}
	wg.Wait()

	if got := d.capsAsked.Load(); got != 1 {
		t.Errorf("--capabilities asked %d times, want 1", got)
	}
	if got := strings.Count(logs.String(), "level=WARN"); got != 1 {
		t.Errorf("warnings = %d, want 1\n%s", got, logs)
	}
}

func TestBackend_Sandbox_aDriverThatDoesNotReportIt(t *testing.T) {
	t.Parallel()

	b, logs := stubBackend(&stubDriver{caps: `{"vocab_version":10}`, job: `{"format":"mp4"}`})

	if _, err := b.Sandbox(context.Background()); err == nil {
		t.Fatal("Sandbox: want an error for a driver that predates spec 0043")
	}

	for range 3 {
		res, err := b.Invoke(context.Background(), afero.NewMemMapFs(), `{"op":"probe"}`)
		if err != nil {
			t.Fatalf("Invoke: %v (an unreported state must not fail the job)", err)
		}
		if res.Sandbox != "" || res.Sandbox.Confined() {
			t.Fatalf("Sandbox = %q, want empty and not confined", res.Sandbox)
		}
	}

	// Matt's choice, 2026-11-21 (spec 0047): unknown warns, once per backend.
	if got := strings.Count(logs.String(), "level=WARN"); got != 1 || !strings.Contains(logs.String(), "unknown") {
		t.Errorf("want exactly one warning that the state is unknown, got %d:\n%s", got, logs)
	}
}

func TestBackend_Invoke_warnsOnAnUnrecognisedState(t *testing.T) {
	t.Parallel()

	b, logs := stubBackend(&stubDriver{job: `{"sandbox":"seccomp-only","outputs":[]}`})

	for range 2 {
		res, err := b.Invoke(context.Background(), afero.NewMemMapFs(), `{"op":"process"}`)
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if res.Sandbox.Confined() {
			t.Fatalf("Sandbox %q reads as confined", res.Sandbox)
		}
	}

	if got := strings.Count(logs.String(), "level=WARN"); got != 1 {
		t.Errorf("warnings = %d, want 1\n%s", got, logs)
	}
}

// A driver whose --capabilities always fails is asked again only after the
// back-off, not on every job.
func TestBackend_Sandbox_backsOffAFailingQuery(t *testing.T) {
	t.Parallel()

	d := &stubDriver{caps: `{"sandbox":"landlock"}`, job: `{"format":"mp4"}`, capsExit: 1}
	d.capsFails.Store(1 << 20)
	b, _ := stubBackend(d)
	clock := newFakeClock(b)

	for range 5 {
		res, err := b.Invoke(context.Background(), afero.NewMemMapFs(), `{"op":"probe"}`)
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if res.Sandbox != "" || res.Sandbox.Confined() {
			t.Fatalf("Sandbox during the back-off = %q, want empty and not confined", res.Sandbox)
		}
		if state, err := b.Sandbox(context.Background()); err == nil || state != "" {
			t.Fatalf("Sandbox() during the back-off = %q, %v; want an error and no state", state, err)
		}
	}
	if got := d.capsAsked.Load(); got != 1 {
		t.Fatalf("--capabilities asked %d times within the back-off, want 1", got)
	}

	clock.advance(sandboxRetryInterval)

	if _, err := b.Invoke(context.Background(), afero.NewMemMapFs(), `{"op":"probe"}`); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := d.capsAsked.Load(); got != 2 {
		t.Errorf("--capabilities asked %d times after the back-off, want 2", got)
	}
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(b *Backend) *fakeClock {
	c := &fakeClock{t: time.Unix(0, 0)}
	b.sandbox.now = c.now

	return c
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.t = c.t.Add(d)
}

func TestBackend_Sandbox_aCancelledQueryIsNotCached(t *testing.T) {
	t.Parallel()

	d := &stubDriver{caps: `{"sandbox":"landlock"}`}
	b, _ := stubBackend(d)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = b.Sandbox(cancelled)

	state, err := b.Sandbox(context.Background())
	if err != nil || state != afmpeg.SandboxLandlock {
		t.Fatalf("Sandbox after a cancelled query = %q, %v; want landlock", state, err)
	}
}

// A capabilities query that fails without an answer must neither report the
// driver confined nor stick: the next job asks again.
func TestBackend_Sandbox_aFailedQueryFailsOpenNowhere(t *testing.T) {
	t.Parallel()

	cases := map[string]*stubDriver{
		"spawn error":   {capsErr: errors.New("EMFILE")},
		"non-zero exit": {capsExit: 1},
	}
	for name, d := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			d.caps, d.job = `{"sandbox":"landlock"}`, `{"format":"mp4"}`
			d.capsFails.Store(1)
			b, _ := stubBackend(d)
			clock := newFakeClock(b)

			if state, err := b.Sandbox(context.Background()); err == nil || state != "" {
				t.Fatalf("Sandbox = %q, %v; want an error and no state", state, err)
			}

			clock.advance(sandboxRetryInterval)

			res, err := b.Invoke(context.Background(), afero.NewMemMapFs(), `{"op":"probe"}`)
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if res.Sandbox != afmpeg.SandboxLandlock {
				t.Fatalf("Sandbox after the failure passed = %q, want landlock from a fresh query", res.Sandbox)
			}
			if got := d.capsAsked.Load(); got != 2 {
				t.Errorf("--capabilities asked %d times, want 2", got)
			}
		})
	}
}

func boolCount(b bool) int {
	if b {
		return 1
	}

	return 0
}
