// Package vfs bridges an afero.Fs to wazero's experimental/sys.FS so a
// WebAssembly guest's WASI filesystem syscalls (path_open, fd_read, fd_write,
// fd_seek, …) read and write the caller's afero filesystem — including a fully
// in-memory MemMapFs — with no host disk access. It is the core of afmpeg
// (spec 0003); the guest only ever sees the afero.Fs it is given.
//
// Two synthetic locations the guest ffmpeg expects are overlaid on top of the
// caller's filesystem: a writable /tmp backed by an isolated in-memory fs (so
// scratch writes never pollute the caller's fs) and /dev/null (a discard sink).
//
// The adapter is invocation-agnostic: it implements the sys.FS and sys.File
// contracts wazero calls and holds no runtime state of its own. The wazero
// module wiring that mounts it (WithSysFSMount) lives in the afmpeg runtime
// (spec 0004), which composes this package with the embedded ffmpeg.wasm.
//
// See docs/explanation/components/vfs-bridge.md for the design narrative.
package vfs
