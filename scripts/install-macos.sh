#!/usr/bin/env bash
# Install the Novex Printer Agent on macOS (user-level).
#
#   ./scripts/install-macos.sh [--bin path/to/binary]
set -euo pipefail

PREFIX="$HOME/bin"
BIN=""

while [ $# -gt 0 ]; do
  case "$1" in
    --bin) BIN="$2"; shift ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

if [ -z "$BIN" ]; then
  if command -v go >/dev/null 2>&1; then
    echo "Building..."
    ( cd "$(dirname "$0")/.." && CGO_ENABLED=0 go build -trimpath -o /tmp/novex-printer-agent ./cmd/agent )
    BIN="/tmp/novex-printer-agent"
  else
    echo "Go not found. Pass --bin <path> with a prebuilt darwin binary." >&2
    exit 1
  fi
fi

mkdir -p "$PREFIX"
install -m 0755 "$BIN" "$PREFIX/novex-printer-agent"
echo "Installed: $PREFIX/novex-printer-agent"

# Start-on-login (writes ~/Library/LaunchAgents/*.plist).
"$PREFIX/novex-printer-agent" --install-autostart

echo
echo "USB note: add the printer to CUPS as a Raw queue, e.g.:"
echo "  lpadmin -p OCOM -E -v \"usb://OCOM/Thermal?serial=ABC123\" -m raw"
echo "See docs/USB.md."
echo
echo "Token: $("$PREFIX/novex-printer-agent" --print-token)"
echo "Run:   $PREFIX/novex-printer-agent"
