#!/usr/bin/env bash
# Build the Windows installer with NSIS.
#
#   MAKENSIS=/path/to/makensis ./packaging/windows/build-nsis.sh
#
# MAKENSIS may point at a Linux-built makensis; the NSIS data directory is
# located automatically (NSISDIR) or must be exported.
#
# Output: dist/MenuVex-Offline-Setup-<version>-x64.exe
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
VERSION="${VERSION:-$(cat "$ROOT/packaging/VERSION")}"
MAKENSIS="${MAKENSIS:-makensis}"
DIST="$ROOT/dist"
WORK="$(mktemp -d)"
OUT="$DIST/MenuVex-Offline-Setup-${VERSION}-x64.exe"
ICON="$ROOT/../src-tauri/icons/icon.ico"

PYTHON_BIN="$(command -v python3 || command -v python)"
[ -n "$PYTHON_BIN" ] || { echo "python3 not found" >&2; exit 1; }

trap 'rm -rf "$WORK"' EXIT

command -v "$MAKENSIS" >/dev/null 2>&1 || { echo "makensis not found: $MAKENSIS" >&2; exit 1; }
[ -f "$ICON" ] || { echo "missing icon: $ICON" >&2; exit 1; }

mkdir -p "$DIST"

echo "==> Building Windows GUI executable"
(
    cd "$ROOT"
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
        -trimpath \
        -ldflags "-H windowsgui -s -w -X main.version=$VERSION" \
        -o "$WORK/MenuVexOffline.exe" \
        ./cmd/coffish-desktop
)

APP_SIZE_KB=$(( ($(stat -c%s "$WORK/MenuVexOffline.exe") + 1023) / 1024 + 512 ))

echo "==> Compiling NSIS installer"
"$MAKENSIS" -V2 -NOCD \
    -DVERSION="$VERSION" \
    -DAPP_EXE="$WORK/MenuVexOffline.exe" \
    -DAPP_ICON="$ICON" \
    -DOUT_FILE="$OUT" \
    -DAPP_SIZE_KB="$APP_SIZE_KB" \
    "$HERE/menuvex-offline.nsi"

[ -f "$OUT" ] || { echo "installer was not produced" >&2; exit 1; }

echo "==> Validating"
"$PYTHON_BIN" - "$OUT" "$WORK/MenuVexOffline.exe" <<'PY'
import struct, sys
installer, payload = sys.argv[1], sys.argv[2]

def pe_info(path):
    data = open(path, "rb").read()
    assert data[:2] == b"MZ", f"{path}: missing MZ header"
    pe = struct.unpack_from("<I", data, 0x3C)[0]
    assert data[pe:pe+4] == b"PE\0\0", f"{path}: missing PE signature"
    machine = struct.unpack_from("<H", data, pe + 4)[0]
    magic = struct.unpack_from("<H", data, pe + 24)[0]
    subsystem = struct.unpack_from("<H", data, pe + 0x5C)[0]
    return data, machine, magic, subsystem

inst, machine, magic, subsystem = pe_info(installer)
# makensis uses the 32-bit NSIS stub; the installed application payload is x64.
assert machine == 0x014c, f"expected 32-bit NSIS stub, got 0x{machine:04x}"
assert magic == 0x10B, "expected PE32 NSIS stub"
assert subsystem == 2, f"expected GUI subsystem, got {subsystem}"
assert b"Nullsoft" in inst, "NSIS signature not found"
assert len(inst) > 1_000_000, "installer payload looks truncated"

pay, pmachine, pmagic, psub = pe_info(payload)
assert pmachine == 0x8664, f"payload is not x86-64 (0x{pmachine:04x})"
assert pmagic == 0x20B, "payload is not PE32+"
assert psub == 2, "payload is not a GUI (no-console) executable"
print(f"installer: PE32 NSIS stub, GUI, {len(inst)} bytes")
print(f"payload:   PE32+ x86-64, GUI subsystem, {len(pay)} bytes")
PY

# Prove that the exact executable is embedded: compile a verification copy with
# the payload stored uncompressed, then look for the payload bytes verbatim.
echo "==> Verifying payload is embedded"
"$MAKENSIS" -V1 -NOCD \
    -DUNCOMPRESSED_PAYLOAD=1 \
    -DVERSION="$VERSION" \
    -DAPP_EXE="$WORK/MenuVexOffline.exe" \
    -DAPP_ICON="$ICON" \
    -DOUT_FILE="$WORK/verify.exe" \
    -DAPP_SIZE_KB="$APP_SIZE_KB" \
    "$HERE/menuvex-offline.nsi"

"$PYTHON_BIN" - "$WORK/verify.exe" "$WORK/MenuVexOffline.exe" <<'PY'
import sys
bundle = open(sys.argv[1], "rb").read()
payload = open(sys.argv[2], "rb").read()
# Compare a large interior slice to keep the check fast and unambiguous.
needle = payload[4096:4096 + 262144]
offset = bundle.find(needle)
assert offset != -1, "payload bytes are not present in the installer"
print(f"payload embedded verbatim at offset {offset}")
PY

echo "OK: $OUT ($(stat -c%s "$OUT") bytes)"