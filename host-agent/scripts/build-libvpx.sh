#!/usr/bin/env bash
# Builds the static libvpx (VP8 encoder only) that the host agent links into
# freedesk.exe, and installs it into host-agent/third_party/libvpx, where the
# cgo directives in internal/vpx look for it.
#
# Run it from an MSYS2 UCRT64 shell: the same gcc that cgo uses must build the
# library, or the two halves of the exe disagree about the C runtime. CI and
# the release workflow run exactly this script.
#
# The source is pinned to a commit, not just a tag: a tag can be moved, a
# commit hash cannot. To update libvpx, change both lines together (see
# docs/RELEASING.md).
#
# Static only (--disable-shared): no libvpx DLL is ever produced, so none can
# end up as a dependency of the exe. cmd/importcheck checks the result.
set -euo pipefail

LIBVPX_TAG=v1.17.0
LIBVPX_COMMIT=6df3ec34557879fff673706f4a1d9fbd0f3a6f0e
LIBVPX_URL=https://chromium.googlesource.com/webm/libvpx

root=$(cd "$(dirname "$0")/.." && pwd) # host-agent
out="$root/third_party/libvpx"
work="$root/third_party/.build/libvpx"

if [ -f "$out/BUILD-COMMIT" ] && [ -f "$out/lib/libvpx.a" ] &&
  [ "$(cat "$out/BUILD-COMMIT")" = "$LIBVPX_COMMIT" ]; then
  echo "libvpx $LIBVPX_TAG ($LIBVPX_COMMIT) is already built in $out"
  exit 0
fi

# An existing checkout is reused, so a machine whose git cannot reach the
# server by itself (an antivirus that intercepts TLS, a proxy) can clone it
# with its own tools first. Either way the commit is checked below.
if [ ! -d "$work/src/.git" ]; then
  rm -rf "$work"
  mkdir -p "$work"
  # A tag checkout is a detached HEAD by design; the commit is checked below.
  # (git also warns that the annotated tag "is not a commit": harmless.)
  git -c advice.detachedHead=false clone --quiet --depth 1 --branch "$LIBVPX_TAG" "$LIBVPX_URL" "$work/src"
fi
actual=$(git -C "$work/src" rev-parse HEAD)
if [ "$actual" != "$LIBVPX_COMMIT" ]; then
  echo "libvpx checkout is at $actual, expected $LIBVPX_COMMIT ($LIBVPX_TAG); refusing to build" >&2
  exit 1
fi

# A compiler named by CC has to be one the makefiles' /bin/sh can run. The
# workflows (and the PowerShell set-up in host-agent/README.md) export CC for
# cgo as a Windows path, D:\...\ucrt64\bin\gcc.exe; configure copies it into
# the makefiles, and /bin/sh reads its backslashes as escapes ("D:a_temp...
# gcc.exe: command not found"). It is the same compiler cgo uses, which is the
# point, so it is kept and only its path is converted.
case "${CC:-}" in
*\\* | [A-Za-z]:*)
  CC=$(cygpath -u "$CC")
  export CC
  ;;
esac
echo "compiler: ${CC:-gcc} ($(${CC:-gcc} -dumpfullversion))"

rm -rf "$work/build" "$out"
mkdir -p "$work/build"
cd "$work/build"
# The VP8 encoder and nothing else: no decoder, no VP9, no tools. Runtime CPU
# detection keeps the SSE2/SSSE3/AVX2 code paths an ffmpeg build would have.
../src/configure \
  --target=x86_64-win64-gcc \
  --prefix="$out" \
  --enable-static \
  --disable-shared \
  --enable-vp8-encoder \
  --disable-vp8-decoder \
  --disable-vp9 \
  --enable-runtime-cpu-detect \
  --as=nasm \
  --disable-examples \
  --disable-tools \
  --disable-docs \
  --disable-unit-tests \
  --disable-install-bins \
  --disable-install-srcs \
  --disable-webm-io \
  --disable-libyuv
make -j"$(nproc)"
make install

# The license travels with the library; cmd/notices quotes it in the license
# texts embedded into freedesk.exe.
cp ../src/LICENSE ../src/PATENTS "$out/"
echo "$LIBVPX_COMMIT" >"$out/BUILD-COMMIT"
echo "libvpx $LIBVPX_TAG built into $out"
