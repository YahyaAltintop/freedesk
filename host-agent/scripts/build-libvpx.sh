#!/usr/bin/env bash
# Builds the static libvpx (VP8 encoder only) that the host agent links into
# itself, and installs it into host-agent/third_party/libvpx, where the cgo
# directives in internal/vpx look for it.
#
# On Windows, run it from an MSYS2 UCRT64 shell: the same gcc that cgo uses
# must build the library, or the two halves of the exe disagree about the C
# runtime. CI and the release workflow run exactly this script.
#
# On macOS, run it from any shell with the Xcode command line tools, plus nasm
# for the Intel half (brew install nasm). It builds the library for Apple
# silicon and for Intel and joins the two into one universal archive, which
# an agent of either kind links against; FD_LIBVPX_ARCHES=arm64 (or x86_64)
# builds just that one, which is quicker for development. Every half targets
# MACOS_MIN, the oldest macOS the agent supports.
#
# The source is pinned to a commit, not just a tag: a tag can be moved, a
# commit hash cannot. To update libvpx, change both lines together (see
# docs/RELEASING.md).
#
# Static only (--disable-shared): no libvpx DLL or dylib is ever produced, so
# none can end up as a dependency of the agent. cmd/importcheck checks the
# result.
set -euo pipefail

LIBVPX_TAG=v1.17.0
LIBVPX_COMMIT=6df3ec34557879fff673706f4a1d9fbd0f3a6f0e
LIBVPX_URL=https://chromium.googlesource.com/webm/libvpx

# The oldest macOS the agent runs on. The agent itself must be built for the
# same one (MACOSX_DEPLOYMENT_TARGET), or the linker warns that the library
# was built for a newer macOS than the program. Go's build cache keys on
# neither that variable nor this library, so a release build of the agent
# starts from an empty cache (go build -a).
MACOS_MIN=13.0

root=$(cd "$(dirname "$0")/.." && pwd) # host-agent
out="$root/third_party/libvpx"
work="$root/third_party/.build/libvpx"

# What the library is built for, recorded next to it so that a library built
# for something else (another set of Mac architectures, say) is rebuilt
# rather than reused.
case "$(uname -s)" in
Darwin)
  # shellcheck disable=SC2086 # split the list into words, on purpose
  arches=$(printf '%s\n' ${FD_LIBVPX_ARCHES:-arm64 x86_64} | sort -u | xargs)
  for arch in $arches; do
    case "$arch" in
    arm64 | x86_64) ;;
    *)
      echo "FD_LIBVPX_ARCHES: unknown architecture '$arch' (arm64 or x86_64)" >&2
      exit 1
      ;;
    esac
  done
  target="macos$MACOS_MIN $arches"
  ;;
*)
  target=x86_64-win64-gcc
  ;;
esac

# A library built before the target was recorded is a Windows one.
if [ -f "$out/BUILD-COMMIT" ] && [ -f "$out/lib/libvpx.a" ] &&
  [ "$(cat "$out/BUILD-COMMIT")" = "$LIBVPX_COMMIT" ] &&
  [ "$(cat "$out/BUILD-TARGET" 2>/dev/null || echo x86_64-win64-gcc)" = "$target" ]; then
  echo "libvpx $LIBVPX_TAG ($LIBVPX_COMMIT) for $target is already built in $out"
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

# The VP8 encoder and nothing else: no decoder, no VP9, no tools. Runtime CPU
# detection keeps the SIMD code paths an ffmpeg build would have: SSE2, SSSE3
# and AVX2 on Intel, Neon and its later extensions on Arm.
options=(
  --enable-static
  --disable-shared
  --enable-vp8-encoder
  --disable-vp8-decoder
  --disable-vp9
  --enable-runtime-cpu-detect
  --disable-examples
  --disable-tools
  --disable-docs
  --disable-unit-tests
  --disable-install-bins
  --disable-install-srcs
  --disable-webm-io
  --disable-libyuv
)

build_windows() {
  # A compiler named by CC has to be one the makefiles' /bin/sh can run. The
  # workflows (and the PowerShell set-up in host-agent/README.md) export CC
  # for cgo as a Windows path, D:\...\ucrt64\bin\gcc.exe; configure copies it
  # into the makefiles, and /bin/sh reads its backslashes as escapes ("D:a_temp
  # ...gcc.exe: command not found"). It is the same compiler cgo uses, which
  # is the point, so it is kept and only its path is converted.
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
  ../src/configure --target=x86_64-win64-gcc --prefix="$out" --as=nasm "${options[@]}"
  make -j"$(nproc)"
  make install
}

build_darwin() {
  echo "compiler: ${CC:-gcc} ($(${CC:-gcc} --version | head -n 1))"
  # Everything compiled below, configure's own test programs included,
  # targets MACOS_MIN.
  export MACOSX_DEPLOYMENT_TARGET=$MACOS_MIN

  rm -rf "$out"
  local arch libs=() asm
  for arch in $arches; do
    # Arm's SIMD code is C intrinsics; only the Intel half has assembly.
    asm=
    if [ "$arch" = x86_64 ]; then
      if ! command -v nasm >/dev/null; then
        echo "nasm is needed for the Intel half of libvpx: brew install nasm" >&2
        exit 1
      fi
      asm=--as=nasm
    fi
    rm -rf "$work/build-$arch" "$work/dist-$arch"
    mkdir -p "$work/build-$arch"
    # libvpx names macOS targets after the Darwin release, and every name
    # from darwin20 to darwin25, the newest 1.17.0 knows, sets the same flags:
    # -arch and the SDK. The macOS the library runs on is MACOS_MIN, whatever
    # the name. Naming it matters all the same: on a Darwin newer than it
    # knows, configure would otherwise fall back to a generic build with no
    # SIMD at all, without a word.
    (
      cd "$work/build-$arch"
      if [ -n "$asm" ]; then
        # nasm records no minimum macOS in its objects unless asked to, and
        # the linker then warns about every one of them ("no platform load
        # command found"). NASMENV hands each nasm run the pragma that
        # records it; the leading "!" makes "!" the separator between the
        # options, as the pragma itself has spaces.
        export NASMENV="!--pragma!macho build_version macos,${MACOS_MIN//./,}"
      fi
      ../src/configure --target="$arch-darwin25-gcc" --prefix="$work/dist-$arch" \
        --enable-pic --extra-cflags="-mmacosx-version-min=$MACOS_MIN" \
        ${asm:+"$asm"} "${options[@]}"
      make -j"$(sysctl -n hw.ncpu)"
      make install
    )
    libs+=("$work/dist-$arch/lib/libvpx.a")
  done

  # One archive with a slice per architecture; the linker takes the one it
  # links for. The installed headers are the same for every architecture.
  mkdir -p "$out/lib"
  if [ "${#libs[@]}" -eq 1 ]; then
    cp "${libs[0]}" "$out/lib/libvpx.a"
  else
    lipo -create "${libs[@]}" -output "$out/lib/libvpx.a"
  fi
  cp -R "$work/dist-${arches%% *}/include" "$out/include"
  lipo -info "$out/lib/libvpx.a"
}

case "$target" in
x86_64-win64-gcc) build_windows ;;
*) build_darwin ;;
esac

# The license travels with the library; cmd/notices quotes it in the license
# texts embedded into the agent.
cp "$work/src/LICENSE" "$work/src/PATENTS" "$out/"
echo "$target" >"$out/BUILD-TARGET"
echo "$LIBVPX_COMMIT" >"$out/BUILD-COMMIT"
echo "libvpx $LIBVPX_TAG for $target built into $out"
