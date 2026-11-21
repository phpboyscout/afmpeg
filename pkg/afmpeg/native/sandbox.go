package native

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/spf13/afero"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// WithLogger sets where the backend reports a driver that ran unconfined (spec
// 0047). Without it the backend logs to slog.Default at the time of the run.
func WithLogger(l *slog.Logger) Option {
	return func(b *Backend) { b.logger = l }
}

// sandboxReport caches the host's answer to --capabilities and makes sure each
// unconfined state is logged once per Backend rather than once per job.
type sandboxReport struct {
	mu      sync.Mutex
	queried bool
	state   afmpeg.Sandbox
	err     error

	warnUnavailable sync.Once
	noteDisabled    sync.Once
}

// Sandbox reports the confinement the driver gets on this host, from its
// --capabilities. The driver is asked once and the answer is cached.
func (b *Backend) Sandbox(ctx context.Context) (afmpeg.Sandbox, error) {
	b.sandbox.mu.Lock()
	defer b.sandbox.mu.Unlock()

	if b.sandbox.queried {
		return b.sandbox.state, b.sandbox.err
	}

	state, settled, err := b.querySandbox(ctx)
	if settled {
		b.sandbox.state, b.sandbox.err, b.sandbox.queried = state, err, true
	}

	return state, err
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
	}
}

func (b *Backend) log() *slog.Logger {
	if b.logger != nil {
		return b.logger
	}

	return slog.Default()
}
