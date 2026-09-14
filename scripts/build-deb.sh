#!/usr/bin/env bash
# Build a Linux .deb package (amd64) for the agent.
# Requires: go, dpkg-deb. Run on Linux.
#
#   ./scripts/build-deb.sh [version]   # default version 1.0.0
set -euo pipefail

VERSION="${1:-1.0.0}"
ROOT="$(dirname "$0")/.."
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

PKG="$STAGE/novex-printer-agent_${VERSION}_amd64"
mkdir -p "$PKG/DEBIAN" "$PKG/usr/bin" "$PKG/etc/udev/rules.d"

cat > "$PKG/DEBIAN/control" <<EOF
Package: novex-printer-agent
Version: $VERSION
Section: utils
Priority: optional
Architecture: amd64
Maintainer: MenuVex <support@menuvex.ir>
Description: Local bridge between web browsers and printers (LAN/TCP + USB).
 Single static binary, no Java, no database.
EOF

cat > "$PKG/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
udevadm control --reload 2>/dev/null || true
echo "Installed novex-printer-agent. Run it once to generate the config + token:"
echo "  novex-printer-agent --print-token"
echo "Enable start-on-login per user with: novex-printer-agent --install-autostart"
EOF
chmod 0755 "$PKG/DEBIAN/postinst"

( cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X github.com/menuvex/novex-printer-agent/internal/platform.Version=$VERSION" \
  -o "$PKG/usr/bin/novex-printer-agent" ./cmd/agent )

cp "$ROOT/scripts/99-novex-printer-agent.rules" "$PKG/etc/udev/rules.d/"

mkdir -p "$ROOT/dist"
dpkg-deb --build "$PKG" "$ROOT/dist/novex-printer-agent_${VERSION}_amd64.deb"
echo "Built: dist/novex-printer-agent_${VERSION}_amd64.deb"
