# MenuVex Printer Agent → MenuVex Offline: capability audit

Date: 2026-10-19  
Audit target: `peyman-dev/menuvex-printer` at `a5f5eec` (Rust + Tauri agent)  
Port target: `offline-menuvex/` (Go + HTMX, JSON local store, zero external Go modules)

## 1. Method and integrity rule

The audit is based only on the checked-in Agent source and its own documentation. UI labels were
not treated as proof of behaviour: every claim below points at the module or function that
implements it. The Agent was **not modified** during this work; all changes are confined to
`offline-menuvex/` and `docs/offline/`.

Files inspected (non-exhaustive but covering every printer path):

- `src-tauri/src/protocol.rs` — command/document envelope, 1 MiB ESC/POS limit, raw-passthrough gate.
- `src-tauri/src/server.rs` — loopback WebSocket server and command dispatch.
- `src-tauri/src/security.rs` — origin allowlist and HMAC pairing (`menuvex-print-agent:v1`).
- `src-tauri/src/main.rs` — Tauri commands exposed to the web UI.
- `src-tauri/src/state.rs`, `config/{mod,storage}.rs` — profiles, routes, limits, SQLite schema.
- `src-tauri/src/print/{queue,mod,layout,escpos}.rs` — job lifecycle, retry policy, rendering.
- `src-tauri/src/printers/{discovery,network,spooler,usb,usb_policy,raw_printer,mod}.rs` — transports.
- `src/App.tsx`, `src/components/{PrinterList,PrinterCard,Settings,ConnectionStatus}.tsx`,
  `src/lib/agent.ts` — operator-facing flows.
- `docs/{architecture,printers,protocol,hardware-testing}.md` — documented contracts and hardware matrix.

## 2. Agent command surface

The Tauri layer exposes `local_command` (a JSON protocol) plus dedicated commands:

| Command | Purpose | Important limits |
|---|---|---|
| `local_command` | JSON envelope for all printer operations | 2 MiB message ceiling, versioned protocol |
| `discover_usb` | USB printer-class discovery | 10 s UI timeout, runs in `spawn_blocking` |
| `get_config` / `save_config` | Profiles, routes, raw settings | Rejects unknown connection `type`; max 16 printers/routes/raw entries |
| `pairing_secret` / `rotate_secret` | HMAC shared secret | OS keyring only; no plaintext fallback |
| `test_connection` | Probe a configured printer | Network probe is bounded (see §4) |
| `raw_printer_platform` / `raw_printer_info` | Raw spooler diagnostics | Read-only inspection of an installed queue |
| `create_generic_raw_target` | Create a "Generic / Text Only" queue | Windows only, uses the source queue's existing port |
| `test_raw_print` | RAW test bytes through a queue | Windows `winspool` / CUPS RAW |

The web UI cannot change the operator-owned global raw switch or a per-printer raw switch. The
Agent deliberately keeps those switches local (`State::enqueue`).

## 3. Connection model and validation

`printers/mod.rs` defines `Connection` as one of:

- `network { host, port }`
- `spooler { queue_name }`
- `usb { vendor_id, product_id, serial, bus, ports, interface, endpoint, alternate }`

Validation rules that matter for a port:

- **Network is literal private IPv4 only.** `printers::network::address` rejects hostnames,
  IPv6, public addresses, loopback, link-local, multicast, `.0` and `.255` hosts. The audit
  confirmed this is implemented in code, not just documented.
- **Queue names** are non-empty, ≤ 256 bytes and contain no control characters.
- **Paper**: 58 mm or 80 mm; raster width 128–832 dots and a multiple of 8.
- **Copies**: 1–3 per profile; **font size**: 12–48; **max printers**: 16.
- **Routes**: role → printer id, unique role, ≤ 16 routes, `autoPrint` flag.
- **Raw passthrough**: global and per-printer gates, 1 KiB … 1 MiB (hard `MAX_ESCPOS` = 1 MiB).

## 4. Transports and their real semantics

### 4.1 Network RAW (TCP)

- Connect timeout **3 s**, per-write timeout **3 s**, overall send deadline **30 s**.
- Probe opens a TCP connection to `host:port` (3 s). Open port is connectivity evidence only.
- Send writes the ESC/POS bytes and closes. **There is no print acknowledgement.** A successful
  write is reported as "sent", not "printed", in this port.

### 4.2 Installed OS print queue (spooler)

- Windows: `EnumPrintersW` for listing; RAW send uses
  `OpenPrinterW → StartDocPrinterW(RAW) → StartPagePrinter → WritePrinter → EndPagePrinter →
  EndDocPrinter → ClosePrinter`.
- Linux/macOS: `lpstat -e` for queues, `lp -d <queue> -o raw` with bytes on stdin.
- Queue acceptance is not physical printing.

### 4.3 Direct USB (libusb)

- Discovery enumerates the bus and keeps only interfaces whose class is `0x07` (printer).
- Identity: VID/PID + serial when present; otherwise bus/topology. This prevents duplicate
  entries for the same device.
- Access is checked and reported (`accessible` / `accessError`).
- Operation deadlines: 10 s presence operations, 25 s discovery, 35 s send guard; bulk writes use
  16 KiB chunks with 3 s per-chunk timeouts.
- USB failures carry a stage (pre-open, post-open/pre-write, mid-write, timeout, unknown).
  Fallback is allowed **only** before the first byte with a confirmed zero-byte count and an
  explicitly configured fallback target. Timeouts and mid-write failures are "uncertain" and must
  never fall back or auto-retry.
- The feature can be compiled without libusb; in that build direct USB is reported as disabled and
  only an explicit fallback is used.

### 4.4 Network discovery

- The Agent scans exactly one `/24` at port `9100`: 254 hosts, 64 workers, 400 ms per host.
- It does not scan public ranges or arbitrary ports. A discovered open port is only a candidate;
  it is never auto-added.

### 4.5 Raw diagnostics

- `inspect_printer` reads driver/port metadata without modifying a queue.
- `create_generic_raw_target` creates a separate Windows queue using the installed
  "Generic / Text Only" driver on an existing USB port; the source queue and vendor driver are
  never modified.
- Raw passthrough transports frontend-authored bytes only after two explicit local gates. This is
  a diagnostic escape hatch, not the normal receipt path.

## 5. Print queue semantics

From `print/queue.rs` and `state.rs`:

- Jobs have stable, idempotent ids; enqueue deduplicates by job id.
- States: `queued → printing → printed | failed`.
- Retry only when `error.retryable && !error.uncertain` and attempts < configured max
  (1–5, default 3).
- Backoff: `min(1 << attempts, 60)` seconds.
- At most 256 active jobs.
- Cancellation is allowed only while queued.
- One printing job per printer at a time (SQL excludes printers with a queued/printing job).
- Crash recovery keeps queued jobs; printing jobs are reconciled on restart.
- A print failure never mutates the source order; order and print state are separate records.

## 6. Security model

- The HTTP origin is allowlisted (`https://menuvex.ir`, `https://www.menuvex.ir`; localhost dev
  origin only in debug builds).
- WebSocket pairing uses HMAC-SHA256 over
  `menuvex-print-agent:v1\n{nonce}\n{origin}` with a key stored in the OS keyring entry
  `ir.menuvex.printer` / `pairing-v1`. There is no plaintext fallback; missing secure storage fails
  with `SECURE_STORAGE_UNAVAILABLE`.
- The Agent listens on loopback only. It is not a LAN print server.

## 7. UI/UX of the Agent (for parity, not proof)

- `PrinterList` manages devices: probe, installed queues, USB scan, manual add, edit, test print,
  stop queue; USB rows show accessibility and fallback target.
- Add/edit form exposes connection type, paper, width dots, copies, cut, font, raw settings and a
  USB fallback target.
- `Settings` shows platform, installed queues, queue inspection (driver/port), raw-target creation
  restricted to Windows with a selected source, and auto-clears the pairing secret after 60 s.
- `PrinterCard` shows status (online/offline/unknown/busy) and actions.

## 8. What MenuVex Offline had before this work

`offline-menuvex/printing.go` and `internal/store/printing.go` already had a working receipt layer:

- one `printer` entity with free-text `connection` (`host:port`), optional `queue`, paper width,
  station, `is_active`;
- `print_job` entities with `pending/printing/printed/failed`, dedupe key, copies and category
  filters;
- dispatch order: spool-directory env override → TCP `host:port` → CUPS `lp -o raw` → Windows
  unsupported;
- automatic policies for create / no-table / checkout / complete, category routes, and order
  status independent of print status.

Missing compared with the Agent:

- no USB discovery, no installed-queue listing, no network scan;
- no layered probe or per-layer evidence;
- no transport-specific validation (a hostname could be stored);
- no printer management centre, wizard, scan page, detail page or queue page;
- no per-transport error classification (`uncertain` did not exist);
- no data migration for printers created before a connection “kind” existed.

## 9. Port decisions (summary)

| Agent capability | Offline approach |
|---|---|
| Rust/Tauri IPC and HMAC pairing | Not applicable: Offline is a single local process behind the existing license/session/CSRF/host-allowlist. Reimplementing a second pairing protocol would add attack surface without a remote caller. |
| libusb direct claim | No CGO/external modules allowed by the project. Linux uses sysfs printer-class discovery plus `/dev/usb/lp*` when `usblp` exists; other systems use the OS queue and say so. |
| Windows winspool RAW | Implemented with the standard library's `syscall` bindings; compile-verified for `windows/amd64`, requires real Windows hardware for final validation. |
| CUPS RAW | Implemented with `lpstat`/`lp` invoked with fixed argv and validated queue names. |
| Raw passthrough gates | Not implemented intentionally; normal receipts are generated server-side and raw remote bytes are not accepted. |
| SQLite job queue | The existing JSON store with atomic temp-file + rename is retained; the equivalent lifecycle is implemented there. |
| Time-based backoff worker | Offline dispatches synchronously and keeps explicit retry actions; automatic retry of ambiguous outcomes is forbidden. |

## 10. Feature coverage status

Status vocabulary (exact terms used by the delivery):

- **«تکمیل و تست‌شده»** — implemented and covered by automated tests that were executed.
  Hardware-independent logic is exercised with fake TCP servers, injected dialers and sysfs
  fixtures where a device is required.
- **«پیاده‌سازی‌شده اما تست‌نشده»** — code exists but no automated test was executed for it.
- **«نیازمند سخت‌افزار واقعی»** — code path is complete and compile-checked, but physical
  verification (paper, driver, firmware) needs a real device.
- **«پیاده‌سازی‌نشده با دلیل»** — deliberately out of scope, with the reason recorded.

| # | Capability | Status | Evidence / reason |
|---|---|---|---|
| 1 | Private IPv4 + port validation | «تکمیل و تست‌شده» | `internal/printer/model_test.go`, `TestValidateNetworkTargetPolicy` |
| 2 | Printer profile validation (paper/copies/width/name/queue/USB path) | «تکمیل و تست‌شده» | `TestProfileValidation` |
| 3 | Duplicate destination prevention | «تکمیل و تست‌شده» | `TestSavePrinterRejectsDuplicateDestinations`, `TestPrinterSaveValidationAndDuplicatePrevention` |
| 4 | Layered network probe (policy → TCP → ESC/POS status) | «تکمیل و تست‌شده» | `probe_test.go`, `TestPrinterProbeLayersAndPersistence` |
| 5 | “TCP open ≠ printer/print success” wording | «تکمیل و تست‌شده» | `TestProbeNeverCallsOpenPortAWorkingPrinter`, `TestTestPrintUsesNewTransportWithoutClaimingPhysicalPrint` |
| 6 | Bounded local `/24` network scan | «تکمیل و تست‌شده» | `scan_test.go`, `TestPrinterNetworkScanProgressAndCancel` |
| 7 | Scan cancel / progress / dedupe | «تکمیل و تست‌شده» | `TestScannerCancelStopsProgress`, `TestScannerIsSingleFlightAndCancellable` |
| 8 | Manual add always available | «تکمیل و تست‌شده» | Page renders + wizard route; `TestPrinterCenterPagesRender` |
| 9 | USB printer-class discovery (Linux sysfs) | «تکمیل و تست‌شده» | `usb_linux_test.go` with a synthetic sysfs tree; only class `0x07` surfaces |
| 10 | USB discovery on Windows/macOS | «پیاده‌سازی‌نشده با دلیل» | Go rewrite avoids libusb/CGO; these systems use an installed queue. The UI states this explicitly. |
| 11 | Direct USB printing via `/dev/usb/lp*` | «نیازمند سخت‌افزار واقعی» | Path validation, timeouts and uncertain-write policy unit-tested; needs a real usblp printer |
| 12 | USB fallback policy (pre-write only) | «پیاده‌سازی‌نشده با دلیل» | Offline has no per-printer fallback target; users add the OS queue as a second printer. Porting the policy without a target editor would create a setting with no behaviour. |
| 13 | CUPS queue parsing, argv safety and raw hand-off policy | «تکمیل و تست‌شده» | `TestParseQueueListNormalizesAndDeduplicates`, `TestSendSpoolerRawValidatesBeforeInvokingLp`, `TestSenderSpooler` (injected sender); no shell is involved |
| 13b | Actual `lp -d … -o raw` invocation against a CUPS daemon | «پیاده‌سازی‌شده اما تست‌نشده» | Code path exists in `spooler_unix.go`; this environment has no CUPS daemon, so the process launch was not executed |
| 13c | Physical print on a CUPS queue | «نیازمند سخت‌افزار واقعی» | Needs a real CUPS installation and printer; simulated tests do not prove paper output |
| 13d | macOS runtime path (same `linux || darwin` code) | «پیاده‌سازی‌شده اما تست‌نشده» | Cross-compiles for `darwin/arm64`; not executed on macOS here |
| 14 | Windows winspool RAW send + queue listing | «نیازمند سخت‌افزار واقعی» | `spooler_windows.go` cross-compiles for `windows/amd64`; no Windows host in this environment |
| 15 | Printer management centre | «تکمیل و تست‌شده» | `TestPrinterCenterPagesRender`, `printers_test.go` |
| 16 | Add-printer wizard (network/spooler/USB) | «تکمیل و تست‌شده» | Wizard page assertions + save validation tests |
| 17 | Network scan page with live progress | «تکمیل و تست‌شده» | `TestPrinterNetworkScanProgressAndCancel` |
| 18 | Printer detail / settings / probe evidence / test print / delete | «تکمیل و تست‌شده» | `TestPrinterProbeLayersAndPersistence`, `TestPrinterDeleteKeepsJobsAndFreesRoutes` |
| 19 | Per-printer active switch, cut, paper width, role | «تکمیل و تست‌شده» | `TestPrinterRolesDriveCashierAndKitchenDestinations`; cut/paper asserted indirectly by profile tests |
| 20 | Print queue page with filters/counters | «تکمیل و تست‌شده» | `TestPrintQueueSafetyAndOrderIndependence`, page assertions |
| 21 | Safe retry (claim-before-send, terminal sent/printed, uncertain blocked) | «تکمیل و تست‌شده» | Concurrent-dispatch test prints once; uncertain retry blocked |
| 22 | Cancel only queued jobs | «تکمیل و تست‌شده» | `TestPrintQueueSafetyAndOrderIndependence` |
| 23 | Job target snapshot and self-contained retry | «تکمیل و تست‌شده» | `TestTestPrintUsesNewTransportWithoutClaimingPhysicalPrint` |
| 24 | Category routing (existing, preserved) | «تکمیل و تست‌شده» | `TestPrinterCategoryRouting` still passes unchanged |
| 25 | Cashier/kitchen role routing | «تکمیل و تست‌شده» | `TestPrinterRolesDriveCashierAndKitchenDestinations`, `TestAutomaticPrintPolicyUsesNewTransport` |
| 26 | Order/checkout print wiring | «تکمیل و تست‌شده» | `TestAutomaticPrintPolicyUsesNewTransport` |
| 27 | Order state independent of print failure | «تکمیل و تست‌شده» | `TestPrintQueueSafetyAndOrderIndependence` |
| 28 | JSON-store migration (`printer_schema=1`) | «تکمیل و تست‌شده» | `internal/store/printers_test.go` |
| 29 | Legacy printer records still print | «تکمیل و تست‌شده» | `TestPrinterOptInGate`, `TestPrinterCategoryRouting` pass on legacy `connection` records |
| 30 | CSRF/host-allowlist/auth preserved | «تکمیل و تست‌شده» | All new POSTs go through the existing middleware; page tests exercise them |
| 31 | Raw passthrough (frontend-authored ESC/POS) | «پیاده‌سازی‌نشده با دلیل» | Offline renders receipts server-side; accepting remote raw bytes would duplicate the Agent's most security-sensitive feature without a business caller |
| 32 | Generic / Text Only queue creation (Windows) | «پیاده‌سازی‌نشده با دلیل» | Requires driver/port management with administrator privileges; instructing operators to install a RAW queue is safer and already supported |
| 33 | HMAC pairing / keyring / WebSocket server | «پیاده‌سازی‌نشده با دلیل» | Offline is a local single-process app; it already has license, session, CSRF and host allowlists. A second pairing protocol has no caller here. |
| 34 | Raster/image receipt layout with custom fonts | «پیاده‌سازی‌نشده با دلیل» | Offline's tested receipt builder is text ESC/POS; switching to raster would change every existing receipt test and add font-rendering dependencies. This is documented rather than silently half-ported. |
| 35 | Automatic retry with backoff | «پیاده‌سازی‌نشده با دلیل» | Offline exposes explicit retry only; there is no background worker. Ambiguous failures must never auto-retry, and adding a scheduler for the safe subset was not required for this delivery. |
| 36 | Crash recovery for jobs left in `printing` | «تکمیل و تست‌شده» | `TestRecoverInterruptedPrintJobsMarksUncertainAndBlocksRetry`; startup recovery marks them uncertain so nothing auto-reprints |
| 37 | Bulk "retry safe failures" only | «تکمیل و تست‌شده» | `TestPrintQueueRetryAllSkipsUncertainAndKeepsHistory` |
| 38 | Edit, activate/deactivate and confirmed delete | «تکمیل و تست‌شده» | `TestPrinterEditActivationDeleteGuards`, `TestPrinterDeleteKeepsJobsAndFreesRoutes` |
| 39 | USB scan UI (accessibility warning, no auto-add) | «تکمیل و تست‌شده» | `TestPrinterUSBScanShowsAccessButNeverAutoAdds` |
| 40 | HTMX scan-progress partial | «تکمیل و تست‌شده» | `TestPrinterNetworkScanProgressAndCancel` also requests the `HX-Request` partial |
