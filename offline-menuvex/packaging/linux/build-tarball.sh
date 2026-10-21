#!/usr/bin/env bash
# Build a portable (no-install) Linux archive: binary + desktop entry + icons.
# This is a convenience archive, not an installer; use build-deb.sh or the
# AppImage for packaged installs.
#
# Output: dist/MenuVex-Offline-<version>-linux-<arch>.tar.gz
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
VERSION="${VERSION:-$(cat "$ROOT/packaging/VERSION")}"
GOARCH_TARGET="${1:-${ARCH:-amd64}}"
DIST="$ROOT/dist"
WORK="$(mktemp -d)"
NAME="MenuVex-Offline-${VERSION}-linux-${GOARCH_TARGET}"
OUT="$DIST/${NAME}.tar.gz"
ICONS="$ROOT/../src-tauri/icons"

trap 'rm -rf "$WORK"' EXIT
mkdir -p "$DIST" "$WORK/$NAME/icons"

echo "==> Building portable archive for linux/$GOARCH_TARGET"
(
    cd "$ROOT"
    CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH_TARGET" go build \
        -tags production \
        -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$VERSION" \
        -o "$WORK/$NAME/menuvex-offline" ./cmd/coffish-desktop
)

cp "$ROOT/packaging/linux/menuvex-offline.desktop" "$WORK/$NAME/"
cp "$ICONS/icon.png" "$WORK/$NAME/icons/menuvex-offline.png"
cp "$ROOT/packaging/linux/postinst" "$WORK/$NAME/" 2>/dev/null || true
rm -f "$WORK/$NAME/postinst"

cat > "$WORK/$NAME/README.txt" <<'EOF'
MenuVex Offline — portable Linux build
======================================

Run:
    ./menuvex-offline

The application serves its embedded web UI on http://127.0.0.1:8787 and opens a
native window. If no GTK3/WebKit2 host is available it falls back to a
Chromium/Chrome app window, then to the default browser.

Requirements for the native window:
    python3, python3-gi, gir1.2-gtk-3.0, gir1.2-webkit2-4.1 (or 4.0)

Data location:
    ~/.config/CoffishDesktop/offline.sqlite (database, license, uploads)
    Nothing is written next to the executable, so this archive can be replaced
    or deleted without losing operational data.

Desktop integration (optional):
    install -Dm644 menuvex-offline.desktop ~/.local/share/applications/menuvex-offline.desktop
    install -Dm644 icons/menuvex-offline.png ~/.local/share/icons/hicolor/512x512/apps/menuvex-offline.png
    update-desktop-database ~/.local/share/applications

This archive is not a system package. Use menuvex-offline_*.deb for a managed
installation (and for automatic dependency handling).
EOF

tar -C "$WORK" -czf "$OUT" "$NAME"
tar -tzf "$OUT" | head -10
echo "OK: $OUT ($(stat -c%s "$OUT") bytes)"
