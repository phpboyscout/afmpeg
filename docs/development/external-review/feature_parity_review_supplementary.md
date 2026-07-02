# Supplementary Feature Parity Review: Deep Dive

This document highlights critical functionality present in native FFmpeg that has been entirely omitted from the `ffmpeg-wasi` JSON spec and C implementation.

## 1. Missing Time Manipulations (Trimming and Seeking)
**Native FFmpeg:** 
Supports fast seeking (`-ss` before the input), output duration limits (`-t`), and end-time limits (`-to`).
**ffmpeg-wasi:** 
The JSON `jobSpec` schema has no fields for start times or durations. The main loop in `process.c` processes the file unconditionally from byte 0 until EOF. 
- **Impact:** You cannot extract a 10-second clip from a 2-hour movie without decoding the entire 2-hour movie up to that point.

## 2. Inability to Read Raw Formats
**Native FFmpeg:**
Allows forcing format and stream configurations using `-f`, `-pixel_format`, `-video_size`, and `-framerate` before an input.
**ffmpeg-wasi:**
The implementation relies entirely on `avformat_open_input(..., NULL, NULL)` and `avformat_find_stream_info`.
- **Impact:** If an input file lacks headers (e.g., raw YUV video, raw PCM audio), `avformat` will fail to guess the format, and the process will abort. The JSON spec needs `input_options` to pass custom dictionaries to the demuxer.

## 3. Missing Metadata and Chapter Passthrough
**Native FFmpeg:**
Automatically attempts to copy global metadata (title, artist, creation time) and chapters from the input to the output. It also supports `-map_metadata`.
**ffmpeg-wasi:**
Only allocates output streams and copies codec parameters. 
```c
avcodec_parameters_from_context(go->ost->codecpar, go->enc);
```
- **Impact:** All ID3 tags, MP4 atoms, rotation flags, and chapter markers present in the original inputs are completely stripped and lost in the resulting output files.

## 4. Forced Output Formats
**Native FFmpeg:**
Allows overriding the guessed output format via `-f format`.
**ffmpeg-wasi:**
Relies entirely on `avformat_alloc_output_context2(&o->ofmt, NULL, NULL, o->path)`.
- **Impact:** It guesses the muxer entirely based on the file extension of `o->path`. If a user wants to output a raw H.264 stream but names the file `output.vid`, the implementation will fail to guess the format and abort.

## 5. PTS (Presentation Time Stamp) and A/V Sync Handling
**Native FFmpeg:**
Contains complex logic for Audio/Video synchronization (`-vsync`, `-async`, `-fps_mode`). It knows how to drop or duplicate frames to maintain constant frame rates (CFR).
**ffmpeg-wasi:**
Blindly forwards the `pts` provided by the `buffersink` directly into the encoder. While `libavfilter` attempts to maintain sync, without explicit options to dictate how desyncs are handled (e.g., inserting silent audio or duplicating video frames), output files generated from corrupted or variable-framerate inputs may suffer from heavy audio drift.
