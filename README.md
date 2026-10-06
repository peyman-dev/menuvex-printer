# MenuVex Printer Agent · 1.0.0

Standalone Tauri 2 / Rust / React agent for local USB and LAN ESC/POS printing, with a typed MenuVex PWA SDK. **No Electron, browser WebUSB access, public hardware listener, or simulated production printers.**

> **Reliability + design-ownership fix (2026-10-06):** the reported "adding a USB printer kills the agent" outage was traced to four defects, all now fixed: the queue worker `break`-ed out permanently on its first internal failure (`state.rs`), `lock().unwrap()` turned one panic into permanently poisoned state mutexes so every `agent.status`/`printers.list`/`queue.list` failed, `server.rs` closed the website's socket when a command handler panicked, and the USB path created a fresh libusb context per call, opened the device on every 10 s status poll and claimed its interface without ever releasing it. A printer failure now fails one job — never a socket, the queue or the process. Unsupported `connection.type` values are rejected by name (`Unsupported printer connection type: "lan"`) instead of a generic parser error, and a new gated `escpos` document lets the frontend send its own finished bytes so the agent no longer has to own the receipt layout. Details, line numbers and the hardware test matrix: [printer failure isolation](docs/printer-failure-isolation.md).
>
> **Receipt design fix (2026-10-04):** operator reports that printed invoices no longer matched the approved template. Cause: `Document::lines()` rendered every line by plain stacking — left aligned regardless of direction, amounts split onto their own line, and separators written with `────────────────` (U+2500), which the bundled Noto Sans Arabic does not contain, so the paper showed boxes or blank lines. The design now lives in [`src-tauri/src/print/layout.rs`](src-tauri/src/print/layout.rs) and follows the MenuVex app invoice template without the logo (centered store header with address/phone, `فاکتور فروش | فیش N` slip row, date/status/order-type/table rows, a four column items table on 80 mm, Persian-digit grouped amounts with the adapter-supplied currency word, emphasised `مبلغ قابل پرداخت`, dashed note frame and the `POWERED BY MENUVEX.IR` brand line) with a Rust unit test per rule, and [`tools/receipt-preview`](tools/receipt-preview/README.md) renders before/after PNGs and checks font coverage in CI. The Legacy Windows agent was aligned to the same design (margin, pixel rules, grouped numbers, the same optional invoice fields). Physical paper acceptance on the real model is still required.
>
> **Delivery status (2026-09-17): implementation / native validation pending, not a certified production release.** The original repository contained only its title README. The MenuVex PWA is not in this checkout. Integration with its order events, legacy WebUSB function, and `/download/print-agent` is intentionally not fabricated. The owner selected a standalone Agent + SDK scope.
>
> Frontend build and 17 SDK tests pass in this environment. Rust/Cargo and Linux native libraries are unavailable; their download endpoints failed. Rust tests, the real SDK→Rust integration test, native installers and physical hardware have **not** been executed here. Native CI configuration is included as an inactive template in `docs/ci/checks.yml`; it has not been run because the current GitHub connection cannot create workflows. Do not distribute this as a tested POS release until the [release gates](docs/release.md) pass.

## Implemented source

- Loopback-only `ws://127.0.0.1:8765`, configurable port, exact Origin/Host allowlist, versioned strict protocol, bounded connections/messages, handshake deadlines, rate limits.
- 256-bit OS-keychain secret; nonce/origin-bound HMAC authentication; browser stores a **non-extractable CryptoKey in IndexedDB**, never a plaintext token in localStorage. Explicit local pairing and rotation.
- OS print-queue (spooler) transport on every platform — Windows `winspool` RAW, CUPS `lp -o raw` on Linux/macOS — so a USB printer prints through its **already installed vendor driver**; no libusb/libusbK driver replacement needed. `printers.installed` lists the queues for one-click setup. Direct USB Printer Class discovery and bulk transfer through `rusb`/libusb remain available for driverless setups.
- Manual private IPv4 LAN setup, TCP/9100 with configurable port and deadlines, plus `discover.network` LAN auto-discovery (the UI's «جستجوی شبکه») so the operator just picks a found IP.
- SQLite WAL persistent jobs, unique IDs, atomic claims, crash recovery, persistent deduplication, cancellation, capped retries and one-click `queue.clear` (cancel queued + delete history, never the printing job). Ambiguous transfers **never automatically replay**.
- Semantic invoice/receipt documents → Rust Arabic shaping/BiDi/rasterization → ESC/POS, **or** frontend-authored raw `escpos` bytes forwarded verbatim when the operator enables `rawPassthrough` for that printer (the frontend then owns the design). Bundled OFL Noto Sans Arabic; configurable actual dot width, paper size, font size, copies, cutter. Free-form receipts support pixel rules, centered lines and right-to-left pipe-separated columns; test tickets print the configured millimeters/dots profile. The renderer never claims generic physical-width sensing. `npm run preview:receipt` renders and checks the design without a printer; see [ESC/POS](docs/escpos.md#printed-design).
- Local printer/profile/routing configuration, independent test print, queue UI, status events, system tray, close-to-hide, optional default-on autostart.
- SDK reconnect, response validation, subscriptions, migration adapters, durable browser routing and optional React provider.
- Device-specific Linux udev setup helper; per-OS native candidate CI; bounded daily logs without tokens/order contents.

## Windows 7 Legacy variant

A separate native Win32/C++ Agent is implemented under [`legacy-windows/`](legacy-windows/README.md). It uses installed Windows RAW spooler queues or direct LAN TCP/9100 to a private IPv4 (chosen per printer), SQLite and Uniscribe Persian raster printing, with the same HMAC protocol and receipt layout hints. Spooler profiles remain explicit internally and use the reserved USB-compatible `queue:` wire marker for older frontends; use SDK `spoolerQueueName()` rather than opening WebUSB. The Windows installer collection stage builds x86/x64 Legacy candidates in addition to the modern output. Real Windows 7 SP1/driver/browser acceptance remains required; no compatibility certification is claimed from modern CI alone.

## Installer downloads for café testing

For owner setup and downloading compiled `.exe`, `.dmg`, `.deb` and `.AppImage` files (not source), follow [the Persian installer guide](docs/installers-fa.md). The workflow template produces clearly named installer-only artifacts with SHA-256 checksums. It must first be activated by a GitHub account with workflow-write permission; no ready binary is claimed until native builds succeed.

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

Direct-USB compatibility is **not universal**: Windows typically needs a compatible WinUSB driver for direct libusb access; changing it can break vendor-driver printing. Printer Class only is deliberately conservative. Prefer an installed Windows RAW spooler queue or LAN where driver changes are unacceptable; spooler keeps the vendor driver in place.

Browser loopback access can be blocked by CSP, browser local-network permissions, enterprise policy or mixed-content handling. “Unreachable” does not prove “not installed.” Test the actual HTTPS PWA on supported browsers; do not instruct cashiers to disable browser security. See [security](docs/security.md).

## Documentation

[Architecture](docs/architecture.md) · [Protocol](docs/protocol.md) · [Printer failure isolation](docs/printer-failure-isolation.md) · [Printers](docs/printers.md) · [چاپ USB روی همهٔ نسخه‌ها](docs/usb-printing-fa.md) · [ESC/POS](docs/escpos.md) · [Security](docs/security.md) · [Troubleshooting](docs/troubleshooting.md) · [عیب‌یابی «Local Agent نمی‌تواند وصل شود»](docs/troubleshooting-local-agent-fa.md) · [Hardware tests](docs/hardware-testing.md) · [Release](docs/release.md).
