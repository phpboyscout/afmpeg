# Performance Review: ffmpeg-wasi

This document outlines the performance characteristics and concerns of the `ffmpeg-wasi` implementation compared to native FFmpeg execution.

## 1. Single-Threaded Execution
WASM via `wazero` runs purely single-threaded. Standard FFmpeg relies heavily on multi-threading to achieve real-time or faster-than-real-time transcoding:
- **Slice/Frame-Level Decoding:** Modern video codecs (H.264, HEVC) divide frames into slices or use frame-level parallelism to decode on multiple cores.
- **Encoding:** Encoders like `libx264` utilize multiple threads for lookahead, motion estimation, and entropy coding.
- **Impact:** This implementation will be bounded to a single host CPU core per `wazero` instance, resulting in significantly slower transcode times, especially for high-resolution video.

## 2. Lack of SIMD Optimization
Native FFmpeg gets a massive performance boost from CPU-specific SIMD instructions (SSE, AVX2, AVX-512 on x86; NEON on ARM). While WASM has a SIMD proposal (128-bit), its adoption and the successful compilation of FFmpeg's hand-written assembly routines into WASM SIMD are historically problematic.
- **Impact:** Software decoders and encoders will run using scalar instructions. Tasks like color space conversion (`swscale`) and motion estimation will be drastically slower than native equivalents.

## 3. WASM/Go Memory Boundary (VFS Bridge)
The project uses an in-memory VFS (`afero` bridged to WASI).
- Every packet read from the input file and every packet written to the output file must cross the WASM memory boundary.
- Wazero's `sysfs` bridge needs to copy data between the Go host memory and the WASM guest memory.
- **Impact:** For high-bitrate video, the sheer volume of memory copying (Go -> WASM -> Go) per frame will introduce substantial I/O overhead compared to native FFmpeg reading directly from a memory-mapped file or OS page cache.

## 4. Forced Decode/Encode Pipeline
The current `process.c` implementation forces all inputs through a `decode -> filtergraph -> encode` pipeline.
- There is no support for stream copying (equivalent to FFmpeg's `-c copy`).
- **Impact:** Even if a user only wants to remux a file (e.g., MKV to MP4) or extract an audio track without re-encoding, `ffmpeg-wasi` will fully decode and re-encode the streams, burning CPU cycles unnecessarily and potentially degrading quality.

## 5. Round-Robin Processing Loop
The main loop in `process.c` reads one frame from each input sequentially.
- **Impact:** While sufficient for simple muxing, this tight, unbuffered loop may introduce latency bottlenecks if one input requires significantly more processing time to demux than another, stalling the entire pipeline.
