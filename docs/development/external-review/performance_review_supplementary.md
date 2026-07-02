# Supplementary Performance Review: Deep Dive

This document provides a highly granular, code-level analysis of the performance bottlenecks present in the `ffmpeg-wasi` implementation.

## 1. High-Frequency `AVFrame` Allocation Overhead
**Location:** `process.c`, inside `pull_sinks(Ctx *c)` and `feed(...)`

**Analysis:**
The `pull_sinks()` function is responsible for draining frames from the filter graph and sending them to the encoders. It begins with:
`AVFrame *f = av_frame_alloc();`
and ends with:
`av_frame_free(&f);`

Because `pull_sinks()` is called by `feed()` for *every single packet read* from any input in the main demuxing loop, the engine is allocating and freeing a complex `AVFrame` struct thousands of times per second (e.g., at least 30-60 times per second per video stream, plus hundreds of times for audio packets).

**Recommendation:**
Pre-allocate a single `AVFrame` within the `Ctx` struct during initialization. In `pull_sinks()`, simply use `av_frame_unref(f)` to reset the frame's internal buffers between uses instead of completely freeing and reallocating the struct memory.

## 2. Inefficient Input Interleaving (Naive Demuxing Loop)
**Location:** `process.c`, the main loop in `op_process()`

**Analysis:**
The main demuxing loop uses a naive round-robin approach:
```c
for (int i = 0; i < c.n_in && rc >= 0; i++) {
    int r = av_read_frame(c.in[i], pkt);
    // ... feed to filtergraph
}
```
If you have multiple inputs (e.g., `in0` is a 4K video, `in1` is an audio track), the loop reads exactly one packet from `in0`, then one from `in1`, blindly. 
If the packets in `in0` and `in1` are severely out of sync in terms of presentation timestamps (PTS), `libavfilter` will be forced to buffer an enormous amount of raw, uncompressed frames in memory while it waits for the lagging input to catch up so it can synchronize them. 

**Recommendation:**
Native FFmpeg avoids this by always reading from the input that currently has the lowest timestamp (earliest PTS). The implementation should track the current PTS of each input and read from the one furthest behind.

## 3. WASI I/O Boundary Block Sizes
**Location:** `libavformat` internals interacting with `sysfs` via Wazero.

**Analysis:**
Every time `av_read_frame` is called, `libavformat`'s internal AVIO context will request bytes from the filesystem. Because this is routed through WASI `fd_read` to Go's `afero` FS, there is a context switch cost. If the AVIO buffer size is left to its default (often 32KB), a high-bitrate video will generate a massive amount of boundary crossings.

**Recommendation:**
Consider explicitly allocating a custom `AVIOContext` with a much larger internal buffer (e.g., 1MB or larger) for both inputs and outputs to amortize the cost of WASM-to-Go boundary crossings.
