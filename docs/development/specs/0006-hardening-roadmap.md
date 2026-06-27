# 0006 — hardening roadmap (deferred)

Status: **DRAFT / ROADMAP** (a deliberately-deferred backlog, not scheduled work. Items
graduate to their own full spec when picked up. Kept so the deferral is a recorded decision,
not a forgotten gap.)
Date: 2026-06-26
Parent: [0001-afmpeg.md](0001-afmpeg.md) §5 (MAY), §8 (Phase 4), §9
Owns: **R-AF-10** (LGPL build-out), **R-AF-11** (native backend), **R-AF-12** (threads/SIMD),
**R-AF-13** (CLI)

## 1. Purpose

Phase 4 (0001 §8) collects the "later/MAY" requirements. None gate v1 (the bridge + `Run` +
the keyrx helper, specs 0002–0005). This spec records them so they're tracked and the
*reasons they're deferred* survive. Each item, when picked up, gets promoted to a numbered
spec (0007+).

## 2. Backlog items

### 2A — LGPL/openh264 build-out (R-AF-10)
The LGPL variant is *produced* by 0002's pipeline but not hardened/validated to the R-AF-3
bar. Graduating it means: validate openh264 H.264 quality against keyrx's needs, document the
self-compiled-openh264 patent posture (0001 §9, D-C), and publish it as the recommended
artifact for permissive public consumers. *Trigger:* first external consumer who can't take
the GPL artifact.

### 2B — Performance: wasm-threads / SIMD (R-AF-12)
The pinned no-pthreads FFmpeg encodes single-threaded → materially slower than native (0001
§9). When wazero gains wasm-threads (and/or SIMD matures), build a threaded `ffmpeg.wasm` and
benchmark. *Trigger:* wazero wasm-threads support lands, or in-memory render perf becomes a
real (not edge) pain. This is also the keyrx spike's explicit re-evaluation trigger.

### 2C — Native backend seam (R-AF-11)
A `Backend` interface selecting a native (purego/CGO libav) path when host libs are present,
with the WASM build as the zero-dependency fallback — for speed. Posture-sensitive (CGO would
re-introduce exactly what 0001 avoids), so any CGO backend must be **opt-in and build-tagged**,
never the default. *Trigger:* a consumer needing native speed who accepts the CGO trade.

### 2D — `cmd/afmpeg` CLI (R-AF-13)
A thin drop-in `ffmpeg`-over-a-virtual-fs CLI for demos/tests/manual use. Likely built on the
go-tool-base CLI framework (the `phpboyscout-gtb-cli` method) for consistency with the org's
other tools. *Trigger:* demand for a standalone binary, or to dogfood the API. Low-risk;
could be pulled forward if useful for 0004's integration testing.

### 2E — `RuntimePool` for parallel invocations (from 0004 D-0004-B)
The pooled, N-instance concurrency mode deferred from 0004 (one-`Run`-at-a-time is v1).
*Trigger:* a consumer running multiple ffmpeg invocations concurrently.

### 2F — Download-and-cache module acquisition (from 0004 D-0004-C)
A helper that fetches the (separate, D-C) `ffmpeg.wasm` artifact by version+sha and caches
it, so consumers don't hand-manage the file — without ever `//go:embed`-ing the GPL build.
*Trigger:* friction from manual `WithModuleFile` management.

## 3. Non-goals (still, per 0001 §2)

Hardware acceleration, real-time/streaming/live capture, a full typed libav* object API,
and Windows-as-a-launch-target remain out of scope.

## 4. Sequencing

All items are post-v1 and independent. Each is promoted to its own spec (0007+) when picked
up, citing this roadmap and 0001.
