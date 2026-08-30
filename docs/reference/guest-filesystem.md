---
title: The guest filesystem
description: What the engine sees: how paths resolve, which locations are synthetic, which syscalls are supported, and what the bridge does not implement.
date: 2026-08-02
tags: [reference, vfs, filesystem, paths]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# The guest filesystem

Every path in a job spec is resolved by afmpeg's filesystem bridge, not by the host operating
system. This page is the contract: what resolves where, which locations are synthetic, and which
operations are simply not implemented.

Why it is built this way is in [the vfs bridge](../explanation/components/vfs-bridge.md).

## How a path resolves

The `afero.Fs` you pass to `Run`/`RunJob`/`Probe`/`Frames` is mounted at the guest's root.

- **Relative and absolute paths are the same path.** Every name is cleaned to an absolute form
  before it is matched or resolved, so `in/clip.mp4`, `./in/clip.mp4` and `/in/clip.mp4` all
  reach the same file.
- **Everything resolves against your filesystem**, except the synthetic locations below.
- **The host filesystem is never touched.** The bridge calls methods on the injected `afero.Fs`
  and nothing else. There is no `os` call and no preopened host directory. With a `MemMapFs`,
  a guest write resolves entirely in RAM.
- **Directories are not created for you.** Your filesystem's own semantics apply; `MemMapFs`
  creates parents implicitly, `OsFs` does not.

## Synthetic locations

| Path | Backed by | Present |
|---|---|---|
| `/tmp` (and everything under it) | a separate, private in-memory filesystem | always |
| `/dev/null` | a discard sink: writes succeed and vanish, reads report EOF | always |
| `/dev/urandom` | the host's `crypto/rand` | always |
| `/dev/random` | the same source as `/dev/urandom`, non-blocking | always |
| `/dev/afmpeg-progress` | a write-only device feeding the progress reporter | **only while `WithProgress` is active** |

Consequences worth knowing before you go looking for a file:

- **Guest scratch writes do not appear in your filesystem, and you cannot read them back.**
  `/tmp` is isolated on purpose, so a muxer's temporary file cannot collide with your output,
  but a fresh in-memory filesystem is created for each invocation and there is no public option to
  supply your own or to inspect it afterwards. Anything you need to keep must be written to a
  path outside `/tmp`.
- **A file you place at `/dev/null`, `/dev/urandom` or `/dev/random` is unreachable.** The
  overlay always wins.
- **`/dev/afmpeg-progress` is conditional**, which is what stops it shadowing a real file: with
  no progress channel attached, the path resolves against your filesystem like any other name.
- **`/dev/urandom` is load-bearing, not a convenience.** Without it the Matroska muxer hangs
  forever during initialisation. It is served unconditionally for that reason.

## Supported operations

| Operation | Behaviour |
|---|---|
| open | Access mode plus `O_APPEND`, `O_CREAT`, `O_EXCL`, `O_TRUNC`, `O_SYNC`. `O_DIRECTORY`, `O_NOFOLLOW`, `O_NONBLOCK`, `O_DSYNC` and `O_RSYNC` have no afero equivalent and are dropped. |
| read / write | At the current offset. A zero-length request short-circuits to `(0, nil)`. |
| pread / pwrite | At an absolute offset, leaving the file offset alone. This is the path the MP4 muxer uses to patch the `moov` atom under `+faststart`. |
| seek | All three whences; anything else is `EINVAL`. Backward seek on a file open for writing is supported and tested, because it is the case the whole design turns on. |
| truncate | Resizes the file. |
| sync / datasync | Both map to the afero file's `Sync`; afero has no data-only flush. |
| stat / lstat | Identical, since the afero backends afmpeg targets do not model symlinks. |
| mkdir, rename, unlink, rmdir | POSIX-faithful: unlinking a directory is `EISDIR`, rmdir on a non-directory is `ENOTDIR`. |

Two conventions the guest depends on:

- **EOF is `n == 0` with no error**, matching POSIX `read` rather than Go's `io.EOF`.
- **Errors are mapped to real errnos.** An afero `*os.PathError` becomes the `ENOENT`, `EEXIST`
  or `EISDIR` the guest would see on a real filesystem, so libav's error handling behaves
  normally.

## What is not implemented

These return `ENOSYS` to the guest:

| Operation | Consequence |
|---|---|
| `chmod` | Permission changes are ignored; the modes on your `afero.Fs` are whatever you set. |
| `link`, `symlink`, `readlink` | No links of either kind. A workflow that expects a symlinked input will not work. |
| `utimens` | Timestamps cannot be set. |

There is also no networking of any kind. See [Limitations](limitations.md#what-afmpeg-cannot-do-at-all).

## Metadata the bridge cannot report faithfully

`stat` is synthesised from an `fs.FileInfo`, which does not carry everything a real one does:

| Field | Reported as |
|---|---|
| link count | always `1`, the POSIX minimum; afero backends do not track links |
| inode number | always `0` |
| access, modification and change time | all three mirror the modification time |

Nothing in libav depends on these, but a consumer inspecting them should not read meaning into
them.

## Renaming across the `/tmp` boundary

A rename resolves **both** names against the backend the *source* path belongs to. Within your
filesystem, or within `/tmp`, that is exactly right. A rename from `/tmp` to one of your paths
(or the reverse) is not a move between the two filesystems and will not put the file where the
name suggests. Nothing afmpeg emits does this; it is worth knowing if you drive the engine with
a hand-written job spec.

## See also

- [The vfs bridge](../explanation/components/vfs-bridge.md): why it works this way
- [Run over an in-memory filesystem](../how-to/run-in-memory.md)
- [Limitations](limitations.md)
