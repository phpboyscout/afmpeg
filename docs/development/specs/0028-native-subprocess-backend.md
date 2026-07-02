# 0028 — native backends (the hardware-acceleration escape hatch)

Status: **PROPOSED** (a strategic architecture spec — records a deliberate direction, not committed
implementation. The primary path (Backend B) is a firm MUST for the design; the third path
(Backend C) is an explicit MAY, deferred.)
Date: 2026-07-02
Parent: [0001](0001-afmpeg.md) (architecture); [0007](0007-libav-direct-engine.md) (the engine we extend)
Source: external review `docs/development/external-review/ARCHITECTURE_REVIEW.md` §6–§12, refined by
the **2026-07-02 bridge spike** (§5) and the decision to prioritise our own native interface.
Revives: **R-AF-11** (native backend), dropped in [0006](0006-hardening-roadmap.md) §2C — but
CGO-free, so the objection that killed it does not apply (§1).
Owns: **R-NATIVE-ENGINE** (MUST — the custom native driver), **R-NATIVE-LOCAL** (MAY — the
local-install HTTP path, deferred).

## 1. Why — and why now it's tractable

Hardware acceleration is structurally impossible inside WASM: there is no WASI GPU standard, wazero
is pure-Go and single-threaded by design, and GPU encode APIs (NVENC/VAAPI/VideoToolbox) require
direct kernel device access (`/dev/nvidia*`, `/dev/dri`) that the sandbox exists to forbid. HW
accel therefore needs *some* out-of-sandbox path. **CGO** (linking `libav*` into the Go binary) is
the obvious one and it detonates every constraint — it taints afmpeg's MIT licence and destroys
CGO-free portability (this is exactly why R-AF-11 was dropped, [0006](0006-hardening-roadmap.md)
§2C). A **subprocess** is the only escape that keeps FFmpeg a *separate artifact*, so afmpeg's Go
stays MIT and CGO-free. This spec adopts that escape — with the WASM backend remaining the safe,
sandboxed **default**.

## 2. The strategy — three backends, one API

All three sit behind the unchanged `RunJob`/`Probe`/`Command` API, selected at `New()`:

```
                         ┌───────────────── afmpeg.Runtime (unchanged API) ─────────────────┐
                         │                                                                   │
   ┌─────────────────────┴──────────┐  ┌──────────────────────────────┐  ┌──────────────────┴───────────────┐
   │ A · WASM  (DEFAULT)            │  │ B · NATIVE CUSTOM DRIVER (MUST)│  │ C · NATIVE LOCAL INSTALL (MAY)     │
   │ wazero + ffmpeg-wasi.wasm     │  │ OUR driver.c, native build     │  │ stock host ffmpeg via HTTP bridge  │
   │ sandboxed · afero via WASI    │  │ same job-spec · custom AVIO/IPC│  │ CLI translation · heavily guarded  │
   │ single-thread · no HW         │  │ threads+SIMD+HW · signed by us │  │ DEFERRED — much later, if ever     │
   └───────────────────────────────┘  └────────────────────────────────┘  └────────────────────────────────────┘
```

The consumer's code never changes; the backend is opaque after `New()`. **A is always the default.**

## 3. Backend B — the native custom driver (MUST — the priority of this spec)

The primary native path is the **inside-out** one: extend the ffmpeg-wasi model to a native target,
rather than driving someone else's binary. It MUST:

- **Reuse our engine, one codebase.** `src/driver.c` + `src/process.c` compile to a **native**
  target (not `wasm32-wasi`); the wasi shims and the setjmp/longjmp lowering simply fall away where
  real libc/`setjmp` exist. One engine, two build targets — every feature/fix lands once.
- **Keep the same interface.** afmpeg drives it with the **JSON job-spec**, byte-for-byte the same
  contract as the WASM backend — **no CLI-translation layer** (the fragile, partial surface Backend
  C needs). The job-spec is already the versioned afmpeg↔engine contract (0007 §4).
- **Bridge I/O with a custom, *seekable* AVIO over IPC.** Instead of `avio_open(path)`, the native
  driver installs an `avio_alloc_context(buf, …, read_cb, write_cb, seek_cb)` whose callbacks speak
  a small framed protocol over a **Unix-domain socket** (a pipe pair, or loopback TCP as a
  portability fallback) to the Go host, which services them against `afero.Fs`. Because there is a
  real `seek_cb`, the sink is **seekable** — so non-fragmented MP4 output works (the one case the
  HTTP bridge cannot do, §5). This recovers CGO's single genuine advantage (direct AVIO↔afero) —
  **without CGO**, across the subprocess boundary. All I/O stays in-memory in afero; nothing touches
  host disk.
- **Deliver hardware acceleration.** The native `libav*` is built with threads + SIMD +
  `--enable-nvenc/vaapi/videotoolbox`; the job-spec already carries `video_codec`, so
  `video_codec:"h264_nvenc"` opens the GPU encoder. HW encoders needing `hwupload` + a device
  context are modest, well-scoped additions to `process.c`.
- **Be our signed artifact.** Native `driver` binaries (target × arch × licence-variant) are built,
  provenance-stamped, and **OpenPGP-signed via the [0010](0010-signed-release-acquisition.md)/[0011](0011-wkd-attestation.md)
  pipeline**, and afmpeg fetches + verifies them (`WithNativeRelease`) exactly as it does the
  `.wasm`. This is a supply-chain trust *upgrade* over Backend C's "trust whatever is on `$PATH`."

## 4. Backend C — native local install via HTTP (MAY — deferred, heavily guarded)

A third path for consumers who **already have** a native ffmpeg and do not want our artifact: drive
the host's stock `ffmpeg`/`ffprobe` (`WithNativeBinary(path)` / `WithNativeFromPATH()`) over an
ephemeral loopback **HTTP bridge** (afero served with Range requests; output via `PUT`), with
`Command` rendered to CLI args. This is the external review's original proposal (§8) — retained as
an *option*, not a priority:

- **Priority: MAY / COULD, deferred to a much later date.** It exists so the design acknowledges the
  "I have my own ffmpeg" case; it is **not** built alongside B.
- **Heavily guarded when it does land:** opt-in only; a per-invocation cryptographic **bearer token**
  required on every request; an OS-ephemeral port bound to `127.0.0.1`, created and `Shutdown()` per
  invocation; the security regression (arbitrary host binary, its full CLI surface, no artifact
  trust) documented prominently. `WithNativeBinary`/`FromPATH` trust is explicitly the consumer's.
- **Known limitation (spike-confirmed, §5):** non-fragmented MP4 output cannot stream over HTTP
  `PUT` — it would require fragmented MP4 or a host-disk temp file. Backend B does not have this
  limitation.

## 5. Spike evidence (2026-07-02)

A real afero↔loopback-HTTP bridge (token-auth, Range-aware) driving native ffmpeg 6.1.1, all I/O in
afero (no host-disk touch):

| Path tested | Result |
|---|---|
| Input read from afero over HTTP **Range** (moov-at-end MP4) | ✅ 21 Range requests — **seeking works** |
| `ffprobe` over the bridge | ✅ format detected |
| Output → fragmented MP4 / MKV / WebM / MPEG-TS (`PUT`) | ✅ all write to afero |
| Output → **non-fragmented MP4** / faststart (`PUT`) | ❌ *muxer needs a backward seek a PUT sink can't do* |

**Reading:** the subprocess + afero-bridge concept is sound (Backend C is *feasible*), but the
non-frag-MP4 failure is precisely why **Backend B's custom *seekable* AVIO is the better design** —
it removes the one limitation the HTTP path can't. The remaining unknown (§10) is a native
`driver.c` build + a custom-AVIO round-trip; the I/O-bridge half is now de-risked.

## 6. Licensing — identical to the ffmpeg-wasi model

The native `driver` binary links `libav*`, so **the binary** carries LGPL (default) / GPL (full) —
but it is a **separate, runtime-acquired subprocess artifact**, never linked/compiled/embedded into
afmpeg's Go. afmpeg stays **MIT**; `driver.c` orchestration stays MIT. This is the exact posture the
`.wasm` already establishes (0007 §5) — the native binary is just another signed artifact in the
same release, on more targets.

## 7. Security posture

Any native path forfeits the WASM sandbox — that is inherent to reaching hardware, not a flaw in the
design. Mitigations, in priority order: **(1)** WASM is always the default (§2); **(2)** Backend B
runs *our own signed, provenance-stamped* binary — a supply-chain trust *upgrade* over stock ffmpeg;
**(3)** Backend C (arbitrary host binary, the weaker trust) is MAY + deferred + heavily guarded;
**(4)** the sandbox loss is documented loudly wherever a native backend is selected; **(5)** strongly
consider shipping the native backends as a **separate opt-in module** (D-0028-E) so the core `afmpeg`
import stays sandboxed, pure, and unable to spawn a subprocess unless the consumer explicitly opts
in.

## 8. Decisions

- **D-0028-A — pursue a native backend for HW-accel. RESOLVED: yes** (this spec) — WASM structurally
  can't, CGO is out, a subprocess is the only MIT-clean escape.
- **D-0028-B — bifurcate; the custom driver is the MUST.** Backend **B** (our native `driver.c` +
  custom seekable AVIO/IPC + job-spec + signed artifact) is the required realization. Backend **C**
  (local-install HTTP) is a **MAY/COULD, deferred** to a much later date.
- **D-0028-C — WASM is always the default.** Non-negotiable; a native backend is never implicit.
- **D-0028-D — native artifacts are signed** via the 0010/0011 pipeline, verified on fetch.
- **D-0028-E — packaging (open, recommend *separate module*).** Ship the native backend(s) as an
  opt-in module / build-tagged package so core `afmpeg` stays sandboxed-pure and dependency-free.
- **D-0028-F — IPC transport (open).** Unix-domain socket as primary (seekable, in-memory, fast);
  loopback TCP or a pipe pair as the portability fallback (Windows AF_UNIX caveats). The framed
  protocol (`read/write/seek/size` ↔ `data/result`) is ours and versioned like the job-spec.
- **Open:** the native build/release matrix size (targets × arch × variant, on top of wasm) — its
  cost feeds [0022](0022-build-size-matrix.md); HW-encoder device availability is the consumer's
  deploy concern, out of afmpeg's control.

## 9. Requirements

- **R-NATIVE-ENGINE (MUST)** — an opt-in native backend built from *our* `driver.c` engine compiled
  native, driven by the *same* JSON job-spec, bridging `afero.Fs` over a *custom seekable* AVIO/IPC
  channel (in-memory, host-disk-free), delivering threads/SIMD/HW-accel, distributed as an
  OpenPGP-signed artifact, CGO-free and MIT-clean, with the WASM backend the default.
- **R-NATIVE-LOCAL (MAY — deferred)** — an opt-in path to a consumer-supplied/stock host ffmpeg over
  a guarded ephemeral HTTP bridge with CLI translation, for consumers who decline our artifact.
  Explicitly not scheduled alongside R-NATIVE-ENGINE.

## 10. De-risking spike & test surface

**Before any implementation commits,** one focused spike closes the remaining unknown: **build
`driver.c` for a native target and prove a minimal custom-AVIO round-trip** — a seekable read *and*
write of a **non-fragmented MP4** driven entirely through `afero.Fs` over a Unix socket, confirming
the `seek_cb` removes the HTTP path's limitation. Then: unit tests for the IPC framing; an
integration test running the native driver over the bridge (gated, like `AFMPEG_TEST_FFMPEG_WASI`);
and an HW-accel smoke test gated on device availability in CI.

## 11. Sequencing & dependencies

Depends on the ffmpeg-wasi engine (0007) and the build/release/signing pipeline (0010/0011); the
native build matrix interacts with [0022](0022-build-size-matrix.md). This is a large new surface
(a native toolchain + the IPC bridge + the packaging module) and is gated on a **real HW-accel /
throughput consumer need** — no speculative build. Backend C is deferred independently and further.

## 12. Definition of done (this proposal)

The direction is decided and bifurcated: Backend B (custom native driver + seekable AVIO/IPC) is the
MUST; Backend C (local HTTP) is a deferred MAY. The HW-accel premise, the CGO-free/MIT posture, the
security trade-off, the licensing model, and the spike evidence are recorded — enough to green-light
the §10 de-risking spike as the next concrete step, with implementation still uncommitted.
