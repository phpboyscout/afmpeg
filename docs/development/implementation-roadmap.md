# Implementation roadmap — build order & prerequisites

**Entry point for picking up implementation.** Phases 0–4 are **shipped** (through **vocab v9** —
job progress side-channel 0031/0032), and Phase 5 (the native backend 0028, the native matrix 0022
on linux/amd64, HEVC/AV1 encode + AV1 decode 0023, the 0008 perf spike) is now **largely shipped**
too. What remains is a menu of **trigger-gated** work, none of it on a critical path — see
[**Pick-up menu**](#pick-up-menu-for-a-future-session) at the foot of this doc for the short list a
future session can grab. This document sequences the specs into phases with dependencies, and lists
what's needed before/during each — see the per-spec status inline below. It supersedes 0012 §7's
parity-only ordering by folding in the review-driven specs (0024–0028) and the 0022 bundling policy.

> **Current anchors (2026-07-13):** afmpeg **v0.11.0**, ffmpeg-wasi **n8.1.2-10**, job-spec
> **vocab v9**. The core roadmap is complete; everything below in "what remains" is optional and
> waits for its trigger (a consumer need, a device, or a measured regression).

## The two tracks

The work splits into two largely-independent tracks that can proceed in parallel (or interleave for
a solo implementer):

- **Engine-capability track** — `ffmpeg-wasi/src/process.c` + `driver.c` + the afmpeg job-spec
  vocabulary. Makes the engine *do* more (stream copy, seek, metadata, frames, input options).
  Independent of which codecs are bundled. Specs: 0013, 0014, 0020, 0021, 0024, 0026, 0027 (afmpeg
  side), 0025.
- **Codec-coverage track** — `ffmpeg-wasi/build/` (`libav.sh` flags, `deps.sh` libs) + the 0022
  profile machinery. Makes the engine *support* more formats/codecs/filters. Specs: 0015, 0016,
  0017, 0018, 0019 (libs), 0022, 0023.

## Phased build order

### Phase 0 — Harden the core (do first; cheap; no deps) — ✅ DONE
- **[0027](specs/0027-runtime-security-hardening.md) runtime security hardening** — the wazero
  memory ceiling above all (a crafted file could OOM-kill the host, undercutting the whole
  "safely process untrusted media" thesis), plus the deadline policy + cJSON guards. Small, high
  value, unblocks nothing but protects everything. **Shipped:** `WithMemoryLimit` (default 512 MB)
  + `WithTimeout` (default 1 h) in `runtime.go` with `-race` acceptance tests; the `cJSON_IsObject`
  guard is committed in `ffmpeg-wasi/src/process.c` for the next engine rebuild.

### Phase 1 — Foundational engine capabilities (Tier-1, highest leverage) — ✅ DONE
Engine track. These reshape what the engine *is* and are prerequisites for later specs. Done as **one
job-spec-vocabulary-versioned batch** (the version-gating mechanism was established here — see
prereqs; it shipped standalone at **v1**, so each spec below bumped the version). **All shipped:
gate v1 → 0013 v2 → 0014 v3 → 0024 v4.**
- **[0013](specs/0013-remux-and-stream-copy.md) remux & stream copy** (`copy` sentinel, bitstream
  filters, concat demuxer) — foundational for 0019 (subtitle copy) & 0020 (cover art). **✅ SHIPPED
  at vocab v2** (verified mp4/mkv/webm; full A/V concat + copy-trim want 0015 ts + 0014 seek). Its
  Matroska output surfaced and fixed a pre-existing WASI entropy hang (`/dev/urandom` now served by
  the vfs).
- **[0014](specs/0014-seeking-and-time-ranges.md) seeking & time ranges** (`-ss/-t/-to`) —
  foundational for 0021; pairs with 0013's keyframe-accurate copy-trim. **✅ SHIPPED at vocab v3**
  (fast/accurate seek, duration/end windows, copy_ts, probe `start_sec`; copy-trim verified).
- **[0024](specs/0024-input-options-and-formats.md) input options & formats** — activate the inert
  `inputs[].options`, forced/raw input format, input stream selection. Cheap; symmetric to 0015.
  **✅ SHIPPED at vocab v4** (demuxer options + typed leftover-key error, `format` forced demuxer,
  raw video/PCM round-trips, `N:v:K` indexed graph inputs; raw demuxers added to the build).

### Phase 2 — Bundling foundation + cheap breadth — ✅ DONE
Codec track. Establish the profile machinery, then flood the intermediate profile with native flags.
**All shipped:** 0022 profile machinery → 0015 containers (v5) → 0017 filters → 0016 codecs.
- **[0022](specs/0022-build-size-matrix.md) profile machinery (WASM side)** — add the `PROFILE`
  build-arg: `lean` = today's build; `intermediate` = a new target (starts == lean). Small
  mechanism; the batches below fill it. **✅ SHIPPED** — `PROFILE=lean|intermediate` in `libav.sh`
  + Dockerfile (lean keeps the legacy artifact name; intermediate is
  `ffmpeg-wasi-intermediate-<variant>.wasm` with an empty `INTERMEDIATE_ENABLE` hook). Native
  matrix + selection API remain downstream (0022 §5/§7, with 0028).
- **[0015](specs/0015-container-coverage.md) / [0016](specs/0016-native-codec-batch.md) /
  [0017](specs/0017-native-filter-batch.md)** — native `--enable-*` batches (containers, decoders/
  native-encoders, filters) → the **intermediate** allowlist (lean stays minimal). No new libs,
  parallelizable, big coverage-per-effort. **0015 ✅ SHIPPED at vocab v5** (mpegts/hls/dash/segment/
  flv/avi/gif + audio containers; `outputs[].format`/`format_options`; HLS segmenting + fMP4 verified;
  switched on 0013's mp4→ts copy + full A/V ts concat). **0017 ✅ SHIPPED** (native filter batch —
  colour/compose/select/palette/deinterlace/loudnorm/…; flag-only, no vocab change; `eq` is GPL-only).
  **0016 ✅ SHIPPED** (native decoders ac3/eac3/dca/prores/dnxhd/dv/mpeg2/4/vc1/wmv3/theora/alac/… +
  encoders ac3/alac/PCM/bmp/tiff; flag-only; decode-only codecs await a media corpus).

### Phase 3 — Codec reach + build-on capabilities — ✅ DONE
- **[0018](specs/0018-lgpl-encoder-expansion.md) LGPL encoders** (Opus/MP3/VP8-9/WebP/Vorbis) —
  external-lib cross-compiles → intermediate; the default-variant codec win. **DONE** — all five
  libs (libopus/libmp3lame/libvorbis/libwebp/libvpx) build + round-trip; libvpx's setjmp rides
  wasm exception-handling (EH feature enabled in the runtime).
- **[0020](specs/0020-metadata-and-chapters.md) metadata & chapters** (needs 0013). **DONE** —
  probe reads container/per-stream tags, chapters, disposition, language; process writes container
  `metadata`, `chapters:"copy"`, per-stream `stream_metadata`; cover art survives the copy path;
  shared `meta.c`; vocab **v7**.
- **[0021](specs/0021-frame-extraction-op.md) frames op** (needs 0014 + 0017's select/thumbnail).
  **DONE** — the third engine op (`frames.c`), four selectors → templated stills; afmpeg `FrameJob`
  + `Runtime.Frames`; vocab **v6**.

### Phase 4 — Cross-cutting + measured perf
- **[0029](specs/0029-meson-cross-compile-toolchain.md) meson cross-compile toolchain** — **DONE**
  (spike → spec → impl): a second cross-compile path in `deps.sh` for meson-only libs
  (harfbuzz/fribidi), unblocking 0019 burn-in and future meson deps (dav1d).
- **[0019](specs/0019-text-and-subtitles.md) text & subtitles** — **DONE** (both mechanisms).
  Burn-in (drawtext + subtitles/ass filters; freetype/harfbuzz/fribidi/libass via 0029) + the
  **subtitle-stream lane** (`AVMEDIA_TYPE_SUBTITLE` in `process.c`; `subtitle_codec` + `N:s` extract/
  convert/copy/embed; native subrip/webvtt/mov_text/ass codecs); vocab **v8**.
- **[0026](specs/0026-engine-hot-path-performance.md) engine hot-path perf** — measure-first; pairs
  with 0008's measurement rig. Only land fixes with a measured win.
- **[0031](specs/0031-job-progress-reporting.md) job progress reporting** — **SHIPPED (both phases)**,
  pulled in by a keyrx progress-indicator need. **Phase A** (afmpeg v0.10.0): host-side
  observed-filesystem progress — `afmpeg.WithProgress` watches bytes read/written at the `afero.Fs`
  boundary, zero engine change. **Phase B** = [0032](specs/0032-engine-progress-side-channel.md).
  Spike: [`spikes/0031-progress-observed-fs`](spikes/0031-progress-observed-fs/).
- **[0032](specs/0032-engine-progress-side-channel.md) engine progress side-channel** — **SHIPPED**
  (0031 phase B; afmpeg v0.11.0 / ffmpeg-wasi n8.1.2-10, vocab **v9**). The engine emits
  `frame`/`out_time_us`/`total_size`(+optional `duration_us`) NDJSON to a `/dev/afmpeg-progress`
  vfs write-device (host-gated by a `progress:true` opt-in); afmpeg merges it onto the same
  `WithProgress` channel — real `Frame`/`OutTime`/host-derived `Speed`, and an accurate `Fraction`
  for file inputs on any v9 engine and for **generative/lavfi** inputs once on **n8.1.2-10+** (the
  `duration_us` follow-up, R-PROGRESS-B2). Older engines fall back to the byte `Fraction`. Speed is
  host-derived (`OutTime/Elapsed`) because the WASI engine is clockless.

### Phase 5 — Strategic / native (largely SHIPPED, 2026-07-05)
- **[0028](specs/0028-native-subprocess-backend.md) native backend (Backend B)** — **SHIPPED**. The
  native `driver.c` ELF + seekable AVIO-over-IPC, wired via `WithBackend` / `native.NewFromRelease`;
  48–58× faster software encode than WASM. WASM stays default; CGO-free.
- **[0022](specs/0022-build-size-matrix.md) native matrix** — **linux/amd64 SHIPPED** (lean/
  intermediate/full × lgpl/gpl, signed). `linux/arm64` + `darwin/arm64` remain (the cross-build cost).
- **[0023](specs/0023-hevc-and-av1.md) HEVC/AV1** — **SHIPPED**: encode via x265 (HEVC, gpl/full) +
  SVT-AV1 (AV1, both/full); **AV1 decode via libdav1d on both runtimes** (WASM + native intermediate,
  single-threaded on wasm). Only HW-accel remains.
- **[0008](specs/0008-performance-strategy.md) perf spike** — **DONE** (`cmd/afmpeg-bench`; the
  encode gap quantified, and answered by the native backend).
- **Still gated:** HW-accel encoders (NVENC/VAAPI/… — the full profile's remaining members, need a
  device); native `linux/arm64` + `darwin/arm64`; concat-over-IPC.
- **[0025](specs/0025-av-sync-and-framerate.md) A/V sync** — deferred; the `fps` filter covers it
  today. Build only on a real VFR complaint.

## Prerequisites & notes (what we'll need)

- **Job-spec vocabulary versioning** — 0013/0014/0015/0019/0020/0021/0024 each bump the versioned
  afmpeg↔engine contract (0007 §4). **Build the version-gating first (in Phase 1 with 0013):**
  afmpeg must fail cleanly on an unknown `op`/field; the version increments **additively, once per
  landed spec**, in merge order.
- **Test media corpus** — many codec/format/filter/subtitle tests need real fixtures beyond the
  synthetic WAV/PNG. Prefer generating with `ffmpeg -f lavfi` at test time; otherwise a small,
  licence-clean corpus. (Flagged in 0012 §6.)
- **External-lib cross-compiles** (Phases 3–5) — each is a `wasm32-wasi`, static, **asm-free** build
  mirroring `deps.sh`'s openh264/x264 pattern: libopus/lame/libvpx/libwebp/libvorbis (0018),
  freetype/libass (0019), x265/dav1d/aom-or-SVT (0023). **libvpx and libass are the tricky build
  systems** — budget for them.
- **Native cross-build toolchains** (Phase 5) — per-platform libav\* + driver for `linux/arm64` and
  `darwin/arm64` (cross-SDKs / per-platform runners). This is the real cost of the native tier, not
  the artifact count.
- **0027 memory-limit default** — decide the value (512 MB–1 GB is the open question in 0027).
- **0028 implementation** — **done** (2026-07-05): the [spike](spikes/0028-custom-avio-bridge/)
  proved the I/O model; the real `driver.c` native build (`TARGET=native`, all three profiles), the
  `O/R/W/S/Z/C` IPC framing, the Go-side afero bridge (`pkg/afmpeg/native`), and the `(profile,
  licence)` selection (`native.NewFromRelease` + `WithReleaseProfile`) all shipped for linux/amd64.
- **Signing scales for free** — one `checksums.txt` + one OpenPGP signature per release covers all
  16 artifacts (0010/0011 unchanged); no per-artifact work.
- **Build/tooling reality** — ffmpeg-wasi builds via Docker (`deps.sh`→`libav.sh`→`driver.sh`,
  heavy but layer-cached: engine edits re-run only the fast driver link). `glab` moves under mise —
  locate with `find ~/.local/share/mise/installs/glab -name glab -type f | head -1`. A Go file
  living beside a `.c` in the module tree needs `//go:build ignore`.

## Start-here summary

~~`0027` (memory limit) → `0013/0014/0024` → `0022` profiles + `0015/0016/0017` native batches →
`0018` + `0020/0021` → `0019` → `0031/0032` progress~~ **← all done (Phases 0–4, through vocab v9).**
And **Phase 5 is largely done too**: the `0028` native backend + `0022` native matrix (linux/amd64),
`0023` HEVC/AV1 encode + AV1 decode (both runtimes), the `0008` perf spike, and the `0017` §Q analysis
output. There is **no forced next step** — the critical path is clear. What remains is the trigger-gated
menu below.

## Pick-up menu for a future session

Nothing here is on a critical path; each item waits for its own trigger. Ordered by how ready it is to
grab (most-ready first). A future session should confirm the trigger still holds before starting.

| Spec | What it adds | Trigger / gate | Rough cost | Ready to pull? |
|------|--------------|----------------|------------|----------------|
| **[0009](specs/0009-afmpeg-cli.md) afmpeg CLI** | A standalone `afmpeg` command-line binary over the library | None — pure Go, no hardware, no consumer needed. The only net-new user-facing surface not otherwise blocked | Medium | **Yes** — spec it → approve → build. Best candidate if the goal is to keep shipping value |
| **[0030](specs/0030-wasm-threading-strategy.md) WASM threading** | Multi-threaded in-browser WASM encode | Wanting *WASM* speed specifically — the native backend already gives 48–58× off-browser, so this only matters for the pure-browser path | Large | Only if a browser-perf need appears; strategy spec exists, impl does not |
| **[0022](specs/0022-build-size-matrix.md) arm64/darwin native** | Native Backend-B on `linux/arm64` + `darwin/arm64` | A consumer needing the native backend off `linux/amd64` (e.g. Apple Silicon). The real cost is the cross-build toolchains/runners, not the artifact count | Large | Only on a platform need |
| **HW-accel encode** | NVENC/VAAPI/QSV/… (the full profile's remaining members) | **Blocked** — needs a GPU/accel device; not available on the headless dev box | Medium once a device exists | No — environment-blocked |
| **[0026](specs/0026-engine-hot-path-performance.md) hot-path micro-opts** | Engine perf tuning | **Measure-first** — pair with the `0008` rig (`cmd/afmpeg-bench`); only land a change with a measured win | Small–Medium | Only with a profile showing a hotspot |
| **[0025](specs/0025-av-sync-and-framerate.md) A/V-sync / framerate** | VFR handling beyond the `fps` filter | **Complaint-first** — the `fps` filter covers today's cases; build only on a real VFR bug | Medium | Only on a complaint |
| **[0033](specs/0033-native-progress-side-channel.md) native phase-B progress** | `Frame`/`OutTime`/`Speed` + accurate `Fraction` for the **native** backend (WASM parity) | **Need-first** — native already has phase-A byte progress; this only helps a *long generative* native job. Needs an engine change + a new native-driver release | Medium | Deferred — spec drafted, revive on a real consumer need |

**How new work lands here:** the last two roadmap pulls (0031 phase A, then 0031/0032 phase B) were
driven by a **keyrx** consumer need, not by this list. If keyrx (or another consumer) surfaces the
next requirement, that sets priority for free — draft a spec, get it to `approved` with the human
(see the `spec-driven-development` skill), then implement. Vocabulary-affecting engine work bumps the
job-spec version additively (next is **v10**) and is cross-repo (ffmpeg-wasi engine change → tag/
release → afmpeg consumes behind the version gate).
