#!/usr/bin/env bash
# Build a real Debian package for MenuVex Offline.
#
#   ./packaging/linux/build-deb.sh [amd64|arm64]
#
# Output: dist/menuvex-offline_<version>_<arch>.deb
# Requires: Go toolchain, dpkg-deb (>= 1.19 for --root-owner-group).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
VERSION="${VERSION:-$(cat "$ROOT/packaging/VERSION")}"
GOARCH_TARGET="${1:-${ARCH:-amd64}}"
DEB_ARCH="$GOARCH_TARGET"
DIST="$ROOT/dist"
STAGE="$(mktemp -d)"
OUT="$DIST/menuvex-offline_${VERSION}_${DEB_ARCH}.deb"

trap 'rm -rf "$STAGE"' EXIT

ICONS="$ROOT/../src-tauri/icons"
for f in icon.png 128x128.png 64x64.png 32x32.png; do
    if [ ! -f "$ICONS/$f" ]; then
        echo "missing icon: $ICONS/$f" >&2
        exit 1
    fi
done

mkdir -p "$DIST" \
         "$STAGE/DEBIAN" \
         "$STAGE/usr/bin" \
         "$STAGE/usr/share/applications" \
         "$STAGE/usr/share/icons/hicolor/512x512/apps" \
         "$STAGE/usr/share/icons/hicolor/128x128/apps" \
         "$STAGE/usr/share/icons/hicolor/64x64/apps" \
         "$STAGE/usr/share/icons/hicolor/32x32/apps" \
         "$STAGE/usr/share/doc/menuvex-offline"

echo "==> Building MenuVex Offline $VERSION for linux/$GOARCH_TARGET"
(
    cd "$ROOT"
    CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH_TARGET" go build \
        -trimpath \
        -ldflags "-s -w -X main.version=$VERSION" \
        -o "$STAGE/usr/bin/menuvex-offline" \
        ./cmd/coffish-desktop
)

cp "$HERE/menuvex-offline.desktop" "$STAGE/usr/share/applications/"
cp "$ICONS/icon.png"      "$STAGE/usr/share/icons/hicolor/512x512/apps/menuvex-offline.png"
cp "$ICONS/128x128.png"   "$STAGE/usr/share/icons/hicolor/128x128/apps/menuvex-offline.png"
cp "$ICONS/64x64.png"     "$STAGE/usr/share/icons/hicolor/64x64/apps/menuvex-offline.png"
cp "$ICONS/32x32.png"     "$STAGE/usr/share/icons/hicolor/32x32/apps/menuvex-offline.png"
cp "$HERE/postinst" "$STAGE/DEBIAN/postinst"
cp "$HERE/postrm"   "$STAGE/DEBIAN/postrm"
chmod 0755 "$STAGE/DEBIAN/postinst" "$STAGE/DEBIAN/postrm"

cat > "$STAGE/usr/share/doc/menuvex-offline/copyright" <<'EOF'
MenuVex Offline — offline café & restaurant management desktop.
Copyright (c) MenuVex. Bundled third-party components keep their own licenses.
Persian (Vazirmatn) web fonts are embedded in the application binary.
EOF

cat > "$STAGE/DEBIAN/control" <<EOF
Package: menuvex-offline
Version: $VERSION
Section: utils
Priority: optional
Architecture: $DEB_ARCH
Maintainer: Peyman Ahmadi <peymangeek@gmail.com>
Installed-Size: $(du -sk "$STAGE/usr" | cut -f1)
Depends: python3, python3-gi, gir1.2-gtk-3.0, gir1.2-webkit2-4.1 | gir1.2-webkit2-4.0
Recommends: fonts-vazirmatn
Homepage: https://menuvex.ir
Description: Offline café and restaurant management desktop
 MenuVex Offline is a self-contained desktop application for offline sales,
 kitchen tickets, menu management and thermal-printer handling. It serves its
 embedded web UI on a loopback port and opens a native GTK/WebKit2 window.
 .
 The SQLite database, license and uploads are stored per user in
 ~/.config/CoffishDesktop, so upgrading or removing the package never deletes
 operational data.
EOF

echo "==> Building $OUT"
dpkg-deb --root-owner-group -Zgzip --build "$STAGE" "$OUT" >/dev/null

echo "==> Validating"
dpkg-deb -I "$OUT"
dpkg-deb -c "$OUT"
test "$(dpkg-deb -f "$OUT" Package)" = "menuvex-offline"
test "$(dpkg-deb -f "$OUT" Architecture)" = "$DEB_ARCH"
LISTING="$(dpkg-deb --contents "$OUT")"
printf '%s\n' "$LISTING" | grep -c '/usr/bin/menuvex-offline$' >/dev/null
echo "OK: $OUT ($(stat -c%s "$OUT") bytes)"
