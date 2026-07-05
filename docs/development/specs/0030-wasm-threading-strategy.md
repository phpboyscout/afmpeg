# 0030 — WASM threading strategy (wazero: stay single-threaded, fork for threads, or supplement)

Status: **DRAFT / SCOPING** (a strategic decision spec, triggered by the 2026-07-05 dav1d-WASM
spike. It frames the question — *is single-threaded WASM our permanent ceiling, or do we invest in
threads?* — catalogues the options, and defines the gate that would trigger a wazero-threads fork.
**Design/decision only; nothing to implement here.** Its value is a defensible answer to "why is the
`.wasm` single-threaded, and when would that change?")
Date: 2026-07-05
Parent: [0007](0007-libav-direct-engine.md) (the engine), [0008](0008-performance-strategy.md) (the
measured encode gap), [0028](0028-native-subprocess-backend.md) (the native backend — today's
"threads" answer), [0023](0023-hevc-and-av1.md) (the thread-hungry codecs)
Owns: **R-AF-15** (the WASM threading strategy / decision record)

## 1. Why

**Our entire WASM build is single-threaded** — FFmpeg `--disable-pthreads`, the openh264
single-thread shim, libvpx, and (as of the 2026-07-05 spike) a deliberately de-threaded libdav1d.
That is not a codec limitation: it is because **[wazero](https://wazero.io) — the pure-Go runtime
afmpeg is built on — does not implement the WebAssembly *threads* proposal** (atomics, shared
memory, `wasi-threads` thread-spawn). Every *other* major runtime does (browsers, wasmtime, wasmer,
Node), so libraries like dav1d ship a first-class **threaded** WASM build by default — the dav1d
spike had to actively neuter that assumption to run on wazero.

A whole class of codecs is **thread-architected** and pays a large single-thread penalty: AV1
*decode* (dav1d), VP9 encode, and the heavy encoders (x265, SVT-AV1) — [0008](0008-performance-strategy.md)
measured the WASM encode gap at **13–63×** native. Single-threaded WASM is a hard ceiling for them.
This spec asks whether we accept that ceiling, or invest to lift it.

## 2. The constraint (why this is hard)

The WASM threads proposal needs three things a runtime must provide: **atomic instructions**
(`memory.atomic.*`), a **shared linear memory**, and a **thread-spawn** host (`wasi-threads`, or Web
Workers in the browser). afmpeg's thesis is wazero specifically: **pure-Go, CGO-free, one static
cross-compiled binary, embeddable**. So "threaded WASM" cannot be bought off the shelf without
either changing the runtime (and its purity guarantees) or extending wazero itself.

## 3. The options

- **A — Status quo: single-threaded wazero + the native backend as the speed escape hatch.**
  Keep the `.wasm` single-threaded (the sandboxed, portable, arch-independent baseline); route
  thread-/encode-bound work to the **[native backend](0028-native-subprocess-backend.md)** (0028),
  which already gives threads + SIMD out-of-process, CGO-free, signed (48–58× faster encode). Cost:
  **zero** — it is what we ship today. Limit: the *sandboxed-**and**-fast* niche is unserved.

- **B — Fork wazero (or contribute upstream) to implement the threads proposal.** Add atomic
  opcodes to both wazero execution engines (the interpreter *and* the optimizing compiler backend),
  a shared linear memory that is safe under Go's concurrency, and a `wasi-threads` `thread-spawn`
  host mapped onto goroutines. Keeps the **pure-Go** thesis intact and delivers **threaded WASM
  decode/encode in-sandbox** — the genuine best of both worlds. Cost: **very large** (wazero core
  engineering + an ongoing burden tracking wazero upstream). This is the only option that serves the
  sandboxed-and-fast niche.

- **C — Supplement with a second, threads-capable runtime (wasmtime-go / wasmer).** These support
  the threads proposal — but their Go bindings use **CGO**, which breaks the exact pure-Go /
  clean-static-cross-compile property afmpeg exists to preserve (and that 0028 deliberately avoided
  by going *out-of-process* rather than CGO-in-process). An in-process CGO runtime is strictly worse
  than the out-of-process native backend we already have, so this option is **dominated by A**.

- **D — Wait for upstream wazero to add threads, then adopt.** No control over timeline; the
  [0008](0008-performance-strategy.md) landscape scan found threads *not* on wazero's near roadmap.
  A watch-item, not a plan.

## 4. The key insight

**The native backend (0028) is already our "threads" answer.** It gives full threads + SIMD,
CGO-free, over a signed out-of-process driver. So the strategic question is not "threaded vs
single-threaded WASM" in the abstract — it reduces to a **narrow niche**:

> Is there a real consumer that needs **fast** decode/encode of a thread-hungry codec **while
> inside the WASM sandbox** — i.e. for whom the native driver's trust/deployment model (spawning a
> signed native subprocess) is *unacceptable*, yet single-threaded speed is *insufficient*?

If a consumer can accept the native driver, they already get threads (option A serves them). Only a
consumer who needs the sandbox **and** the speed is unserved — and only **option B** serves them.

## 5. Decision & gate

- **D-0030-A — default: stay single-threaded (option A).** The `.wasm` remains the portable,
  sandboxed, single-threaded baseline; the native backend is the threaded/fast tier. No work.
- **D-0030-B — pursue the wazero-threads fork (option B) only on a concrete trigger:** a real
  consumer need for *sandboxed + fast* on a thread-hungry codec, where the native backend is
  demonstrably unacceptable. Absent that trigger, B is not started — the cost is too high to
  speculate on.
- **D-0030-C — reject option C (CGO runtime):** it violates the pure-Go thesis and is dominated by
  the out-of-process native backend.

## 6. If B is triggered — the scoping spike it would need

A follow-on spike (its own spec) would size the wazero-threads work: (a) the subset of the threads
proposal actually required to run FFmpeg's `pthread`-threaded libs (atomics + shared memory +
`wasi-threads` `thread-spawn`); (b) atomic-opcode support in wazero's **interpreter and** compiler
engines; (c) a shared linear memory safe under Go's race semantics; (d) the `thread-spawn` →
goroutine-pool mapping and its lifecycle; (e) an effort/maintenance estimate vs. the measured
speed-up on representative clips. This spec's job is to *frame* that spike, not run it.

## 7. Requirements

- **R-AF-15** (this spec owns it): the WASM threading ceiling is *documented and gated* — the
  single-threaded-by-wazero cause is recorded, the four options catalogued with costs, the
  native-backend-as-threads-answer insight stated, and the concrete trigger + scope for a
  wazero-threads fork defined. No runtime work ships from this spec; it is the decision record the
  performance and codec specs cite when "make the `.wasm` faster for a threaded codec" comes up.

## 8. Dependencies & sequencing

- **Informed by [0008](0008-performance-strategy.md)** — the measured encode gap is the quantitative
  case. The 2026-07-05 dav1d-WASM spike **succeeded** (a single-threaded, atomics-free libdav1d builds
  and decodes AV1 on wazero — proving the ceiling is real but the floor is usable); a throughput
  number on representative 1080p clips would sharpen the fast-vs-good-enough line.
- **Complementary to [0028](0028-native-subprocess-backend.md)** — the native backend is the
  status-quo answer this spec leans on; option B is only justified where 0028 is unacceptable.
- **Sequencing:** dormant. Revisited only when a consumer trips D-0030-B's gate. Until then the
  single-threaded WASM builds (incl. libdav1d AV1 decode) stand as the sandboxed baseline.
