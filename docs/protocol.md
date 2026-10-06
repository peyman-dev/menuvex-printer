# Protocol v1

Endpoint: `ws://127.0.0.1:8765/` (configurable high port). UTF-8 JSON text only; max 128 KiB input. Exact production Origins: `https://menuvex.ir`, `https://www.menuvex.ir`. Debug builds additionally allow exactly `http://localhost:5173` and `http://127.0.0.1:5173`. Missing/null origins, alternate Host names, query strings and other paths are rejected at upgrade. No URL token.

## Authentication

Agent sends:

```json
{
  "type": "hello",
  "version": 1,
  "agentVersion": "1.0.0",
  "authentication": "hmac-sha256",
  "serverProof": "<base64 server HMAC>",
  "nonce": "<base64 of 32 random bytes>"
}
```

PWA imports the locally supplied 32-byte base64 secret as non-extractable WebCrypto HMAC/SHA-256 signing key, stores the CryptoKey in IndexedDB, and computes:

```text
base64(HMAC-SHA256(key, UTF8("menuvex-print-agent:v1\n" + nonce + "\n" + exactOrigin)))
```

First verify `serverProof` using the same key with the distinct message domain `menuvex-print-agent:server:v1\n<nonce>\n<origin>`. Only a key-holding Agent should receive authenticated commands. Then within ten seconds:

```json
{ "version": 1, "requestId": "auth:1", "type": "authenticate", "proof": "<base64 signature>" }
```

Success: `{"type":"authenticated","version":1,"requestId":"auth:1","agentVersion":"1.0.0"}`. Failure returns `AUTH_FAILED` and closes. Fresh nonce each connection prevents proof replay. Only one authentication attempt per socket. Authentication is stronger than sending a reusable plaintext token every connection; version 1 uses proof, **not** the request's illustrative `token` field.

## Commands

Every request has `version: 1`, unique correlation `requestId`, and `type`. Unknown fields and commands rejected. IDs: 1–128 ASCII alphanumeric, `-`, `_`, `:`, `.`. Integers must be integers, not strings. No control characters except newline in document text.

| Type                 | Additional fields          | Response data                                   |
| -------------------- | -------------------------- | ----------------------------------------------- |
| `hello`, `ping`      | none                       | version, agentVersion                           |
| `agent.status`       | none                       | ready, agentVersion, port, routes, serverError  |
| `printers.list`      | none                       | configured printers with status                 |
| `discover.network`   | none                       | LAN candidates `[{host, port}]` on port 9100    |
| `printers.installed` | none                       | OS print queues `[{queueName}]` (spooler/CUPS)  |
| `printer.get`        | printerId                  | one configured printer                          |
| `printer.save`       | printer                    | the saved printer (create or update by id)      |
| `printer.test`       | printerId, jobId           | persistent test job                             |
| `print`              | printerId, jobId, document | existing/new persistent job                     |
| `print.status`       | jobId                      | job                                             |
| `queue.list`         | none                       | active-first / recent history, at most 500 jobs |
| `queue.cancel`       | jobId                      | cancelled job; only queued jobs cancellable     |
| `queue.clear`        | none                       | `{cancelled, removed}` counts (see below)       |
| `agent.shutdown`     | none                       | `LOCAL_CONFIRMATION_REQUIRED` (tray only)       |

Successful command response:

```json
{ "type": "response", "version": 1, "requestId": "r1", "data": {} }
```

Error response:

```json
{
  "type": "error",
  "version": 1,
  "requestId": "r1",
  "error": {
    "code": "PRINTER_NOT_FOUND",
    "message": "Printer is not configured",
    "retryable": false,
    "uncertain": false
  }
}
```

Malformed envelopes may not have a usable requestId; server sends an uncorrelated error and closes. SDK validates envelopes **and** method-specific response data, uses request deadlines and rejects pending promises on disconnect.

`discover.network` performs a passive scan of the local subnet for hosts accepting a RAW TCP connection on port 9100 and returns candidates `[{host, port}]`; it never sends print data. A candidate is only confirmed by `printer.test`. This lets the web app list printers without the operator finding IPs through OS tools.

`printers.installed` lists the print queues already installed on the operating system (`EnumPrinters` on Windows, `lpstat -e` on CUPS platforms) as `[{queueName}]`, for configuring a `spooler` connection. Both agents answer it.

`queue.clear` empties the queue in one call: every `queued` job becomes `cancelled` (each emits a `print.cancelled` event) and all finished rows (`completed`, `failed`, `cancelled`) are deleted, followed by a `resync` event. A job currently `printing` is never touched — its bytes may already be at the printer. **Deleting history removes the duplicate-submission protection of those job IDs**: a cleared ID submitted again prints again. The response is `{cancelled, removed}` counts.

## Semantic documents

```json
{
  "version": 1,
  "requestId": "r1",
  "type": "print",
  "jobId": "order:1842:invoice",
  "printerId": "invoice-printer",
  "document": {
    "type": "invoice",
    "data": {
      "storeName": "نام فروشگاه",
      "orderNumber": "1842",
      "items": [{ "name": "نام کالا", "quantity": 1, "unitPrice": 240000 }],
      "total": 240000,
      "footer": "با سپاس"
    }
  }
}
```

The illustration is protocol documentation, not seeded order data. Total is supplied by the real business adapter (taxes/discounts may make it differ from sum of items); Agent does not recalculate business accounting. Item quantity 1–9999; unitPrice ≤900,000,000; total ≤9,000,000,000,000; max 100 items. Fractional quantities require a future version/business adapter, not silent rounding. Currency is not automatically converted.

Invoices also accept **optional** app-template fields, printed only when present (additive; both agents accept them): `title` (slip title, default `فاکتور فروش`; byte limit 128), `address` (500), `phone` (64), `date` (64, passed through verbatim — the agent does not read clocks or convert calendars), `status` (128), `orderType` (128), `table` (64), `note` (500), `currency` (32, the word appended to amounts, e.g. `تومان`) and `subtotal` (integer ≤9,000,000,000,000, shown as `جمع اقلام`). The agent never invents any of them: omitted fields simply do not print, amounts stay bare without `currency`, and `subtotal` is never derived from the items.

`receipt`: `{ "type":"receipt", "lines":["..."] }`, max 100 lines / 500 UTF-8 bytes each. Ordinary lines align by writing direction. A line made from at least three dash/equal/box-rule marks becomes a pixel separator; `[center] text` (or `[center]: text`) centers a line; two-to-four pipe-separated cells become right-to-left columns; and an empty line adds spacing. Rendered output ≤4096 pixel rows; longer receipts must be explicitly split into stable sub-job IDs. No image URL variant is exposed. QR/barcode/drawer are encoder APIs only, not remote hardware commands.

### Frontend-owned layout: `escpos`

`invoice` and `receipt` are **semantic**: the agent decides the paper layout (see [ESC/POS](escpos.md#printed-design)). When the frontend must keep its own receipt template exactly, it sends the finished bytes instead and the agent only transports them:

```json
{
  "version": 1,
  "requestId": "r1",
  "type": "print",
  "jobId": "order:1842:invoice",
  "printerId": "invoice-printer",
  "document": { "type": "escpos", "data": "<base64 ESC/POS>" }
}
```

`commands` (array of byte values, ≤16 KiB) and `data` (base64, for full raster receipts) may be combined; `commands` are sent first, then the decoded `data`. Total decoded size ≤98,304 bytes. The agent performs **no** rendering, no shaping, no font substitution and appends no `ESC @`, feed or cut of its own — the bytes are handed to the transport unchanged, so the frontend is the sole source of truth for that document.

Because raw bytes can also drive a cash drawer or reconfigure a printer, `escpos` is refused with `RAW_PASSTHROUGH_DISABLED` unless the operator turned on **raw passthrough** for that printer in the local agent window (`rawPassthrough` on the printer profile, default `false`). A remote `printer.save` cannot switch it on: the stored local value always wins. The Legacy Windows agent does not implement `escpos` at all.

`printer.test` uses the selected profile's configured `widthDots` for the raster and prints a `Paper profile | <mm> mm | <dots> dots` label plus sample columns and a pixel rule. Generic ESC/POS and OS spooler APIs cannot reliably detect the physical paper roll width; the label describes the saved profile and the operator should compare it with the printer manual.

The **design** of the paper is the Agent's (right margin for Persian, left margin for amounts, pixel separators, emphasized store name and total — see [ESC/POS](escpos.md#printed-design)); the PWA only supplies semantics. Ordinary `receipt` lines hang on the margin of their writing direction. Optional receipt hints are `---` (pixel rule), `[center] text` (centered line), and two-to-four pipe-separated cells (right-to-left columns); empty lines add spacing. The same hints work in the Legacy Windows renderer.

Paper mm, actual dots, copies, font and cut are stored local profile fields; no remote `width` override. Roles (`invoice`, `kitchen`, `bar`) and `autoPrint` appear in status. PWA respects them; Agent does not subscribe directly to MenuVex order events.

## Events and status semantics

- `printer.status`: `{type,version,printerId,status}`.
- `print.queued`, `print.printing`, `print.completed`, `print.failed`, `print.cancelled`: `{type,version,job}`.
- `resync`: refresh printers and queue; events are notifications, SQLite is authoritative.

Job fields: jobId, printerId, status, attempts, createdAt (Unix seconds), nextAt (Unix seconds), error (nullable). Payload intentionally omitted from queue/status responses. `completed` means transport handoff, not paper verification.

On reconnect SDK authenticates and refreshes both lists; existing subscriptions survive. It does **not** automatically resend unresolved print commands. Call `getJob` then resubmit the same logical ID when appropriate.

## Spooler descriptor compatibility

The stored printer profile and transport remain truthful: spooler profiles use `connection: { "type": "spooler", "queueName": "installed OS queue name" }` internally. For `printers.list` and `printer.get`, both agents also publish the reserved USB-compatible wire descriptor `{ "type": "usb", "vendorId": 0, "productId": 0, "serial": "queue:<queue name>", "bus": 0, "ports": [], "interface": 0, "endpoint": 0, "alternate": 0 }`. This compatibility form is not a physical USB device; check the SDK's `spoolerQueueName(connection)` before treating a `usb` descriptor as USB. Never request WebUSB for a descriptor with that queue marker.

`printer.save` accepts either the explicit `spooler` form or that exact reserved USB-shaped form and normalizes it back to a spooler profile before validation/persistence. VID/PID zero is reserved for this purpose; normal USB profiles continue to require nonzero IDs. `printers.installed` remains the authoritative queue picker. `completed` means the OS spooler accepted the document, not that paper came out. See `legacy-windows/README.md` for Legacy-specific limits.

## Connection types

`connection.type` is `"network"`, `"usb"` or `"spooler"` and nothing else. An unsupported value is rejected **by name** so the operator sees what their client sent:

```json
{
  "code": "UNSUPPORTED_CONNECTION_TYPE",
  "message": "Unsupported printer connection type: \"lan\". Supported: \"network\", \"usb\", \"spooler\".",
  "retryable": false,
  "uncertain": false
}
```

The modern agent raises it on `printer.save` and in the local `save_config` command; the SDK raises the same wording client-side before sending; the Legacy Windows agent answers with the same code and its own supported list (`spooler`, `network`). The echoed value is truncated to 64 characters and stripped of control characters, so a hostile payload cannot forge a log line or an error banner.

The website-facing wire (`printers.list`, `printer.get`, `printer.save`) only ever carries `network` or `usb` — `spooler` is projected onto the reserved USB-shaped descriptor described above — so a frontend validator that knows only those two discriminators still parses every printer. Use `spoolerQueueName(connection)` before reading `type`.

## Failure isolation

A printer failure is contained to that printer's job. Concretely:

- A command handler that panics answers that one request with `AGENT_INTERNAL_ERROR`; the WebSocket stays open and every other printer keeps working.
- The queue worker never terminates on a job failure: it logs, backs off (1 s → 15 s) and continues. Sustained internal failures surface as `serverError: "QUEUE_DEGRADED: …"` while jobs are still accepted and retried.
- A poisoned internal lock is recovered, not escalated. Before this, one panic permanently poisoned a mutex and every later `agent.status` / `printers.list` / `queue.list` failed, which looked exactly like "the website lost the agent".
- Every USB operation runs on its own thread with a hard deadline (10 s probe, 25 s discovery, 35 s print) and releases its claimed interface on every path, so a wedged device cannot pin the agent or the desktop window.

Structured log events (daily rolling log, `RUST_BACKTRACE=1`): `PRINTER_DISCOVERY_STARTED`, `PRINTER_FOUND`, `PRINTER_DISCOVERY_FINISHED`, `PRINTER_REGISTERED`, `PRINTER_STATUS_CHANGED`, `USB_DEVICE_OPENED`, `USB_INTERFACE_CLAIMED`, `USB_INTERFACE_RELEASED`, `PRINT_JOB_RECEIVED`, `PRINT_JOB_STARTED`, `PRINT_JOB_COMPLETED`, `PRINT_JOB_RETRY_SCHEDULED`, `PRINT_JOB_FAILED`, `TRANSPORT_SEND_FAILED`, `QUEUE_WORKER_ERROR`, `QUEUE_WORKER_PANIC`, `MUTEX_POISON_RECOVERED`, `COMMAND_PANIC`, `TASK_STOPPED`, `PANIC`. Errors carry `printer_id`, `connection`, `vendor_id`, `product_id`, the error code and a backtrace; receipt contents and pairing secrets are never logged.
