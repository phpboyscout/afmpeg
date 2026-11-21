package afmpeg

// Sandbox is the filesystem confinement an engine invocation ran under (spec
// 0047). Only [SandboxWASI] and [SandboxLandlock] keep it off host disk.
type Sandbox string

const (
	// SandboxWASI is the WebAssembly backend, confined by the WASI runtime.
	SandboxWASI Sandbox = "wasi"
	// SandboxLandlock is the native driver with its Landlock sandbox installed.
	SandboxLandlock Sandbox = "landlock"
	// SandboxUnavailable is the native driver on a host that cannot sandbox it,
	// so it ran unconfined (spec 0043 OQ1).
	SandboxUnavailable Sandbox = "unavailable"
	// SandboxDisabled is the native driver with its sandbox switched off on
	// purpose, by AFMPEG_NO_SANDBOX=1.
	SandboxDisabled Sandbox = "disabled"
)

// Confined reports whether s keeps an invocation off host disk. An empty or
// unrecognised state is not confined.
func (s Sandbox) Confined() bool {
	return s == SandboxWASI || s == SandboxLandlock
}
