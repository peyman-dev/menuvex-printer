#!/usr/bin/env bash
# Build a real type-2 AppImage for MenuVex Offline.
#
#   APPIMAGETOOL=/path/to/appimagetool ./packaging/appimage/build-appimage.sh [x86_64|aarch64]
#
# The AppImage is an ELF runtime (magic 0x41 0x49 0x02 at offset 8) with a
# squashfs image appended; it is not a renamed executable. Building requires
# appimagetool and mksquashfs from squashfs-tools.
#
# Output: dist/MenuVex-Offline-<version>-<arch>.AppImage
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
VERSION="${VERSION:-$(cat "$ROOT/packaging/VERSION")}"
ARCH_IN="${1:-${ARCH:-x86_64}}"
case "$ARCH_IN" in
    x86_64|amd64)  ARCH=x86_64;  GOARCH=amd64 ;;
    aarch64|arm64) ARCH=aarch64; GOARCH=arm64 ;;
    *) echo "unsupported arch: $ARCH_IN" >&2; exit 2 ;;
esac
DIST="$ROOT/dist"
WORK="$(mktemp -d)"
APPDIR="$WORK/MenuVexOffline.AppDir"
OUT="$DIST/MenuVex-Offline-${VERSION}-${ARCH}.AppImage"
ICONS="$ROOT/../src-tauri/icons"
APPIMAGETOOL="${APPIMAGETOOL:-appimagetool}"
RUNTIME_FILE="${RUNTIME_FILE:-}"

PYTHON_BIN="$(command -v python3 || command -v python)"
[ -n "$PYTHON_BIN" ] || { echo "python3 not found" >&2; exit 1; }

trap 'rm -rf "$WORK"' EXIT

command -v "$APPIMAGETOOL" >/dev/null 2>&1 || { echo "appimagetool not found: $APPIMAGETOOL" >&2; exit 1; }
command -v mksquashfs >/dev/null 2>&1 || { echo "mksquashfs not found" >&2; exit 1; }
mkdir -p "$DIST"

echo "==> Building MenuVex Offline $VERSION for linux/$GOARCH"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/icons/hicolor/512x512/apps"
(
    cd "$ROOT"
    CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build \
        -trimpath -buildvcs=false \
        -ldflags "-s -w -X main.version=$VERSION" \
        -o "$APPDIR/usr/bin/menuvex-offline" \
        ./cmd/coffish-desktop
)
chmod 0755 "$APPDIR/usr/bin/menuvex-offline"

cp "$HERE/AppRun" "$APPDIR/AppRun"
chmod 0755 "$APPDIR/AppRun"
cp "$ROOT/packaging/linux/menuvex-offline.desktop" "$APPDIR/menuvex-offline.desktop"
sed -i 's/^Exec=.*/Exec=menuvex-offline/' "$APPDIR/menuvex-offline.desktop"
cp "$ICONS/icon.png" "$APPDIR/menuvex-offline.png"
cp "$ICONS/icon.png" "$APPDIR/usr/share/icons/hicolor/512x512/apps/menuvex-offline.png"

echo "==> Packaging with appimagetool ($ARCH)"
TOOL_ARGS=()
# If appimagetool itself is an AppImage, avoid requiring FUSE in CI.
if [ "$(head -c 11 "$APPIMAGETOOL" 2>/dev/null | tail -c 3 | od -An -tx1 | tr -d ' \n')" = "414902" ]; then
    TOOL_ARGS+=(--appimage-extract-and-run)
fi
if [ -n "$RUNTIME_FILE" ]; then
    # Offline builds: reuse the type-2 runtime extracted from a local
    # appimagetool AppImage instead of downloading it from GitHub Releases.
    TOOL_ARGS+=(--runtime-file "$RUNTIME_FILE")
fi
ARCH="$ARCH" "$APPIMAGETOOL" "${TOOL_ARGS[@]}" "$APPDIR" "$OUT" >/dev/null
[ -f "$OUT" ] || { echo "AppImage was not produced" >&2; exit 1; }

echo "==> Validating container structure"
"$PYTHON_BIN" - "$OUT" "$WORK/offset" <<'PY'
import struct, sys
path = sys.argv[1]
data = open(path, "rb").read()
assert data[:4] == b"\x7fELF", "runtime is not an ELF file"
assert data[8:11] == b"AI\x02", "missing AppImage type-2 magic at offset 8"
assert data[4] == 2, "runtime is not 64-bit ELF"
# The squashfs superblock starts right after the runtime; appimagetool aligns
# the image, so search the tail for the 'hsqs' magic instead of assuming an
# offset.
idx = data.rfind(b"hsqs")
assert idx > 0, "no squashfs image found"
# Validate the squashfs superblock fields at the magic.
block_size = struct.unpack_from("<I", data, idx + 12)[0]
inode_count = struct.unpack_from("<I", data, idx + 4)[0]
assert 4096 <= block_size <= 1048576, f"unexpected squashfs block size {block_size}"
assert inode_count > 0, "empty squashfs image"
print(f"ELF runtime + AI\\x02 magic (type 2), squashfs at offset {idx} "
      f"(block size {block_size}, inodes {inode_count}), {len(data)} bytes")
open(sys.argv[2], "w").write(str(idx))
PY
OFFSET="$(cat "$WORK/offset")"

echo "==> Validating bundled payload (AppImage extract mode, no FUSE)"
rm -rf "$WORK/extract"
mkdir -p "$WORK/extract"
(
    cd "$WORK/extract"
    "$OUT" --appimage-extract >/dev/null 2>&1 || true
)
APPRUN="$WORK/extract/squashfs-root/AppRun"
BIN="$WORK/extract/squashfs-root/usr/bin/menuvex-offline"
if [ ! -x "$BIN" ]; then
    # Cross-architecture build: the runtime cannot execute here, so unpack the
    # squashfs directly at the validated offset and inspect the payload.
    echo "runtime is not executable on this host; unpacking squashfs at $OFFSET"
    command -v unsquashfs >/dev/null 2>&1 || { echo "unsquashfs not found" >&2; exit 1; }
    rm -rf "$WORK/extract/squashfs-root"
    unsquashfs -o "$OFFSET" -d "$WORK/extract/squashfs-root" "$OUT" >/dev/null
fi
if [ ! -x "$BIN" ]; then
    echo "extraction did not produce the application binary" >&2
    exit 1
fi
ELF_MACHINE="$(head -c 20 "$BIN" | od -An -tx1 | tr -d ' \n' | cut -c37-40)"
case "$GOARCH:$ELF_MACHINE" in
    amd64:3e00|arm64:b700) echo "payload architecture matches $GOARCH" ;;
    *) echo "payload architecture mismatch (ELF machine $ELF_MACHINE for $GOARCH)" >&2; exit 1 ;;
esac
test -x "$APPRUN"
test -f "$WORK/extract/squashfs-root/menuvex-offline.desktop"
if [ "$GOARCH" = "amd64" ]; then
    "$BIN" -version
else
    echo "arm64 payload built; execution requires an arm64 host (not available here)"
fi
echo "extract test OK"

echo "OK: $OUT ($(stat -c%s "$OUT") bytes)"
