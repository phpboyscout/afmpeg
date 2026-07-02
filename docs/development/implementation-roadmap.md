# Implementation roadmap — build order & prerequisites

**Entry point for picking up implementation.** Every spec (0013–0028) is currently **design-only —
nothing is built**. This document sequences them into phases with dependencies, and lists what's
needed before/during each. It supersedes 0012 §7's parity-only ordering by folding in the
review-driven specs (0024–0028) and the 0022 bundling policy.

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

### Phase 1 — Foundational engine capabilities (Tier-1, highest leverage)
Engine track. These reshape what the engine *is* and are prerequisites for later specs. Do as **one
job-spec-vocabulary-versioned batch** (establish the version-gating mechanism here — see prereqs).
- **[0013](specs/0013-remux-and-stream-copy.md) remux & stream copy** (`copy` sentinel, bitstream
  filters, concat demuxer) — foundational for 0019 (subtitle copy) & 0020 (cover art).
- **[0014](specs/0014-seeking-and-time-ranges.md) seeking & time ranges** (`-ss/-t/-to`) —
  foundational for 0021; pairs with 0013's keyframe-accurate copy-trim.
- **[0024](specs/0024-input-options-and-formats.md) input options & formats** — activate the inert
  `inputs[].options`, forced/raw input format, input stream selection. Cheap; symmetric to 0015.

### Phase 2 — Bundling foundation + cheap breadth
Codec track. Establish the profile machinery, then flood the intermediate profile with native flags.
- **[0022](specs/0022-build-size-matrix.md) profile machinery (WASM side)** — add the `PROFILE`
  build-arg: `lean` = today's build; `intermediate` = a new target (starts == lean). Small
  mechanism; the batches below fill it.
- **[0015](specs/0015-container-coverage.md) / [0016](specs/0016-native-codec-batch.md) /
  [0017](specs/0017-native-filter-batch.md)** — native `--enable-*` batches (containers, decoders/
  native-encoders, filters) → the **intermediate** allowlist (lean stays minimal). No new libs,
  parallelizable, big coverage-per-effort.

### Phase 3 — Codec reach + build-on capabilities
- **[0018](specs/0018-lgpl-encoder-expansion.md) LGPL encoders** (Opus/MP3/VP8-9/WebP/Vorbis) —
  external-lib cross-compiles → intermediate; the default-variant codec win.
- **[0020](specs/0020-metadata-and-chapters.md) metadata & chapters** (needs 0013).
- **[0021](specs/0021-frame-extraction-op.md) frames op** (needs 0014 + 0017's select/thumbnail).

### Phase 4 — Cross-cutting + measured perf
- **[0019](specs/0019-text-and-subtitles.md) text & subtitles** — freetype + libass libs **and** a
  new `AVMEDIA_TYPE_SUBTITLE` lane in `process.c`. The biggest cross-cutting feature.
- **[0026](specs/0026-engine-hot-path-performance.md) engine hot-path perf** — measure-first; pairs
  with 0008's measurement rig. Only land fixes with a measured win.

### Phase 5 — Strategic / gated (separate future efforts)
- **[0028](specs/0028-native-subprocess-backend.md) native backend (Backend B)** — spike-validated,
  but **gated on a real HW-accel consumer need**. Depends on 0022's native matrix + the native
  cross-build toolchains. Large surface (native driver build + IPC bridge + selection API).
- **[0022](specs/0022-build-size-matrix.md) native matrix** — the per-platform native builds; lands
  *with* 0028.
- **[0023](specs/0023-hevc-and-av1.md) HEVC/AV1** — mostly full-native; depends on 0028 + 0008.
- **[0008](specs/0008-performance-strategy.md) perf spike** — gated on a consumer perf target.
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
- **0028 implementation prerequisite** — the [spike](spikes/0028-custom-avio-bridge/) proved the
  I/O model; implementation needs the real `driver.c` native build + the IPC framing protocol + the
  Go-side afero bridge + the `(profile, licence, platform)` selection API.
- **Signing scales for free** — one `checksums.txt` + one OpenPGP signature per release covers all
  16 artifacts (0010/0011 unchanged); no per-artifact work.
- **Build/tooling reality** — ffmpeg-wasi builds via Docker (`deps.sh`→`libav.sh`→`driver.sh`,
  heavy but layer-cached: engine edits re-run only the fast driver link). `glab` moves under mise —
  locate with `find ~/.local/share/mise/installs/glab -name glab -type f | head -1`. A Go file
  living beside a `.c` in the module tree needs `//go:build ignore`.

## Start-here summary

`0027` (memory limit) → the `0013/0014/0024` engine batch → `0022` profile machinery + the
`0015/0016/0017` native batches → `0018` + `0020/0021` → `0019` + `0026`. Everything in Phase 5 is
gated on an external trigger (a HW-accel consumer, a perf target) and can wait.
