#!/usr/bin/env bash
# Validate a directory of built installers without installing them.
#
#   ./packaging/verify-artifacts.sh [directory]     (default: dist)
#
# Checks are structural: package metadata, ELF/PE/Mach-O headers, AppImage type-2
# magic, squashfs superblocks and the UDIF trailer. Runtime/install tests are
# performed separately (see docs/offline/installers.md and the CI workflow).
set -euo pipefail

DIR="${1:-dist}"
PYTHON_BIN="$(command -v python3 || command -v python)"

[ -d "$DIR" ] || { echo "no such directory: $DIR" >&2; exit 1; }

echo "==> SHA256SUMS"
if [ -f "$DIR/SHA256SUMS" ]; then
    ( cd "$DIR" && sha256sum -c SHA256SUMS )
else
    echo "no SHA256SUMS in $DIR (skipped)"
fi

echo "==> Debian packages"
for deb in "$DIR"/*.deb; do
    [ -e "$deb" ] || continue
    pkg="$(dpkg-deb -f "$deb" Package)"
    arch="$(dpkg-deb -f "$deb" Architecture)"
    ver="$(dpkg-deb -f "$deb" Version)"
    files="$(dpkg-deb -c "$deb")"
    printf '%s\n' "$files" | grep -q '/usr/bin/menuvex-offline$'
    printf '%-45s %s %s %s (%s bytes)\n' "$(basename "$deb")" "$pkg" "$ver" "$arch" "$(stat -c%s "$deb")"
done

echo "==> AppImages"
for appimage in "$DIR"/*.AppImage; do
    [ -e "$appimage" ] || continue
    "$PYTHON_BIN" - "$appimage" <<'PY'
import struct, sys
path = sys.argv[1]
data = open(path, "rb").read()
assert data[:4] == b"\x7fELF", "not an ELF runtime"
assert data[8:11] == b"AI\x02", "missing AppImage type-2 magic"
idx = data.rfind(b"hsqs")
assert idx > 0, "no squashfs image"
assert struct.unpack_from("<HH", data, idx + 28) == (4, 0), "not a squashfs 4.0 image"
inodes = struct.unpack_from("<I", data, idx + 4)[0]
print(f"{path}: ELF + AI\\x02, squashfs at {idx}, {inodes} inodes, {len(data)} bytes")
PY
done

echo "==> Windows installers"
for exe in "$DIR"/*.exe; do
    [ -e "$exe" ] || continue
    "$PYTHON_BIN" - "$exe" <<'PY'
import struct, sys
path = sys.argv[1]
data = open(path, "rb").read()
assert data[:2] == b"MZ", "missing MZ"
pe = struct.unpack_from("<I", data, 0x3C)[0]
assert data[pe:pe+4] == b"PE\0\0", "missing PE signature"
machine = struct.unpack_from("<H", data, pe + 4)[0]
subsystem = struct.unpack_from("<H", data, pe + 0x5C)[0]
assert subsystem == 2, "installer is not a GUI application"
print(f"{path}: PE machine 0x{machine:04x}, GUI subsystem, {len(data)} bytes")
PY
    if command -v 7zz >/dev/null 2>&1; then
        report="$(7zz l "$exe" 2>/dev/null || true)"
        printf '%s\n' "$report" | grep -q 'Type = Nsis'
        printf '%s\n' "$report" | grep -q 'MenuVexOffline.exe'
        printf '%s\n' "$report" | grep -q 'Uninstall.exe'
        printf '%-45s NSIS archive: payload + Uninstall.exe present\n' "$(basename "$exe")"
    fi
done

echo "==> Disk images"
for dmg in "$DIR"/*.dmg; do
    [ -e "$dmg" ] || continue
    "$PYTHON_BIN" - "$dmg" <<'PY'
import struct, sys
path = sys.argv[1]
data = open(path, "rb").read()
koly = data.rfind(b"koly")
assert koly != -1, "missing koly trailer"
assert len(data) - koly == 512, "koly block is not the last 512 bytes"
version, header_size = struct.unpack_from(">II", data, koly + 4)
assert header_size == 512, "unexpected koly header size"
length = struct.unpack_from(">Q", data, koly + 0x20)[0]
assert length > 0, "empty data fork"
print(f"{path}: UDIF koly v{version}, data fork {length} bytes, {len(data)} bytes")
PY
done

echo "==> Portable archives"
for tar in "$DIR"/*.tar.gz; do
    [ -e "$tar" ] || continue
    listing="$(tar -tzf "$tar")"
    printf '%s\n' "$listing" | grep -c '/menuvex-offline$' >/dev/null
    printf '%-45s contains menuvex-offline (%s bytes)\n' "$(basename "$tar")" "$(stat -c%s "$tar")"
done

echo "ALL STRUCTURAL CHECKS PASSED"
