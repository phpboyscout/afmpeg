---
title: Reuse a Runtime across many invocations
description: Compile the wasm module once at startup and share one afmpeg.Runtime for the process lifetime — and how its one-at-a-time serialisation affects throughput.
date: 2026-06-29
tags: [how-to, runtime, performance, concurrency]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Reuse a Runtime across many invocations

`afmpeg.New` **compiles** the wasm module — the single most expensive step. Do it **once**,
keep the `*Runtime`, and run as many jobs through it as you like. Re-creating a `Runtime` per
job recompiles the module every time and throws away the win.

This guide covers the long-lived pattern: building the `Runtime` at startup, sharing it across
requests, and what its one-at-a-time serialisation means for throughput. For a single
end-to-end run see [run over an in-memory filesystem](run-in-memory.md).

## Build once at startup, hold for the process lifetime

Compile during startup and store the `Runtime` on whatever owns your service's lifetime — a
struct field, a long-lived value, etc. `Run`, `RunJob`, and `Probe` are all safe to call
concurrently from many goroutines on the same `Runtime`.

```go
// Service holds the compiled engine for the process lifetime.
type Service struct {
    engine *afmpeg.Runtime
}

func NewService(ctx context.Context, modulePath string) (*Service, error) {
    // Compile the module exactly once.
    engine, err := afmpeg.New(ctx, afmpeg.WithModuleFile(modulePath))
    if err != nil {
        return nil, err
    }

    return &Service{engine: engine}, nil
}

// Reuse it on every request — no recompilation.
func (s *Service) Transcode(ctx context.Context, in []byte) ([]byte, error) {
    fs := afero.NewMemMapFs()
    _ = afero.WriteFile(fs, "in.mp4", in, 0o644)

    cmd := afmpeg.NewCommand(
        afmpeg.WithInput("in.mp4"),
        afmpeg.WithOutput("out.mp4", afmpeg.VideoCodec("libopenh264"), afmpeg.AudioCodec("aac")),
    )

    if _, err := s.engine.RunJob(ctx, fs, cmd); err != nil {
        return nil, err
    }

    return afero.ReadFile(fs, "out.mp4")
}

// Release the compiled module on shutdown.
func (s *Service) Close(ctx context.Context) error { return s.engine.Close(ctx) }
```

Each call gets its **own** `afero.Fs`, so jobs never see each other's files even though they
share the engine.

## Throughput: invocations serialise

A `Runtime` runs **one invocation at a time** — `Run`/`RunJob`/`Probe` take an internal lock,
so concurrent callers queue rather than execute in parallel (spec
[0004](../development/specs/0004-runtime-and-api.md) D-0004-B). That keeps the engine safe to
share, but it means a single `Runtime` does **not** give you parallelism.

To actually run jobs in parallel, build **more than one** `Runtime` and hand work out across
them — each compiles the module once:

```go
// A fixed fleet of engines for parallel work.
engines := make([]*afmpeg.Runtime, n)
for i := range engines {
    engines[i], err = afmpeg.New(ctx, afmpeg.WithModuleBytes(moduleBytes))
    if err != nil {
        return err
    }
}
// Round-robin or hand each worker its own engine; Close them all on shutdown.
```

`WithModuleBytes` avoids re-reading the file `n` times. A built-in `RuntimePool` that manages
this for you is on the [roadmap](../development/specs/0006-hardening-roadmap.md) (§2E); for now
a small fixed fleet is all it takes.

## Checklist

- **Compile once.** One `New` per process (or per pool slot), not per job.
- **Share freely.** Concurrent `Run`/`RunJob`/`Probe` on one `Runtime` are safe; they serialise.
- **Parallelise with more `Runtime`s**, not more calls on one.
- **`Close` on shutdown** to release the compiled module and the wazero runtime.
