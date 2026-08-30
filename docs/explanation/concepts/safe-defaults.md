---
title: Why a Runtime is capped, deadlined and serialised
description: The reasoning behind afmpeg's three out-of-the-box constraints: a 512 MB guest memory cap, a one-hour invocation deadline, and one job at a time, plus what they do not protect.
date: 2026-08-02
tags: [explanation, hardening, defaults, concurrency]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Why a Runtime is capped, deadlined and serialised

A `Runtime` you build with no options at all is already constrained in three ways: its guest
memory is capped at 512 MB, every invocation runs under a one-hour deadline, and invocations
run one at a time. None of those is a performance tuning knob that happens to default to
something. Each answers a specific way the thing goes wrong.

The values themselves, and how to change them, are in
[runtime options](../../reference/runtime-options.md). This page is why they are there.

## Why the defaults are on rather than opt-in

afmpeg's purpose is processing media you did not create. A user upload, a file fetched from a
URL, an artifact from a pipeline someone else owns. That is the normal case, not the paranoid
one.

A library whose safe configuration is opt-in gets deployed unsafely, because the person wiring
it up is thinking about getting a transcode working, not about what a malformed MP4 does to
their process. Making the constraints the default inverts that: the unsafe configuration is the
one you have to ask for, in a call that names what you are removing. `WithMemoryLimit(0)` is
hard to write by accident and hard to miss in review.

The cost is real and it is accepted: a legitimate job that needs more than 512 MB fails until
someone raises the limit. That is a better failure than the alternative, because it fails
loudly, in one place, with a number to change.

## What the memory cap actually prevents

A media file declares its own dimensions, and a decoder allocates from what it is told before it
has decoded a single pixel. A file claiming enormous frame dimensions asks libav to allocate
gigabytes, and it will try.

Without a cap that allocation lands in the host process. Go cannot recover from it, and the process
is killed by the kernel, taking every other request in flight with it. One bad upload becomes an
outage.

With the cap, the guest's `memory.grow` fails instead. libav sees an allocation failure, handles
it as the error path it already has for `ENOMEM`, and the engine exits non-zero. You get a
`Result` with a failure in it, the `Runtime` is still usable, and every other job carries on.
That is the entire trade: turning a host-level crash into a job-level failure.

It is worth being clear about what the number is and is not. 512 MB is a working ceiling
generous enough for ordinary decode and encode work and far below the multi-gigabyte demand a
crafted file makes. It is not a measurement of *your* workload. Large frames, many parallel
filter buffers, or a filtergraph holding several streams in flight can legitimately need more,
raise it deliberately rather than removing it, so the ceiling still exists.

## Why an hour, and why your deadline always wins

The deadline addresses a different failure: not allocating too much, but never finishing. A
decode loop that does not terminate holds the invocation slot forever. Because a `Runtime`
serialises, that is not one stuck job. It is every subsequent job on that `Runtime`, queued
behind something that will never end.

An hour is not a guess at how long your jobs take. It is deliberately far longer than any
reasonable job, because the default exists to catch *pathological* runs, not slow ones. A
default tight enough to be useful as a job timeout would break legitimate long encodes, and the
people who need a real timeout have a much better tool for it: their own context.

Which is why the caller's deadline is never overridden. If the context you pass already carries
one, afmpeg uses it exactly as given: you know what your job should cost and afmpeg does not.
The default only fills the gap left by a `context.Background()`, where otherwise nothing bounds
the run at all.

One detail follows from the serialisation: the clock starts when the invocation acquires its
slot, not when you called. Time spent queued behind another job does not eat the budget, so a
30-second deadline means 30 seconds of work rather than 30 seconds of waiting. A call that is
still queued when its own context is cancelled gives up immediately rather than waiting for the
job ahead of it.

## Why one invocation at a time

This one is not a safety default in the same sense. It is a property of what a `Runtime` is.

The expensive thing afmpeg does is compile the WebAssembly module, which is why you build a
`Runtime` once and keep it. What is shared is that compiled module and the wazero runtime around
it. What is not shared is the per-invocation state: the mounted filesystem, the captured output
streams, and the setjmp/longjmp snapshot store that lets a C `longjmp` in the guest unwind
correctly.

Serialising invocations is what keeps that per-invocation state simple and lock-free. Each run
gets a fresh store and a fresh mount, and nothing has to reason about two guests interleaving
inside one runtime.

The consequence is the thing to internalise: **a `Runtime` is safe to share and does not give
you parallelism.** Calling it from ten goroutines is correct, and it is also ten jobs in a
queue. Parallelism comes from building more than one `Runtime`, and each pays the compile cost
once, and after that they are independent. The queue is context-aware, so a caller with a short
deadline is not stuck behind a long job; it gives up while queued rather than blocking for the
job in front.

## What these defaults do not protect you from

Being explicit about the edges matters more than the reassurance:

- **They do not apply to the native backend.** A native driver is an ordinary subprocess with
  your process's privileges; the memory cap is a wazero setting and there is no wazero. The
  deadline still applies, because `Run` imposes it above the backend seam. This is the trade you
  accept when you opt in, and it is why WASM stays the default.
- **They do not bound your filesystem.** A job that writes a very large output fills whatever
  `afero.Fs` you gave it. With a `MemMapFs` that is host RAM, outside the guest's cap entirely.
  If the output size is attacker-influenced, bound it yourself.
- **They do not bound CPU.** A job can burn a core for the whole hour before the deadline fires.
- **They do not make the engine trusted.** They contain a compromised or malfunctioning decode;
  they do not verify that the module is the one you meant to run. That is what
  [release verification](release-verification.md) is for, and it is a separate mechanism with a
  separate failure mode.

## See also

- [Runtime options](../../reference/runtime-options.md): the exact values and how to change them
- [Reuse a Runtime across many invocations](../../how-to/reuse-a-runtime.md): the long-lived pattern
- [Limitations](../../reference/limitations.md): the other boundaries
