# 0005 — the ffmpeg command builder

Status: **IMPLEMENTED** (shipped in afmpeg v0.4.0 as the **job-spec builder** — `JobSpec`/`RunJob`
in `pkg/afmpeg/command.go`, not the `ffmpeg`-arg `RunCommand` this spec first sketched: v0.4.0
removed the CLI transport in favour of typed inputs/filtergraph/outputs. Implements spec 0001
R-AF-7. Reframed 2026-06-27 from a keyrx-specific reel helper to a general, use-case-agnostic
builder.)
Date: 2026-06-27 (reframed from the 2026-06-26 reel-helper draft)
Parent: [0001-afmpeg.md](0001-afmpeg.md) §4 (R-AF-7), §7 (consumer integration)
Owns: **R-AF-7** (a higher-level builder so callers don't hand-assemble arg slices)

## 0. Why this was reframed

The first draft of this spec was a *reel render helper* that ported keryx's
`buildArgs` verbatim — a crossfade-stills-plus-audio-mix timeline with libx264/AAC/
alimiter/+faststart baked in. That made keryx's highly opinionated reel structure
afmpeg's public API. **afmpeg is a general-purpose ffmpeg toolkit; keryx is the first
reference customer, not the API author.** A reel is one composition among countless
ffmpeg workflows (transcode, scale, crop, overlay, concat, thumbnail, audio extract,
mux, …). This spec is therefore a *general command builder*; keryx's reel is built **by
keryx**, on top of this builder (or raw `Run`), and lives in keryx's repo.

## 1. Purpose

A typed, composable way to construct **any** ffmpeg invocation — inputs (each with its
own options), an optional filter graph, and outputs (each with codec/quality/map
options) — without hand-concatenating string arguments. It produces the arg slice that
`Runtime.Run` (spec 0004) executes over the vfs bridge. `Run(ctx, fs, args…)` remains
the universal primitive; the builder is ergonomic sugar over it, not a replacement.

The builder makes **no assumption about the workflow**: it models ffmpeg's own command
structure, not a use case.

## 2. The command structure modelled

```
ffmpeg [global opts] {[input opts] -i INPUT}…  [-filter_complex GRAPH]  {[output opts] [-map …] OUTPUT}…
```

That shape — globals, N inputs each with per-input options, an optional filtergraph,
M outputs each with per-output options — is the whole abstraction. Filtergraph syntax
is ffmpeg's own string DSL (already fully general); the builder does not try to model
individual filters, it just places the graph correctly.

## 3. API (sketch — confirm in review)

The shape is a **hybrid, deliberately idiomatic Go**: a plain `Command` *struct* is the
canonical, inspectable data model (fill it directly for full control), and `NewCommand`
is an ergonomic constructor that applies **sane defaults + functional options** on top —
for callers who want the variadic `With*` feel or the baked defaults. A command is
*data*, so the struct is primary; functional options are reserved for *constructing* it.

```go
package afmpeg

// Command is a declarative description of an ffmpeg invocation. Construct it
// directly as a struct (zero value usable) or via NewCommand for defaults +
// options. Args() renders it to the argument slice Run executes (pure, no I/O).
type Command struct {
	Global        Global
	Inputs        []Input
	FilterComplex string
	Outputs       []Output
}

type Global struct {
	OverwriteOutput bool     // -y
	LogLevel        string   // -loglevel (e.g. "error"); "" = ffmpeg default
	Raw             []string // arbitrary global flags
}

type Input struct {
	Path     string
	Loop     bool     // -loop 1
	Duration float64  // -t (pre-input)
	Format   string   // -f
	Raw      []string // arbitrary pre-input flags (e.g. -ss)
}

type Output struct {
	Path       string
	Map        []string // -map …
	VideoCodec string   // -c:v
	AudioCodec string   // -c:a
	PixelFormat string  // -pix_fmt
	Format     string   // -f (container)
	Raw        []string // arbitrary per-output flags (e.g. -crf, -b:v, -frames:v, -movflags)
}

func (c Command) Args() []string

// NewCommand builds a Command from sane defaults and functional options.
func NewCommand(opts ...CommandOption) Command

// Convenience on Runtime: build → run in one call.
func (r *Runtime) RunCommand(ctx context.Context, fs afero.Fs, c Command) (Result, error)

// A curated option roster (the struct carries the long tail to avoid polluting
// the package namespace and input/output name collisions):
type CommandOption func(*Command) // OverwriteOutput(); WithInput(path, …InputOption);
                                  // WithFilterComplex(g); WithOutput(path, …OutputOption); GlobalRaw(args…)
type InputOption   func(*Input)   // Loop(); Duration(d); InputFormat(f); InputRaw(args…)
type OutputOption  func(*Output)  // Map(label); VideoCodec(c); AudioCodec(c);
                                  // PixelFormat(p); OutputFormat(f); OutputRaw(args…)
```

Design rules:
- **The struct is complete; the option roster is curated.** Every ffmpeg flag is reachable
  via a struct field or a `Raw` slice, so an unmodelled flag never blocks a workflow; the
  `With*`/option funcs cover the common 80% ergonomically.
- **Two equally-valid entry points.** `Command{…}` (explicit, exactly what you set — zero
  value = no defaults) and `NewCommand(…)` (sane defaults + options). Same rendered args.
- **Ordering is enforced** by `Args()` (globals → inputs → filtergraph → maps/outputs).
- **Pure data.** `Args()` has no I/O and the `Command` is comparable/copyable/serialisable
  (a pipeline can come from YAML/JSON); only `RunCommand` touches the runtime.

### D-0005-A — the "sane defaults" NewCommand bakes. RESOLVED 2026-06-27 (Matt)
`NewCommand` bakes **`OverwriteOutput = true` (-y) and `LogLevel = "error"`** (quiet
logging for programmatic callers) — and **no codec/quality/pixel-format opinion** (that
would re-introduce consumer bias; ffmpeg's container-based defaults apply, or the caller
sets them). A caller wanting ffmpeg's full logs overrides `LogLevel`. A zero-value
`Command{}` struct bakes **no** defaults (fully explicit, `LogLevel == ""` → ffmpeg's
default verbosity).

## 4. The generality bar (validation)

The builder MUST express a spread of unrelated workflows — this is how we prove it isn't
reel-shaped. Golden tests assert the produced args for at least:

- `R-0005-1` **Transcode** — `-i in.mkv -c:v libx264 -crf 23 -c:a aac out.mp4`.
- `R-0005-2` **Scale / filter** — a simple `-vf scale=…` (or `-filter_complex`) resize.
- `R-0005-3` **Overlay** — two inputs + a `-filter_complex` overlay + `-map`.
- `R-0005-4` **Concat** — multiple inputs via the concat filter.
- `R-0005-5` **Thumbnail** — single frame out (`-frames:v 1`) to an image.
- `R-0005-6` **Audio extract** — `-vn -c:a` to an audio file.
- `R-0005-7` **The keryx crossfade reel** — expressible as one example (inputs looped,
  an `xfade` chain + `amix`/`alimiter` filtergraph, libx264/AAC mp4), proving the builder
  still covers the original use case **without** that use case being privileged in the API.
- `R-0005-8` **Raw escape hatch** — an unmodelled flag passes through via `Raw*`.

Plus: `RunCommand` runs a built command end-to-end over a MemMapFs (composing 0003/0004),
no host-fs access.

## 5. Consumer integration (keryx, and anyone)

keryx adapts to afmpeg, not the reverse. keryx's `Renderer` keeps owning the *reel*
decisions (which segments, which crossfade, the encode profile) and builds an
`afmpeg.Command` (or arg slice) for them in **keyrx's** repo, then calls `RunCommand`/
`Run` with the in-memory worktree fs. This lifts keryx's in-memory render lock-out
(spec 0001 §7) while keeping reel structure out of afmpeg. The same is true for any
other consumer: afmpeg gives them the toolkit; the workflow is theirs.

## 6. Requirements summary

- `R-0005-A` A pure `Command` **struct** (globals/inputs/filtergraph/outputs) whose
  `Args()` emits a correct, correctly-ordered arg slice (R-AF-7). Usable as a zero value.
- `R-0005-B` A `NewCommand(opts…)` constructor applying sane defaults (D-0005-A) + a
  curated set of functional options (`With*`/`Input/OutputOption`); both entry points
  render identical args. Every flag remains reachable via a struct field or a `Raw` slice,
  so no workflow is blocked.
- `R-0005-C` Validated across the §4 unrelated workflows (not just a reel), via **both**
  the struct and `NewCommand` forms.
- `R-0005-D` `RunCommand` convenience; end-to-end over a MemMapFs with no host-fs access.
- `R-0005-E` No keryx-specific types, constants, or assumptions in afmpeg.

## 7. Definition of done

- `Command` + options implemented; `Args()` golden-tested across the §4 workflows.
- `RunCommand` runs a built command end-to-end (gated full-encode validation waits on the
  real `ffmpeg.wasm`, spec 0002).
- ≥90% coverage on new `pkg/afmpeg` code; `-race`; `CGO_ENABLED=0`; lint clean.
- Diátaxis how-to(s) for common workflows; package doc + any sentinel catalogued.
- A note (here and in keryx) that the keryx reel is built on this, in keryx's repo.

## 8. Sequencing

Depends on **0004** (`Run`/`RunCommand`). Independent of **0002** for the builder itself
(pure arg construction); full-encode parity waits on the real module. The keryx reel
adapter is a separate keyrx-repo change citing this spec.
