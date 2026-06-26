# 0005 — the render helper + keyrx Renderer backend

Status: **DRAFT** (component spec; not started. Implements spec 0001 §7 / R-AF-7. Fast-follow
to 0004 per D-E. Review before building.)
Date: 2026-06-26
Parent: [0001-afmpeg.md](0001-afmpeg.md) §4 (R-AF-7), §7 (keyrx integration), §10 (D-E)
Owns: **R-AF-7** (timeline render helper)

## 1. Purpose

Two layers on top of `Run` (0004):
1. A **timeline render helper** in `pkg/afmpeg` that mirrors keyrx's `provider.Timeline` so
   callers don't hand-build the ffmpeg filtergraph (R-AF-7).
2. A **keyrx `Renderer` adapter** that registers as `providers.render: afmpeg` and hands the
   in-memory worktree's afero fs straight to afmpeg — lifting keyrx's in-memory render
   lock-out (0001 §7; keyrx spec 0015 D1; the spike's watch-list trigger).

This is the **fast-follow** to v1 (D-E): it only starts once `Run` is proven end-to-end (0004
R-0004-8).

## 2. The exact consumer contract (verified against keyrx)

The helper must reproduce, byte-for-graph, what keyrx's local renderer builds today, so afmpeg
output is a drop-in. From `keyrx/pkg/provider/provider.go`:

```go
type Segment    struct { MediaPath string; DurationSec float64 }
type AudioTrack  struct { Path string; Gain, DelaySec, FadeOut float64 }
type Timeline    struct { Width, Height, FPS int; XFadeSec float64
                          Segments []Segment; Audio []AudioTrack; OutputPath string }
type Video       struct { Path string; Width, Height int; DurationSec float64 }
type Renderer interface { Render(ctx context.Context, t Timeline) (Video, error) }
```

And the filtergraph to reproduce (from `keyrx/internal/render/ffmpeg/ffmpeg.go`):
- **Inputs:** each segment `-loop 1 -t <dur> -i <path>`; each audio `-i <path>`.
- **Video:** `[i:v]scale=W:H,fps=R,setsar=1[vi]` per segment, then an `xfade=transition=fade:
  duration=X:offset=O` chain (offset accumulates `dur - xfade`).
- **Audio:** per track `adelay=ms|ms` (if delay) `,volume=g` (`,afade=t=out:st=S:d=F` if
  fade), then `amix=inputs=M:normalize=0:duration=longest,alimiter=limit=0.95`.
- **Encode:** `-map [xN] -map [aout] -c:a aac -b:a 160k -t <total> -r <fps> -pix_fmt yuv420p
  -c:v libx264 -crf 20 -movflags +faststart out.mp4`.
- **Total duration:** `Σ dur − (n−1)·xfade`.

afmpeg should **reuse this construction** rather than reinvent it — the cleanest path is to
port `buildArgs`/`videoGraph`/`audioGraph` into `pkg/afmpeg` (they are already pure and
unit-tested in keyrx), so the helper builds the args and calls `Run`.

## 3. Design

```
afmpeg.Timeline ──(RenderHelper.buildArgs)──► []string ──► Runtime.Run(ctx, fs, args…)
        ▲                                                          │
        │  (mirrors provider.Timeline)                             ▼
keyrx provider.Renderer  ◄──(adapter in keyrx/internal/render/afmpeg)── Result → provider.Video
```

- **In afmpeg (`pkg/afmpeg`):** `type Timeline struct {…}` (afmpeg's own, field-compatible
  with keyrx's), `func (r *Runtime) Render(ctx, fs, t Timeline) (RenderResult, error)` that
  builds the args (ported `buildArgs`) and calls `Run`, mapping a non-zero exit to a wrapped
  error carrying the stderr tail (matching keyrx's 1500-byte tail).
- **In keyrx (`internal/render/afmpeg`, a new adapter — lives in the keyrx repo, specced
  here for the contract):** implements `provider.Renderer`; in `init()` does
  `provider.RenderFactory.Register("afmpeg", newAfmpeg)`. `Render` translates
  `provider.Timeline` → `afmpeg.Timeline`, supplies the worktree's `afero.Fs`, calls
  `Runtime.Render`, returns `provider.Video`. No keyrx call-site changes (registry pattern,
  keyrx 0001 §3.4).

## 4. Requirements

- `R-0005-1` (R-AF-7) `afmpeg.Timeline` is field-compatible with `provider.Timeline`; the
  helper builds the §2 filtergraph and renders via `Run`.
- `R-0005-2` **Graph parity:** for a given timeline, afmpeg's generated args equal keyrx's
  current `buildArgs` output (a golden test ports keyrx's `ffmpeg_test.go` expectations).
- `R-0005-3` **Output parity:** the rendered mp4 matches the native-ffmpeg render within
  tolerance (geometry, duration, codecs, playability) — the 0001 §7 "validate parity"
  requirement. Exact tolerance defined in the test (duration ±1 frame, same W/H/codec).
- `R-0005-4` The keyrx adapter registers under `"afmpeg"` and is selected by
  `providers.render: afmpeg` with **no call-site changes** (keyrx registry).
- `R-0005-5` A non-zero ffmpeg exit becomes a wrapped error with the stderr tail (parity with
  keyrx's current error surface).
- `R-0005-6` The in-memory path: keyrx hands the worktree afero fs to afmpeg; render runs with
  **no host fs access** (composes 0003/0004) — lifting the 0015-D1 lock-out for in-memory
  projects.

## 5. Test strategy

- **Graph golden tests** (afmpeg): port keyrx's `internal/render/ffmpeg/ffmpeg_test.go` table
  cases; assert identical args. Pure, fast, `t.Parallel()`.
- **Render parity** (gated integration): render a fixture timeline both ways (native ffmpeg
  vs afmpeg) and compare the outputs' probed properties.
- **keyrx wiring test** (in keyrx): the factory resolves `"afmpeg"`; an in-memory worktree
  renders end-to-end with a host-fs-denying assertion.
- Coverage ≥90% on the new `pkg/afmpeg` helper code.

## 6. Definition of done

- `afmpeg.Render`/`afmpeg.Timeline` implemented; graph golden tests green.
- Gated parity test passes (afmpeg output ≈ native).
- keyrx `internal/render/afmpeg` adapter registers + resolves via config (this part is a
  keyrx-repo change, cited back to this spec and keyrx 0015).
- keyrx's in-memory render lock-out documented as lifted when afmpeg is selected.

## 7. Sequencing

Depends on **0004** (`Run` proven end-to-end) and **0002** (the real module for parity). The
keyrx adapter half lands in the keyrx repo as a follow-up MR citing this spec — afmpeg ships
the helper; keyrx ships the wiring.
