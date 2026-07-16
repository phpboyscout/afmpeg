# 0033 — native progress side-channel (0031 phase B for Backend B)

Status: **DEFERRED** (draft retained as the decision record) — considered 2026-07-16 and **not**
scheduled: the value for the native backend is marginal and the cost (engine change + a new signed
native-driver release) is disproportionate. Trigger to revive in §6. Do not implement until it is
revived and moved to `approved`.
Date: 2026-07-16
Parent: [0032](0032-engine-progress-side-channel.md) (this completes its deferred native half — 0032
§Scope "Native backend parity … the record schema is shared so native can adopt it later", D7);
[0028](0028-native-subprocess-backend.md) (the native driver + IPC host this extends);
[0031](0031-job-progress-reporting.md) (the `Progress` stream both phases feed)
Owns: **R-PROGRESS-B-NATIVE** — engine-emitted frame/time/speed progress for the **native** backend,
on the same `WithProgress` channel as WASM.

## 1. Why

[0032](0032-engine-progress-side-channel.md) shipped phase B for the **WASM** backend: the engine
writes NDJSON `{frame,out_time_us,total_size,duration_us}` records to a `/dev/afmpeg-progress` device
that afmpeg's vfs serves, and the host merges them onto `WithProgress`. It **explicitly deferred the
native backend** (0032 §Scope, D7): "this spec is the WASM side. The record schema is shared so
native can adopt it later."

Today the native backend delivers **phase A only** (byte progress observed at the `afero.Fs`
boundary). `Frame`/`OutTime`/`Speed` stay zero. The reason is structural, not a bug:

- `ffmpeg-wasi/src/progress.c` opens the device with a **raw POSIX `open("/dev/afmpeg-progress")`**,
  not through libav's AVIO.
- The native IPC bridge (`nativeio.c`) only wraps **AVIO** (media file I/O). A raw `open()` in the
  native driver therefore hits the **real host filesystem**, finds no such file, and the emitter
  goes **inert** (`fd < 0`, best-effort no-op).

So the native driver emits nothing over any host-visible channel. Closing the gap needs a **native
transport** for the records — a cross-repo change (engine + host), which is why 0032 deferred it.

## 2. Scope

**In:**
- **ffmpeg-wasi (engine):** in the **native** build, `progress.c` emits its existing records over a
  host-provided transport instead of the raw device path. No record-schema change (0032's schema is
  reused verbatim). Needs a native-driver rebuild + a new release tag.
- **afmpeg (host):** the native backend provisions that transport when `WithProgress` is active,
  reads the records, and feeds the **existing** `progressReporter` engine sink — the same merge path
  and `Progress` fields as WASM.

**Out:**
- The record schema, the `Progress` public type, and the `WithProgress` API — all unchanged (0031
  §6.1 / 0032). This spec adds a **transport**, not a surface.
- `linux/arm64` / `darwin/arm64` native drivers (still gated on the cross-build toolchains, 0022).
- Any change to WASM phase B.

## 3. Decisions

- **D1 — transport (OPEN).** The native driver emits records over a **dedicated Unix socket** whose
  path afmpeg passes in the env var **`AFMPEG_NATIVE_PROGRESS_SOCKET`**, mirroring
  `AFMPEG_NATIVE_SOCKET`. `progress.c`'s native branch connects once and writes the NDJSON lines;
  the host reads lines and feeds the sink.
  - *Rejected — multiplex over the existing `io.sock`:* that socket is **one-connection-per-opened-file**
    and framed for AVIO (`O/R/W/S/Z/C`); interleaving a byte-stream of progress lines would
    complicate the framing and couple a best-effort channel to the critical I/O path. A dedicated,
    independent socket keeps progress strictly best-effort and out of the I/O framing.
  - *Rejected — a second framed protocol:* progress is a one-way append stream; newline-delimited
    JSON over a bare socket is sufficient and matches the WASM device's line semantics exactly.
- **D2 — opt-in & best-effort.** afmpeg provisions the socket (and sets the env) **only** when
  `WithProgress` is active (the same condition that already stamps `progress:true`). With the env
  unset, `progress.c`'s native branch is inert. Any socket error — connect, write, host gone — is
  swallowed; progress I/O must **never** block, slow, or fail the encode (identical contract to the
  WASM device).
- **D3 — host seam (OPEN).** The per-invocation sink lives in `ctx` (attached by
  `startProgress` → `withProgressSink`, unexported in package `afmpeg`). The native backend is a
  separate package (`pkg/afmpeg/native`), so it needs an **exported accessor** —
  `afmpeg.ProgressSinkFromContext(ctx) func([]byte)` (returns nil when no channel is attached). The
  native `Backend.Invoke` reads it, and when non-nil provisions the progress socket for that run.
  - *Rejected — a method on the `Backend` interface:* the sink is per-invocation, not
    per-construction; threading it through `Invoke`'s existing `ctx` avoids widening the interface
    and matches how the WASM backend already discovers the sink.
- **D4 — schema reuse.** The records are byte-for-byte 0032's
  `{frame,out_time_us,total_size,duration_us}`, parsed by the **same** `engineRecord`/`noteEngine`
  path. Native therefore gains identical `Frame`/`OutTime`/host-derived `Speed`, and the accurate
  `out_time/duration` `Fraction` on drivers built from the `duration_us`-emitting engine.
- **D5 — driver version floor.** Native phase-B records only flow from a driver built **after** this
  lands. Older native drivers stay phase-A-only (the env is simply ignored). afmpeg does not hard-gate
  on it — absence degrades cleanly to byte progress, exactly like a pre-v9 WASM engine.

## 4. Open questions

- **Q1 (D1):** dedicated socket vs. reusing `io.sock` with a distinguished sentinel name (e.g. the
  host special-cases an Open of `/dev/afmpeg-progress` and turns that one connection into the
  progress stream). The sentinel route reuses the existing dial/serve loop and needs **no new env
  var** — but it does require `progress.c`'s native branch to route through AVIO rather than raw
  `open()`. Decide before implementing.
- **Q2 (D3):** exact exported name/location of the sink accessor (`ProgressSinkFromContext` vs.
  folding it into a small `backendContext` helper). Cosmetic; settle at review.
- **Q3:** does native `Speed` want the driver's own wall-clock (it has a real monotonic clock, unlike
  the clockless WASI engine) instead of the host-derived `OutTime/Elapsed`? Cheap to add; decide
  whether it's worth diverging the two backends' `Speed` derivation.

## 5. Resolved

- **2026-07-16 — deferred, not scheduled (the whole spec).** Reviewed the value/cost with the user.
  The native backend **already** delivers phase-A byte progress (`Fraction`/`InputBytes`/
  `OutputBytes`) entirely host-side, no engine change — that is the progress native was designed to
  have, and it ships today. Phase B adds only `Frame`/`OutTime`/`Speed` and an accurate `Fraction`
  for generative/lavfi inputs. Because native runs 48–58× faster than WASM, fine-grained live
  progress matters far less, and the sole real gain (a real `Fraction` on **long generative** native
  jobs) is narrow. Weighed against an engine change to `progress.c`, a native-driver rebuild, and a
  **new signed ffmpeg-wasi release**, the cost is disproportionate. Kept as the decision record so
  the option isn't re-litigated from scratch. D1/Q1 (transport) left open — decide only if revived.

## 6. Revival trigger

Pull this only if a **real consumer need** appears — specifically a long-running **generative**
(`lavfi`, no input file) job on the **native** backend where the phase-A byte `Fraction` is `-1` and
the user needs a determinate bar or a `Frame`/`OutTime`/`Speed` readout. If that surfaces: resolve
D1/Q1 (transport), move to `approved`, then implement (engine + host + a new native-driver release).
Absent that trigger, phase-A byte progress remains the native contract (documented in
[watch-job-progress](../../how-to/watch-job-progress.md) §Limits).
