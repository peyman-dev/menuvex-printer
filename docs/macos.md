# macOS

Tauri deployment minimum is 11.0, but actual minimum must be validated against resolved dependencies. Apple Silicon and Intel use separate build candidates; no untested universal binary is claimed.

Install Xcode Command Line Tools, Node 22 and stable Rust on a native runner:

```sh
npm ci
npm test
cargo test --manifest-path src-tauri/Cargo.toml --no-default-features
# Apple Silicon:
rustup target add aarch64-apple-darwin
npm run tauri -- build --target aarch64-apple-darwin --bundles dmg
# On an Intel runner:
rustup target add x86_64-apple-darwin
npm run tauri -- build --target x86_64-apple-darwin --bundles dmg
```

Universal build can be evaluated later with both Rust targets and Tauri's `universal-apple-darwin` target; validate libusb linking and both architectures before offering it. No macOS build was executed in this session.

## Runtime

Move the signed app from DMG to Applications before enabling autostart. The Tauri autostart plugin uses a LaunchAgent; Settings controls it. Initial enabled default is registered when the app runs. macOS login-item approval can be affected by user/security policy. Keychain may require an OS confirmation to access/create the pairing secret; that is setup, not browser USB permission per print. Reject insecure plaintext fallback if Keychain is unavailable.

libusb uses macOS USB APIs; exclusive access conflicts with another driver/process can prevent claim. This implementation does not silently detach macOS kernel drivers. Verify Printer Class endpoints and test the actual model. USB-C adapters/hubs, device reconnect topology and vendor drivers can affect identity/ownership. No assumption of universal USB compatibility.

## Signing/notarization

Use Developer ID signing, hardened runtime and Apple's notarization process for an outside-App-Store distribution. Configure organizational signing/notarization secrets in CI per [Tauri distribution documentation](https://v2.tauri.app/distribute/sign/macos/). Do not commit certificates or passwords. Test Gatekeeper/quarantine on a clean machine, keychain continuity on signed upgrades, tray visibility, login launch and USB access. macOS App Sandbox/Mac App Store distribution imposes additional entitlement/USB constraints and is not this release target. Do not advise disabling SIP/Gatekeeper.

Before uninstall: disable login launch in Settings, rotate key if decommissioning, Quit and remove the application. Inspect the app's LaunchAgent/login entry. Receipt DB and Keychain entry remain until explicitly removed with retention approval. Signed/unattended uninstall cleanup is not yet certified.
