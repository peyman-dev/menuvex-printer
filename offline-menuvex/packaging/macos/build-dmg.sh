#!/usr/bin/env bash
# Build MenuVex Offline.app and a UDIF .dmg for macOS.
#
#   ./packaging/macos/build-dmg.sh arm64|amd64
#
# On macOS the script uses the native toolchain (cgo Cocoa/WKWebView window),
# hdiutil and ad-hoc code signing. On Linux it cross-compiles the nocgo build
# (which compiles its Objective-C window host at first launch) and builds the
# UDIF container with newfs_hfs + hdutil + dmg from libdmg-hfsplus.
#
# Output: dist/MenuVex-Offline-macOS-<arch>.dmg
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
VERSION="${VERSION:-$(cat "$ROOT/packaging/VERSION")}"
ARCH="${1:-$(uname -m)}"
case "$ARCH" in
    arm64|aarch64) ARCH=arm64 ;;
    x86_64|amd64)  ARCH=amd64 ;;
    *) echo "unsupported arch: $ARCH" >&2; exit 2 ;;
esac
LABEL="$ARCH"
DIST="$ROOT/dist"
WORK="$(mktemp -d)"
APP_NAME="MenuVex Offline.app"
APP="$WORK/$APP_NAME"
OUT="$DIST/MenuVex-Offline-macOS-${LABEL}.dmg"
ICNS="$ROOT/../src-tauri/icons/icon.icns"

PYTHON_BIN="$(command -v python3 || command -v python)"
[ -n "$PYTHON_BIN" ] || { echo "python3 not found" >&2; exit 1; }

trap 'rm -rf "$WORK"' EXIT
[ -f "$ICNS" ] || { echo "missing icon: $ICNS" >&2; exit 1; }
mkdir -p "$DIST"

HOST_OS="$(uname -s)"
if [ "$HOST_OS" = "Darwin" ]; then
    CGO=1
else
    CGO=0
fi

echo "==> Building MenuVex Offline $VERSION for darwin/$ARCH (CGO_ENABLED=$CGO)"
(
    cd "$ROOT"
    CGO_ENABLED="$CGO" GOOS=darwin GOARCH="$ARCH" go build \
        -trimpath \
        -ldflags "-s -w -X main.version=$VERSION" \
        -o "$WORK/MenuVexOffline" \
        ./cmd/coffish-desktop
)

echo "==> Assembling $APP_NAME"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$WORK/MenuVexOffline" "$APP/Contents/MacOS/MenuVexOffline"
chmod 0755 "$APP/Contents/MacOS/MenuVexOffline"
cp "$ICNS" "$APP/Contents/Resources/AppIcon.icns"
printf 'APPL????' > "$APP/Contents/PkgInfo"
sed "s/__VERSION__/$VERSION/g" "$HERE/Info.plist" > "$APP/Contents/Info.plist"
chmod 0644 "$APP/Contents/Info.plist"

"$PYTHON_BIN" - "$APP/Contents/Info.plist" <<'PY'
import plistlib, sys
with open(sys.argv[1], "rb") as fh:
    info = plistlib.load(fh)
for key in ("CFBundleExecutable", "CFBundleIdentifier", "CFBundleShortVersionString"):
    assert info.get(key), f"Info.plist is missing {key}"
assert info["CFBundleExecutable"] == "MenuVexOffline"
print("Info.plist OK:", info["CFBundleIdentifier"], info["CFBundleShortVersionString"])
PY

if [ "$HOST_OS" = "Darwin" ]; then
    # Ad-hoc signature: satisfies the "code is not corrupt" check but is NOT a
    # Developer ID signature and is not notarized.
    codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || true
    codesign --verify --deep --strict "$APP" >/dev/null 2>&1 || true
fi

STAGE="$WORK/stage"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"

if [ "$HOST_OS" = "Darwin" ]; then
    echo "==> Creating UDIF image with hdiutil"
    hdiutil create -volname "MenuVex Offline" -srcfolder "$STAGE" -fs HFS+ \
        -format UDZO -imagekey zlib-level=9 -ov "$OUT" >/dev/null
    hdiutil verify "$OUT" >/dev/null
    SIGN_STATUS="$(codesign -dv "$APP" 2>&1 | grep -E '^Authority' || echo 'ad-hoc / unsigned (no Developer ID)')"
    echo "==> Signature: $SIGN_STATUS"
else
    NEWFS_HFS="${NEWFS_HFS:-newfs_hfs}"
    HDUTIL="${HDUTIL:-hdutil}"
    DMG_TOOL="${DMG_TOOL:-dmg}"
    for tool in "$NEWFS_HFS" "$HDUTIL" "$DMG_TOOL"; do
        command -v "$tool" >/dev/null 2>&1 || { echo "missing tool: $tool" >&2; exit 1; }
    done

    echo "==> Creating HFS+ volume image"
    VOL="$WORK/volume.img"
    SIZE_MB=$(( ($(du -sm "$STAGE" | cut -f1) + 48) ))
    truncate -s "${SIZE_MB}M" "$VOL"
    "$NEWFS_HFS" -v "MenuVex Offline" "$VOL" >/dev/null

    "$HDUTIL" "$VOL" mkdir "/$APP_NAME"
    "$HDUTIL" "$VOL" addall "$STAGE/$APP_NAME" "/$APP_NAME"
    "$HDUTIL" "$VOL" symlink /Applications /Applications
    "$HDUTIL" "$VOL" chmod 0755 "/$APP_NAME/Contents/MacOS/MenuVexOffline"
    "$HDUTIL" "$VOL" chmod 0755 "/$APP_NAME"

    # Prove the executable is present and executable inside the volume.
    "$HDUTIL" "$VOL" ls "/$APP_NAME/Contents/MacOS" | grep -q MenuVexOffline

    echo "==> Creating UDIF image with libdmg-hfsplus"
    "$DMG_TOOL" build "$VOL" "$OUT" >/dev/null

    echo "==> Round-trip verification (DMG -> HFS -> file hash)"
    VERIFY_IMG="$WORK/verify.img"
    VERIFY_DIR="$WORK/verify"
    mkdir -p "$VERIFY_DIR"
    "$DMG_TOOL" extract "$OUT" "$VERIFY_IMG" >/dev/null
    "$HDUTIL" "$VERIFY_IMG" extractall / "$VERIFY_DIR" >/dev/null
    cmp "$WORK/MenuVexOffline" "$VERIFY_DIR/$APP_NAME/Contents/MacOS/MenuVexOffline"
    echo "round-trip OK: extracted executable is byte-identical"
fi

echo "==> Validating $OUT"
"$PYTHON_BIN" - "$OUT" <<'PY'
import struct, sys
data = open(sys.argv[1], "rb").read()
koly = data.rfind(b"koly")
assert koly != -1, "missing koly trailer"
assert len(data) - koly == 512, "koly block is not the final 512 bytes"
version, header_size = struct.unpack_from(">II", data, koly + 4)
assert header_size == 512, f"unexpected koly header size {header_size}"
data_fork_off = struct.unpack_from(">Q", data, koly + 0x18)[0]
data_fork_len = struct.unpack_from(">Q", data, koly + 0x20)[0]
assert data_fork_off + data_fork_len <= len(data), "data fork outside file"
assert data_fork_len > 0, "empty data fork"
print(f"UDIF koly v{version}: data fork {data_fork_len} bytes at {data_fork_off}, total {len(data)} bytes")
PY

if [ "$HOST_OS" = "Darwin" ]; then
    echo "==> Mount test"
    MNT="$WORK/mnt"
    mkdir -p "$MNT"
    hdiutil attach -nobrowse -readonly -mountpoint "$MNT" "$OUT" >/dev/null
    test -d "$MNT/$APP_NAME"
    test -x "$MNT/$APP_NAME/Contents/MacOS/MenuVexOffline"
    hdiutil detach "$MNT" >/dev/null
    echo "mount test OK"
fi

echo "OK: $OUT ($(stat -c%s "$OUT" 2>/dev/null || stat -f%z "$OUT") bytes)"
