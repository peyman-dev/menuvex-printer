# Linux: Ubuntu/Debian desktop

Use a normal logged-in graphical user with an unlocked Secret Service (e.g. GNOME Keyring). A headless system/keyring-less session fails closed. Never run the Agent as root. AppImage needs an installed desktop session/tray integration and may need distribution-specific FUSE compatibility; verify on the target distro.

## Build prerequisites and commands

```sh
sudo apt-get update
sudo apt-get install -y build-essential pkg-config libusb-1.0-0-dev libdbus-1-dev \
  libgtk-3-dev libwebkit2gtk-4.1-dev libayatana-appindicator3-dev \
  librsvg2-dev patchelf libssl-dev
# Install stable Rust via the official rustup instructions; Node.js 22 separately.
npm ci
npm test
cargo test --manifest-path src-tauri/Cargo.toml --no-default-features
npm run tauri -- build --target x86_64-unknown-linux-gnu --bundles deb,appimage
```

These native steps could not execute in the sandbox because package/toolchain endpoints were inaccessible. `.deb` is configured for desktop dependencies; Noto Arabic is also embedded in the binary. Tauri creates desktop integration; autostart uses the logged-in desktop user and can be disabled in Settings. Install a verified `.deb` via `sudo apt install ./<actual-file>.deb`, then run the Agent without sudo. No wildcard device rule is shipped during package installation because the intended VID/PID is not known yet.

## One-time USB setup

Find the exact vendor/product IDs from `lsusb` or the Agent's USB discovery. An administrator runs the included helper with those actual four-hex-digit IDs:

```sh
sudo bash scripts/linux-usb-permissions.sh VVVV PPPP
```

`VVVV PPPP` are placeholders to replace, not literal valid IDs. The helper validates inputs, writes **one** `/etc/udev/rules.d/70-menuvex-VVVV-PPPP.rules`, and reloads udev:

```udev
SUBSYSTEM=="usb", ENV{DEVTYPE}=="usb_device", ATTR{idVendor}=="VVVV", ATTR{idProduct}=="PPPP", TAG+="uaccess"
```

Order 70 runs before systemd's seat-late ACL assignment. Replug the printer; the active local desktop user obtains access via logind/udev. No `MODE=0666`, no all-USB rule, no broad vendor wildcard, no permanent root Agent. This handles active-seat desktop use; custom non-logind/remote POS sessions need a reviewed device-specific group rule and explicit group membership instead.

On Linux the transport enables libusb auto-detach for the claimed printer interface, with automatic reattach on release. Avoid simultaneous CUPS/vendor/Agent use. USB presence does not prove claim permissions; test print is the access test. The helper is part of technician setup, not invoked with hidden privilege escalation from React. A guided privileged setup broker is a remaining installer usability gate.

Remove only the installed device rule when decommissioning:

```sh
sudo bash scripts/linux-usb-permissions.sh VVVV PPPP --remove
```

Disable autostart, Quit, remove package/AppImage, remove its desktop/autostart entries if present. DB/dedup data and Secret Service entry are intentionally retained; delete only after explicit retention approval. Do not remove shared printer drivers or another application’s udev rules.
