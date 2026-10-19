# Printer integration architecture (MenuVex Offline)

Status: implemented on `arena/9d25b45b-menuvex-printer`  
Scope: `offline-menuvex/` only. The Rust/Tauri MenuVex Printer Agent is read-only reference material and
was not modified.

## 1. Goals and non-goals

Goals:

- Let a café operator discover, add, verify and manage receipt printers from inside Offline.
- Keep the existing receipt/order behaviour and tests intact while adding real transports.
- Never let a print failure lose or mutate an order.
- Never claim a physical print when only a TCP write or spool hand-off happened.
- Keep network scanning inside the operator's chosen local `/24`, bounded and cancellable.
- Keep OS-specific code isolated behind small interfaces and cover it with tests where possible.

Non-goals (with reasons in the audit's coverage table):

- Reimplementing the Agent's WebSocket/HMAC pairing and keyring (Offline is local and already has
  license/session/CSRF/host allowlists).
- Raw passthrough of frontend-authored ESC/POS.
- Creating Windows printer queues/drivers.
- Raster/image receipt rendering with custom fonts.

## 2. Component map

```
┌───────────────────────────── Browser (HTMX) ─────────────────────────────┐
│  /?page=printers         /?page=printer-new        /?page=printer-scan   │
│  /?page=printer&id=…     /?page=print-queue        /?page=settings       │
│  ui/printers.html (templates) + ui/static/app.css (printer classes)      │
└───────────────────────────────┬──────────────────────────────────────────┘
                                │ form posts + HTMX polling
┌───────────────────────────────▼──────────────────────────────────────────┐
│ offline-menuvex/printers.go            HTTP handlers + scan manager glue │
│ offline-menuvex/printing.go            receipt rendering + dispatch      │
│ offline-menuvex/app.go                 routes, view data, auth/CSRF gate │
└───────────────┬───────────────────────────────────────────────┬──────────┘
                │                                               │
┌───────────────▼────────────────────┐        ┌─────────────────▼──────────┐
│ internal/store                     │        │ internal/printer           │
│  printers.go  profiles + migration │        │  model.go   validation     │
│  printing.go  jobs + state machine │        │  probe.go   layered probe  │
│                                    │        │  scan.go    bounded scan   │
│  JSON file, atomic rename, mutex   │        │  transport.go send policy  │
└────────────────────────────────────┘        │  spooler_*.go   CUPS/Win   │
                                              │  usb_*.go       sysfs/usblp│
                                              └────────────────────────────┘
```

There is no new external dependency: `go.mod` still has zero requirements.

## 3. Package boundaries

### `internal/printer` (pure domain + transports)

- `model.go` — `Profile`, `USBDevice`, validation, defaults, `ConnectionKey`, display helpers.
- `errors.go` — `OpError{code,message,retryable,uncertain}`.
- `probe.go` — `Prober` with connect → ESC/POS real-time status request → result layers.
- `scan.go` — local `/24` detection, single-flight bounded scanner with progress and cancel.
- `transport.go` — `Sender` with a 1 MiB ceiling, network/spooler/USB dispatch, timeout and
  uncertainty classification.
- `spooler_unix.go` / `spooler_windows.go` / `spooler_other.go` — OS-specific listing and RAW hand-off.
- `usb_linux.go` / `usb_other.go` — sysfs discovery and `/dev/usb/lp*` opening.

The package never imports `store` or `desktop`. It can be unit-tested without HTTP or a database.

### `internal/store`

- `printers.go` — JSON entity mapping, validation, duplicate prevention, migration, check evidence.
- `printing.go` — job creation, dedupe, claim/fail/cancel, counts, filtering.
- `store.go` already owns the mutexed `clone → mutate → marshal → temp + fsync + rename` transaction.

### `desktop` (HTTP + templates)

- `printers.go` — pages data, wizard parsing, probe persistence, scan endpoints, queue actions.
- `printing.go` — receipt lines/ESC/POS and `dispatchPrintJob` choosing legacy vs new transport.
- `app.go` — routes, view fields, render data switch, global printer switch.
- `ui/printers.html` — `printerPages` and `scanResults` templates.
- `ui/static/app.css` — printer centre, wizard, probe, scan and queue styles.

## 4. Persistence model

No new database was introduced. Printers remain entities of kind `printer` in the existing JSON
store; print jobs remain `print_job`. This preserves backup/export, tombstones, versioning and the
test helpers.

### 4.1 Printer entity fields

| Field | Type | Behaviour |
|---|---|---|
| `title` / `name` | string | Display name (≤128 runes, no control chars) |
| `model` | string | Display metadata only |
| `kind` | `network` \| `spooler` \| `usb` | Selects transport |
| `host`, `port` | string | Network target; literal private IPv4 + 1–65535 |
| `queue_name` / `queue` | string | OS print queue, validated, never passed through a shell |
| `usb_path`, `usb_vid`, `usb_pid`, `usb_serial`, `usb_bus`, `usb_port`, `usb_product`, `usb_manufacturer` | string | USB identity; direct printing requires `/dev/usb/lp*` |
| `paper_width` | `58mm` \| `80mm` | Controls ESC/POS line width (32 / 48 columns) |
| `width_dots` | integer | Raster width validation (128–832, %8); not exposed as an extra UI knob |
| `copies` | integer | 1–3 profile validation (routing copies still come from routes) |
| `cut` | bool | If false, `GS V` is omitted from the generated payload |
| `role` | `cashier` \| `kitchen` \| `all` \| empty | Chooses customer vs kitchen destination |
| `station` | string | Human station label; also used by the legacy station matcher |
| `is_active` | bool | Inactive printers receive no automatic jobs and are skipped by discovery selection |
| `connection` | string | Legacy compatibility field, kept in sync for network printers |
| `last_check_at/state/detail/stages` | string/JSON | Persisted evidence of the latest probe |

`ProfileFromEntity` is deliberately permissive: older records with only `connection`/`queue` remain
readable. A missing `kind` means “legacy path” until migrated.

### 4.2 Migration (`printer_schema = 1`)

`Store.MigratePrinterSchema` runs once from `New` inside the normal transaction:

1. if `meta.printer_schema == "1"`, do nothing;
2. for each live printer, infer a kind only when the existing values are valid
   (private-IPv4 `host:port` → network; queue → spooler; `/dev/...` → USB);
3. fill missing `paper_width`/`width_dots`/`cut`/`is_active` defaults;
4. set the metadata marker.

It never rewrites an operator-visible value, never invents a kind for invalid legacy data, and is
idempotent (tested). Legacy records without an inferred kind continue through the old sender.

## 5. Print-job state machine

```
                 claim (atomic)
 pending ───────────────────────► printing ──► sent        (new transports; hand-off)
    │                                │
    │ cancel (queued only)           ├────────► printed     (legacy sender)
    ▼                                │
 cancelled                          └────────► failed ── retry (safe only) ──► printing
                                                     │
                                                     └─ uncertain=true ──► no automatic/one-click retry
```

- `ClaimPrintJob` increments attempts and moves to `printing` before any byte is sent. A second
  request sees `printing`, `sent` or `printed` and is refused. This is what makes repeated HTTP
  requests and double clicks safe.
- `FailPrintJob` records `uncertain`; `Retryable()` is false when uncertain, attempts ≥ 5, or the
  job is terminal.
- `CancelPrintJob` only accepts `pending`.
- `sent` means the transport accepted the bytes. Physical paper is unverifiable over RAW. Legacy
  jobs keep `printed`, so existing data and tests are unchanged. No summary message calls a `sent`
  job “printed”; the UI appends “بدون تأیید چاپ”.
- On startup `RecoverInterruptedPrintJobs` closes any job left in `printing` as `failed` +
  `uncertain` with an explanatory message. Recovery is idempotent and deliberately does not retry,
  because a crash may have happened after bytes reached paper.
- Job payloads snapshot `printer_kind`, `target` and `cut`; retry works from the snapshot if the
  printer was edited, and the live profile is preferred when present.

## 6. Dispatch flow

`Application.dispatchPrintJob`:

1. atomically claim the job;
2. load the order/profile and build receipt lines (category filtering unchanged);
3. render text ESC/POS with the job's paper width and cutter flag;
4. if `job.PrinterKind != ""`: resolve the live profile, else reconstruct from the snapshot, then
   call `Sender.Send` once per copy;
5. if `PrinterKind == ""`: use the original `sendToPrinter` path unchanged (spool dir → TCP → CUPS);
6. persist `sent`/`printed` on success or `failed` (+uncertain) on error.

Order status is never touched here. Automatic policies call the same dispatcher, so category,
cashier/kitchen, checkout/completion and manual printing all share the same safety rules.

## 7. Probe design (layered evidence)

`Prober.Probe(host, port)`:

1. **Address policy** — literal private IPv4, valid port; invalid input is never dialed.
2. **TCP reachability** — 3 s connect. Failure → `offline`.
3. **ESC/POS status** — `DLE EOT 1` (`10 04 01`) with a 1.5 s read window.
   - valid status byte → `online` (likely printer);
   - any other response → `unconfirmed`;
   - silence/timeout → `unconfirmed`, because many RAW bridges do not answer.

Each layer is persisted as a stage with pass/warn/fail/skip. The UI shows all layers. A probe never
sends a receipt, and its summary never says “printed”.

For spooler printers the probe verifies the queue is installed; for USB it verifies the character
device exists. Both clearly mark physical confirmation as skipped.

## 8. Scan design

- Allowed ranges come from `LocalNetworks()`: active, non-loopback, private IPv4 interfaces,
  reduced to the containing `/24`. If none exists, the scan form is replaced with guidance; manual
  add remains available.
- The form is a select of those ranges, not free-form CIDR, so an internet sweep is not expressible.
  The server repeats the validation (`/24`, canonical CIDR equality, exact membership in the
  allowed list) and rejects everything else with an operator-readable error.
- Policy: 254 hosts, 32 workers, 400 ms per host, 20 s total, one active scan.
- `Scanner` state is in memory. The page polls every second with `hx-get`; when the scan finishes
  the polling attribute disappears. Results are deduplicated by host and sorted numerically.
- Cancel propagates a context cancel to workers.
- Results are candidates only. Adding requires the normal wizard and probe/test print.

## 9. Transport safety matrix

| Transport | OS | Listing | Send | Success meaning | Real-hardware validation |
|---|---|---|---|---|---|
| Network RAW | all | manual/probe | `net.Dial` + write deadline | TCP write completed | simulated fake printer |
| CUPS | Linux/macOS | `lpstat -e` | `lp -d <validated> -o raw`, bytes on stdin | queue accepted | pending |
| Windows spooler | Windows | PowerShell/WMIC queue list | winspool RAW sequence | spool accepted | pending |
| USB | Linux | sysfs class `0x07` + `usbmon`/`usbmisc` mapping | write `/dev/usb/lp*` with timeout | device write completed | pending |

Argument safety: queue names are validated (no control chars), passed as a single argv element,
and no user text is ever concatenated into a shell command. USB paths must be absolute,
clean, and under `/dev/usb/lp`.

## 10. UI map

| Page | Query | Content |
|---|---|---|
| Printer centre | `/?page=printers` | inventory cards, counters, global print switch, recent jobs |
| Add wizard | `/?page=printer-new` | connection type, target fields, USB scan, queue datalist, role/paper/cut/active |
| Network scan | `/?page=printer-scan` | allowed `/24` list, bounded scan, live progress, results, cancel |
| Printer detail | `/?page=printer&id=…` | edit form, probe evidence, test print, activate/deactivate, delete (explicit checkbox) |
| Print queue | `/?page=print-queue` | counters, status/printer filters, retry, retry-all safe failures, cancel queued |

All pages use the existing layout, RTL, Peyda/YekanBakh fonts, `.panel-card`, `.data-table`,
`.badge`, toast seeds and HTMX navigation. No `alert()` or `confirm()` is used.

### Routes

Existing: `POST /local/print-test`, `/local/print-order`, `/local/print-retry`,
`/local/print-policy`, `/local/printer-route`, `/local/printer-route-toggle`,
`POST /local/printer-toggle`.

Added:

- `POST /local/printer-save`, `/local/printer-active`, `/local/printer-delete`
- `POST /local/printer-probe` (`ajax=1` returns JSON, otherwise a detail-page toast)
- `POST /local/printer-usb-scan`
- `POST /local/printer-scan`, `GET /local/printer-scan-status?scan=…`,
  `POST /local/printer-scan-cancel`
- `POST /local/print-queue-retry`, `/local/print-queue-retry-all`, `/local/print-queue-cancel`

All new non-GET routes inherit the existing middleware order: host allowlist → CSP/no-store →
1 MiB body limit → CSRF (non-GET outside `/api/v1/`) → license → session. GET status polling is
read-only and exempt from CSRF, like every other GET.

## 11. Timeout and failure policy

| Operation | Bound |
|---|---|
| Network connect | 3 s |
| Network write | 3 s per write, 10 s deadline in the new sender |
| Probe status read | 1.5 s after connect |
| Scan host | 400 ms |
| Scan total | 20 s |
| CUPS command | 4 s list, 20 s send |
| USB open/write | 10 s overall send guard |
| Windows spool | 25 s guard |

Failure classes:

- **retryable** — the transport confirms no bytes were handed over (connect refused, zero-byte
  write failure, missing queue).
- **uncertain** — any partial write, write timeout, or failure after an earlier copy succeeded.
  The UI blocks one-click retry and tells the operator to inspect paper.
- **invalid/config** — validation errors; never retried automatically.

## 12. Compatibility and rollout

- Existing `printer` records and their tests keep working: no `kind` → legacy sender.
- Network records saved by the new UI also write the legacy `connection` field, so category
  routing and older UI paths still see them.
- `printer_enabled` semantics are preserved; the global switch now honours `return_page`.
- Deleting a printer removes its category routes but keeps orders and print-job history.
- The existing settings page keeps all test-locked text and forms; the new centre is linked from
  the navigation and from settings/cards.
