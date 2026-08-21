# AGENTS.md

This file provides guidance to AI coding agents (Claude Code, agy, codex, etc.) when working with code in this repository.

Ways of working live in the phpboyscout skills and are not repeated here, since
naming a skill ages better than restating it.

## What this is

`gitlab.com/phpboyscout/afmpeg` is a pure-Go FFmpeg binding that runs on a
virtual filesystem, with no CGO and no host FFmpeg install. FFmpeg is supplied
as a separate WebAssembly module and executed via wazero.

The point of it is that the media never has to exist as a file. A caller hands
`RunJob` an `afero.Fs`, which can be a `MemMapFs`, and the guest engine's WASI
filesystem calls are answered out of that (`internal/vfs`). keryx produced the
requirement, because it renders reels from a repository cloned into RAM and the
ffmpeg binary insists on real paths. afmpeg is deliberately not shaped around
keryx: `Command` renders the engine's JSON job spec and stops there, so a reel or
a timeline is composed in the consumer's code (spec 0005).

Things it deliberately does not do:

- **It does not build FFmpeg.** Nothing here compiles C. The one `.c` file in the
  tree is `docs/development/spikes/0028-custom-avio-bridge/avio_spike.c`, kept as
  the record of a spike.
- **It does not embed the module.** `WithModuleRelease` / `WithModuleURL` /
  `WithModuleFile` all take it from outside, and that is a licensing decision, not
  an oversight: FFmpeg with x264 is GPL, so the copyleft obligation attaches to
  whoever fetches and bundles the artifact rather than to this library (spec 0001
  section 10, D-C).
- **It does not link libav.** The native backend is a subprocess talking over a
  Unix socket precisely so that going faster does not cost the CGO-free property
  the project exists for. `just build` compiles with `CGO_ENABLED=0`.
- **It does not shell out to a host ffmpeg.** The single exception is
  `cmd/afmpeg-bench`, which needs a native baseline to compare against, and is an
  investigation tool rather than shipped surface.
- **It has no CLI.** Spec 0009 is `DRAFT` and blocked on value being proven.

Its direct toolkit dependencies are `go/errors` and `go/signing`; the rest of
`go.mod` is afero, wazero, godog and `golang.org/x/image`.

### Where the boundary with ffmpeg-wasi falls

ffmpeg-wasi is where all the C lives: the `wasm32-wasi` module, the native driver
ELF, the FFmpeg configure flags, the codec and filter allowlists, and the engine
that reads a job spec and drives libav directly. afmpeg is the Go half and owns
the afero-to-wazero bridge, `Runtime` and its hardening (memory ceiling, deadline),
the `Command` builder, result and progress parsing, and signature-verified
acquisition of both artifacts.

Two consequences worth holding on to.

**The contract between them is one integer.** `vocabVersion` in
`pkg/afmpeg/vocab.go` (currently 9) is stamped on every process and probe spec,
and `evaluateVocab` rejects an engine that reports a lower one. It is one
directional by design: a newer engine, or a module that answers nothing, is
tolerated so `Runtime` stays able to run any wasm module. So if a change needs the
engine to understand a new field, the work starts in ffmpeg-wasi and lands here
second, behind a version bump.

**The specs cover both repos and all live here.** ffmpeg-wasi has no wiki. Engine
and build specs (0035 to 0038, 0041, 0042, 0043) sit in
[afmpeg's wiki](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/home)
alongside the Go ones, and that wiki is canonical for a spec's status.

## Where it has got to

`CHANGELOG.md` has the current version and the implementation roadmap's anchor
block carries the triple that matters (afmpeg, engine tag, vocab version). Prefer
both to the README, whose status block has been left behind by releases before.

**Stable, and safe to build on.** The public API (`New`, `Run`, `RunJob`, `Probe`,
`Frames`, `Close`), the `Command` builder, certified release acquisition
(`WithModuleRelease`, `native.NewFromRelease`) and progress (`WithProgress`) are
all shipped and have been through several releases. Roadmap phases 0 to 4 are
done, and most of phase 5 with them. The vfs bridge is the oldest part of the
codebase and the least likely to move.

**Still moving: the native backend's filesystem boundary.** Specs 0039 and 0040
were written, reviewed twice and marked `REJECTED`, superseded by 0043 (`DRAFT`).
0041, on what the IPC protocol cannot express, is also `DRAFT`. Read 0043 before
touching `pkg/afmpeg/native`; the shape it proposes deletes code rather than
adding to it.

**Not moving, and waiting on a trigger rather than on effort.** The CLI (0009),
WASM threading (0030), native arm64 and darwin (0022), hardware-accelerated
encoders, A/V sync (0025) and native phase-B progress (0033). The roadmap's
pick-up menu names the trigger for each. None of them is on a critical path, so
"nobody has got to it" is the wrong reading.

## The traps

**A green `just ci` says nothing about whether the engine works.** The gate is
`tidy`, `generate`, `test`, `test-race`, `lint`, and every test that touches a
real module or driver is gated on an environment variable
(`AFMPEG_TEST_FFMPEG_WASI`, `AFMPEG_TEST_FFMPEG_WASI_INTERMEDIATE`,
`AFMPEG_TEST_NATIVE_DRIVER*`). The MR pipeline sets none of them, so those tests
skip in CI, always. Run `just test-integration` with both profiles before
believing an engine-facing change. The profiles are not interchangeable either:
an intermediate module satisfies a lean test, a lean one does not satisfy an
intermediate test, and `integrationModule` deliberately refuses to fall back so
you get one honest skip instead of a dozen failures about a missing muxer.

**"No host disk touched" is a property of the WASM backend, not of afmpeg.** On
wazero the guest only ever sees the `afero.Fs` plus a synthetic `/tmp` and
`/dev/null`. On the native backend it is currently untrue: spec 0043 records that
libav reaches the filesystem through four channels and the IPC bridge covers one
and a half, so path-taking options (subtitles and ass, lut3d, curves, drawtext,
libx264 stats) and the URL-protocol layer go to the host's real filesystem. Until
0043 lands, do not write the guarantee as backend-independent, in docs or in a
commit message.

**Benchmarks taken on a busy machine are wrong and look right.**
`cmd/afmpeg-bench` puts a lane that cannot use a second core against one that can.
ffmpeg-wasi configures the wasm target `--disable-pthreads --disable-w32threads
--disable-os2threads --disable-asm --disable-x86asm` (`build/libav.sh`) and passes
none of those on the native target; on the baseline side `-native` defaults to
whatever `ffmpeg` is on `PATH`, and the CLI asks every decoder for `threads=auto`
(`fftools/ffmpeg_dec.c`) while libx264's codec defaults ask for
`X264_THREADS_AUTO`. The two lanes therefore lose different amounts when
something else on the box is using the cores, and the report header records the
CPU count and the ffmpeg version but nothing at all about load. A run taken under
contention reads exactly like a valid one. The single giveaway is a ratio below 1,
WASM apparently beating native: ffmpeg-wasi#9 has a run filed `CONTAMINATED` that
produced a 0.4x row, from measuring against the distro's ffmpeg 6.1.1 under load
instead of a driver built from the engine tag under test. Pass `-native-driver`
and pin `-native` deliberately.

**The published speed figures are three different quantities, and none of them is
verified against the current engine.** The 48 to 58x in the README, the roadmap
and ffmpeg-wasi's docs comes from the 0028 spike
(`docs/development/spikes/0028-perf/REPORT-native.md`): WASM openh264 against our
native driver, same encoder both sides, 320x240. Spec 0030's argument rests on a
different number again, 13 to 63x, from the 2026-07-05 dav1d spike, and the spec
now carries a note calling its re-measurement a prerequisite for the decision it
frames rather than a tidy-up. ffmpeg-wasi#9 is open to re-measure on n9.0.1; its
2026-08-21 note posts unlanded runs, says the published figure is not
contradicted by them, and keeps the ticket open because the recorded measurement
it asks for still does not exist. Quote a figure by attributing it to the report
or spec it came from, with the comparison and the resolution attached, and do not
restate any of them as a current property of the software. Beware in particular
of a spread quoted as one range: a 7.5x thumbnail, a 46x driver comparison and a
287x CLI comparison are not endpoints of the same measurement.

**A `Runtime` runs one job at a time.** Invocations are serialised behind a
size-1 semaphore, so concurrency means a pool of Runtimes, not concurrent calls
on one. Compiling the module is the expensive step, so build the pool once.

## The quality gate

`just ci` runs `tidy`, `generate`, `test`, `test-race` and `lint`.

## Which skills apply here

| When | Skill |
|---|---|
| Starting any non-trivial change, or numbering a new spec | `spec-driven-development` |
| A change that belongs in ffmpeg-wasi rather than here | `raise-a-forge-issue`, `cross-repo-worktree` |
| Adding a test that needs a built module or driver | `env-gated-integration-tests` |
| Editing `pkg/afmpeg/features/` or the godog steps in `bdd_test.go` | `bdd-when-and-how`, `write-godog-scenarios` |
| Faking exec, `time.Now`, network or filesystem in a test | `race-safe-test-injection` |
| Answering a question that needs a measurement first | `spike-before-spec` |
| Reaching for a dependency the toolkit may already have | `use-the-go-toolkit` |
| Editing anything under `docs/` | `diataxis-docs` |
| Writing anything others will read and check, benchmark numbers above all | `checkable-claims` |
| Before `glab mr create` on this repo (run `just ci` first) | `verify-before-pr` |
| Writing a commit message or a merge request description | `conventional-commits`, `pre-1-0-release-safety` |
| Committing, branching, merging, or opening a merge request | `forge-publish-workflow` |

> Skills are a Claude Code mechanism, shipped by the
> [phpboyscout marketplace](https://gitlab.com/phpboyscout/claude-code-plugins).
> An agent without them should treat a named skill as a topic to ask about
> rather than a file it can load.

## House rules

- Linear history. Rebase and fast-forward; never squash-merge from the UI.
- Conventional Commits, and the type decides whether a release is cut. Only
  `feat` and `fix` release.
- No AI attribution in anything published, and never at-mention anyone.
- Never cut a release yourself. That is the maintainer's call, every time.
