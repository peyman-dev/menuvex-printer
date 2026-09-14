#!/usr/bin/env bash
# Install the Novex Printer Agent on Linux (user-level, no sudo required
# unless you choose a system-wide prefix).
#
#   ./scripts/install-linux.sh [--system] [--bin path/to/binary]
#
# --system installs to /usr/local/bin (needs sudo); default is ~/.local/bin.
set -euo pipefail

PREFIX="$HOME/.local/bin"
BIN=""

while [ $# -gt 0 ]; do
  case "$1" in
    --system) PREFIX="/usr/local/bin" ;;
    --bin) BIN="$2"; shift ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

if [ -z "$BIN" ]; then
  # Build from source if Go is available, else look for a prebuilt binary.
  if command -v go >/dev/null 2>&1; then
    echo "Building..."
    ( cd "$(dirname "$0")/.." && CGO_ENABLED=0 go build -trimpath -o /tmp/novex-printer-agent ./cmd/agent )
    BIN="/tmp/novex-printer-agent"
  elif [ -f ./dist/novex-printer-agent-linux-amd64 ]; then
    BIN="./dist/novex-printer-agent-linux-amd64"
  else
    echo "Go not found and no prebuilt binary. Pass --bin <path>." >&2
    exit 1
  fi
fi

mkdir -p "$PREFIX"
INSTALL="install -m 0755"
if [ ! -w "$PREFIX" ]; then
  echo "Need write access to $PREFIX (re-run with sudo or drop --system)."
  exit 1
fi
$INSTALL "$BIN" "$PREFIX/novex-printer-agent"
echo "Installed: $PREFIX/novex-printer-agent"

# Start-on-login (writes ~/.config/autostart/*.desktop).
"$PREFIX/novex-printer-agent" --install-autostart

echo
echo "USB note: your user needs write access to /dev/usb/lp*."
echo "  sudo usermod -aG lp \"\$USER\"   # then log out/in"
echo "  (or install scripts/99-novex-printer-agent.rules as a udev rule)"
echo
echo "Token: $("$PREFIX/novex-printer-agent" --print-token)"
echo "Run:   $PREFIX/novex-printer-agent"
