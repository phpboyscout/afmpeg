# AGENTS.md

This file provides guidance to AI coding agents (Claude Code, agy, codex, etc.) when working with code in this repository.

Ways of working live in the phpboyscout skills and are not repeated here, since
naming a skill ages better than restating it.

## What this is

`gitlab.com/phpboyscout/afmpeg` is a pure-Go FFmpeg binding that runs on a
virtual filesystem, with no CGO and no host FFmpeg install. The point of it is
that the media never has to exist as a file: a caller hands `RunJob` an
`afero.Fs`, which can be a `MemMapFs`, and the guest engine's WASI filesystem
calls are answered out of that.

It does not build FFmpeg and it does not embed the WebAssembly module.
`WithModuleRelease`, `WithModuleURL` and `WithModuleFile` all take the module
from outside, which is a licensing decision rather than an oversight: FFmpeg
with x264 is GPL, so the copyleft obligation attaches to whoever bundles the
artifact (spec 0001 section 10). The native backend is a subprocess over a Unix
socket rather than a linked library, so that going faster does not cost the
CGO-free property the project exists for. There is no CLI; spec 0009 is `DRAFT`
and blocked on value being proven.

**Where the boundary with ffmpeg-wasi falls.** All the C lives there: the wasm
module, the native driver, the FFmpeg configure flags, and the engine that reads
a job spec and drives libav. The contract between the two repos is a single
integer, `vocabVersion` in `pkg/afmpeg/vocab.go`, so a change that needs the
engine to understand a new field starts in ffmpeg-wasi and lands here second.
ffmpeg-wasi has no wiki, so the engine and build specs (0035 to 0038, 0041 to
0043) live in
[afmpeg's wiki](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/home)
alongside the Go ones, and that wiki is canonical for a spec's status.

## Where it has got to

The public API, the `Command` builder, certified release acquisition and
progress reporting are shipped and stable, and the vfs bridge is the oldest and
least likely part to move. The native backend's filesystem boundary is
mid-redesign, so read spec 0043 (`DRAFT`, superseding 0039 and 0040, both
`REJECTED`) before touching `pkg/afmpeg/native`. `CHANGELOG.md` and the
roadmap's anchor block carry the current afmpeg, engine tag and vocab triple;
prefer both to the README, whose status block has been left behind before.

## The traps

**A green `just ci` says nothing about whether the engine works.** Every test
that touches a real module or driver is gated on an `AFMPEG_TEST_*` environment
variable and the MR pipeline sets none of them, so those tests skip in CI,
always. Run `just test-integration` with both the lean and intermediate profiles
before believing an engine-facing change.

**"No host disk touched" is a property of the WASM backend, not of afmpeg.** On
wazero the guest only sees the `afero.Fs` plus a synthetic `/tmp` and
`/dev/null`. On the native backend it is currently untrue: spec 0043 records the
channels through which libav still reaches the host filesystem. Until 0043
lands, do not write the guarantee as backend-independent.

**The published speed figures are several different quantities.** The docs now
say ~50x (openh264) to ~170x (libx264) for software encode, re-measured on
n9.0.1 in
[the 2026-08 report](https://gitlab.com/phpboyscout/afmpeg/-/wikis/reports/2026-08-native-vs-wasm-speed).
Two things that report settles and neither is obvious: the **encoder** moves the
ratio by 3.5x, and the **FFmpeg version** does not move it at all (n8.1.2 and
n9.0.1 agree to within noise), so it does not need re-measuring every bump. Spec
0030 rests on a different 13 to 63x from the dav1d spike — a separate quantity,
do not run the ranges together. Attribute a figure to the report it came from
with its comparison attached. Fresh numbers need care too: `cmd/afmpeg-bench`
puts a lane that cannot use a second core against one that can, so a run taken
under contention reads exactly like a valid one. Pass `-native-driver`, pin
`-native` deliberately, and sample twice — the reel ratio's denominator is a
sub-300ms measurement and swings 13% between runs.

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
