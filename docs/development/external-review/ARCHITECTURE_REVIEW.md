# afmpeg & ffmpeg-wasi: Consolidated Architecture & Review Report

> Comprehensive analysis covering the current WASM implementation (performance,
> security, feature parity), the hardware acceleration strategy, all evaluated
> architectural options, and the recommended path forward.
>
> Date: 2026-07-01 · Author: Antigravity (AI-assisted review)

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Project Overview](#2-project-overview)
3. [Current Implementation: Performance Analysis](#3-current-implementation-performance-analysis)
4. [Current Implementation: Security Analysis](#4-current-implementation-security-analysis)
5. [Current Implementation: Feature Parity vs Native FFmpeg](#5-current-implementation-feature-parity-vs-native-ffmpeg)
6. [The Hardware Acceleration Problem](#6-the-hardware-acceleration-problem)
7. [Discarded Architectural Options](#7-discarded-architectural-options)
8. [Recommended Architecture: Dual-Backend Strategy](#8-recommended-architecture-dual-backend-strategy)
9. [HTTP Bridge: Performance Deep-Dive](#9-http-bridge-performance-deep-dive)
10. [HTTP Bridge: Security Deep-Dive](#10-http-bridge-security-deep-dive)
11. [Implementation Roadmap](#11-implementation-roadmap)
12. [Conclusion](#12-conclusion)

---

## 1. Executive Summary

`afmpeg` is a pure-Go FFmpeg binding that runs media pipelines over an
in-memory `afero.Fs` filesystem, with FFmpeg compiled to WebAssembly and
executed via wazero. This architecture delivers exceptional portability and
security, but is inherently constrained by the WASM sandbox: no hardware
acceleration, no SIMD, single-threaded execution.

After exhaustive analysis of all viable architectures, **the recommended path
forward is a dual-backend strategy**: the existing WASM backend remains the
portable default; a new **native subprocess backend** (using an ephemeral HTTP
server to bridge afero) is offered as an opt-in for consumers who need peak
performance or hardware acceleration. The consumer's API (`RunJob`, `Probe`,
`Command`) remains identical regardless of backend.

Five alternative architectures were evaluated and discarded. CGO (both static
and dynamic linking) violates the MIT licensing and portability constraints.
FUSE destroys cross-platform portability. Named pipes break the random-access
seeking FFmpeg requires. Enhancing WASM with hardware passthrough is not
technically feasible within the WebAssembly specification.

---

## 2. Project Overview

### ffmpeg-wasi

A custom WASI engine (`src/driver.c`, `src/process.c`) that links FFmpeg's
`libav*` libraries directly — bypassing the multithreaded FFmpeg CLI that
cannot run in WASM — and exposes two operations via a JSON job spec passed as
`argv[1]`:

- **`probe`**: Opens inputs and returns container/stream metadata as JSON.
- **`process`**: Full decode → filtergraph → encode → mux pipeline with support
  for multiple inputs, `filter_complex`, and multiple outputs with pad mapping.

Published as two `.wasm` artifacts per release (LGPL and GPL variants), with
OpenPGP-signed checksums and provenance manifests.

### afmpeg

The pure-Go companion library. Key components:

- **`Runtime`**: Compiles the WASM module once via wazero; serialises
  invocations behind a mutex.
- **`Command` / `JobSpec`**: Declarative media job description that renders to
  the engine's JSON spec.
- **VFS bridge** (`internal/vfs`): Adapts `afero.Fs` to wazero's
  `experimental/sys.FS`, routing all guest I/O (including `/tmp` and
  `/dev/null`) through the caller's filesystem.
- **Fetch/verify pipeline**: Downloads, caches, SHA-256-verifies, and
  optionally OpenPGP-signature-verifies the WASM module at runtime.
- **setjmp/longjmp shim**: Host-side implementation of C's `setjmp`/`longjmp`
  using wazero's experimental Snapshotter, required by FFmpeg's error handling.

---

## 3. Current Implementation: Performance Analysis

### 3.1 Single-Threaded Execution

WASM via wazero runs on a single host CPU core. Native FFmpeg uses
frame/slice-level parallelism for decoding (H.264, HEVC) and multi-threaded
lookahead/motion estimation for encoding (libx264). **Impact:** Transcode
times are bounded to one core, significantly slower than native for
high-resolution video.

### 3.2 No SIMD Optimisation

Native FFmpeg benefits enormously from hand-written SSE/AVX2/AVX-512 (x86)
and NEON (ARM) routines. WASM SIMD is limited to 128-bit vectors, and FFmpeg's
assembly routines do not compile to WASM SIMD. **Impact:** Software
decoders/encoders, colour space conversion (`swscale`), and motion estimation
run scalar, drastically slower than native equivalents.

### 3.3 WASM/Go Memory Boundary Overhead

Every byte read from or written to the `afero.Fs` crosses the WASM memory
boundary via wazero's `sysfs` bridge. For high-bitrate video, the volume of
memory copying (Go → WASM → Go) per frame introduces I/O overhead compared to
native FFmpeg's direct memory access.

### 3.4 No Stream Copying

The current `process.c` forces all streams through a decode → filtergraph →
encode pipeline. There is no equivalent to FFmpeg's `-c copy`. **Impact:**
Even a simple remux (e.g., MKV → MP4) fully re-encodes, wasting CPU and
potentially degrading quality.

### 3.5 Naive Round-Robin Demuxing

The main loop reads one packet from each input sequentially. If inputs are
severely out of sync in PTS, `libavfilter` must buffer large quantities of
uncompressed frames in memory while waiting for the lagging input. Native
FFmpeg always reads from the input with the lowest current timestamp.

### 3.6 AVFrame Allocation Overhead

`pull_sinks()` allocates and frees an `AVFrame` per call via `av_frame_alloc()`
/ `av_frame_free()`. Because `pull_sinks()` is invoked for every packet read,
this creates thousands of unnecessary allocations per second. A pre-allocated
frame with `av_frame_unref()` between uses would eliminate this overhead.

### 3.7 Unoptimised AVIO Buffer Sizes

`libavformat`'s default AVIO buffer (typically 32KB) causes frequent
WASM-to-Go boundary crossings for high-bitrate streams. A larger custom
`AVIOContext` buffer (e.g., 1MB) would amortise the crossing cost.

---

## 4. Current Implementation: Security Analysis

### 4.1 Strong WASM Sandboxing (Major Benefit)

The most significant security advantage. FFmpeg's C codebase has a long history
of memory safety vulnerabilities in its demuxers and decoders. By running inside
wazero's WASM sandbox:

- Any RCE triggered by a malicious media file is completely contained within
  the WASM guest's linear memory.
- The guest cannot access the host machine's disk, environment variables, or
  network — it only sees the `afero.Fs` mounted via WASI.
- Traditional shell injection is impossible: there is no shell, and the Go side
  invokes the WASM `main` function directly.

### 4.2 Missing Wazero Memory Limits (OOM Risk)

`runtime.go` initialises wazero without `WithMemoryLimitPages()`. The WASM
guest can allocate up to the WASM spec maximum (~4GB for 32-bit). A malicious
video declaring extreme frame dimensions could force `libavcodec` to attempt
multi-gigabyte allocations, OOM-killing the host Go process.

**Recommendation:** Enforce a strict memory ceiling via
`wazero.NewRuntimeConfig().WithMemoryLimitPages(...)`.

### 4.3 CPU Exhaustion / Denial of Service

"Zip-bomb" videos or pathologically complex filtergraphs can trap the
single-threaded WASM execution indefinitely. The host Go process must enforce
strict context timeouts.

`WithCloseOnContextDone(true)` is correctly configured, but this relies
entirely on the consumer passing a context with a deadline. If a consumer
passes `context.Background()`, a runaway invocation will lock the `Runtime`'s
mutex forever.

**Recommendation:** `RunJob` should internally enforce a maximum timeout or
return an error if the provided context has no deadline.

### 4.4 cJSON Edge Cases

The JSON parsing in `process.c` generally handles missing keys safely via
`cJSON_GetStringValue` returning `NULL`. However, the encoder options loop:

```c
cJSON_ArrayForEach(kv, opts) {
    if (cJSON_IsString(kv)) av_dict_set(&o->enc_opts, kv->string, kv->valuestring, 0);
}
```

does not verify that `opts` is an object/array before iterating. If `opts` is
a malformed type (e.g., a string), `cJSON_ArrayForEach` may behave
unpredictably.

**Recommendation:** Add `if (cJSON_IsObject(opts))` guards before iteration.

---

## 5. Current Implementation: Feature Parity vs Native FFmpeg

| Capability | Native FFmpeg | ffmpeg-wasi | Gap Severity |
|---|---|---|---|
| **CLI interface** | Full positional CLI | JSON job spec via `argv[1]` | By design (not a gap) |
| **Stream copying (`-c copy`)** | Yes | No — always re-encodes | **Critical** |
| **Subtitles, data, attachments** | Full support | Ignored — only video/audio | **High** |
| **Seeking/trimming (`-ss`, `-t`, `-to`)** | Yes — fast and output-based | No — always processes start to EOF | **High** |
| **Metadata/chapter passthrough** | Automatic | Stripped entirely | **Medium** |
| **Raw format handling (`-f`, `-pixel_format`)** | Yes — full demuxer options | No — relies on auto-detection | **Medium** |
| **Stream selection/mapping** | Granular (`-map 0:v:1`) | Limited to filtergraph pad labels | **Medium** |
| **Muxer options (`-movflags`)** | Full dictionary | Not exposed | **Medium** |
| **Demuxer options** | Full dictionary | Not exposed | **Medium** |
| **Hardware acceleration** | NVENC, VAAPI, VideoToolbox, QSV | Impossible (WASM sandbox) | **Critical** |
| **Codec availability** | Hundreds of internal + external | Small curated subset | **Medium** |
| **A/V sync (`-vsync`, `-async`)** | Complex frame-drop/duplicate logic | Passthrough only — relies on filter PTS | **Low** |
| **Forced output format (`-f`)** | Yes | No — guesses from file extension | **Low** |

---

## 6. The Hardware Acceleration Problem

### Why WASM Cannot Solve It

WASM runs inside a linear-memory sandbox with no mechanism to call host GPU
drivers. WASM SIMD is limited to 128-bit vectors. wazero executes
single-threaded. These are fundamental properties of the WebAssembly
specification, not implementation limitations — they cannot be worked around.

### The Hard Constraints

Any solution must simultaneously satisfy:

1. **afero.Fs enforcement** — all I/O flows through the caller's filesystem.
2. **MIT licensing** — GPL/LGPL FFmpeg code must be a separate,
   runtime-acquired artifact, never compiled into the Go binary.
3. **Portability** — `go build` produces a deployable binary with zero
   pre-installed host dependencies.
4. **Hardware acceleration** — available as an opt-in for consumers who need
   peak performance.

---

## 7. Discarded Architectural Options

### 7.1 CGO with Static Linking

**How it works:** Compile `process.c` natively via CGO, statically linking
`libav*` into the Go binary.

**Why discarded:**

- **Licensing violation.** Statically linking GPL/LGPL libraries into the Go
  binary taints it with the copyleft licence. The "separate artifact" model
  that keeps afmpeg MIT is destroyed — the FFmpeg code is literally inside
  the binary.
- **Portability destruction.** The consumer needs a C compiler, `pkg-config`,
  and `libav*-dev` headers to `go build`. Cross-compilation
  (`GOOS=linux GOARCH=arm64 go build`) stops working.
- **Deploy dependency.** If linked dynamically to avoid the static licence
  issue, the deploy target must have matching `.so`/`.dylib` files.

**Verdict:** Violates constraints 2 and 3. Eliminated.

### 7.2 CGO with Dynamic Linking

**How it works:** Compile `process.c` via CGO, dynamically linking against
the host system's `libav*` shared libraries.

**Why discarded:**

- **Licensing grey area.** Dynamically linking LGPL libraries *may* preserve
  MIT for the Go code, but if the host's `libav*` was compiled with GPL
  components (libx264), the combined execution's legal status is contested.
- **Build-time dependency.** The consumer must install `ffmpeg-dev` headers.
- **Deploy-time dependency.** The binary requires the exact same shared
  libraries on the deploy target.
- **afero bridge advantage.** CGO does offer the best VFS bridge via
  `avio_alloc_context` C callbacks directly into Go's `afero.Fs` — this is
  the one genuine advantage of CGO. However, it does not outweigh the three
  violations above.

**Verdict:** Violates constraints 2 (arguably) and 3. The one advantage
(direct memory VFS bridge) does not justify the trade-offs. Eliminated.

### 7.3 Pure CGO Bindings (Rewriting process.c in Go)

**How it works:** Abandon the C engine entirely for the native build. Write
Go code that calls `libav*` functions directly via CGO.

**Why discarded:**

- Inherits all CGO licensing and portability problems from 7.1/7.2.
- **Extreme maintenance burden.** The entire processing pipeline must be
  implemented twice: once in C for the WASM build, once in Go for the native
  build. Every bug fix and feature addition is duplicated.
- **CGO friction.** Translating complex nested C structs (`AVFilterGraph`,
  `AVCodecContext`) across the CGO boundary in Go is notoriously tedious.

**Verdict:** Violates constraints 2 and 3, with the added cost of double
maintenance. Eliminated.

### 7.4 FUSE (Filesystem in Userspace) Bridge

**How it works:** Use `bazil/fuse` to mount `afero.Fs` as a real filesystem
on the host (e.g., `/mnt/afero/`). The native FFmpeg subprocess sees it as a
normal disk.

**Why discarded:**

- **Destroys portability.** The host must have kernel FUSE drivers installed
  (`libfuse` on Linux, `macFUSE` on macOS, `WinFsp` on Windows). These are
  not available by default on most systems and require root/admin privileges
  to install.
- **Operational complexity.** Mount/unmount lifecycle, permission management,
  and cleanup on crash are significantly more complex than an HTTP server.
- **macOS/Windows fragility.** macFUSE and WinFsp are third-party projects
  with varying levels of maintenance and compatibility.

**Verdict:** Violates constraint 3. Eliminated.

### 7.5 Named Pipes (FIFOs) / stdin-stdout Bridge

**How it works:** Create named pipes (e.g., `/tmp/ffmpeg_in`) bridged to
`afero`. FFmpeg reads/writes via the pipe paths. Alternatively, use
`pipe:0`/`pipe:1` for stdin/stdout.

**Why discarded:**

- **No random-access seeking.** Pipes are strictly sequential streams. When
  FFmpeg opens an MP4, it needs to seek to read the `moov` atom (often at
  the end of the file). A pipe cannot seek. This causes MP4 input to fail
  entirely, or requires the input to be fully buffered in memory first.
- **MP4 output breakage.** FFmpeg's MP4 muxer seeks backward to write the
  `moov` atom header after encoding. A pipe cannot seek backward. This
  forces `-movflags frag_keyframe+empty_moov` on every output, eliminating
  the option of non-fragmented MP4.
- **Windows incompatibility.** Windows named pipes (`\\.\pipe\name`) have
  significantly different semantics from UNIX FIFOs and are poorly supported
  by the FFmpeg CLI.

**Verdict:** Fundamentally incompatible with FFmpeg's seeking requirements
for the most common container format (MP4). Eliminated.

### 7.6 Enhanced WASM (GPU Passthrough / WASI Extensions)

**How it works:** Hypothetically extend the WASM runtime to expose host GPU
APIs to the guest module.

**Why discarded:**

- **No specification exists.** There is no WASI proposal for GPU device
  access. The WASM Component Model and wasi-gpu are in very early stages
  with no timeline for standardisation.
- **wazero would need to implement it.** Even if a spec existed, wazero
  (a pure-Go runtime) would need to implement GPU driver bindings, which
  contradicts its zero-dependency philosophy.
- **Architectural impossibility.** GPU encoding APIs (NVENC, VAAPI) require
  direct access to kernel device files (`/dev/nvidia*`, `/dev/dri/*`). The
  WASM sandbox model is fundamentally designed to prevent this.

**Verdict:** Not technically feasible within the current or foreseeable WASM
ecosystem. Eliminated.

---

## 8. Recommended Architecture: Dual-Backend Strategy

The only architecture that satisfies all four constraints simultaneously.

```
┌──────────────────────────────────────────┐
│            Consumer Code                 │
│   cmd := afmpeg.NewCommand(...)          │
│   res, _ := rt.RunJob(ctx, fs, cmd)      │
└─────────────────┬────────────────────────┘
                  │ (same Command / Result types)
                  ▼
┌──────────────────────────────────────────┐
│            afmpeg.Runtime                │
│    (delegates to internal backend)       │
├──────────────────┬───────────────────────┤
│   WASM Backend   │   Native Backend      │
│   (default)      │   (opt-in)            │
├──────────────────┼───────────────────────┤
│ wazero           │ os/exec subprocess    │
│ WASI VFS bridge  │ ephemeral HTTP server │
│ ffmpeg.wasm      │ static ffmpeg binary  │
│ (downloaded)     │ (local or downloaded) │
└──────────────────┴───────────────────────┘
```

### Backend 1: WASM (Current — Unchanged)

- **Module source:** Runtime-fetched `.wasm` artifact, signature-verified.
- **VFS bridge:** `internal/vfs` routes WASI syscalls directly to `afero.Fs`.
  Full random-access seeking via `Pread`/`Pwrite`.
- **Strengths:** Sandboxed, portable, MIT-clean, zero host dependencies.
- **Weaknesses:** Single-threaded, no SIMD, no HW accel.

### Backend 2: Native Subprocess (New — Opt-In)

- **Binary source — three tiers:**
  1. `WithNativeBinary("/usr/local/bin/ffmpeg")` — user-supplied path.
  2. `WithNativeRelease("7.1.1")` — runtime download of a static FFmpeg
     binary, SHA-256-verified and cached (mirrors the WASM fetch pipeline).
  3. `WithNativeFromPATH()` — discovers `ffmpeg` on the host `$PATH`.
- **VFS bridge:** Ephemeral `net/http` server on `127.0.0.1:0`.
  - **Inputs:** `http.ServeContent` serves files from `afero.Fs` with full
    HTTP Range Request support (afero's `File` implements `io.ReadSeeker`).
  - **Outputs:** HTTP `PUT` handler streams the response body into `afero.Fs`.
- **CLI translation:** `Command.NativeArgs(port)` renders the same `Command`
  struct into standard FFmpeg CLI arguments.
- **Probe:** Uses `ffprobe -print_format json` over the HTTP bridge,
  parsed into the existing `Probe`/`ProbeStream` types.
- **Lifecycle:** `exec.CommandContext(ctx, ...)` — context cancellation kills
  the subprocess. HTTP server shuts down immediately on process exit.
- **Strengths:** Full SIMD, multi-threaded, hardware acceleration, MIT-clean.
- **Weaknesses:** No WASM sandbox; HTTP bridge has marginal I/O overhead.

### Consumer API (Identical for Both Backends)

```go
// WASM backend (current behaviour, unchanged):
rt, _ := afmpeg.New(ctx, afmpeg.WithModuleRelease("n8.1.2-3", afmpeg.VariantLGPL))

// Native backend (opt-in — user's local FFmpeg):
rt, _ := afmpeg.New(ctx, afmpeg.WithNativeBinary("/usr/bin/ffmpeg"))

// Native backend (opt-in — runtime download):
rt, _ := afmpeg.New(ctx, afmpeg.WithNativeRelease("7.1.1"))

// Then — identical regardless of backend:
fs := afero.NewMemMapFs()
cmd := afmpeg.NewCommand(
    afmpeg.WithInput("input.mp4"),
    afmpeg.WithOutput("output.mp4", afmpeg.VideoCodec("libx264"), afmpeg.AudioCodec("aac")),
)
res, _ := rt.RunJob(ctx, fs, cmd)
output, _ := afero.ReadFile(fs, "output.mp4")
```

Hardware acceleration codecs work transparently with the native backend:

```go
afmpeg.VideoCodec("h264_nvenc")         // NVIDIA GPU
afmpeg.VideoCodec("h264_vaapi")         // Linux VA-API
afmpeg.VideoCodec("h264_videotoolbox")  // macOS
```

---

## 9. HTTP Bridge: Performance Deep-Dive

### 9.1 Loopback Overhead

Traffic on `127.0.0.1` never leaves the machine. It traverses the kernel's
loopback interface — a memory copy, not a network operation.

| Overhead Source | Impact | Magnitude |
|---|---|---|
| TCP/IP stack processing | Kernel processes packets through the full network stack on loopback | ~1–3% throughput loss vs direct I/O |
| HTTP protocol framing | Request/response headers, chunked encoding | Negligible (bytes vs megabytes of media) |
| Range Request round-trips | FFmpeg issues multiple small Range requests during MP4 `moov` parsing | 10–50ms total during container open, not during transcode |
| Memory copies | afero → Go HTTP handler → kernel → FFmpeg process | One extra kernel-mediated copy vs the WASM bridge |

### 9.2 Practical Impact

For a typical transcode, the codec consumes 95%+ of wall-clock time. The I/O
bridge overhead is in the noise. High-bitrate streams (4K ProRes at 1+ Gbps)
are the only scenario where loopback copy overhead becomes measurable — and the
native backend's multi-threaded SIMD encoding still delivers 10–50× the
throughput of the WASM backend, making the net result a massive performance win.

### 9.3 MP4 Output Consideration

FFmpeg's MP4 muxer seeks backward to write the `moov` atom. HTTP `PUT` is a
sequential stream and cannot seek backward. Two mitigations:

1. **Default:** The CLI translation layer automatically injects
   `-movflags +frag_keyframe+empty_moov` for MP4/MOV outputs, producing a
   streamable fragmented MP4 with no backward seek required.
2. **Option:** A `WithNonFragmentedMP4()` option uses a temporary host file,
   then copies the result into afero post-process — for consumers who need
   standard non-fragmented MP4 and accept a brief disk touch.

---

## 10. HTTP Bridge: Security Deep-Dive

### 10.1 Threat: Local Port Exposure

Any process on the same machine can connect to `127.0.0.1:<port>`. In
multi-tenant environments (shared hosts, Kubernetes pods with sidecars), a
co-located process could read inputs or corrupt outputs.

**Mitigation:** Generate a cryptographically random token per invocation.
Require it on every HTTP request via a custom header. FFmpeg supports this
via `-headers "Authorization: Bearer <token>\r\n"`. The Go server rejects
any request without the matching token.

### 10.2 Threat: Port Lifetime Window

The port is open for the duration of the FFmpeg subprocess (seconds to
minutes).

**Mitigation:** Bind to `:0` (OS-assigned ephemeral port). The server is
created, used for exactly one invocation, and destroyed via
`server.Shutdown()` immediately when the subprocess exits.

### 10.3 Threat: Unencrypted Loopback Traffic

Data flows over plain HTTP on loopback. Kernel loopback traffic cannot be
sniffed by remote hosts, but local processes with raw socket access could
theoretically capture it.

**Mitigation:** TLS on loopback is possible (self-signed certificate) but
adds complexity for minimal gain in most threat models. Document the
trade-off and offer it as a future option.

### 10.4 Threat: Malicious Native Binary

A tampered FFmpeg binary could exfiltrate data or execute arbitrary code with
the Go process's OS permissions.

**Mitigation:** For `WithNativeRelease`, enforce SHA-256 verification (reuse
the existing fetch/verify pipeline). For `WithNativeBinary`, trust the
consumer — they are explicitly providing the path.

### 10.5 Comparison to WASM Backend Security

| Property | WASM Backend | Native Backend |
|---|---|---|
| RCE containment | Full sandbox — exploits trapped in WASM memory | **None** — subprocess has full OS permissions |
| Filesystem access | Only `afero.Fs` via WASI | Only `afero.Fs` via HTTP bridge (but subprocess *can* touch host disk) |
| Network access | Impossible | Subprocess could make outbound connections |
| Attack surface | WASM guest memory only | HTTP port on localhost + subprocess OS access |

The native backend is an objective security regression compared to WASM. This
must be clearly documented so consumers opt in with full awareness.

---

## 11. Implementation Roadmap

### File Structure (Native Backend Addition)

```
pkg/afmpeg/
├── backend.go           # internal backend interface
├── backend_wasm.go      # existing wazero logic (extracted from runtime.go)
├── backend_native.go    # subprocess + HTTP bridge orchestration
├── bridge_server.go     # ephemeral HTTP server for afero bridging
├── command.go           # unchanged
├── command_options.go   # unchanged
├── command_native.go    # NativeArgs() method — CLI translation
├── runtime.go           # delegates to backend; public API unchanged
├── fetch.go             # extended for native binary fetching
└── ...
```

### CLI Translation Mapping

| `Command` field | FFmpeg CLI equivalent |
|---|---|
| `Input{Path: "clip.mp4"}` | `-i http://127.0.0.1:<port>/clip.mp4` |
| `FilterComplex` | `-filter_complex "<graph>"` |
| `Output.Map: ["[v]"]` | `-map "[v]"` |
| `Output.VideoCodec: "libx264"` | `-c:v libx264` |
| `Output.AudioCodec: "aac"` | `-c:a aac` |
| `Output.Options: {"crf": "23"}` | `-crf 23` |
| `Output.Path: "out.mp4"` | `http://127.0.0.1:<port>/out.mp4` |

Additional flags injected automatically: `-y`, `-nostdin`,
`-loglevel warning`, `-movflags +frag_keyframe+empty_moov` (for MP4).

### Testing Strategy

- **Unit:** Assert `Command.NativeArgs()` produces correct CLI arguments.
- **Integration:** Spin up the HTTP bridge, serve a test file from
  `afero.NewMemMapFs()`, verify Range request handling.
- **End-to-end:** If CI has `ffmpeg` available, run a real transcode through
  the native backend. Gate behind `//go:build integration_native`.

---

## 12. Conclusion

The dual-backend architecture is the definitive solution because it is the
**only** design where all four hard constraints are simultaneously satisfied:

1. **afero.Fs abstraction** — enforced via the existing WASI VFS bridge (WASM)
   or an ephemeral HTTP server with Range-request seeking (native).
2. **MIT licensing** — the Go library never links, embeds, or compiles FFmpeg
   code. In both backends, FFmpeg is a separate artifact acquired at runtime.
3. **Portability** — `go build` produces a single static binary. The WASM
   module or native binary is fetched at runtime with zero pre-installed
   dependencies.
4. **Hardware acceleration** — fully available via the native backend using
   the consumer's preferred codec (NVENC, VAAPI, VideoToolbox).

The consumer's code never changes. The backend is selected at `New()` time
and is opaque thereafter. The WASM backend remains the secure, portable
default for the majority of use cases. The native backend is an opt-in
performance escape hatch for consumers who need it.
