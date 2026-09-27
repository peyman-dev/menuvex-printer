# MenuVex Printer Agent Legacy — Windows 7 SP1 candidate

A separate C++17 / Win32 implementation for older cashier computers. It does not replace the modern Rust/Tauri agent. No Electron, WebView2, Node.js, Rust toolchain or browser extension is required on the cashier's computer. The application uses static MSVC runtime linkage; the installer is per-user NSIS.

**Status: source implementation / supervised test candidate. A successful Windows Server 2022 CI build is not a Windows 7 compatibility certificate.** Actual Windows 7 SP1 x86/x64, the specific cashier printer/driver, Persian output and old-browser integration must pass acceptance before nationwide rollout. The installer is not signed. Never download replacement KERNEL32 DLLs or disable OS security.

## What is implemented

- Native Win32 window, system tray, close-to-hide, explicit Quit, single-instance mutex and optional default-on HKCU login startup.
- Windows installed-printer discovery (local and connected Windows queues), named persistent profiles, invoice/kitchen/bar routes, independent test print and queue/cancel UI.
- Windows RAW spooler transport using OpenPrinter/StartDocPrinter/WritePrinter. Printer drivers must pass ESC/POS unchanged; do not use XPS/host-based-only drivers. No WinUSB driver replacement is performed.
- Direct LAN TCP/9100 transport (Winsock2) to a private RFC1918 IPv4 with a configurable port and deadlines, mirroring the modern Agent's rules (no DNS, public, loopback or link-local). A per-printer connection-type selector chooses a Windows queue or a LAN IP, plus a "check connection" probe.
- SQLite WAL + FULL sync, immutable profile snapshots, unique logical IDs, 256-active-job cap, FIFO per printer, crash recovery, safe pre-submission retries, no automatic replay after uncertain submission.
- Semantic invoice/receipt → bundled Noto Sans Arabic + Uniscribe shaping/BiDi + GDI raster → bounded GS v 0 ESC/POS stripes. Paper mm, actual dots, copies, font size and cutter configurable locally.
- Loopback-only WebSocket v1, exact Origin/Host/path checks, mutual HMAC-SHA256 handshake using BCrypt, 256-bit Credential Manager key, local reveal/rotation, size/rate/authentication limits and events/resync. No browser-origin configuration mutation.
- Isolated spooler child process with a 30s parent deadline. A blocked driver cannot block the WebSocket thread. Any timeout/partial write/child crash after possible submission is `PRINT_OUTCOME_UNKNOWN`, never auto-replayed. An earlier copy being sent makes later copy failure ambiguous too.
- User-only data directory DACL (plus SYSTEM), bounded/rotated lifecycle-code-only log, private temporary spool data and deletion after normal handoff. No receipt content/key logged.

## What is deliberately different

A printer is described truthfully as `connection: { type: "spooler", queueName: "..." }` for a Windows queue, or `connection: { type: "network", host, port }` for direct LAN — the `network` form is identical to the modern Agent and already accepted by the shared SDK. Old SDK copies that validate only `usb` / `network` must be updated before pairing a `spooler` printer; `sdk/tests/spooler.test.ts` covers compatibility. The modern Tauri backend is unchanged and still configures its own USB/network profiles.

The wire handshake, semantic documents, job schema and command names match `docs/protocol.md`. `completed` means the bytes were handed to the transport — to the Windows spooler for a queue (not physical paper; the queue may still hold an offline job), or accepted over TCP for direct LAN. Status is `unknown` unless Agent itself is sending (`busy`). Do not falsely show that an installed queue is physically online. Windows queue discovery does not provide USB hotplug monitoring. The UI deliberately tells operators to inspect Windows Devices and Printers for physical status.

LAN in Legacy offers two options per printer. Direct LAN: enter the printer's private IPv4 and port (normally 9100); no DNS, public, loopback or link-local address is accepted and the port is adjustable locally, exactly like the modern Agent — no Windows driver is needed. Windows queue: install the LAN printer as a Windows RAW spooler queue (e.g. an approved Standard TCP/IP port/driver) and select it, when a vendor driver is required. Use the "check connection" probe to confirm reachability; a successful probe only opens the TCP connection and is not proof of paper output.

Legacy has a separate data directory and Credential Manager entry; modern credentials/history are not silently imported. Both default to port 8765: **quit the other Agent before using Legacy**. Do not migrate jobs between their independent DBs automatically. Reconcile queued/unknown jobs before switching variants. On port conflict the UI remains available and the worker pauses; change port and Quit/reopen if intentionally using another port. The website CSP/SDK must use the same port.

The native UI uses Persian controls/tray labels, right-to-left captions, a green/light palette and a Persian-capable system font. Technical fields remain LTR. It does not recreate the complete React UI, require WebView2 or launch a browser to show local secrets. See `../docs/connection-compatibility-fa.md` for website schema migration and UI limitations.

## Building (developer/CI machines only)

Prerequisites: Visual Studio 2022 C++ x86/x64 tools, Windows SDK, CMake 3.24+, Git, NSIS, Node.js 22/npm for the real SDK integration tests. All C++ dependencies are pinned to immutable revisions in CMakeLists.txt. JSON, SQLite, WebSocket++ and standalone Asio sources are fetched only on build machines.

```powershell
npm ci
powershell -NoProfile -File scripts/build-legacy.ps1
```

`-InstallBuildTools` additionally permits installation of NSIS via Chocolatey on the CI machine; this is never run by customer installers. Existing `Build MenuVex Installers` Windows CI calls this build script through the installer-collection stage. No new GitHub workflow permissions are required because the existing workflow is unchanged. Non-Windows build jobs and modern app runtime are unchanged.

Outputs, only after tests/import audit/NSIS succeed:

```text
dist/legacy-installers/x86/MenuVex-Printer-Agent-Legacy-1.0.0-windows7-x86-setup.exe
dist/legacy-installers/x64/MenuVex-Printer-Agent-Legacy-1.0.0-windows7-x64-setup.exe
```

The existing `MenuVex-Installer-Windows-x64` Actions artifact now contains the modern installer at the root and a clearly named `Legacy-Windows7/x86` / `Legacy-Windows7/x64` subdirectory. The outer artifact name describes the original modern output, not every nested candidate. Extract and distribute the correct individual installer; do not give a Windows 7 user the modern root setup.exe. Each legacy folder includes a checksum, manifest, import audit and redistribution licenses.

## Compatibility guards (necessary, not sufficient)

`WINVER`, `_WIN32_WINNT`, `NTDDI_VERSION` target Windows 7, subsystem is 6.01, and no WebView is linked. Installer refuses pre-Win7/SP1 and refuses x64 on a 32-bit OS. `dumpbin /imports` rejects known newer kernel/UI API imports, dynamic CRT/API-set dependencies and WebView2. This is a conservative static denylist, not a complete recursive OS API compatibility proof. Do not remove a failing guard merely to get a green build; inspect it, use a verified compatible toolchain, and test on clean Win7 images. Modern compiler/stdlib updates can change minimum OS requirements even with these defines.

## Tests

- Portable core tests: schema/unknown/duplicate fields, limits, semantic invoice, raster bytes, persisted queue/dedup, concurrency, retry/backoff, cancellation and crash recovery. No production fake transport.
- Windows integration executable: actual new SDK + WebSocket + BCrypt mutual HMAC + SQLite + Uniscribe/GDI renderer; test-only handoff counts physical transport attempts (no actual printer). Test key constructor is gated by `MENUVEX_TESTING`; no environment-key override in the shipped executable.
- Build script runs CTest for x86 and x64 and audits the shipped executable, then packages it. Physical driver calls remain a manual acceptance requirement.

## Cashier setup

1. Check Control Panel → System → System type: choose x86 for 32-bit; x64 for 64-bit. `winver` alone does not tell architecture.
2. Install the matching Legacy setup.exe; do not install any developer packages.
3. Ensure the thermal printer is already installed in Windows and its vendor test works.
4. Quit modern Agent. Open Legacy → choose the connection type: for a Windows queue, Refresh Windows printers and pick the exact queue; for direct LAN, enter the printer's private IP and port 9100 → set profile name, paper/dots, role → Save settings.
5. Test print. Inspect joined Persian letters, mixed numbers, long-line wrapping, clipping and cutter on real paper.
6. Update the website SDK for `spooler`. Pair using the local reveal button only on the official MenuVex origin. No plain key in localStorage.
7. Keep unknown-outcome jobs out of automatic retries; inspect the Windows queue and actual paper first.

Uninstall requires tray Quit, removes app/shortcuts/startup registry/uninstall entry and retains DB/credential for safe reinstall. Receipt retention/decommissioning requires explicit administrator approval: data `%LOCALAPPDATA%\MenuVexPrinterLegacy`, credential `MenuVex.Printer.Legacy.Pairing.v1`. Unexpected shutdown can leave a private `payload-*.json`; after fully stopping parent/child processes, a technician can remove stale payload files, but never delete dedup DB rows merely to force printing.

## Release acceptance checklist

- Clean Windows 7 SP1 x86 AND x64, no developer runtimes: install, launch, tray, login startup, upgrade/uninstall, OS import/load behavior.
- Printer plugged/unplugged/offline/paused/out of paper; driver blocking; killed child; system restart during handoff; duplicate events; second-copy failure; multiple profiles.
- Real Persian invoices at actual 58mm/80mm dot widths; correct direction, shaping, punctuation/Latin/digits and wrapping.
- Existing Windows driver RAW support; no disruption of vendor print tools or other POS software. LAN Windows queue if used.
- Old browser + real HTTPS PWA compatibility, CSP, HMAC/IndexedDB persistence and reconnect. No guarantee that an outdated browser can run the modern Next.js site. Windows 7/browser security risk remains; network isolation and least-privilege cashier accounts reduce but do not eliminate it.
- Auth denial, replay, oversized/deep JSON, unknown commands, slow clients, locked Credential Manager, missing spooler service, disk full.
- Signing/distribution policy and accurate legacy download labels. No nationwide support claim until the matrix is signed off.
