# MenuVex Printer Agent · 1.0.0

Standalone Tauri 2 / Rust / React agent for local USB and LAN ESC/POS printing, with a typed MenuVex PWA SDK. **No Electron, browser WebUSB access, public hardware listener, or simulated production printers.**

> **Delivery status (2026-09-17): implementation / native validation pending, not a certified production release.** The original repository contained only its title README. The MenuVex PWA is not in this checkout. Integration with its order events, legacy WebUSB function, and `/download/print-agent` is intentionally not fabricated. The owner selected a standalone Agent + SDK scope.
>
> Frontend build and 17 SDK tests pass in this environment. Rust/Cargo and Linux native libraries are unavailable; their download endpoints failed. Rust tests, the real SDK→Rust integration test, native installers and physical hardware have **not** been executed here. Native CI configuration is included as an inactive template in `docs/ci/checks.yml`; it has not been run because the current GitHub connection cannot create workflows. Do not distribute this as a tested POS release until the [release gates](docs/release.md) pass.

## Implemented source

- Loopback-only `ws://127.0.0.1:8765`, configurable port, exact Origin/Host allowlist, versioned strict protocol, bounded connections/messages, handshake deadlines, rate limits.
- 256-bit OS-keychain secret; nonce/origin-bound HMAC authentication; browser stores a **non-extractable CryptoKey in IndexedDB**, never a plaintext token in localStorage. Explicit local pairing and rotation.
- USB Printer Class discovery and bulk transfer through `rusb`/libusb. Manual private IPv4 LAN setup, TCP/9100 with configurable port and deadlines. No network scanner.
- SQLite WAL persistent jobs, unique IDs, atomic claims, crash recovery, persistent deduplication, cancellation and capped retries. Ambiguous transfers **never automatically replay**.
- Semantic invoice/receipt documents → Rust Arabic shaping/BiDi/rasterization → ESC/POS. Bundled OFL Noto Sans Arabic; configurable actual dot width, paper size, font size, copies, cutter.
- Local printer/profile/routing configuration, independent test print, queue UI, status events, system tray, close-to-hide, optional default-on autostart.
- SDK reconnect, response validation, subscriptions, migration adapters, durable browser routing and optional React provider.
- Device-specific Linux udev setup helper; per-OS native candidate CI; bounded daily logs without tokens/order contents.

## Installation for operators

No verified download binaries are published by this change. Obtain a **signed and hardware-validated** installer from your MenuVex administrator once the release gates pass. Do not use download URLs claiming an existing release that has not been built.

1. Install and open the Agent under your normal desktop user (not Administrator/root).
2. USB: use **جستجوی USB**, select the device, save its profile. Resolve driver/udev setup once as described below. LAN: add its private IPv4 address and port, normally 9100.
3. Set the real printer width (often 384 dots for 58mm or 576 for 80mm), then use **بررسی اتصال** and **چاپ آزمایشی**. Inspect the Persian output and cutter operation.
4. Assign invoice/kitchen/bar routes in Settings. Show the pairing key locally; enter it only into the official MenuVex pairing UI implemented with `client.pair(secret)`.
5. After PWA integration, order events call `client.print()` with a stable order-derived ID. No repeated USB device chooser. Closing the Agent window hides it; use **Quit** from the tray to stop.

Platform instructions: [Windows](docs/windows.md) · [Linux](docs/linux.md) · [macOS](docs/macos.md).

## Development

Use Node.js 22, npm (lockfile included), stable Rust and the [Tauri prerequisites](https://v2.tauri.app/start/prerequisites/).

```sh
npm ci
npm test
npm run build
npm run dev                  # UI preview only, no hardware bridge in a browser
npm run tauri -- dev         # Actual desktop app, requires native prerequisites
npm run test:rust            # Rust unit tests + real WS SDK integration (npm ci first)
```

`npm run dev` binds the UI preview to `0.0.0.0:1420`. **This is not the Agent WebSocket**: that listener always binds `127.0.0.1` in Rust. The browser preview uses no localhost backend request and honestly shows unavailable native capabilities.

See [development](docs/development.md) for exact validation status and the test-only integration harness.

## Build candidates

Run on the target OS after installing its prerequisites:

```sh
# Windows x64 (PowerShell / Developer terminal)
npm run tauri -- build --target x86_64-pc-windows-msvc --bundles nsis

# Linux x64
npm run tauri -- build --target x86_64-unknown-linux-gnu --bundles deb,appimage

# macOS Apple Silicon
rustup target add aarch64-apple-darwin
npm run tauri -- build --target aarch64-apple-darwin --bundles dmg

# macOS Intel (on a supported Intel runner)
rustup target add x86_64-apple-darwin
npm run tauri -- build --target x86_64-apple-darwin --bundles dmg
```

These are configured native build commands, **not commands verified in this sandbox**. Candidate artifacts are under `src-tauri/target/<target>/release/bundle`. Production requires signing/notarization, resolved and reviewed `Cargo.lock`, clean-machine tests and the hardware matrix. CI does not publish a release automatically.

## SDK integration

```ts
import { printerAgent } from './lib/printer'; // re-export sdk/src in the actual PWA

// Pair once in explicit setup UI; immediately clear the input afterwards:
// await printerAgent.pair(secretFromLocalAgent);
await printerAgent.connect();
const status = await printerAgent.getStatus();
const route = status.routes.find((r) => r.role === 'invoice');
if (route?.autoPrint) {
  await printerAgent.print({
    jobId: `order:${order.id}:invoice`,
    printerId: route.printerId,
    document: { type: 'invoice', data: invoiceAdapter(order) },
  });
}
```

`order` and `invoiceAdapter` belong to the real PWA; no invented order schema is supplied. Monetary values are integer units chosen consistently by the PWA (rial/toman must not be silently converted). Width is a **trusted local printer profile**, not a per-message device-control instruction.

`print()` resolves when durably queued (or returns an existing job), not when physically printed. Observe `onPrintJob` or `getJob(jobId)`. Resend the **same** ID after an uncertain acknowledgement; never automatically switch transport or invent a new ID. See [integration](docs/integration.md).

## Reliability boundaries

RAW ESC/POS has no transactional exactly-once physical-print acknowledgement. `completed` means all bytes were accepted by the transport, **not** proof of paper output. A partial write or crash during `printing` becomes `failed / PRINT_OUTCOME_UNKNOWN`; a human must inspect the paper. Reprinting deliberately uses a new ID. Pre-send offline failures retry at 2, 4, 8… seconds, capped at 60 seconds and configured attempts.

USB compatibility is **not universal**: Windows typically needs a compatible WinUSB driver for direct libusb access; changing it can break vendor-driver printing. Printer Class only is deliberately conservative. Prefer LAN where driver changes are unacceptable; a Windows RAW spooler transport is not included.

Browser loopback access can be blocked by CSP, browser local-network permissions, enterprise policy or mixed-content handling. “Unreachable” does not prove “not installed.” Test the actual HTTPS PWA on supported browsers; do not instruct cashiers to disable browser security. See [security](docs/security.md).

## Documentation

[Architecture](docs/architecture.md) · [Protocol](docs/protocol.md) · [Printers](docs/printers.md) · [ESC/POS](docs/escpos.md) · [Security](docs/security.md) · [Troubleshooting](docs/troubleshooting.md) · [Hardware tests](docs/hardware-testing.md) · [Release](docs/release.md).
