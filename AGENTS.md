# AGENTS.md

This file provides guidance to AI coding agents (Claude Code, agy, codex, etc.) when working with code in this repository.

**This file is a seed.** It carries what could be derived from the repository
and checked. What this is really for, where it has got to, and the traps it sets
are not here yet. Issue #9 tracks filling that in.

Ways of working live in the phpboyscout skills and are not repeated here, since
naming a skill ages better than restating it.

## What this is

`gitlab.com/phpboyscout/afmpeg` is a pure-Go FFmpeg binding that runs on a
virtual filesystem, with no CGO and no host FFmpeg install. FFmpeg is supplied
as a separate WebAssembly module and executed via wazero.

**It pairs with [`ffmpeg-wasi`](https://gitlab.com/phpboyscout/ffmpeg-wasi)**,
which builds that WebAssembly module. The pairing is real but is not a Go
module dependency, so `go.mod` will not show it and a change on either side can
break the other quietly.

Its direct toolkit dependencies are `go/errors` and `go/signing`.

**The README carries performance multipliers.** They are workload-dependent by
nature, so verify a figure against a measurement before repeating it anywhere
it will be read as a property of the software. See **checkable-claims**.

## The quality gate

`just ci` runs `tidy`, `generate`, `test`, `test-race` and `lint`.

## Which skills apply here

| When | Skill |
|---|---|
| Before `glab mr create` on this repo | `verify-before-pr` |
| Faking exec, `time.Now`, network or filesystem in a test | `race-safe-test-injection` |
| Reaching for a dependency the toolkit may already have | `use-the-go-toolkit` |
| Writing anything others will read and check | `checkable-claims` |
| Writing a commit message or a merge request description | `conventional-commits`, `pre-1-0-release-safety` |
| Committing, branching, merging, or opening a merge request | `forge-publish-workflow` |
| Working in a repo other than the one you were invoked in | `cross-repo-worktree` |

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

