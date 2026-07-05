# Changelog

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
