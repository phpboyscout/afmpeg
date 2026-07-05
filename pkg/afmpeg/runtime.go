package afmpeg

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
)

const (
	// guestName is argv[0] presented to the guest module.
	guestName = "ffmpeg"

	// stderrTailBytes bounds how much module stderr is surfaced on a failure.
	stderrTailBytes = 1500

	// wasmPageSize is the WebAssembly linear-memory page size (64 KiB); a memory
	// limit in bytes is converted to a whole number of pages, rounded up.
	wasmPageSize = 64 << 10

	// maxMemoryLimitPages is the wasm32 ceiling (65536 pages == 4 GiB). wazero's
	// WithMemoryLimitPages panics above this, so a byte limit is clamped to it.
	maxMemoryLimitPages = 1 << 16

	// defaultMemoryLimitBytes caps guest linear memory out of the box so a crafted
	// input declaring outsized dimensions fails cleanly (guest ENOMEM) instead of
	// OOM-killing the host (spec 0027 §4A, D-0027-B). WithMemoryLimit overrides;
	// WithMemoryLimit(0) removes the cap.
	defaultMemoryLimitBytes = 512 << 20

	// defaultTimeout bounds every invocation out of the box so a non-terminating
	// decode cannot hold the Runtime mutex indefinitely (spec 0027 §4B, D-0027-C).
	// WithTimeout overrides; WithTimeout(0) removes the deadline. Only imposed when
	// the caller's context carries no deadline of its own.
	defaultTimeout = 1 * time.Hour
)

// ErrNoModule is returned by New when no wasm module source is configured. The
// module is never embedded, so a WithModule* option is mandatory (spec 0004 D-C).
var ErrNoModule = errors.New("afmpeg: no wasm module configured (use WithModuleRelease, WithModuleURL, WithModuleFile, WithModuleBytes, or WithModuleFS)")

// Runtime drives an engine backend behind a stable API. Build it once with New —
// compiling the wasm module (the default backend) is the expensive step — and
// reuse it. Run serialises invocations: one at a time per Runtime (spec 0004
// D-0004-B).
type Runtime struct {
	// backend runs each invocation. The wasm backend (wazero) is the default; an
	// opt-in native backend (spec 0028), injected via WithBackend, is swapped in
	// here without touching Run.
	backend Backend

	// sem is a size-1 semaphore serialising invocations. Unlike a Mutex it is
	// acquired with a select against the caller's context, so a queued Run/Probe
	// with a short deadline (or a cancelled one) returns promptly instead of
	// blocking for the full in-flight job.
	sem chan struct{}

	mu     sync.Mutex // guards closed
	closed bool

	// timeout is the default per-invocation deadline imposed by Run when the
	// caller's context carries none (spec 0027 §4B). Zero disables it.
	timeout time.Duration
}

// Result is the outcome of a Run: the exit code and the module's captured stdout
// and stderr. A non-zero ExitCode is reported here with a nil error; only
// host-side failures (module instantiation, the vfs bridge, context
// cancellation) return an error. The ffmpeg-wasi engine returns its structured
// results (process status, probe info) as JSON on Stdout.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Probe describes a media input: its container format, duration, start offset,
// and streams.
type Probe struct {
	Format      string
	DurationSec float64
	StartSec    float64 // the container's start time (nonzero e.g. after a copy_ts trim)
	Streams     []ProbeStream

	// Tags is the container-level metadata (spec 0020) — title/artist/…
	Tags map[string]string
	// Chapters is the container's chapter list (spec 0020).
	Chapters []Chapter
}

// Chapter is one chapter of a probed input (spec 0020): its bounds in seconds and
// its title tag.
type Chapter struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Title string  `json:"title"`
}

// ProbeStream is one stream within a probed input.
type ProbeStream struct {
	Index      int    `json:"index"`
	Type       string `json:"type"` // "video" or "audio"
	Codec      string `json:"codec"`
	Width      int    `json:"width"`       // video
	Height     int    `json:"height"`      // video
	SampleRate int    `json:"sample_rate"` // audio
	Channels   int    `json:"channels"`    // audio

	// Metadata (spec 0020): the stream's language, its set disposition flags
	// (default/forced/attached_pic/…), and its tags.
	Language    string            `json:"language"`
	Disposition []string          `json:"disposition"`
	Tags        map[string]string `json:"tags"`
}

// config accumulates New's options. memoryLimitBytes and timeout start at their
// safe defaults so an out-of-the-box Runtime is hardened even with no option set
// (spec 0027 D-0027-A); an option may lower, raise, or (with 0) disable each.
type config struct {
	module           []byte
	fetch            func(context.Context) ([]byte, error)
	backend          Backend // non-nil → a WithBackend override replaces the wasm default
	memoryLimitBytes int
	timeout          time.Duration
}

// Option configures a Runtime at construction.
type Option func(*config) error

// WithModuleBytes supplies the ffmpeg.wasm module as raw bytes.
func WithModuleBytes(module []byte) Option {
	return func(c *config) error {
		c.module = module

		return nil
	}
}

// WithModuleFile loads the ffmpeg.wasm module from a host filesystem path.
func WithModuleFile(path string) Option {
	return func(c *config) error {
		module, err := os.ReadFile(path) //nolint:gosec // caller-supplied module path is intended (spec 0004 D-C)
		if err != nil {
			return errors.Wrap(err, "afmpeg: read module file")
		}

		c.module = module

		return nil
	}
}

// WithModuleFS loads the ffmpeg.wasm module from an afero filesystem.
func WithModuleFS(fs afero.Fs, path string) Option {
	return func(c *config) error {
		module, err := afero.ReadFile(fs, path)
		if err != nil {
			return errors.Wrap(err, "afmpeg: read module from fs")
		}

		c.module = module

		return nil
	}
}

// WithBackend replaces the default sandboxed wasm backend with a custom one — the
// opt-in native subprocess backend (spec 0028), constructed in a separate package
// (pkg/afmpeg/native) and passed here. When set, the WithModule* options are
// ignored: no wasm module is compiled, and the Runtime drives the provided backend
// behind the unchanged RunJob/Probe/Frames API. WASM stays the default — a Runtime
// built without this option is fully sandboxed (spec 0028 D-0028-C). New still
// preflights the vocabulary against the backend, so an outdated native engine is
// rejected just as an outdated module is.
func WithBackend(b Backend) Option {
	return func(c *config) error {
		c.backend = b

		return nil
	}
}

// WithMemoryLimit caps the guest's linear memory at bytes (rounded up to whole
// 64 KiB wasm pages, clamped to the wasm32 maximum of 4 GiB). Past the cap the
// guest's memory.grow fails, so an over-allocating decode returns ENOMEM and
// exits non-zero instead of OOM-killing the host (spec 0027 §4A). A default of
// 512 MB applies when this option is absent; WithMemoryLimit(0) removes the cap
// for a consumer that knowingly wants the unbounded behaviour.
func WithMemoryLimit(bytes int) Option {
	return func(c *config) error {
		c.memoryLimitBytes = bytes

		return nil
	}
}

// WithTimeout sets the default maximum duration of a single invocation. Run
// imposes it only when the caller's context carries no deadline of its own; a
// caller deadline is always honoured as-is (spec 0027 §4B). A default of 1 hour
// applies when this option is absent; WithTimeout(0) removes the default deadline.
func WithTimeout(d time.Duration) Option {
	return func(c *config) error {
		c.timeout = d

		return nil
	}
}

// resolveConfig applies the options over the hardened defaults and resolves the
// module source (a deferred fetch is run with New's context), returning a config
// whose module bytes are ready to compile or ErrNoModule if none was supplied.
func resolveConfig(ctx context.Context, opts []Option) (*config, error) {
	cfg := &config{
		memoryLimitBytes: defaultMemoryLimitBytes,
		timeout:          defaultTimeout,
	}

	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}

	// A custom backend (WithBackend) supplies the engine itself, so no wasm module
	// is needed or resolved — the module options are inert in that mode.
	if cfg.backend != nil {
		return cfg, nil
	}

	// A deferred source (e.g. WithModuleURL) is resolved here, with New's context,
	// unless module bytes were supplied directly.
	if len(cfg.module) == 0 && cfg.fetch != nil {
		module, err := cfg.fetch(ctx)
		if err != nil {
			return nil, err
		}

		cfg.module = module
	}

	if len(cfg.module) == 0 {
		return nil, ErrNoModule
	}

	return cfg, nil
}

// New returns a reusable Runtime. By default it compiles the configured wasm
// module once (exactly one WithModule* option is required); with WithBackend it
// drives the supplied backend instead and compiles nothing.
func New(ctx context.Context, opts ...Option) (*Runtime, error) {
	cfg, err := resolveConfig(ctx, opts)
	if err != nil {
		return nil, err
	}

	be := cfg.backend
	if be == nil {
		if be, err = newWASMBackend(ctx, cfg); err != nil {
			return nil, err
		}
	}

	runtime := &Runtime{backend: be, sem: make(chan struct{}, 1), timeout: cfg.timeout}

	// Fail loudly now if the module is a gated ffmpeg-wasi engine too old for the
	// vocabulary this afmpeg emits, rather than silently dropping new fields at
	// the first job (roadmap Phase 1 version-gating). A generic/pre-gate module
	// is tolerated — Runtime stays able to run any wasm module.
	if err := runtime.preflightVocab(ctx); err != nil {
		_ = runtime.Close(ctx)

		return nil, err
	}

	return runtime, nil
}

// memoryLimitPages converts a byte ceiling to whole wasm pages, rounded up and
// clamped to the wasm32 maximum. A non-positive limit means "no cap" and yields
// 0, signalling New to leave the runtime's memory unbounded.
func memoryLimitPages(bytes int) uint32 {
	if bytes <= 0 {
		return 0
	}

	pages := (bytes + wasmPageSize - 1) / wasmPageSize
	if pages > maxMemoryLimitPages {
		return maxMemoryLimitPages
	}

	return uint32(pages) //nolint:gosec // clamped to maxMemoryLimitPages above
}

// Close releases the runtime's resources. It waits for any in-flight invocation
// to finish first (respecting ctx) so a running job is never yanked out from
// under itself, then marks the runtime closed — a subsequent Run fails cleanly.
func (r *Runtime) Close(ctx context.Context) error {
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "afmpeg: close: cancelled waiting for an in-flight run")
	}

	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()

	return r.backend.Close(ctx)
}

// withDeadline imposes the default invocation deadline on ctx when the caller
// brought none (spec 0027 §4B), returning the (possibly wrapped) context and a
// cancel func the caller must defer. A caller's own deadline is left untouched.
func (r *Runtime) withDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); !ok && r.timeout > 0 {
		return context.WithTimeout(ctx, r.timeout)
	}

	return ctx, func() {}
}

// Run executes one ffmpeg invocation with its filesystem bridged to fs (paths in
// args resolve against fs, e.g. "in/clip.mp4", "out/reel.mp4"). It returns the
// exit code and captured stderr; a non-zero exit is reported in Result with a
// nil error. Only host-side failures return a non-nil error.
func (r *Runtime) Run(ctx context.Context, fs afero.Fs, args ...string) (Result, error) {
	// Acquire the invocation slot, but honour the caller's context while queued so
	// a short-deadline (or cancelled) call doesn't block for the whole in-flight
	// job — it returns promptly instead.
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return Result{}, errors.Wrap(ctx.Err(), "afmpeg: run: cancelled while queued")
	}

	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()

	if closed {
		return Result{}, errors.New("afmpeg: run on a closed runtime")
	}

	// Impose the default deadline now that we hold the slot, so the budget covers
	// execution rather than time spent queueing (spec 0027 §4B); a caller's own
	// deadline is honoured as-is.
	ctx, cancel := r.withDeadline(ctx)
	defer cancel()

	return r.backend.Invoke(ctx, fs, args...)
}

// Probe reports a media file's container/stream info over the fs bridge, via the
// ffmpeg-wasi engine's probe op (`{"op":"probe"}`), reading the structured JSON it
// returns on stdout. It needs the ffmpeg-wasi engine (the structured module). For
// a raw/headerless input that only opens with a forced demuxer or demuxer options,
// use ProbeInput.
func (r *Runtime) Probe(ctx context.Context, fs afero.Fs, path string) (Probe, error) {
	return r.ProbeInput(ctx, fs, Input{Path: path})
}

// ProbeInput probes one Input, forwarding its Format (forced demuxer) and Options
// (demuxer dictionary) — so a rawvideo/PCM input, probeable only with those (spec
// 0024), works through the typed API just as it does for a process job.
func (r *Runtime) ProbeInput(ctx context.Context, fs afero.Fs, input Input) (Probe, error) {
	spec, err := json.Marshal(jobSpec{
		Op: "probe", Version: vocabVersion,
		Inputs: []jobInput{{Path: input.Path, Format: input.Format, Options: input.Options}},
	})
	if err != nil {
		return Probe{}, errors.Wrap(err, "afmpeg: marshal probe spec")
	}

	res, err := r.Run(ctx, fs, string(spec))
	if err != nil {
		return Probe{}, err
	}

	if res.ExitCode != 0 {
		return Probe{}, errors.Newf("afmpeg: probe %q: %s", input.Path, tail(res.Stderr))
	}

	var out struct {
		Inputs []struct {
			Error       string            `json:"error"`
			Format      string            `json:"format"`
			DurationSec float64           `json:"duration_sec"`
			StartSec    float64           `json:"start_sec"`
			Streams     []ProbeStream     `json:"streams"`
			Tags        map[string]string `json:"tags"`
			Chapters    []Chapter         `json:"chapters"`
		} `json:"inputs"`
	}

	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		return Probe{}, errors.Wrapf(err, "afmpeg: probe %q: parse engine output", input.Path)
	}

	if len(out.Inputs) == 0 {
		return Probe{}, errors.Newf("afmpeg: probe %q: no input in engine output", input.Path)
	}

	in := out.Inputs[0]
	if in.Error != "" {
		return Probe{}, errors.Newf("afmpeg: probe %q: %s", input.Path, in.Error)
	}

	return Probe{
		Format: in.Format, DurationSec: in.DurationSec, StartSec: in.StartSec,
		Streams: in.Streams, Tags: in.Tags, Chapters: in.Chapters,
	}, nil
}

// tail returns the last stderrTailBytes of s, for surfacing an error tail.
func tail(s string) string {
	if len(s) > stderrTailBytes {
		return s[len(s)-stderrTailBytes:]
	}

	return s
}
