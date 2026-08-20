# Specs

Specs live in the [project wiki](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/home),
not in this repository.

A spec is a point-in-time decision record — written once, true of a moment, read
later for its conclusions. Keeping them here buried the living documentation they
sat beside, so they moved. Contributor guides, engineering standards and testing
conventions stay in `docs/`, because those change with the code.

| Spec | Title | Status |
|---|---|---|
| [0001](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0001-afmpeg) | afmpeg: pure-Go FFmpeg on a virtual filesystem | `APPROVED` |
| [0002](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0002-wasm-build-pipeline) | the FFmpeg→WASI build pipeline | `REJECTED` |
| [0003](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0003-vfs-bridge) | the afero ↔ wazero vfs bridge | `IMPLEMENTED` |
| [0004](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0004-runtime-and-api) | the runtime & public API (`Run` / `Probe`) | `IMPLEMENTED` |
| [0005](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0005-render-helper-and-keyrx-backend) | the ffmpeg command builder | `IMPLEMENTED` |
| [0006](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0006-hardening-roadmap) | hardening roadmap (deferred) | `IMPLEMENTED` |
| [0007](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0007-libav-direct-engine) | the libav-direct engine + the ffmpeg-wasi project | `IMPLEMENTED` |
| [0008](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0008-performance-strategy) | performance strategy (spike) | `IMPLEMENTED` |
| [0009](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0009-afmpeg-cli) | `cmd/afmpeg` CLI (deferred — value unproven) | `DRAFT` · blocked |
| [0010](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0010-signed-release-acquisition) | signed, release-aware module acquisition | `IMPLEMENTED` |
| [0011](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0011-wkd-attestation) | WKD key distribution + the embedded↔WKD cross-check | `IMPLEMENTED` |
| [0012](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0012-feature-parity-roadmap) | feature-parity roadmap (toward a complete standalone offering) | `APPROVED` |
| [0013](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0013-remux-and-stream-copy) | remux & stream copy | `IMPLEMENTED` |
| [0014](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0014-seeking-and-time-ranges) | seeking & time ranges | `IMPLEMENTED` |
| [0015](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0015-container-coverage) | container coverage | `IMPLEMENTED` |
| [0016](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0016-native-codec-batch) | native codec batch | `IMPLEMENTED` |
| [0017](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0017-native-filter-batch) | native filter batch | `IMPLEMENTED` |
| [0018](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0018-lgpl-encoder-expansion) | LGPL encoder expansion | `IMPLEMENTED` |
| [0019](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0019-text-and-subtitles) | text & subtitles | `IMPLEMENTED` |
| [0020](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0020-metadata-and-chapters) | metadata & chapters | `IMPLEMENTED` |
| [0021](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0021-frame-extraction-op) | frame extraction op | `IMPLEMENTED` |
| [0022](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0022-build-size-matrix) | the build & distribution matrix | `IMPLEMENTED` |
| [0023](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0023-hevc-and-av1) | HEVC & AV1 (heavy codecs) | `IMPLEMENTED` |
| [0024](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0024-input-options-and-formats) | input demuxer options, forced/raw formats & stream selection | `IMPLEMENTED` |
| [0025](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0025-av-sync-and-framerate) | A/V sync & frame-rate control | `IN REVIEW` |
| [0026](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0026-engine-hot-path-performance) | engine hot-path performance | `IMPLEMENTED` |
| [0027](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0027-runtime-security-hardening) | runtime security hardening | `IMPLEMENTED` |
| [0028](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0028-native-subprocess-backend) | native backends (the hardware-acceleration escape hatch) | `IMPLEMENTED` |
| [0029](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0029-meson-cross-compile-toolchain) | meson cross-compile toolchain | `IMPLEMENTED` |
| [0030](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0030-wasm-threading-strategy) | WASM threading strategy (wazero: stay single-threaded, fork for threads, or supplement) | `DRAFT` |
| [0031](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0031-job-progress-reporting) | job progress reporting | `IMPLEMENTED` |
| [0032](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0032-engine-progress-side-channel) | engine progress side-channel (0031 phase B) | `IMPLEMENTED` |
| [0033](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0033-native-progress-side-channel) | native progress side-channel (0031 phase B for Backend B) | `DRAFT` |
| [0034](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0034-fraction-source-precedence) | Fraction source precedence (progress correctness) | `IMPLEMENTED` |
| [0035](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0035-engine-builds-on-a-merge-request) | engine builds on a merge request (ffmpeg-wasi CI evidence before the tag) | `IMPLEMENTED` |
| [0036](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0036-engine-test-suite) | engine test suite (the instrument for the n9 bump) | `IMPLEMENTED` |
| [0037](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0037-engine-parity-and-ipc-host) | engine test suite phase D — parity and the IPC host | `IMPLEMENTED` |
| [0038](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0038-engine-on-ffmpeg-n9) | the engine on FFmpeg n9.0.1 | `IMPLEMENTED` |
| [0039](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0039-native-driver-filesystem-boundary) | the native driver's filesystem boundary (routing path-taking filter/codec options over the IPC bridge) | `DRAFT` |
| [0040](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0040-native-driver-sandbox) | the native driver sandbox (opt-in Landlock confinement, 0039's guarantee enforced by the kernel) | `DRAFT` |
| [0041](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0041-ipc-protocol-primitives) | the IPC protocol's missing primitives (read failure, rename, and asking what exists) | `DRAFT` |
| [0042](https://gitlab.com/phpboyscout/afmpeg/-/wikis/specs/0042-fontconfig-and-adaptive-demuxers) | fontconfig, the hls/dash demuxers, and the filesystem surface | `DRAFT` |

## Referring to a spec

By **number and name** — "0020, the explainable-cull spec" — never by date. The
number is a stable handle; a date is not something anyone remembers.

## Writing a new one

Claim the next number first, then draft against the canonical shape. See the
`spec-driven-development` skill in the phpboyscout marketplace.
