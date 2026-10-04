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

`receipt`: `{ "type":"receipt", "lines":["..."] }`, max 100 lines / 500 UTF-8 bytes each. Invoice store name 300 bytes, item name 300, order number 128, footer 1000. Rust byte-length limits are authoritative (JS length checks can be less restrictive for multibyte text). Rendered output ≤4096 pixel rows; longer receipts must be explicitly split into stable sub-job IDs. No raw ESC/POS or image URL variant is exposed in v1. QR/barcode/drawer are encoder APIs only, not remote hardware commands.

The **design** of the paper is the Agent's (right margin for Persian, left margin for amounts, pixel separators, emphasised store name and total — see [ESC/POS](escpos.md#printed-design)); the PWA only supplies semantics. `receipt` lines are printed as given, each hanging on the margin of its own writing direction. Empty `footer`/`orderNumber` simply omit their line; nothing is invented for them.

Paper mm, actual dots, copies, font and cut are stored local profile fields; no remote `width` override. Roles (`invoice`, `kitchen`, `bar`) and `autoPrint` appear in status. PWA respects them; Agent does not subscribe directly to MenuVex order events.

## Events and status semantics

- `printer.status`: `{type,version,printerId,status}`.
- `print.queued`, `print.printing`, `print.completed`, `print.failed`, `print.cancelled`: `{type,version,job}`.
- `resync`: refresh printers and queue; events are notifications, SQLite is authoritative.

Job fields: jobId, printerId, status, attempts, createdAt (Unix seconds), nextAt (Unix seconds), error (nullable). Payload intentionally omitted from queue/status responses. `completed` means transport handoff, not paper verification.

On reconnect SDK authenticates and refreshes both lists; existing subscriptions survive. It does **not** automatically resend unresolved print commands. Call `getJob` then resubmit the same logical ID when appropriate.

## Legacy Windows additive printer descriptor

The separate native Legacy implementation reports `connection: { "type": "spooler", "queueName": "installed Windows queue name" }`. It must not fabricate USB VID/PID or LAN addresses. Updated SDK accepts this alongside existing USB/network descriptors. An older copied SDK may reject this new descriptor and must be updated before pairing Legacy. The modern Rust backend is unchanged. Legacy status is conservatively `unknown`/`busy`; `completed` means Windows spooler handoff, not physical paper. See `legacy-windows/README.md` for variant-specific limits and installer validation.
