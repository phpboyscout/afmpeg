# 0027 — runtime security hardening

Status: **IMPLEMENTED** (promoted from the external review to Phase 0 of the
[implementation roadmap](../implementation-roadmap.md) and built. Host fixes 4A + 4B shipped in
afmpeg with `-race` acceptance tests; engine fix 4C is committed in `ffmpeg-wasi/src/process.c`
and ships in the next routine engine rebuild. D-0027-B resolved to **512 MB**; D-0027-C resolved
to **impose-a-default-deadline (1 h) with override**.)
Date: 2026-07-02
Parent: [0007](0007-libav-direct-engine.md) (the engine this hosts); [0004](0004-runtime-and-api.md) (the `Runtime`/`New` surface these options extend)
Source: external commissioned review — [`ffmpeg-wasi/security_review.md`](../../../../ffmpeg-wasi/security_review.md) + [`security_review_supplementary.md`](../../../../ffmpeg-wasi/security_review_supplementary.md)
Owns: **R-SEC-HARDENING**

## 1. Why — the sandbox *is* the product, and these gaps undercut it

afmpeg's entire thesis (0012 §2; 0001) is **"safely process untrusted media"**: FFmpeg is a huge
C codebase with a long history of demuxer/decoder memory bugs, and the wazero WASM sandbox
*contains* any RCE a crafted file triggers to the guest's linear memory — the guest can't touch
the host disk, env, or network. That containment is real and is the reason to exist.

But containment of *code execution* is not containment of *resource abuse*. The review confirms
two host-side denial-of-service holes and one guest-side robustness nit that let a crafted (or
merely pathological) input harm the **host** without ever escaping the sandbox:

- a decoder that allocates multi-GB can **OOM-kill the host Go process** (no memory ceiling), and
- a decode loop that never terminates can **hang an invocation and wedge the `Runtime`** (no
  enforced deadline).

Because the sandbox is the value proposition, a gap that lets untrusted media take down the host
is **higher-priority than a generic robustness fix would be** — it directly falsifies the
headline promise. This spec closes the two DoS holes (finding 1 is the headline) and tidies the
JSON nit, all as **additive, back-compatible** options with safe defaults.

## 2. Validation — the three findings (all code-confirmed)

1. **No wazero memory limit (OOM risk) — the important one.** `runtime.go` `New()` builds the
   runtime as `wazero.NewRuntimeConfig().WithCoreFeatures(runtimeCoreFeatures).WithCloseOnContextDone(true)`
   — there is **no `WithMemoryLimitPages(...)`**. So the guest can grow to the wasm32 maximum
   (~4 GB). A crafted file declaring huge dimensions (e.g. 65535×65535) makes libavcodec/
   libavfilter allocate gigabytes for uncompressed frames, consuming host RAM until the OS
   OOM-kills the process. This directly defeats "safely process untrusted media."
2. **No hard timeout / mutex-hold risk.** `Run()` takes `r.mu.Lock()` for the whole invocation
   (serialise-one-at-a-time, 0004 D-0004-B). `WithCloseOnContextDone(true)` **is** set (good — a
   cancelled ctx aborts the guest), but nothing enforces a *deadline*. A consumer passing
   `context.Background()` into a pathological decode loop hangs the invocation and holds `r.mu`
   **forever**, permanently wedging the `Runtime` (and any `RunJob`/`Probe` behind the same mutex).
3. **cJSON type guards (minor robustness).** `process.c` `parse_output` iterates the options with
   `cJSON_ArrayForEach(kv, opts) { if (cJSON_IsString(kv)) … }` **without first checking
   `cJSON_IsObject(opts)`**. A malformed `options` (e.g. a JSON string instead of an object)
   iterates unpredictably. **Low severity** — `options` comes from the *trusted caller's job
   spec*, not the untrusted media — but the guard is cheap and belongs with the other two.

## 3. Scope

- **In:** a configurable guest **memory ceiling** with a safe default (finding 1); a **deadline
  policy** for `Run`/`RunJob`/`Probe` (finding 2); **nested-type guards** in `process.c`
  `parse_output` (finding 3). New host options, additive; one small engine-side C change.
- **Out:** CPU-time metering beyond the deadline; per-op resource budgets; changing the
  one-at-a-time serialisation model (0004 D-0004-B) or the vfs; anything that touches the
  trust/signing model (0010/0011). No new dependency, no licence impact (native/Go, nil).

## 4. Approach — per fix

### 4A. The memory ceiling (finding 1 — the headline)

wazero's `RuntimeConfig.WithMemoryLimitPages(pages)` caps guest linear memory (one page = 64 KiB);
past the cap a guest `memory.grow` fails, so libav's allocation returns an error the guest handles
(clean `AVERROR(ENOMEM)` → non-zero exit) **instead of** the host being OOM-killed. The fix:

- A new host option **`WithMemoryLimit(bytes int)`** (bytes for ergonomics; internally converted to
  pages, rounded up: `pages = ceil(bytes / 65536)`, clamped to the wasm32 max of 65536 pages / 4 GB).
- Applied in `New()` on the `RuntimeConfig` before compile: `…WithMemoryLimitPages(pages)`.
- **A default is set even when the option is absent** — the whole point is that the *out-of-the-box*
  runtime is safe, not only a runtime a careful consumer opts into. Default proposed **512 MB**
  (D-0027-B, open). `WithMemoryLimit(0)` (or a sentinel) means "no cap" for the rare consumer who
  knowingly wants the old behaviour.

### 4B. The deadline policy (finding 2)

`WithCloseOnContextDone(true)` already makes a *cancelled* context abort the guest; the gap is that
a context might never be cancelled. Fix — enforce a deadline at the `Run`/`RunJob`/`Probe` boundary:

- A new host option **`WithTimeout(d time.Duration)`** sets a default maximum invocation duration.
- Before locking `r.mu` and invoking, if the caller's ctx has no deadline, derive one:
  `ctx, cancel = context.WithTimeout(ctx, effectiveTimeout); defer cancel()`. If the caller's ctx
  *already* has an earlier deadline, honour it (take the min — never extend the caller's bound).
- On expiry the existing `exitCodeFrom` path already maps `ctx.Err()` to a wrapped
  `"invocation aborted"` error, and `WithCloseOnContextDone` tears down the guest — so the mutex is
  released and the `Runtime` stays usable. **D-0027-C (open):** impose-a-default-deadline (proposed)
  vs refuse-a-context-without-a-deadline (stricter, but a harsher API contract).

### 4C. The JSON guards (finding 3)

In `process.c` `parse_output`, guard the container type before iterating: only walk `options` when
`cJSON_IsObject(opts)` (and, defensively, apply the same shape to any other nested-container walk
that assumes an object/array). Cheap, keeps the driver predictable on a malformed-but-trusted spec.
Low severity, bundled here so the review's three items land together.

## 5. API impact — additive, back-compatible

Two new `Option`s on `New` (0004 surface), no signature changes, no behaviour change for existing
callers *except* the safer defaults:

```go
rt, err := afmpeg.New(ctx,
    afmpeg.WithModuleRelease("n8.1.2-5", afmpeg.VariantLGPL),
    afmpeg.WithMemoryLimit(512<<20),   // 512 MB guest ceiling (default if omitted)
    afmpeg.WithTimeout(1*time.Hour),   // per-invocation deadline (default if omitted)
)
```

- **Defaults do the protecting.** Omitting both yields a *hardened* runtime (512 MB cap + a default
  deadline), not today's unbounded one — this is the deliberate, and only, behavioural change.
  It is a safety tightening, not a break: existing well-behaved workloads fit comfortably under a
  512 MB / 1-hour envelope.
- `WithTimeout` interacts with `WithCloseOnContextDone` (already on) — the option supplies the
  deadline that teardown then acts on; the two compose.
- No change to `WithModuleURL`/`WithModuleRelease`, the vfs, or the `Command`/job-spec vocabulary.
  Engine change (4C) is internal to the driver; no vocabulary bump.

## 6. Decisions + open questions

- **D-0027-A — harden by default, opt out explicitly. ADOPTED.** The default `Runtime` is safe
  (a memory cap and a deadline apply with no option set); a consumer must *explicitly* pass a
  sentinel (`WithMemoryLimit(0)` / `WithTimeout(0)`) to remove a bound. A sandbox whose protections
  are off-by-default is not a sandbox.
- **D-0027-B — memory ceiling as bytes, default value. RESOLVED → 512 MB.** Option takes bytes
  (converted to pages internally). Chose the review's lower suggestion over 1 GB: safer for the
  multi-tenant/`RuntimePool` (0006 §2E) case, and `WithMemoryLimit` raises it for a legitimate 4K
  job. Implemented as `defaultMemoryLimitBytes = 512 << 20` in `runtime.go`.
- **D-0027-C — deadline policy: impose vs refuse. RESOLVED → impose-with-override.**
  Impose-a-default-deadline (friendlier — `context.Background()` still works, just bounded) over
  refuse-a-context-without-a-deadline (harsher API contract). Default `defaultTimeout = 1 * time.Hour`;
  `Run` imposes it only when the caller's context carries no deadline, and honours an earlier caller
  deadline as-is (never extends it).
- **D-0027-D — JSON guard scope. ADOPTED.** Guarded the `options` walk with `cJSON_IsObject` in
  `parse_output`; did not attempt a full schema validator — the job spec is trusted input, so this
  is defence-in-depth, not a security boundary.

## 7. Requirements

- **R-SEC-HARDENING** — Untrusted media processed through afmpeg cannot, by resource abuse alone,
  take down or wedge the host: (a) guest linear memory is capped at a configurable ceiling
  (default-on) so an over-allocating decode fails cleanly instead of OOM-killing the host;
  (b) every invocation runs under an enforced deadline (default-on) so a non-terminating decode
  cannot hold `r.mu` indefinitely; (c) the engine's job-spec parsing guards nested-container types
  before iterating. All additive and back-compatible bar the safer defaults.

## 8. Test surface

- **Memory ceiling (finding 1).** A fixture/job that provokes a large libav allocation (a spec or
  crafted input declaring outsized dimensions) run under a low `WithMemoryLimit` → **assert a clean
  non-zero exit / bounded error, and that the host process survives** (RSS stays bounded), i.e. an
  `ENOMEM`-style guest failure, not a host OOM. The negative control: the same job under a generous
  cap succeeds.
- **Deadline (finding 2).** An invocation given a context with **no deadline** against a
  never-terminating (or artificially long) job → **assert it returns a bounded "aborted" error
  within ~the configured timeout, `r.mu` is released, and a subsequent `Run` on the same `Runtime`
  succeeds** (proving no permanent wedge). Also: a caller deadline earlier than the default is
  honoured (min, not extended).
- **JSON guards (finding 3).** `process.c` unit/integration: a job spec whose `options` is a string
  (not an object) → parsed without misbehaviour, predictable result (skip/ignore), no crash.
- Standard bars per the dev method: table-driven, `t.Parallel()`, `go test -race` clean; the two
  DoS tests are the load-bearing acceptance criteria.

## 9. Dependencies

- **wazero** — already a dependency; `WithMemoryLimitPages` is in the pinned version (no new dep).
- **ffmpeg-wasi** engine (0007) for 4C — a one-line-scale guard in `src/process.c`; ships in a
  routine engine rebuild (next `n8.1.2-N`), no vocabulary bump, no afmpeg re-pin required.
- Independent of the trust/signing specs (0010/0011) and the parity roadmap (0012). Composes with
  `RuntimePool` (0006 §2E) — a per-instance memory cap is exactly what a multi-tenant pool wants.

## 10. Definition of done

The decision on this PROPOSED spec is recorded. If accepted: a default `afmpeg.New` runtime caps
guest memory and enforces an invocation deadline with **no option required**; `WithMemoryLimit` and
`WithTimeout` let a consumer tune or (explicitly) remove each bound; `parse_output` guards its
nested-container walks. The two host-side DoS tests (over-allocation → clean failure not host OOM;
no-deadline hang → bounded, `Runtime` still usable) pass under `-race`, D-0027-B/C are resolved, and
the how-to/reference docs note the new defaults and options. If declined: the findings stand
recorded here as known, accepted residual risk.
