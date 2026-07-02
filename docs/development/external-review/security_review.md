# Security Review: ffmpeg-wasi

This document outlines the security posture, benefits, and potential vulnerabilities of the `ffmpeg-wasi` implementation.

## 1. Strong WASM Sandboxing (Benefit)
The most significant security advantage of this architecture is the WASM sandbox provided by `wazero`.
- **Isolation:** FFmpeg is a massive C codebase that historically suffers from memory safety vulnerabilities (buffer overflows, out-of-bounds reads/writes) in its myriad of demuxers and decoders.
- **Containment:** By compiling FFmpeg to WASM and running it in `wazero`, any Remote Code Execution (RCE) vulnerability triggered by a maliciously crafted media file is completely contained within the WASM guest memory.
- **Filesystem Access:** The WASI setup explicitly restricts the guest to the provided `afero` in-memory filesystem. The guest cannot access the host machine's disk, environment variables, or network.

## 2. C-Side Memory Safety
The `driver.c` and `process.c` files are written in C and handle dynamic memory allocation and JSON parsing (`cJSON`).
- **Missing Validations:** The JSON parsing assumes well-formed structures in many places. While `cJSON` handles basic type checking, missing keys or unexpectedly deep JSON trees might cause NULL pointer dereferences or excessive memory allocation.
- **String Formats:** `snprintf` is used safely in most places, but care must be taken with string manipulations when constructing the filter graph or handling codec names to prevent stack-based overflows.

## 3. Denial of Service (DoS) Risk
While the host is protected from RCE, it is still vulnerable to DoS attacks via the WASM module:
- **Memory Exhaustion:** A malicious video file (e.g., declaring impossibly large frame dimensions) could cause `libavcodec` or `libavfilter` to attempt massive memory allocations. If the `wazero` runtime is not configured with strict memory limits, this could lead to an Out-Of-Memory (OOM) kill of the host Go process.
- **CPU Exhaustion:** "Zip bomb" style video files or incredibly complex filtergraphs can trap the single-threaded WASM execution in a nearly infinite loop, burning CPU cycles. The Go host must implement strict context cancellations (`ctx` timeouts) to kill runaway `wazero` invocations.

## 4. `argv[1]` Job Specification
The entire job is serialized as a JSON string and passed to the WASM module as `argv[1]`.
- **Command Injection:** Since there is no shell interpreter inside the WASM module (the Go side invokes the WASM `main` function directly via `wazero`), traditional shell injection is impossible.
- **Argument Size Limits:** Extremely large `jobSpec` JSON strings (e.g., massive filtergraphs) might exceed WASI argument length limits or cause excessive allocation on startup.

## Recommendations
1. Ensure the `wazero` instantiation explicitly limits guest memory to a sane upper bound (e.g., 512MB or 1GB, depending on the expected video resolution).
2. Enforce strict timeouts on the `RunJob` context in the Go wrapper.
3. Add robust NULL checks to the `cJSON` extraction logic in `driver.c` and `process.c`.
