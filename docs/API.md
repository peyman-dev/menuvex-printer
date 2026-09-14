# Novex Printer Agent — API Reference

Base URL (default): `http://127.0.0.1:8765` — host and port are configurable
(see [Configuration](#configuration)). The agent binds to `127.0.0.1` by
default and must stay on localhost.

All responses are JSON. All endpoints except `GET /health` require:

```
Authorization: Bearer <token>
```

The token is generated on first run and printed on startup; retrieve it
anytime with `novex-printer-agent --print-token`.

Printer IDs in URL paths must be URL-escaped (`encodeURIComponent`), because
they contain `:` characters, e.g. `tcp%3A192.168.1.50%3A9100`.

## Printer IDs

| Scheme | Example | Meaning |
|---|---|---|
| `tcp:<host>:<port>` | `tcp:192.168.1.50:9100` | LAN/TCP printer. Port is never assumed — always part of the ID. Host may be an IP or DNS name. |
| `usb:<vid>:<pid>:<serial>` | `usb:04B8:0202:ABC123` | USB printer (Linux). VID/PID are uppercase hex. |
| `usb:win:<name>` | `usb:win:OCOM Printer` | USB printer (Windows spooler name). |
| `usb:cups:<queue>` | `usb:cups:OCOM_USB` | USB printer (macOS CUPS Raw queue). |

## Endpoints

### `GET /health` (no auth)

Liveness probe for "Agent: Connected" indicators.

```bash
curl http://127.0.0.1:8765/health
```

```json
{
  "status": "ok",
  "version": "1.0.0",
  "platform": "linux",
  "arch": "amd64",
  "time": "2026-09-14T14:00:00Z",
  "uptimeSeconds": 42
}
```

### `GET /api/v1/info`

Agent capabilities, including honest USB support info.

```json
{
  "version": "1.0.0",
  "platform": "linux",
  "arch": "amd64",
  "host": "127.0.0.1",
  "port": 8765,
  "usbSupported": true,
  "usbDetail": "Linux usblp backend (/dev/usb/lp* + sysfs discovery)",
  "time": "2026-09-14T14:00:00Z"
}
```

### `GET /api/v1/printers`

List registered LAN printers + live USB devices.

Query parameters:

| Param | Default | Meaning |
|---|---|---|
| `scan` | `false` | `true` also probes local /24 subnets for open printer ports |
| `ports` | `9100` | Comma-separated ports to probe, e.g. `ports=9100,515` |
| `scanTimeoutMs` | config `scanTimeoutMs` (8000) | Scan budget, capped at 30000 |

```bash
curl -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:8765/api/v1/printers?scan=true'
```

```json
{
  "printers": [
    {
      "id": "tcp:192.168.1.50:9100",
      "name": "OCOM",
      "type": "network",
      "status": "registered",
      "address": "192.168.1.50",
      "port": 9100
    },
    {
      "id": "usb:04B8:0202:ABC123",
      "name": "OCOM Thermal Printer",
      "type": "usb",
      "status": "available",
      "vendorId": 1208,
      "productId": 514,
      "serial": "ABC123",
      "manufacturer": "OCOM",
      "product": "Thermal Printer",
      "detail": "/dev/usb/lp0"
    }
  ],
  "scanned": true
}
```

Printer `status` is one of `available`, `registered`, `connected`, `error`.

### `POST /api/v1/printers`

Register a LAN printer by friendly name (persisted to the config file).

```bash
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"OCOM","address":"192.168.1.50","port":9100}' \
  http://127.0.0.1:8765/api/v1/printers
```

→ `201` with `{"success":true,"printerId":"tcp:192.168.1.50:9100","printer":{...}}`.

### `GET /api/v1/printers/:id`

Printer detail. `tcp:` IDs resolve ad-hoc (no registration needed);
`usb:` IDs resolve from live discovery.

### `DELETE /api/v1/printers/:id`

Remove a previously registered `tcp:` printer. (Ad-hoc `tcp:` IDs keep
working after removal; only the saved name is deleted.)

### `POST /api/v1/printers/:id/connect`

Open (and cache) a connection. Optional — `print` works without it.

→ `{"success":true,"printerId":"...","connected":true}`

### `POST /api/v1/printers/:id/disconnect`

Close the cached connection (no-op success when not connected).

→ `{"success":true,"printerId":"...","connected":false}`

### `POST /api/v1/printers/:id/print`

Send **raw bytes** — delivered to the printer untouched. The web app
generates ESC/POS / ZPL / EPL / CPCL itself.

JSON form (base64):

```bash
# ESC/POS: init + "Hi" + cut
DATA=$(printf '\x1b\x40Hi\x1d\x56\x00' | base64 -w0)
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"data\":\"$DATA\"}" \
  'http://127.0.0.1:8765/api/v1/printers/tcp%3A192.168.1.50%3A9100/print'
```

Raw form (`application/octet-stream` body = exact payload):

```bash
printf '\x1b\x40Hi\x1d\x56\x00' | curl -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/octet-stream' --data-binary @- \
  'http://127.0.0.1:8765/api/v1/printers/tcp%3A192.168.1.50%3A9100/print'
```

Success → `{"success":true,"printerId":"..."}`
Failure → `{"success":false,"error":{"code":"...","message":"..."}}`

Limits: max 10 MiB per request. Concurrent prints to the **same** printer
are serialized (one print at a time per printer); different printers print
in parallel.

### `GET /api/v1/printers/:id/status`

```json
{
  "printerId": "tcp:192.168.1.50:9100",
  "connected": true,
  "status": "connected",
  "lastPrintAt": "2026-09-14T14:00:00Z"
}
```

On failure the status is `"error"` with a `lastError` message.

### `GET /ws?token=...` (WebSocket, optional)

Real-time events. HTTP is enough for printing; the socket is only for
live status. Browsers authenticate via the `token` query parameter.

```
ws://127.0.0.1:8765/ws?token=<token>
```

Messages (server → client, JSON text frames):

```json
{"event":"agent.hello","version":"1.0.0"}
{"event":"printer.connected","printerId":"tcp:192.168.1.50:9100"}
{"event":"printer.disconnected","printerId":"tcp:192.168.1.50:9100"}
{"event":"printer.error","printerId":"tcp:192.168.1.50:9100","message":"PRINTER_CONNECTION_FAILED: ..."}
```

The server sends a WebSocket ping every 30 s (browsers answer automatically).

## Error codes

Stable `error.code` values — match on these, never on `message`:

| Code | HTTP | Meaning |
|---|---|---|
| `INVALID_REQUEST` | 400 | Bad JSON, bad base64, empty payload, bad register body |
| `INVALID_PRINTER_ID` | 400 | Malformed ID (want `tcp:<host>:<port>` or `usb:...`) |
| `PAYLOAD_TOO_LARGE` | 400 | Print payload exceeds 10 MiB |
| `UNAUTHORIZED` | 401 | Missing/invalid bearer token (or `token` for `/ws`) |
| `FORBIDDEN_ORIGIN` | 403 | Browser `Origin` not in `trustedOrigins` |
| `NOT_FOUND` / `PRINTER_NOT_FOUND` | 404 | Unknown endpoint / printer |
| `USB_DEVICE_NOT_FOUND` | 404 | USB device unplugged or wrong ID |
| `METHOD_NOT_ALLOWED` | 405 | Wrong HTTP method (`Allow` header set) |
| `USB_UNSUPPORTED` | 501 | No USB backend on this platform/build |
| `PRINTER_CONNECTION_FAILED` | 502 | TCP connect refused/unreachable, USB open failed, CUPS down |
| `PRINTER_WRITE_FAILED` | 502 | Write to the printer failed mid-job |
| `SCAN_FAILED` | 502 | LAN discovery failed |
| `PRINTER_TIMEOUT` | 504 | Connect or write timed out |
| `INTERNAL_ERROR` | 500 | Anything else |

## CORS

Only origins listed in `trustedOrigins` are accepted (exact match; `"*"`
may be configured explicitly but is discouraged). Requests without an
`Origin` header (curl, native apps) always pass. Preflights (`OPTIONS`)
are answered with `Allow-Methods: GET, POST, DELETE, OPTIONS` and
`Allow-Headers: Authorization, Content-Type`.

## Configuration

`host`, `port`, `trustedOrigins`, `token`, registered `printers` and
timeouts live in the OS config file (created on first run):

```json
{
  "host": "127.0.0.1",
  "port": 8765,
  "token": "...",
  "trustedOrigins": ["https://menuvex.ir", "http://localhost:3000"],
  "printers": [{"name": "OCOM", "address": "192.168.1.50", "port": 9100}],
  "connectTimeoutMs": 5000,
  "writeTimeoutMs": 10000,
  "scanTimeoutMs": 8000
}
```

CLI flags `--host`, `--port`, `--config` override the file (not persisted).
