#!/usr/bin/env bash
# Administrator runs this setup helper, NEVER the agent itself.
set -euo pipefail
if [[ $# -lt 2 || $# -gt 3 || ! "$1" =~ ^[[:xdigit:]]{4}$ || ! "$2" =~ ^[[:xdigit:]]{4}$ ]]; then
  echo "Usage: sudo $0 VENDOR_ID PRODUCT_ID [--remove] (4 hexadecimal digits each)" >&2; exit 2
fi
[[ $EUID -eq 0 ]] || { echo 'Run this setup helper with sudo, not the Agent.' >&2; exit 1; }
vendor=${1,,}; product=${2,,}; rule="/etc/udev/rules.d/70-menuvex-${vendor}-${product}.rules"
if [[ ${3:-} == --remove ]]; then
  rm -f -- "$rule"
elif [[ $# == 2 ]]; then
  # Only this VID/PID, only USB devices, ACL only to the active local login session.
  printf 'SUBSYSTEM=="usb", ENV{DEVTYPE}=="usb_device", ATTR{idVendor}=="%s", ATTR{idProduct}=="%s", TAG+="uaccess"\n' "$vendor" "$product" > "$rule"
  chmod 0644 "$rule"
else
  echo 'Only --remove is supported as the third argument.' >&2; exit 2
fi
udevadm control --reload-rules
printf 'Rules updated for %s:%s. Unplug and reconnect this printer. Run Agent as the logged-in desktop user.\n' "$vendor" "$product"
