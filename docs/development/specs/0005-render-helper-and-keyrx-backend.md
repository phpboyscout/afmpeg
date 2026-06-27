# 0005 — the ffmpeg command builder

Status: **DRAFT** (component spec; reframed 2026-06-27 from a keryx-specific "reel
render helper" to a general, use-case-agnostic command builder. Implements spec 0001
R-AF-7. Review before building.)
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

```go
package afmpeg

// Command assembles an ffmpeg invocation. Built fluently; Args() yields the
// argument slice for Runtime.Run. Zero value is a usable empty command.
type Command struct { /* … */ }

func NewCommand(opts ...GlobalOption) *Command
func (c *Command) Input(path string, opts ...InputOption) *Command
func (c *Command) FilterComplex(graph string) *Command
func (c *Command) Output(path string, opts ...OutputOption) *Command
func (c *Command) Args() []string

// Convenience on Runtime: build → run in one call.
func (r *Runtime) RunCommand(ctx context.Context, fs afero.Fs, c *Command) (Result, error)

// Options are functional and composable; each also has an escape hatch (Raw/Args)
// for flags the typed surface doesn't model, so the builder never blocks a workflow.
type GlobalOption func(*command)   // e.g. OverwriteOutput() → -y ; RawGlobal(args…)
type InputOption  func(*input)     // e.g. Loop(), Duration(d), Format(f), Seek(d), RawInput(args…)
type OutputOption func(*output)    // e.g. Map(label), VideoCodec(c), AudioCodec(c),
                                   //      CRF(n), VideoBitrate(b), AudioBitrate(b),
                                   //      PixelFormat(p), Format(container), FrameRate(r),
                                   //      Duration(d), MovFlags(f), RawOutput(args…)
```

Design rules:
- **Every typed surface has a raw escape hatch** (`RawGlobal/RawInput/RawOutput`), so an
  unmodelled flag never forces a user back to hand-building the whole arg slice.
- **Ordering is enforced** (globals → inputs → filtergraph → maps/outputs), which is the
  thing that's actually fiddly to get right by hand.
- The builder is **pure** (`Args()` has no I/O); only `RunCommand` touches the runtime.

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

- `R-0005-A` A pure `Command` builder that models globals/inputs/filtergraph/outputs and
  emits a correct, correctly-ordered arg slice (R-AF-7).
- `R-0005-B` Typed options for the common cases **plus** a raw escape hatch on every
  scope, so no workflow is blocked.
- `R-0005-C` Validated across the §4 unrelated workflows (not just a reel).
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
