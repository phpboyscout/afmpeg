# Changelog

## [v0.12.0](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.12.0)

[Compare to previous version](https://gitlab.com/phpboyscout/afmpeg/-/compare/v0.11.2...v0.12.0)

### Features

- **keys**: rotate trust anchors to the v2 dual-trust set ([607d747](https://gitlab.com/phpboyscout/afmpeg/-/commit/607d747137034eca0174d70457f2cba3fba7e297))

## [v0.11.2](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.11.2)

[Compare to previous version](https://gitlab.com/phpboyscout/afmpeg/-/compare/v0.11.1...v0.11.2)

### Bug Fixes

- **deps**: update module gitlab.com/phpboyscout/go/signing to v0.2.2 ([68b60c3](https://gitlab.com/phpboyscout/afmpeg/-/commit/68b60c3f04e0e8be2e51d13ea01f78bbd113ef4a))

## [v0.11.1](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.11.1)

[Compare to previous version](https://gitlab.com/phpboyscout/afmpeg/-/compare/v0.11.0...v0.11.1)

### Bug Fixes

- guard workflow dedup rule so release tag pipelines fire ([1da6211](https://gitlab.com/phpboyscout/afmpeg/-/commit/1da62117bb849dcb17a655469edb1deed05f36ac))

## [v0.11.0](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.11.0)

### Features

- **progress**: surface engine progress over /dev/afmpeg-progress (0032 phase B)

### Bug Fixes

- **deps**: update gomod-weekly

## [v0.10.0](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.10.0)

### Features

- **afmpeg**: live job progress via WithProgress (spec 0031 phase A)

## [v0.9.0](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.9.0)

### Features

- ProcessResult.Analysis — structured analysis-filter output

## [v0.8.0](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.8.0)

### Features

- ProfileFull — native.NewFromRelease selects the full (HEVC/AV1) driver
- native.NewFromRelease honours WithReleaseProfile (intermediate driver)
- native.NewFromRelease — certified native driver acquisition (0028)
- native backend IPC host (pkg/afmpeg/native, 0028 Unit 3)
- export the Backend seam + WithBackend option (0028 Unit 2)

## [v0.7.0](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.7.0)

### Features

- certified acquisition of the intermediate profile

### Bug Fixes

- list all five module options in the ErrNoModule message

## [v0.6.0](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.6.0)

### Features

- **0019**: subtitle streams — Output.SubtitleCodec + round-trips
- **0019**: text/subtitle burn-in tests + meson toolchain spec (0029)
- **0020**: metadata & chapters — Probe/Output model + round-trip
- **0021**: frames op — typed FrameJob emitter + Runtime.Frames

### Bug Fixes

- review hardening — context-aware Run, safe Close, ProbeInput, validation

## [v0.5.0](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.5.0)

### Features

- **0018**: LGPL external encoders — round-trip tests + EH runtime feature
- **afmpeg**: container muxer selection & format options (spec 0015)
- **afmpeg**: input options, forced formats & indexed selection (spec 0024)
- **afmpeg**: seeking & time ranges vocabulary (spec 0014)
- **afmpeg**: stream copy, bitstream filters & concat vocabulary (spec 0013)
- **afmpeg**: gate the job-spec vocabulary version with a New() preflight
- **afmpeg**: cap guest memory and bound invocation time (spec 0027)
- **afmpeg**: WKD cross-check on certified release verification (spec 0011)
- **afmpeg**: verify releases via gitlab.com/phpboyscout/signing (OpenPGP)
- **afmpeg**: WithModuleRelease — signed, verified release acquisition (0010)

### Bug Fixes

- **vfs**: serve /dev/urandom so entropy-seeded muxers don't hang

## [v0.4.0](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.4.0)

### Features

- **afmpeg**: job-spec only — fix Probe transport, remove the CLI path

## [v0.3.0](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.3.0)

### Features

- **afmpeg**: Command.JobSpec() — generic ffmpeg-wasi job-spec emitter
- **afmpeg**: expose Result.Stdout + validate the ffmpeg-wasi seam
- **afmpeg**: WithModuleURL — download + cache the wasm module

### Bug Fixes

- **afmpeg**: probe via `ffmpeg -i` stderr, not ffprobe args

## [v0.2.0](https://gitlab.com/phpboyscout/afmpeg/-/releases/v0.2.0)

### Features

- **afmpeg**: load real ffmpeg.wasm — env setjmp/longjmp + core features
- **afmpeg**: general ffmpeg command builder (spec 0005)
- **afmpeg**: runtime New/Run/Probe over the vfs bridge (spec 0004)
- **vfs**: afero ↔ wazero sys.FS bridge (spec 0003)

### Bug Fixes

- **ci**: add the pinned docs toolchain lock file
