# Changelog

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
