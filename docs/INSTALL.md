# Install & run the Novex Printer Agent

> راهنمای قدم‌به‌قدم فارسی (صفر تا صد): [SETUP_FA.md](SETUP_FA.md)

The agent is a **single static binary** — no Java, no runtime, no admin
rights needed for the default user-level install.

## 1. Get a binary

**Option A — download a release archive** (recommended for end users):

| Platform | File |
|---|---|
| Windows x64/ARM64 | `novex-printer-agent-windows-<arch>-v1.0.0.zip` → `NovexPrinterAgent.exe` |
| macOS Intel/Apple Silicon | `novex-printer-agent-darwin-<arch>-v1.0.0.tar.gz` |
| Linux x64/ARM64 | `novex-printer-agent-linux-<arch>-v1.0.0.tar.gz` or the `.deb` |

**Option B — build from source** (needs Go ≥ 1.21, no CGO, no downloads
besides the toolchain — the module has zero dependencies):

```bash
git clone https://github.com/menuvex/novex-printer-agent.git
cd novex-printer-agent
make build            # native binary
make build-all        # all 6 platform binaries into dist/
```

## 2. Install per OS

### Linux

```bash
./scripts/install-linux.sh            # → ~/.local/bin + autostart
./scripts/install-linux.sh --system   # → /usr/local/bin (needs write access)
```

For USB printers also grant device access (see [docs/USB.md](USB.md)):

```bash
sudo usermod -aG lp $USER   # log out/in afterwards
# and/or:
sudo cp scripts/99-novex-printer-agent.rules /etc/udev/rules.d/
sudo udevadm control --reload
```

`.deb` package: `./scripts/build-deb.sh 1.0.0` → `dist/*.deb`
(`dpkg -i` it; ships the udev rule; autostart stays per-user via the
binary's `--install-autostart`).

AppImage: any single-binary AppImage wrapper works (e.g. `appimagetool`
with `AppRun` exec'ing the binary). No recipe is shipped in v1 because
`~/.local/bin` + autostart already covers the use case.

### macOS

```bash
./scripts/install-macos.sh            # → ~/bin + LaunchAgent autostart
```

Add USB printers to CUPS as **Raw** queues (see [docs/USB.md](USB.md)):

```bash
lpadmin -p OCOM_USB -E -v "usb://OCOM/Thermal?serial=ABC123" -m raw
```

`.dmg` distribution: the release `.tar.gz` contains the one binary; wrap
it in a DMG with Disk Utility or `hdiutil create -volname "Novex Printer
Agent" -srcfolder <folder>` as part of your release process. (Code signing
+ notarization are operator steps — see below.)

`.app` bundle: v1 is intentionally a headless CLI (no tray UI); if you need
a dock icon, wrap the binary in an Automator/Platypus app that runs it.

### Windows

```powershell
powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1
```

This copies `NovexPrinterAgent.exe` to `%LOCALAPPDATA%\NovexPrinterAgent`
and registers start-on-login (hidden `.vbs` launcher in the Startup
folder — no console window).

Full installer: open `scripts/novex-printer-agent.iss` in
[Inno Setup](https://jrsoftware.org/isinfo.php) (free) and compile →
`dist/NovexPrinterAgent-Setup-1.0.0.exe`. It installs per-user (no admin),
registers autostart and launches the agent.

For USB printers, install the printer once in *Settings → Printers* (any
driver; the agent uses RAW passthrough). See [docs/USB.md](USB.md).

## 3. First run

```bash
novex-printer-agent
# → Config: .../config.json (token auto-generated on first run)
# → API token: <paste this into your web app>
# → Novex Printer Agent v1.0.0 starting on http://127.0.0.1:8765
```

Useful flags:

```
--print-token           print the API token and exit
--port 8765 --host 127.0.0.1   bind overrides (not persisted)
--config <path>         custom config file
--install-autostart / --uninstall-autostart / --autostart-status
--version
```

Verify: `curl http://127.0.0.1:8765/health` → `{"status":"ok",...}`,
then open the built-in console at http://127.0.0.1:8765/ in a browser,
paste the token and run a test print.

## 4. Autostart details

| OS | Login item |
|---|---|
| Linux | `~/.config/autostart/novex-printer-agent.desktop` |
| macOS | `~/Library/LaunchAgents/ir.menuvex.printeragent.plist` (KeepAlive) |
| Windows | `%APPDATA%\...\Startup\NovexPrinterAgent.vbs` (hidden) |

Managed entirely by the binary itself — no systemd/root/launchd-admin
steps needed.

## 5. Signing notes (for distributors)

Unsigned binaries trigger OS warnings (Windows SmartScreen, macOS
Gatekeeper "cannot verify developer"). For public distribution, sign with
your own certificates (orders of magnitude cheaper problem than QZ Tray's
Java stack, but still an operator step):

- **Windows:** `signtool sign` with an Authenticode cert; SmartScreen
  reputation builds over time / with an EV cert.
- **macOS:** `codesign --sign "Developer ID Application: …"` +
  `xcrun notarytool submit` + `staple`.
- **Linux:** no signing needed (GPG-sign the release archives if desired).
