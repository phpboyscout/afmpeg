package native

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/spf13/afero"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// WithLogger sets where the backend reports a driver that ran unconfined (spec
// 0047). Without it the backend logs to slog.Default at the time of the run.
func WithLogger(l *slog.Logger) Option {
	return func(b *Backend) { b.logger = l }
}

// sandboxRetryInterval bounds how often a driver whose --capabilities keeps
// failing is asked again, so a broken driver does not cost a spawn per job.
const sandboxRetryInterval = 30 * time.Second

// sandboxReport caches the host's answer to --capabilities and makes sure each
// state that is not confined is logged once per Backend rather than once per job.
type sandboxReport struct {
	mu      sync.Mutex
	queried bool
	state   afmpeg.Sandbox
	err     error

	retryAfter time.Time
	now        func() time.Time // seam for tests; defaults to time.Now

	warnUnavailable sync.Once
	warnUnknown     sync.Once
	noteDisabled    sync.Once
}

// Sandbox reports the confinement the driver gets on this host, from its
// --capabilities. A definite answer is cached for the Backend's lifetime; a
// failed query is retried, at most every sandboxRetryInterval.
func (b *Backend) Sandbox(ctx context.Context) (afmpeg.Sandbox, error) {
	b.sandbox.mu.Lock()
	defer b.sandbox.mu.Unlock()

	if b.sandbox.queried {
		return b.sandbox.state, b.sandbox.err
	}

	if b.sandbox.clock().Before(b.sandbox.retryAfter) {
		return "", b.sandbox.err
	}

	state, settled, err := b.querySandbox(ctx)
	switch {
	case settled:
		b.sandbox.state, b.sandbox.err, b.sandbox.queried = state, err, true
	case ctx.Err() == nil:
		// A cancelled caller says nothing about the driver, so it earns no back-off.
		b.sandbox.err, b.sandbox.retryAfter = err, b.sandbox.clock().Add(sandboxRetryInterval)
	}

	return state, err
}

func (r *sandboxReport) clock() time.Time {
	if r.now != nil {
		return r.now()
	}

	return time.Now()
}

// querySandbox asks over the socket because that is the only mode in which the
// driver installs its sandbox; asked without one it would truthfully say
// "unavailable" on any host. settled is false for a failure that says nothing
// about the host (a cancelled caller, a failed spawn, a crash), so the next
// caller asks again rather than inheriting it.
func (b *Backend) querySandbox(ctx context.Context) (state afmpeg.Sandbox, settled bool, err error) {
	res, err := b.invoke(ctx, afero.NewMemMapFs(), "--capabilities")
	if err != nil {
		return "", false, errors.Wrap(err, "native: query sandbox state")
	}

	if res.ExitCode != 0 {
		return "", false, errors.Newf("native: --capabilities exited %d", res.ExitCode)
	}

	state = reportedSandbox(res.Stdout)
	if state == "" {
		return "", true, errors.New("native: the driver does not report its sandbox state; it predates spec 0043")
	}

	return state, true, nil
}

func reportedSandbox(stdout string) afmpeg.Sandbox {
	var out struct {
		Sandbox afmpeg.Sandbox `json:"sandbox"`
	}
	if json.Unmarshal([]byte(stdout), &out) != nil {
		return ""
	}

	return out.Sandbox
}

func (b *Backend) observe(state afmpeg.Sandbox) {
	switch state {
	case afmpeg.SandboxUnavailable:
		b.sandbox.warnUnavailable.Do(func() {
			b.log().Warn("afmpeg: the native driver is running without its filesystem sandbox, "+
				"because this host cannot install Landlock; a filter option that names a file "+
				"can read host disk (afmpeg spec 0047)",
				"sandbox", string(state), "driver", b.binary)
		})
	case afmpeg.SandboxDisabled:
		b.sandbox.noteDisabled.Do(func() {
			b.log().Info("afmpeg: the native driver's filesystem sandbox is disabled by "+
				"AFMPEG_NO_SANDBOX=1 (afmpeg spec 0047)",
				"sandbox", string(state), "driver", b.binary)
		})
	case afmpeg.SandboxLandlock, afmpeg.SandboxWASI:
	default:
		// Matt's choice, 2026-11-21 (spec 0047): an unreported state warns too.
		b.sandbox.warnUnknown.Do(func() {
			b.log().Warn("afmpeg: the native driver did not report its filesystem sandbox "+
				"state, so whether it is confined is unknown; treat it as unconfined "+
				"(afmpeg spec 0047)",
				"sandbox", string(state), "driver", b.binary)
		})
	}
}

func (b *Backend) log() *slog.Logger {
	if b.logger != nil {
		return b.logger
	}

	return slog.Default()
}
