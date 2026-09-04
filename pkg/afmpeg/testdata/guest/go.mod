// Standalone module for the WASI test guest. Kept separate from the afmpeg
// module so it carries no dependencies and is compiled (GOOS=wasip1) on demand
// by the runtime tests — it stands in for ffmpeg.wasm to exercise Run/Probe and
// the vfs mount through a real WASI host.
module afmpegtestguest

go 1.27.1
