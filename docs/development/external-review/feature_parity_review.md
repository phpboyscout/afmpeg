# Feature Parity Comparison: ffmpeg-wasi vs. Native FFmpeg

This document compares the limited `ffmpeg-wasi` custom implementation against the full native FFmpeg CLI.

## 1. Command-Line Interface vs. JSON Job Spec
- **Native FFmpeg:** Boasts a massive, highly flexible (and notoriously complex) CLI capable of handling inputs, outputs, mapping, metadata, hardware acceleration, and stream selection using a positional argument syntax.
- **ffmpeg-wasi:** Completely drops the CLI. Relies on a declarative, structured JSON schema (`Command` -> `jobSpec`). While easier to program against programmatically (from Go), it sacrifices the ad-hoc expressiveness of the CLI.

## 2. Stream Copying (`-c copy`)
- **Native FFmpeg:** Supports `-c copy` to remux files or extract streams without re-encoding, preserving original quality and operating near disk I/O speeds.
- **ffmpeg-wasi:** **Missing.** All streams are unconditionally decoded, passed through a filtergraph (or a default passthrough graph), and re-encoded. This is a massive feature gap.

## 3. Subtitles, Data, and Attachments
- **Native FFmpeg:** Full support for handling subtitle streams (text and bitmap), data streams, and attachments (like fonts in MKV).
- **ffmpeg-wasi:** **Missing.** The implementation explicitly only checks for and handles `AVMEDIA_TYPE_VIDEO` and `AVMEDIA_TYPE_AUDIO`. All other stream types are ignored and dropped from the output.

## 4. Stream Selection and Mapping
- **Native FFmpeg:** The `-map` flag allows granular selection of streams (e.g., `-map 0:v:1` to select the second video stream of the first input).
- **ffmpeg-wasi:** Uses a simplified `map` array on outputs, which correlates only to the output pads of the filtergraph (e.g., `"[vout]"`). It lacks direct stream mapping from input to output without passing through the filtergraph.

## 5. Advanced Options (Demuxer, Muxer, Global)
- **Native FFmpeg:** Allows configuring options at every stage: input demuxers (e.g., `-f dshow`), input codecs, global options, filter options, output codecs, and output muxers (e.g., `-movflags +faststart`).
- **ffmpeg-wasi:** Only supports applying a generic dictionary of options (`enc_opts`) to the *encoder* (`avcodec_open2`). It completely lacks the ability to pass options to the format demuxers (`avformat_open_input`) or muxers (`avformat_write_header`).

## 6. Hardware Acceleration
- **Native FFmpeg:** Supports a wide array of HW acceleration APIs (NVENC/NVDEC, VAAPI, VideoToolbox, QSV) for drastically improved performance and reduced CPU usage.
- **ffmpeg-wasi:** **Impossible.** Running inside a WASM sandbox prevents any access to the host machine's GPU or hardware encoding ASIC. It is strictly a software-only implementation.

## 7. Codec Availability
- **Native FFmpeg:** Often compiled with hundreds of internal and external codecs.
- **ffmpeg-wasi:** Codec availability is strictly limited to what can be successfully cross-compiled to WASM and linked statically into the `ffmpeg-wasi` binary. The `report()` function reveals a small, curated subset (e.g., `libopenh264`, `libx264`, `aac`, `flac`, `mjpeg`).

## Summary
`ffmpeg-wasi` is a specialized, sandboxed media processing engine designed for programmatic use in Go via an in-memory VFS. It achieves excellent security isolation at the heavy cost of performance (single-threaded, no SIMD/HW accel) and lacks critical functionality like stream copying, subtitle support, and advanced muxer/demuxer configurations found in native FFmpeg.
