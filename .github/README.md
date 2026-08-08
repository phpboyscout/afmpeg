# afmpeg

**A pure-Go FFmpeg binding that runs on a virtual filesystem.** No CGO, no host
FFmpeg install, no temp files: FFmpeg is supplied as a separate WebAssembly module
and executed via [wazero](https://wazero.io/), with its I/O bridged to an
[`afero.Fs`](https://github.com/spf13/afero). Inputs and outputs can live entirely
in memory, and the whole thing cross-compiles to a single static binary.

> **This is a read-only mirror. The canonical repository is on GitLab:**
> **https://gitlab.com/phpboyscout/afmpeg**
>
> Issues and merge requests are handled there.

## Installing

The module path is the GitLab one:

```
go get gitlab.com/phpboyscout/afmpeg
```

`go get github.com/phpboyscout/afmpeg` will not work. The mirror is here for
browsing and reference only.

The WebAssembly build of FFmpeg lives in its own project,
[ffmpeg-wasi](https://gitlab.com/phpboyscout/ffmpeg-wasi), which is usable on its
own by anything that can host a WASI runtime.

## Documentation

Full documentation: **https://afmpeg.phpboyscout.uk**

Background on the design and what the sandbox does and does not buy you:
[Introducing afmpeg and ffmpeg-wasi](https://phpboyscout.uk/introducing-afmpeg-and-ffmpeg-wasi/).
