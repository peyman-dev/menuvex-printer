# Windows 10/11

Target: x64 desktop, per-user NSIS installation, WebView2. Install the MSVC C++ build tools / Windows SDK, current stable Rust MSVC and Node 22. Consult [Tauri prerequisites](https://v2.tauri.app/start/prerequisites/).

```powershell
npm ci
npm test
cargo test --manifest-path src-tauri/Cargo.toml --no-default-features
npm run tauri -- build --target x86_64-pc-windows-msvc --bundles nsis
```

Artifacts: `src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/`. Commands are configured, not verified on Windows during this session. Installer is configured `currentUser`; Agent runs without permanent elevation. WebView2 installation and USB driver provisioning may require one-time admin assistance depending on enterprise policies. Autostart plugin registers login startup on first successful run, enabled by default and changeable in Settings.

## Direct USB limitation

Direct USB is an opt-in Cargo feature and is **omitted from default builds**. Add `--features libusb` to a Cargo/Tauri build to include it. `rusb` wraps the vendored libusb library, but on Windows the selected USB device/interface still needs a compatible binding, typically WinUSB (and supported libusb Windows backends such as UsbDk where configured). A vendor printer driver/USBPRINT installation is **not automatically accessible** via direct libusb. The Agent never installs, replaces or modifies a driver. Use the installed Windows print queue by default; if direct USB is specifically required, use a vendor-approved package or have an administrator carefully provision only the exact interface with a verified tool such as Zadig. Replacing the driver can disable vendor utilities/spooler printing; composite-device interfaces need particular care. The same printer cannot safely be owned concurrently by legacy WebUSB, another Agent and vendor software.

An optional USB fallback can be configured locally per USB profile to an explicit installed queue or private-LAN printer. It is empty by default, never inferred, and is used only for a confirmed failure before the first job byte. Partial write, timeout, unknown state or a failure after an earlier copy never falls back. See [USB failover policy](usb-failover.md).

The direct-USB Agent only enumerates USB Printer Class (0x07) bulk OUT interfaces. Vendor-specific models need a reviewed, model-specific transport/profile extension. For printers already installed in Windows, choose an OS print queue (`spooler`) to send RAW ESC/POS through the existing vendor driver; this avoids replacing its USB driver. Direct LAN/9100 is also supported for private IPv4. The installer does not claim automatic universal USB driver installation or model certification; run the native UI test print and inspect the paper after setup.

## Release/signing/uninstall

Sign executable and NSIS installer using your organizational certificate/CI secret store; timestamp and verify signatures. Unsigned candidates can trigger SmartScreen; never instruct operators to disable protections globally. Validate on clean Windows 10 and 11, locked-down cashier account, first install, upgrade and uninstall.

Before uninstall: disable autostart in Settings, drain/cancel jobs, rotate pairing key if decommissioning, Quit, then uninstall through Apps. Current v1 does **not** automate keychain/receipt DB deletion or driver rollback. Intentionally retained dedup history prevents accidental replay on reinstall; administrators decide retention and remove the `ir.menuvex.printer` application-data directory and `pairing-v1` Credential Manager entry only with approval. Verify startup entries/shortcuts are gone. Fully unattended cleanup is a release gate, not a claimed implemented feature.
