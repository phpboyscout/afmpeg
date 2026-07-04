# 0029 — meson cross-compile toolchain

Status: **IMPLEMENTED** (Phase 4 of the [implementation roadmap](../implementation-roadmap.md).
A build-system capability spec, born from a spike during [0019](0019-text-and-subtitles.md): a
second cross-compile toolchain — **meson** — in ffmpeg-wasi's `build/deps.sh` beside the existing
autotools/custom-configure path, so meson-only libraries cross-compile to `wasm32-wasi`. Delivered
as `write_meson_cross` + `build_harfbuzz` + `build_fribidi` (image gains `meson`/`ninja`/`xz-utils`);
its first consumer is 0019's burn-in (harfbuzz + fribidi → libass). Gotcha resolved: `-DHB_NO_MT`
for harfbuzz (meson pulls the threads dep → clang emits wasm atomics wazero rejects) and stripping
`-pthread` from the generated `.pc` files.)
Date: 2026-07-04
Parent: [0018](0018-lgpl-encoder-expansion.md) (the `deps.sh` external-lib cross-compile pattern);
[0019](0019-text-and-subtitles.md) (the first consumer — burn-in fonts/shaping); [0022](0022-build-size-matrix.md)
Owns: **R-BUILD-MESON** — meson cross-compilation of external libs to wasm32-wasi.

## 1. Why

`build/deps.sh` cross-compiles every external library with **autotools** (`./configure`) or a
library's **own configure** (x264, libvpx). That covered every lib through [0018](0018-lgpl-encoder-expansion.md).
[0019](0019-text-and-subtitles.md) (text & subtitles) is the first to need libraries that **only
ship a meson build**: **harfbuzz** and **fribidi**. And it is not optional — FFmpeg n8.1.2 raised
the bar:

- `drawtext_filter_deps="libfreetype libharfbuzz"` — the `drawtext` filter now **requires
  harfbuzz** (not just freetype). Without libharfbuzz, configure silently soft-disables
  `drawtext` (the same class of quiet soft-disable as libvpx's encoders in 0018).
- **libass requires fribidi** (a mandatory `PKG_CHECK_MODULES` — `--disable-fribidi` is a no-op),
  so the `subtitles`/`ass` burn-in filters need it too.

harfbuzz and fribidi dropped autotools; their release tarballs carry only `meson.build`. So the
engine needs a **meson cross-compile capability** to ship any subtitle/text burn-in at all. This
spec adds it once, generically, so future meson-only deps (e.g. dav1d for [0023](0023-hevc-and-av1.md))
reuse it.

## 2. Scope

**In:** a reusable meson cross-compile path in `build/deps.sh` and its image prerequisites —
- image packages: `meson`, `ninja-build`, `xz-utils` (harfbuzz/fribidi ship `.tar.xz`);
- a generated **meson cross-file** for `wasm32-wasi` derived from `toolchain.sh`'s `$CFLAGS`;
- the first two consumers: `build_harfbuzz` (C++) and `build_fribidi` (C), static, into `$PREFIX`.

**Out (referenced, not owned):** the autotools/own-configure paths (unchanged, still used for
freetype/libass/the 0018 libs); *which* libs a profile bundles ([0022](0022-build-size-matrix.md));
the burn-in filters + libass wiring themselves ([0019](0019-text-and-subtitles.md) — this spec only
supplies the toolchain they stand on).

## 3. Approach / design — the meson cross-file

`fetch_tarball` already handles `.tar.xz` (it uses `tar xf`, which auto-detects compression). A
`write_meson_cross` helper emits `/tmp/wasi-cross.txt` from the exported toolchain environment:

```ini
[binaries]
c = 'clang'
cpp = 'clang++'
ar = 'llvm-ar'
strip = 'llvm-strip'
pkg-config = 'pkg-config'
[host_machine]
system = 'wasi'
cpu_family = 'wasm32'
cpu = 'wasm32'
endian = 'little'
[properties]
needs_exe_wrapper = true            # cross: never run the wasm output during the build
[built-in options]
c_args   = [ <each word of $CFLAGS, single-quoted> ]
c_link_args = [ '--target=wasm32-wasip1', '--sysroot=…', <WASI_EMULATED_LIBS> ]
cpp_args / cpp_link_args = <same>
```

The load-bearing pieces: `needs_exe_wrapper = true` (meson must not try to *run* the wasm binaries
its sanity check builds — cross mode assumes success on compile+link); `system = wasi` +
`cpu_family = wasm32`; and threading the **exact** `$CFLAGS` (the `--target`, `--sysroot`, the WASM
feature flags, the SjLj lowering, the `-I$PREFIX/include` and `-D_WASI_EMULATED_*`) into
`c_args`/`cpp_args` so meson compiles identically to the rest of the build. A lib is then:

```sh
meson setup build --cross-file /tmp/wasi-cross.txt --prefix="$PREFIX" \
  --default-library=static --buildtype=minsize <-Dfeature toggles>
ninja -C build install
```

harfbuzz needs `-Dfreetype=enabled` (built first, autotools) and everything else off
(`glib/gobject/cairo/icu/tests/docs/utilities`); it is **C++**, so the final engine link already
pulls `-lc++`/`-lc++abi`. fribidi needs only `-Ddocs=false -Dtests=false -Dbin=false`.

## 4. Build impact

- `build/Dockerfile`: `meson ninja-build xz-utils` added to the apt layer.
- `build/deps.sh`: `write_meson_cross` + `build_harfbuzz` + `build_fribidi`; `fetch_tarball` uses
  `tar xf` (xz-aware). All gated to the **intermediate** profile (they exist for 0019's burn-in).
- `build/driver.sh`: `dep_rank` orders the new archives (consumers before providers: `ass` <
  `harfbuzz` < the `freetype`/`fribidi` leaves), since wasm-ld has no `--start-group`.
- No afmpeg / job-spec impact — this is purely a build capability.

## 5. Licensing & size

harfbuzz is **MIT**, fribidi **LGPL-2.1** — both LGPL-compatible, so they ride the **default
variant** with no GPL trigger. Size: harfbuzz is the heavyweight of the pair (~3 MB static, a
shaping engine); fribidi is small. Both land in **intermediate**, not lean ([0022](0022-build-size-matrix.md)).
**Build-time cost:** harfbuzz compiles slowly under `-Oz` (~5–8 min), lengthening CI — an accepted
cost, isolated to the cached deps layer.

## 6. Decisions & open questions

- **D-0029-A — add meson as a *second* toolchain, don't migrate.** The autotools/own-configure
  path stays for every lib that has one; meson is used only where a lib is meson-only.
- **D-0029-B — derive the cross-file from `$CFLAGS`, don't hand-maintain it.** One source of truth
  for the wasm target flags; a `toolchain.sh` change flows into meson automatically.
- **D-0029-C — `needs_exe_wrapper = true`.** The sandbox can't run wasm during the build; meson's
  cross mode must assume compile+link success (which is what the rest of `deps.sh` relies on too).
- **Open:** whether to pin meson/ninja versions (currently the image's apt versions); whether future
  meson deps (dav1d for 0023) want a shared `build_meson_lib` helper vs per-lib functions.

## 7. Requirements

- **R-BUILD-MESON** — `build/deps.sh` can cross-compile a meson-based C or C++ library to
  `wasm32-wasi` (static, asm-free, single-threaded, into `$PREFIX` with a usable `.pc`), via a
  cross-file generated from the toolchain, with no impact on the autotools path.

## 8. Validation

Spike (2026-07-04, throwaway wasi-sdk container): **fribidi** (C, 139 KB), **freetype** (autotools,
725 KB), and **harfbuzz** (C++, 3.1 MB) all cross-compiled and installed with working `.pc` files.
The end-to-end proof is [0019](0019-text-and-subtitles.md)'s burn-in: the built module runs the
`drawtext` and `subtitles` filters against a mounted font, exercised by afmpeg's integration tests.

## 9. Definition of done

The meson cross-file recipe + `write_meson_cross`/`build_harfbuzz`/`build_fribidi` are in
`build/deps.sh`, the image carries meson/ninja/xz, the four build combos build, and the capability
is proven by 0019's burn-in filters working end-to-end. The decisions (D-0029-A…C) are recorded;
the open questions (version pinning, a shared meson helper) are deferred to the next meson consumer.
