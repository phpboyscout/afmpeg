# 0028 — a native subprocess backend (dual-backend strategy)

Status: **PROPOSED** (from the external review — a *significant, strategic* decision, not a routine
feature. Recorded so we can decide deliberately; it partially contradicts afmpeg's founding
security thesis, so it needs an explicit yes/no.)
Date: 2026-07-02
Parent: [0001](0001-afmpeg.md) (architecture); [0007](0007-libav-direct-engine.md)
Source: external review — `afmpeg/ARCHITECTURE_REVIEW.md` §6–§12
Revives: **R-AF-11** (native backend), dropped in [0006](0006-hardening-roadmap.md) §2C — but from
a new angle that sidesteps the original objection (see §3).
Owns: **R-NATIVE-BACKEND**

## 1. The proposal

Offer **two interchangeable backends** behind the unchanged `RunJob`/`Probe`/`Command` API,
selected at `New()`:

- **WASM (default, unchanged):** wazero + the signed `ffmpeg-wasi` module over the afero VFS —
  sandboxed, portable, CGO-free.
- **Native (opt-in):** a real `ffmpeg`/`ffprobe` **subprocess** (`os/exec`), with afero bridged to
  it by an **ephemeral loopback HTTP server** — inputs served via `http.ServeContent` (HTTP Range
  → the random-access seeking FFmpeg needs for MP4 `moov`), outputs streamed in via an HTTP `PUT`
  handler. `Command` renders to CLI args (`NativeArgs`). Unlocks **hardware acceleration**
  (NVENC/VAAPI/VideoToolbox), multi-threading, and SIMD — the WASM envelope's hard ceilings (§6).

## 2. Validation — is it feasible?

Yes; the design is sound and the review's reasoning holds up:

- **Loopback HTTP bridge works.** afero's `File` is an `io.ReadSeeker`, so `http.ServeContent`
  gives real Range-request seeking — this clears the bar that killed the named-pipe option (no
  backward seek → MP4 fails). Confirmed against the review's §7.5 vs §8 analysis.
- **The afero invariant holds for inputs, bends for one output case.** MP4/MOV muxing seeks
  backward to write `moov`; an HTTP `PUT` stream can't. Mitigations: default to fragmented MP4
  (`-movflags +frag_keyframe+empty_moov`), or a temp host-file then copy into afero. So the
  "everything through afero" invariant is **fully preserved for inputs and fragmented output, and
  softened (a brief host-disk touch) only for non-fragmented MP4 output**.
- **MIT stays clean.** FFmpeg is a *subprocess*, never linked/compiled/embedded — the Go binary
  contains no GPL/LGPL code. The runtime-acquired-artifact model (as with the `.wasm`) extends
  cleanly to a static `ffmpeg` binary.

## 3. Why this is not the R-AF-11 we dropped

[0006](0006-hardening-roadmap.md) §2C dropped the native backend because it assumed **CGO**
(linking `libav*` into the Go binary) — which forfeits CGO-free portability and re-taints the MIT
licence, the exact posture afmpeg exists to avoid. **This proposal uses a subprocess + an HTTP
bridge, not CGO** — no linking, no C toolchain at `go build`, cross-compilation intact, MIT intact.
So the specific objection that killed R-AF-11 does not apply here. That is what makes it worth
re-opening.

## 4. Design sketch

```
Runtime → backend interface ──┬── backend_wasm  (today's runtime.go, extracted)
                              └── backend_native (os/exec + ephemeral HTTP bridge)
```

- **Binary source (mirrors 0010's fetch/verify):** `WithNativeBinary(path)` · `WithNativeFromPATH()`
  · `WithNativeRelease(ver)` (download a static build, SHA-256- + ideally OpenPGP-verified via the
  0010/0011 pipeline).
- **Bridge:** `net/http` on `127.0.0.1:0` (OS-ephemeral port), one server per invocation,
  `Shutdown()` on exit; a per-invocation **bearer token** required on every request
  (`-headers "Authorization: …"`) so a co-located process can't read inputs / poison outputs.
- **Translation:** `Command.NativeArgs(port, token)` → `-i http://…/in.mp4 -filter_complex … -map …
  -c:v … -c:a … http://…/out.mp4`, plus `-y -nostdin -loglevel warning`. `Probe` → `ffprobe
  -print_format json` parsed into the existing `Probe`/`ProbeStream`.
- **Lifecycle:** `exec.CommandContext(ctx, …)` — ctx cancellation kills the subprocess; the server
  dies with it.

## 5. The trade-offs — this is the crux of the decision

| Dimension | WASM (default) | Native (proposed) |
|---|---|---|
| **Untrusted-media containment** | full sandbox — RCE trapped in guest memory | **none** — subprocess runs with the host's OS permissions |
| Filesystem reach | only afero (WASI) | afero via HTTP, **but the subprocess can also touch host disk / make outbound connections** |
| Performance | single-thread, no SIMD | multi-thread + SIMD + HW accel (10–50× on video) |
| Portability | `go build`, zero deps | `go build`, but needs a runtime `ffmpeg` binary |
| Licensing | MIT-clean | MIT-clean (subprocess, not linked) |
| Maintenance | one backend | **two** backends + HTTP bridge + CLI translation + ffprobe parsing |

The tension is real and central: **afmpeg's defining value proposition is "safely process
untrusted media in a sandbox."** A native subprocess is, in the review's own words, "an objective
security regression" — it has full OS access. Adding it means afmpeg is no longer *only* the safe
option; it becomes "safe by default, with a fast unsafe escape hatch." That is a **positioning
decision**, not just an engineering one.

## 6. Decisions & open questions (for the discussion)

- **D-0028-A — include it at all?** The core question. Pro: unlocks HW-accel/perf that WASM
  structurally cannot, with the CGO objection removed (§3). Con: introduces a non-sandboxed path
  into a project whose brand *is* the sandbox. *(Recommendation in §8.)*
- **D-0028-B — if yes, where does it live?** To protect the core's purity, strongly prefer a
  **separate opt-in module** (e.g. `afmpeg-native`, or a build-tagged subpackage) so the default
  `afmpeg` import stays sandboxed, dependency-free, and unable to spawn a subprocess unless the
  consumer *explicitly* opts in. The core `Backend` interface lives in `afmpeg`; the native impl
  ships separately.
- **D-0028-C — default is always WASM.** Non-negotiable if included: the safe backend is the
  default; native is never implicit.
- **D-0028-D — verify the native binary.** `WithNativeRelease` must reuse the 0010/0011
  signature-verification pipeline; document `WithNativeBinary`/`FromPATH` as "you own the trust."
- **Open:** non-fragmented MP4 policy (temp-file touch vs fragmented-only?); the loopback-token +
  ephemeral-port hardening (§10 of the review); whether HW-accel device access (`/dev/dri`,
  `/dev/nvidia*`) is even reachable in the consumer's deploy target (out of afmpeg's control).

## 7. Requirements

- **R-NATIVE-BACKEND** — an opt-in native subprocess backend, API-identical to the WASM backend,
  bridging afero over an authenticated ephemeral loopback HTTP server, CGO-free and MIT-clean, with
  the WASM backend remaining the default. The security regression is documented prominently.

## 8. Recommendation (for the discussion, not a decision)

The engineering is genuinely clever — the HTTP bridge defeats the objection that dropped R-AF-11,
and the API-transparency is elegant. But it sits in direct tension with the security posture that
*is* the product. My recommendation, if we pursue it: **build it as a separate opt-in module, keep
WASM the unconditional default, signature-verify the binary, and document the sandbox loss loudly**
— so afmpeg-the-core stays the pure sandboxed offering and native is a clearly-labelled,
separately-imported escape hatch for consumers who knowingly trade safety for HW-accel throughput.
If we are unwilling to own a non-sandboxed path *at all*, the honest answer is to decline it and
point performance-bound consumers at running native FFmpeg themselves — afmpeg need not be the
tool for every job.

## 9. Definition of done (this proposal)

The proposal is validated for feasibility (§2), distinguished from the dropped R-AF-11 (§3),
sketched (§4), and its central security/positioning trade-off laid out honestly (§5) with a
concrete containment recommendation (§6/§8) — enough to make an informed **include / decline**
decision. No implementation is implied by this DRAFT.
