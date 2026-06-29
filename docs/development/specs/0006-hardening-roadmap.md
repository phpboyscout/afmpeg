# 0006 — hardening roadmap (deferred)

Status: **DISPATCHED** (2026-06-29 — every backlog item is now done, promoted to its own spec,
or dropped; see the disposition table in §2. Kept as the record of what was deferred and where
it went.)
Date: 2026-06-26 (dispatched 2026-06-29)
Parent: [0001-afmpeg.md](0001-afmpeg.md) §5 (MAY), §8 (Phase 4), §9
Owns: **R-AF-10** (LGPL build-out — **done**). R-AF-12 → [0008](0008-performance-strategy.md);
R-AF-13 → [0009](0009-afmpeg-cli.md); R-AF-11 **dropped**.

## 1. Purpose

Phase 4 (0001 §8) collects the "later/MAY" requirements. None gate v1 (the bridge + `Run` +
the keyrx helper, specs 0002–0005). This spec records them so they're tracked and the
*reasons they're deferred* survive. Each item, when picked up, gets promoted to a numbered
spec (0007+).

## 2. Backlog items

**Disposition (2026-06-29):**

| Item | Was | Now |
|---|---|---|
| 2A — LGPL/openh264 (R-AF-10) | deferred | ✅ **done** (ffmpeg-wasi `n8.1.2-2`) |
| 2B — perf: threads/SIMD (R-AF-12) | deferred-on-trigger | **promoted → [0008](0008-performance-strategy.md)** (spike) |
| 2C — native backend (R-AF-11) | deferred-on-trigger | ❌ **dropped** (see below) |
| 2D — `cmd/afmpeg` CLI (R-AF-13) | deferred-on-trigger | **promoted → [0009](0009-afmpeg-cli.md)** (deferred, value-unproven) |
| 2E — `RuntimePool` | deferred | **folded into [0008](0008-performance-strategy.md)** (the real parallelism lever absent threads) |
| 2F — download-and-cache | deferred-on-trigger | ✅ **done** (`WithModuleURL`); certified variant → **[0010](0010-signed-release-acquisition.md)** |

### 2A — LGPL/openh264 build-out (R-AF-10) — **done** (ffmpeg-wasi `n8.1.2-2`)
openh264 (BSD) now ships in **both** variants' build, giving the default LGPL artifact H.264
encode without `--enable-gpl`. The cross-compile, the single-threaded pthread shim, and the
`ForceIntraFrame` wasm-arity fix live in ffmpeg-wasi `build/`; the self-compiled-openh264 patent
posture (0001 §9, D-C) is documented in ffmpeg-wasi `docs/explanation/licensing.md`. Validated
end-to-end by `TestIntegration_H264Encode_OpenH264` (PNG → yuv420p → H.264/mp4, in memory).
*Remaining:* spot-check openh264 quality/bitrate against keyrx's reels when keyrx adopts it.

### 2B — Performance (R-AF-12) → promoted to [0008](0008-performance-strategy.md)
Originally "wait for wasm-threads, then build a threaded `ffmpeg.wasm`." The 2026 landscape
killed that premise — wazero has no Wasm threads, `wasi-threads` is a *legacy* proposal, the
successor (shared-everything-threads) is unimplemented with no date, and Wasm SIMD is unusable
by our `--disable-asm` FFmpeg. Reframed as a measurement-first **spike** in 0008 (is
single-threaded encode actually a problem, and which *non-threaded* lever — instance-level
parallelism, build tuning — helps).

### 2C — Native backend seam (R-AF-11) → **dropped** (2026-06-29)
A native (purego/CGO libav) backend for speed. **Dropped:** with the libav-direct engine
shipped, native isn't needed, and a CGO backend re-introduces exactly the posture 0001 exists
to avoid. The *only* scenario that would revive it is a **specific capability ffmpeg-wasi
genuinely cannot deliver**, exposed as a separate opt-in backend — and that case is unproven.
Until something concrete fails the Wasm engine, there is no value to chase here. Not a roadmap
item; revisit only if a real gap forces it.

### 2D — `cmd/afmpeg` CLI (R-AF-13) → promoted to [0009](0009-afmpeg-cli.md)
Note: the old "drop-in `ffmpeg`-over-a-virtual-fs" framing is **off the table** — v0.4.0 removed
the ffmpeg-arg path on purpose. 0009 carries the item as a **deferred, value-unproven** goal:
any CLI is job-spec-native, and it must first justify its existence against the cost of a second
public surface.

### 2E — `RuntimePool` for parallel invocations → folded into [0008](0008-performance-strategy.md)
With intra-encode threads unavailable, instance-level fan-out (N module instances across cores)
is the *only* real parallelism lever, so it belongs with the performance investigation rather
than as a standalone deferral. Evaluated there before any build.

### 2F — Download-and-cache module acquisition (R-AF / D-0004-C) — **done**
Shipped as `WithModuleURL` (+ `WithSHA256`, `WithGunzip`, `WithCacheDir`, `WithHTTPClient`): it
fetches the artifact by URL, verifies the checksum, and caches under the OS cache dir, never
`//go:embed`-ing the GPL build. See the [obtain-a-module](../how-to/obtain-a-module.md) how-to.
*Residual → promoted to [0010](0010-signed-release-acquisition.md):* the URL+SHA primitive is
right for bring-your-own builds, but a second **certified** path — `WithModuleRelease(tag,
variant)` with KMS-signed checksum + provenance verification — is now specced in 0010.

## 3. Non-goals (still, per 0001 §2)

Hardware acceleration, real-time/streaming/live capture, a full typed libav* object API,
and Windows-as-a-launch-target remain out of scope.

## 4. Sequencing

All items are post-v1 and independent. Each is promoted to its own spec (0007+) when picked
up, citing this roadmap and 0001.
