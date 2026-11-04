#!/usr/bin/env bash
# Builds FreeDesk.app, the host agent for macOS, and FreeDesk.dmg, the one file
# that is downloaded: a disk image that opens on the app and a shortcut to
# Applications, to drag it onto. CI and the release workflow run exactly this
# script.
#
# The program inside the app is one universal binary: cmd/host built for
# Apple silicon and for Intel, joined with lipo. It links libvpx in, so
# scripts/build-libvpx.sh must have built it for both (its default on a Mac),
# and it embeds the license texts, so those are written first, as for the
# Windows exe: go run ./cmd/notices -o internal/licenses/generated/LICENSES.txt.
# cmd/importcheck then checks the program, so an app that needs a library
# macOS does not ship, has no video encoder or lacks those texts is never
# signed or packed.
#
# The app is always signed. With FD_SIGN_IDENTITY, a "Developer ID
# Application" certificate in a keychain, the app and the disk image are
# signed so that Apple can notarize them (the release workflow does that
# next). Without it the app's signature is ad-hoc: the least macOS needs
# before it lets the operator give the program the permissions it asks for,
# but with no publisher, so macOS refuses to open the app until the operator
# chooses Open Anyway in System Settings.
#
# A release has Apple notarize the app and staples the ticket to the app
# itself before the disk image is made around it, so that the copy the
# operator drags out opens without asking Apple: --no-dmg stops after the
# app, and --dmg-only, afterwards, packs the app found in --out. Without a
# certificate the two halves run as one.
#
# Usage: scripts/bundle-mac.sh [--version X.Y.Z] [--out DIR] [--no-dmg | --dmg-only]
#   --version  the version the program reports and the app is stamped with
#              (default: the source's own, appVersion in cmd/host/main.go)
#   --out      where FreeDesk.app and FreeDesk.dmg go (default: dist; a
#              relative DIR counts from host-agent, wherever this is run from)
#   --no-dmg   stop after FreeDesk.app
#   --dmg-only make FreeDesk.dmg from the FreeDesk.app already in --out
#   FD_LDFLAGS        more -ldflags for go build: the release's -X settings
#   FD_SIGN_IDENTITY  the codesign identity; unset for an ad-hoc signature
set -euo pipefail

# The oldest macOS the agent runs on, as in scripts/build-libvpx.sh.
MACOS_MIN=13.0

root=$(cd "$(dirname "$0")/.." && pwd) # host-agent
cd "$root"

version=""
out="$root/dist"
make_app=1
make_dmg=1
while [ $# -gt 0 ]; do
  case "$1" in
  --version)
    version=${2:?--version needs a value}
    shift 2
    ;;
  --out)
    out=${2:?--out needs a folder}
    shift 2
    ;;
  --no-dmg)
    make_dmg=0
    shift
    ;;
  --dmg-only)
    make_app=0
    shift
    ;;
  *)
    echo "usage: scripts/bundle-mac.sh [--version X.Y.Z] [--out DIR] [--no-dmg | --dmg-only]" >&2
    exit 2
    ;;
  esac
done
if [ "$make_app" = 0 ] && [ "$make_dmg" = 0 ]; then
  echo "--no-dmg and --dmg-only together leave nothing to do" >&2
  exit 2
fi
if [ "$(uname -s)" != Darwin ]; then
  echo "bundle-mac.sh builds the macOS app, on a Mac" >&2
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$out"

if [ "$make_app" = 1 ]; then
  if [ -z "$version" ]; then
    version=$(sed -n 's/^var appVersion = "\(.*\)"$/\1/p' cmd/host/main.go)
  fi
  # What the app's Info.plist says: digits and dots only, so a pre-release
  # (0.9.0-rc1) is 0.9.0 there, and the program itself reports the whole tag.
  plist_version=${version%%-*}
  if ! printf '%s' "$plist_version" | grep -Eq '^[0-9]+(\.[0-9]+){0,2}$'; then
    echo "version '$version' does not start with X.Y.Z" >&2
    exit 1
  fi
  libvpx=third_party/libvpx/lib/libvpx.a
  for arch in arm64 x86_64; do
    if ! lipo "$libvpx" -verify_arch "$arch" 2>/dev/null; then
      echo "no $arch libvpx in $libvpx: run scripts/build-libvpx.sh (it builds both halves by default)" >&2
      exit 1
    fi
  done
  licenses=internal/licenses/generated/LICENSES.txt
  if [ ! -s "$licenses" ]; then
    echo "no license texts in $licenses: go run ./cmd/notices -o $licenses" >&2
    exit 1
  fi

  # Every C and Objective-C object, libvpx's included, is built for MACOS_MIN.
  # Go's build cache keys on neither the deployment target nor libvpx.a, so an
  # earlier build's objects could be for something else: the app is built from
  # an empty cache of its own.
  export CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=$MACOS_MIN GOCACHE="$tmp/gocache"
  ldflags="-s -w -X main.appVersion=$version ${FD_LDFLAGS:-}"
  for goarch in arm64 amd64; do
    echo "building cmd/host $version for $goarch"
    GOARCH=$goarch go build -trimpath -ldflags "$ldflags" -o "$tmp/freedesk-$goarch" ./cmd/host
  done
  lipo -create "$tmp/freedesk-arm64" "$tmp/freedesk-amd64" -output "$tmp/freedesk"
  for arch in arm64 x86_64; do
    minos=$(vtool -arch "$arch" -show-build "$tmp/freedesk" | awk '$1 == "minos" { print $2 }')
    if [ "$minos" != "$MACOS_MIN" ]; then
      echo "the $arch program is built for macOS '$minos', not $MACOS_MIN" >&2
      exit 1
    fi
  done
  # Checked before it is signed: no library macOS does not ship, built with
  # cgo, carrying exactly the license texts above.
  go run ./cmd/importcheck -licenses "$licenses" "$tmp/freedesk"

  app="$tmp/FreeDesk.app"
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
  cp "$tmp/freedesk" "$app/Contents/MacOS/freedesk"
  sed "s/@VERSION@/$plist_version/g" cmd/host/macos/Info.plist >"$app/Contents/Info.plist"
  plutil -lint -s "$app/Contents/Info.plist"
  printf 'APPL????' >"$app/Contents/PkgInfo"

  # The icon, from the Windows exe's: shrunk onto the grid of a macOS icon,
  # whose rounded square leaves a margin all round, at every size the Finder
  # and the Dock ask for up to the 256 pixels the picture has.
  iconset="$tmp/FreeDesk.iconset"
  mkdir "$iconset"
  sips -z 206 206 cmd/host/winres/icon.png --out "$tmp/icon-206.png" >/dev/null
  sips -p 256 256 "$tmp/icon-206.png" --out "$tmp/icon.png" >/dev/null
  for size in 16 32 128 256; do
    sips -z "$size" "$size" "$tmp/icon.png" --out "$iconset/icon_${size}x${size}.png" >/dev/null
    if [ $((size * 2)) -le 256 ]; then
      sips -z $((size * 2)) $((size * 2)) "$tmp/icon.png" --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
    fi
  done
  iconutil -c icns "$iconset" -o "$app/Contents/Resources/FreeDesk.icns"

  if [ -n "${FD_SIGN_IDENTITY:-}" ]; then
    # Hardened runtime and a secure timestamp: what notarization asks for.
    codesign --force --options runtime --timestamp --sign "$FD_SIGN_IDENTITY" "$app"
  else
    codesign --force --sign - "$app"
  fi
  codesign --verify --strict --verbose=2 "$app"

  # ditto copies what an app must keep: the signature, the links and the
  # file modes.
  rm -rf "$out/FreeDesk.app"
  ditto "$app" "$out/FreeDesk.app"
else
  if [ ! -d "$out/FreeDesk.app" ]; then
    echo "no FreeDesk.app in $out to pack: run without --dmg-only first" >&2
    exit 1
  fi
  version=$(plutil -extract CFBundleShortVersionString raw -o - "$out/FreeDesk.app/Contents/Info.plist")
fi

if [ "$make_dmg" = 1 ]; then
  # The disk image: the app and a link to Applications, read-only and
  # compressed. hdiutil now and then finds the new image busy while macOS
  # still looks at it, so it gets a few tries.
  rm -rf "$out/FreeDesk.dmg"
  stage="$tmp/dmg"
  mkdir "$stage"
  ditto "$out/FreeDesk.app" "$stage/FreeDesk.app"
  ln -s /Applications "$stage/Applications"
  for try in 1 2 3; do
    if hdiutil create -quiet -volname FreeDesk -srcfolder "$stage" -fs HFS+ -format UDZO -ov "$out/FreeDesk.dmg"; then
      break
    fi
    if [ "$try" = 3 ]; then
      echo "hdiutil could not make the disk image" >&2
      exit 1
    fi
    sleep 5
  done
  hdiutil verify -quiet "$out/FreeDesk.dmg"
  if [ -n "${FD_SIGN_IDENTITY:-}" ]; then
    codesign --force --timestamp --sign "$FD_SIGN_IDENTITY" "$out/FreeDesk.dmg"
    codesign --verify --strict --verbose=2 "$out/FreeDesk.dmg"
  fi
fi

echo "FreeDesk $version ($(lipo -archs "$out/FreeDesk.app/Contents/MacOS/freedesk"), macOS $MACOS_MIN or newer)"
# Identifier= is always there, so the pipe never ends on a reader that quits.
codesign -dv --verbose=2 "$out/FreeDesk.app" 2>&1 | grep -E '^(Identifier|Authority|Signature)='
if [ "$make_dmg" = 1 ]; then
  ls -l "$out/FreeDesk.dmg"
  shasum -a 256 "$out/FreeDesk.dmg"
fi
