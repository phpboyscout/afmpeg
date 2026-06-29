# 0009 — `cmd/afmpeg` CLI (deferred — value unproven)

Status: **DRAFT / DEFERRED** (a deliberately-deferred goal whose *value is not yet
established*. This spec exists to make — or fail to make — the case for an afmpeg CLI **before**
any code. No implementation until the value question (§3) is answered.)
Date: 2026-06-29
Parent: [0001-afmpeg.md](0001-afmpeg.md) §5 (R-AF-13)
Supersedes: [0006-hardening-roadmap.md](0006-hardening-roadmap.md) §2D
Owns: **R-AF-13** (CLI)

## 1. Context

0006 §2D floated a thin `cmd/afmpeg` CLI — "a drop-in `ffmpeg` over a virtual fs." Since then,
v0.4.0 **deliberately removed** the ffmpeg-CLI arg transport: the engine is **job-spec only**
(spec [0007](0007-libav-direct-engine.md) D-FW-B; `Args`/`RunCommand`/`Global` and CLI-option
parsing are gone). That changes the calculus, and this spec records two things: a hard
constraint, and an open value question.

## 2. Constraint (binding)

**D-0009-A — we will not reintroduce an `ffmpeg`-argument-compatible CLI.** A faithful drop-in
`ffmpeg` (parse `-i in -vf … -c:v … out`) rebuilds the exact arg-parsing surface v0.4.0 deleted,
re-coupling afmpeg to the CLI grammar the libav-direct pivot exists to escape. Any CLI this spec
ever justifies is **job-spec-native**, not `ffmpeg`-compatible. This is settled regardless of §3.

## 3. The open question (the point of this spec)

**Is there enough value in *any* afmpeg CLI to build one?** Today: unproven. The library is the
product; a CLI must earn its place against the cost of a second public surface to maintain,
document, and version.

Arguments **for** (to be tested, not assumed):

- **Dogfooding** — exercising the engine without writing Go surfaces rough edges in the
  `Command`/job-spec API.
- **Demos / marketing** — "here is a sandboxed, in-memory ffmpeg you can run" is a tangible
  artifact for the project's narrative (0007 R-FW-8).
- **Ad-hoc / CI use** — a one-shot binary for scripts and pipelines that don't embed Go.

Arguments **against**:

- **`tools/run` already exists** in ffmpeg-wasi for driving a module directly; some of the
  "just run it" need is already met outside afmpeg.
- **Surface cost** — a CLI is API: flags become a compatibility contract, it needs its own
  Diátaxis reference, and it dilutes the "afmpeg is a library" message.
- **No demand yet** — no consumer has asked; building it speculatively is a guess.

## 4. Candidate shape (only if §3 resolves "yes")

A job-spec-native CLI, thin over the existing `pkg/afmpeg` surface, likely on the
go-tool-base CLI framework (org consistency, per 0006 §2D):

- `afmpeg run <spec.json> [--root DIR]` — run a job spec with a real directory mounted as the vfs.
- `afmpeg probe <file>` — the `Probe` op, JSON out.
- Possibly typed flags → `Command` builder → job spec, for the common transcode case.
- `--module` / reuse the `WithModuleURL` acquisition (spec 0004 D-0004-C) so it can fetch+cache.

The CLI stays a **thin adapter** (per the dev method: "library before CLI; logic lives in
`pkg/`"). It introduces no engine capability the library lacks.

## 5. Non-goals

- `ffmpeg`-argument compatibility (D-0009-A).
- Any engine/job-spec capability that doesn't already exist in `pkg/afmpeg`.

## 6. Requirements

- **R-AF-13** *(gated on §3)* — *If* value is established, a thin, job-spec-native `cmd/afmpeg`
  CLI over the existing library surface, with its own reference docs. **Blocked** until the
  value question is answered affirmatively; this spec is the gate.

## 7. Definition of done (this spec)

Either: the value question (§3) is answered **yes** with a concrete justification and a trigger,
promoting R-AF-13 to an implementation spec — or it is answered **no/​not-yet** and R-AF-13 stays
parked here with the reasoning recorded. The constraint in §2 stands either way.
