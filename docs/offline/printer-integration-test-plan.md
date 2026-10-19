# Printer integration test plan and execution report

Date: 2026-10-19  
Module: `offline-menuvex` (`github.com/sinaqaffari/coffish/desktop`)  
Toolchain used: Go 1.27.2 linux/amd64 (module declares `go 1.23`), `CGO_ENABLED=0`

This document separates **executed** automated checks from **simulated** hardware and from
**not-yet-executed** physical checks. Nothing below is reported as passing unless the command was
actually run in this workspace.

## 1. Status legend

| Term | Meaning |
|---|---|
| PASS (executed) | Command ran in this workspace and exited 0 |
| SIMULATED | Fake TCP printer, injected dialer, synthetic sysfs tree or fake queue sender |
| CROSS-COMPILE ONLY | Build succeeded for another OS; runtime behaviour not exercised |
| NOT RUN | Requires hardware/OS not present here |
| FAIL | Observed failure (none in the final run) |

## 2. Commands actually executed

```bash
cd offline-menuvex

# Formatting (only files touched by this delivery)
gofmt -l app.go printers.go printing.go internal/printer internal/store

# Build + static analysis
go build ./...
go vet ./...

# Full automated suite
CGO_ENABLED=0 go test ./... -count=1

# Cross-compilation (no execution)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build ./...
```

Observed results:

| Command | Result |
|---|---|
| `go build ./...` | PASS |
| `go vet ./...` | PASS (no diagnostics) |
| `gofmt -l` on touched files | PASS (no output; two unrelated pre-existing files with older formatting were deliberately left untouched) |
| `CGO_ENABLED=0 go test ./...` | PASS — desktop `ok` 3.036 s; `internal/printer` `ok` 0.213 s; `internal/store` `ok` 0.083 s; `internal/syncclient` `ok` 0.030 s |
| `GOOS=windows GOARCH=amd64 … go build ./...` | PASS (compile/link only) |
| `GOOS=darwin GOARCH=arm64 … go build ./...` | PASS (compile/link only) |
| `go test -race` | NOT RUN — the environment has no CGO toolchain, and `-race` requires CGO |

Top-level test functions discovered by `go test -list '.*'`:

- desktop package: 61 (includes the pre-existing offline suite)
- `internal/printer`: 27
- `internal/store`: 25

## 3. Unit tests — `internal/printer`

| Test | What it proves | Result |
|---|---|---|
| `TestValidateNetworkTargetPolicy` | Private IPv4 only; loopback allowed only as documented local extension; rejects public, `.0`, `.255`, link-local, multicast, hostname, IPv6, bad ports | PASS |
| `TestProfileValidation` | Paper 58/80, width 128–832 %8, copies 1–3, queue control chars, USB path confinement, empty name | PASS |
| `TestProfileUtility` | 58 mm → 32 columns, 80 mm → 48 columns, connection/target labels | PASS |
| `TestConnectionKeyPreventsDuplicateDestinations` | Network and queue identities are normalized and dedupe-compatible | PASS |
| `TestProbeReportsProtocolResponseAsOnline` | Three layers; valid ESC/POS reply → online/likely printer | PASS |
| `TestProbeNeverCallsOpenPortAWorkingPrinter` | Silent open port → reachable but **unconfirmed**, never likely printer; bounded time | PASS |
| `TestProbeClosedPortIsOffline` | Closed port → offline with skipped protocol layer | PASS |
| `TestProbeRejectsPublicAndHostnameWithoutDialing` | Invalid targets are never dialed | PASS |
| `TestPlausibleStatus` | Accepts ESC/POS status byte shapes, rejects printable ASCII/odd bytes | PASS |
| `TestScannerFindsOnlyOpenHostsInsideAllowedRange` | 254 hosts, one deduplicated result, sorted, correct port | PASS |
| `TestScannerRejectsRangesOutsideLocalList` | Public, other, /16, /25, loopback, bad ports, malformed CIDR rejected | PASS |
| `TestScannerHostLimitIsEnforced` | Policy host cap enforced | PASS |
| `TestScannerIsSingleFlightAndCancellable` | Second concurrent scan refused; first cancellable | PASS |
| `TestScannerCancelStopsProgress` | Cancellation flag set, progress stops early, finished scan not cancellable | PASS |
| `TestLocalNetworksNeverReturnsUnsafeRanges` | Returned ranges are `/24`, private and non-loopback (or a clear error when no LAN exists) | PASS |
| `TestDiscoverUSBReadsOnlyPrinterClassDevices` | Synthetic sysfs: only interface class `0x07`; HID excluded; usblp path mapped; missing usblp explained; no direct-access claim | PASS |
| `TestDiscoverUSBHandlesMissingSysfs` | Missing sysfs is handled without panic; public API converts to empty list | PASS |
| `TestUSBIdentityDeduplicatesByPathAndSerial` | Path-first identity, then VID/PID/serial | PASS |
| `TestParseQueueListNormalizesAndDeduplicates` | Trim, dedupe, sort, control-char rejection | PASS |
| `TestSendSpoolerRawValidatesBeforeInvokingLp` | Invalid queue rejected before any process launch | PASS |
| `TestListQueuesIsBoundedAndNeverPanics` | Real `lpstat` lookup is time-bounded and never panics (no CUPS daemon here, so the positive-list path was not exercised) | PASS (error path) |
| `TestListQueuesIsBoundedAndNeverPanics` | Environment-dependent, but no panic and non-empty errors | PASS |
| `TestDiscoverUSBIsReadOnlyAndDeduplicated` | Public discovery never fails on a machine without USB | PASS |
| `TestSenderNetworkDeliversAllBytes` | Real loopback TCP server receives exact payload | PASS |
| `TestSenderClassifiesFailures` | Connect failure retryable; partial write and timeout uncertain | PASS |
| `TestSenderRejectsUnsafePayloads` | Empty and >1 MiB payloads rejected | PASS |
| `TestSenderSpooler` | Injected queue sender receives queue+bytes; failure → uncertain | PASS |
| `TestSenderUSB` | Full write success; zero-byte failure retryable; partial write uncertain; unsupported platform fails with guidance | PASS |

USB tests use a **synthetic sysfs tree**, not a physical device. They prove parser and policy
behaviour, not that a specific printer model works.

## 4. Persistence tests — `internal/store`

| Test | What it proves | Result |
|---|---|---|
| `TestPrintersMigrationInfersLegacyConnectionsIdempotently` | Legacy network/queue records gain valid kinds and defaults; unknown `USB` connection stays legacy; second migration does not bump versions | PASS |
| `TestSavePrinterRejectsDuplicateDestinations` | Duplicate network target and duplicate queue rejected | PASS |
| `TestRecoverInterruptedPrintJobsMarksUncertainAndBlocksRetry` | A job left in `printing` is recovered as uncertain, non-retryable, and recovery is idempotent | PASS |

## 5. Application/integration tests — `desktop`

### 5.1 Pages and UI state

| Test | UI state verified | Result |
|---|---|---|
| `TestPrinterCenterPagesRender` | Printer centre empty state, counters, nav destination, wizard fields, scan page bounds copy, queue counters, detail/edit/probe/test/delete sections | PASS |
| Page assertions inside `TestPrinterNetworkScanProgressAndCancel` | Open host, “پورت باز” badge, “افزودن این نشانی”, no auto-add | PASS |
| Existing `TestPrinterOptInGate` | Global switch still hides POS print actions and refuses jobs while disabled | PASS |
| Existing `TestPrinterCategoryRouting` | Settings page locked strings and category routing semantics unchanged | PASS |

### 5.2 Behaviour

| Test | What it proves | Result |
|---|---|---|
| `TestPrinterSaveValidationAndDuplicatePrevention` | Public IP/hostname/`.0`/unsafe USB rejected; valid profile persisted with legacy `connection`; duplicate destination refused; first printer enables the gate | PASS |
| `TestPrinterProbeLayersAndPersistence` | Probe sends `10 04 01`, receives `0x12`, reports online, persists three stages, detail page shows all layers and “آنلاین”; closed port reports offline | PASS |
| `TestTestPrintUsesNewTransportWithoutClaimingPhysicalPrint` | Real TCP fake receives the receipt; job status is `sent`; message says “ارسال شد” + “تأیید فیزیکی”; target snapshot stored | PASS |
| `TestPrinterNetworkScanProgressAndCancel` | Start redirect contains scan id; JSON progress completes with only `.7`; HTMX partial contains the result; page shows it; internet range refused; blocking scan cancels | PASS |
| `TestPrintQueueSafetyAndOrderIndependence` | Sent job cannot be retried and printer gets one payload; two concurrent dispatches print once; failed print leaves order status unchanged; uncertain job blocked; queued job cancellable; failed/sent not cancellable | PASS |
| `TestPrinterRolesDriveCashierAndKitchenDestinations` | Cashier role receives customer receipt, kitchen role receives kitchen ticket | PASS |
| `TestAutomaticPrintPolicyUsesNewTransport` | POS create → kitchen ticket via `sent`; completion → cashier receipt via `sent`; order remains completed | PASS |
| `TestPrinterDeleteKeepsJobsAndFreesRoutes` | Deletion requires explicit confirmation; removes printer and its routes; keeps job history | PASS |
| `TestPrinterEditActivationDeleteGuards` | Editing keeps the legacy `connection` in sync; activation toggles; delete without confirmation is refused | PASS |
| `TestPrintQueueRetryAllSkipsUncertainAndKeepsHistory` | Bulk retry sends nothing for an uncertain job and preserves its history | PASS |
| `TestPrinterUSBScanShowsAccessButNeverAutoAdds` | USB scan page shows accessible vs non-usblp devices and creates no printer | PASS |

## 6. Simulated integration harness

The suite uses:

1. **Fake thermal printer** (`printing_test.go`): loopback TCP listener that records every byte
   until EOF. Used for delivery, payload-content, copy-count and concurrency assertions.
2. **Protocol responder** (`printers_test.go`, `probe_test.go`): reads the 3-byte ESC/POS status
   request and replies `0x12`; proves the layered probe path.
3. **Injected dialer** (`Scanner`): marks exactly one host in an allowed `/24` as open; proves
   scan bounds, dedupe, progress and cancellation without touching a real network.
4. **Injected queue sender** (`Sender.QueueSend`): proves payload/queue hand-off and uncertainty
   handling without running `lp`.
5. **Synthetic sysfs tree** (`usb_linux_test.go`): proves only Printer Class devices appear and
   that a device without `usblp` is reported as not directly usable.

What simulation does **not** prove:

- that a particular printer firmware understands every ESC/POS command;
- that bytes physically reach paper;
- CUPS/Win32 driver behaviour;
- USB enumeration on a real bus or permissions/udev configuration.

## 7. Manual hardware checklist (NOT RUN in this workspace)

Each item is a release gate; record the device, OS, driver and observed paper output.

### 7.1 Network printer

- [ ] Add a private-IP printer on port 9100 via the wizard.
- [ ] Probe: expect address + TCP pass; protocol layer pass only if the printer answers `DLE EOT`.
- [ ] Test print: expect a page named “فیش آزمایشی چاپگر حرارتی”; UI says “ارسال شد” and explicitly
      does not claim physical success.
- [ ] Print a real customer receipt and a kitchen ticket; verify Arabic/Persian text, totals,
      separator width for 58/80 mm.
- [ ] Disable the printer; confirm POS hides print actions and no job is created.
- [ ] Change the IP to an unused private address; print; confirm the job fails, the order is intact,
      and retry is offered only as a safe pre-send failure.

### 7.2 Installed queue (CUPS)

- [ ] Install a queue with `lpstat -e` visible.
- [ ] Wizard lists the queue and datalist accepts it.
- [ ] Probe reports “queue installed” and marks physical confirmation skipped.
- [ ] Test print reaches the queue; `lpstat -o` shows/clears the job.

### 7.3 Windows spooler

- [ ] Build/run `GOOS=windows` binary on a Windows host.
- [ ] Queue list appears via PowerShell/WMIC.
- [ ] Probe detects the installed queue.
- [ ] RAW test print produces readable output (vendor driver may reinterpret RAW; note it).
- [ ] Verify permission errors surface as actionable messages, not silent failures.

### 7.4 USB

- [ ] On Linux with `usblp` loaded, open the wizard → USB scan.
- [ ] Only printer-class devices appear; HID/storage/hubs do not.
- [ ] A device without `usblp` shows “قابل چاپ مستقیم نیست”, not an add action.
- [ ] Add a device with `/dev/usb/lpN`; probe finds the character device; test print reaches paper.
- [ ] Unplug during print; job becomes uncertain and retry is blocked.
- [ ] Confirm no duplicate appears after repeated scans.

### 7.5 Network scan

- [ ] On a café LAN, scan the offered `/24`; verify progress and completion under 20 s.
- [ ] Cancel mid-scan; verify workers stop and page shows “متوقف شد”.
- [ ] Verify a host with port 9100 open but no printer is not presented as a confirmed printer.
- [ ] Confirm no result is added automatically and manual add still works with no LAN detected.

## 8. Regression coverage

The complete pre-existing suite was executed unchanged and passed. This includes receipt design,
order creation/payment, printer opt-in gate, print policies, category routing, POS, inventory,
staff, finance and store tests. The Agent source was not compiled or modified by this test plan.

## 9. Exit criteria

- All automated suites above pass (`go test ./...`).
- No new external Go module is required.
- Windows and macOS cross-compiles succeed.
- Automated HTML assertions cover every new page and critical state.
- Physical checklist is signed off per printer model before claiming hardware support.
- Until then, the coverage table keeps USB/Windows/CUPS physical rows in
  «نیازمند سخت‌افزار واقعی» and never reports them as fully tested.
