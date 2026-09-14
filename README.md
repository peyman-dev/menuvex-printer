# Novex Printer Agent

A lightweight, QZ Tray-style **local bridge between web browsers and printers** —
with no Java, no database, and no cloud. One static binary per OS.

```
Browser Web App (Next.js, …)
        ↓  localhost HTTP (JSON + base64)
Novex Printer Agent (Go, 127.0.0.1:8765)
        ↓  LAN/TCP socket  ·  native USB
Thermal / label printer
```

The agent is a **transport bridge, nothing more**: discover printer →
connect → send bytes → return result. The web app generates ESC/POS, ZPL,
EPL, CPCL (anything) itself; the agent delivers the bytes untouched.

- **LAN/TCP printing** (primary): raw socket writes to `tcp:<host>:<port>`,
  configurable port, timeouts, reconnect, per-printer mutex, subnet discovery.
- **USB printing**: real native backends — Linux `usblp`, Windows Winspool
  RAW, macOS CUPS IPP to Raw queues. Honest per-OS capabilities, never faked
  (see [docs/USB.md](docs/USB.md)).
- **Local API**: JSON over HTTP + optional WebSocket events, bearer token,
  explicit CORS origins, loopback-only bind by default.
- **TypeScript SDK**: `sdk/typescript` — zero dependencies, browser + Node.
- **Web console**: served by the agent at `/` — connect, list, test-print raw ESC/POS.

## 1. Project structure

```
cmd/agent/                  agent entrypoint (flags, startup, shutdown)
internal/
  api/                      HTTP routes + WebSocket hub + embedded web console (demo.html)
  printer/                  printing engine: registry, TCP transport, error codes
  network/                  LAN subnet scanner (open printer ports)
  usb/                      USBPrinter interface + Linux/Windows/macOS backends + mock
  security/                 bearer-token auth + explicit-origin CORS
  config/                   OS-specific config file + token generation
  platform/                 version + start-on-login (all OSes, pure Go)
sdk/typescript/             TypeScript SDK (zero deps) + tests
(web console served by the agent itself at http://127.0.0.1:8765/)
scripts/                    installers, .deb builder, Inno Setup script, udev rule
docs/                       API.md · USB.md · INSTALL.md
ci/github-ci.yml              GitHub Actions matrix (copy to .github/workflows/ — see ci/README.md)
```

Platform code is isolated: `internal/usb/usb_{linux,windows,darwin}.go`
(plus `usb_unsupported.go` fallback). Everything is **Go stdlib only, no
CGO**, so cross-compiling is a one-liner per OS.

## 2. How to run in development

```bash
# needs Go ≥ 1.21 (no other dependencies)
go run ./cmd/agent
# → prints config path + API token, serves http://127.0.0.1:8765

# run the test suite (mock TCP printers + mock USB, no hardware needed)
go test ./...

# TypeScript SDK
cd sdk/typescript && npm install && npm test
```

Open the built-in console at http://127.0.0.1:8765/ in a browser, paste
the token, and test-print. For TCP testing without hardware, point it at
any `tcp:127.0.0.1:<port>` listener (the Go tests spin such mocks up
automatically).

## 3. How to build Windows

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o NovexPrinterAgent.exe ./cmd/agent
# ARM64: GOARCH=arm64
```

## 4. How to build macOS

```bash
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o NovexPrinterAgent ./cmd/agent
# Intel: GOARCH=amd64
```

## 5. How to build Linux

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o novex-printer-agent ./cmd/agent
# or: make build  (native) · make build-all  (all 6 binaries into dist/)
```

## 6. How to install the agent

Full guide: [docs/INSTALL.md](docs/INSTALL.md). Short version:

```bash
# Linux / macOS (user-level, sets up start-on-login)
./scripts/install-linux.sh
./scripts/install-macos.sh
```

```powershell
# Windows (user-level, sets up start-on-login)
powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1
```

Packaging: `make package` (zips/tarballs), `scripts/build-deb.sh` (`.deb`),
`scripts/novex-printer-agent.iss` (Windows installer via free Inno Setup).
The agent manages its own start-on-login:
`novex-printer-agent --install-autostart`. v1 is intentionally headless
(no tray UI — the printing engine needs no UI at all).

## 7. API documentation

Full reference: [docs/API.md](docs/API.md).

```
GET    /                              built-in web console (no auth)
GET    /health                        (no auth)
GET    /api/v1/info
GET    /api/v1/printers[?scan=true&ports=9100]
POST   /api/v1/printers               {name, address, port}
GET    /api/v1/printers/:id
DELETE /api/v1/printers/:id
POST   /api/v1/printers/:id/connect
POST   /api/v1/printers/:id/disconnect
POST   /api/v1/printers/:id/print     {data: "<base64>"} or octet-stream
GET    /api/v1/printers/:id/status
GET    /ws?token=...                  WebSocket events (optional)
```

Success: `{"success":true,"printerId":"..."}` ·
Failure: `{"success":false,"error":{"code":"PRINTER_CONNECTION_FAILED","message":"..."}}`
with [stable error codes](docs/API.md#error-codes).

## 8. TypeScript SDK usage

```ts
import { NovexPrinterAgent } from "novex-printer-agent";

const agent = new NovexPrinterAgent({ host: "127.0.0.1", port: 8765, token: "..." });
await agent.connect();
const printers = await agent.getPrinters();          // or { scan: true }
await agent.print({ printerId: "tcp:192.168.1.50:9100", data: escPosBytes }); // Uint8Array
```

See [sdk/typescript/README.md](sdk/typescript/README.md) for the full API
(`connectPrinter`, `getStatus`, `subscribe`, …) and a Next.js snippet.

## 9. TCP printer configuration example

```bash
TOKEN=$(novex-printer-agent --print-token)

# Register once by name (persisted in the agent config):
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"OCOM","address":"192.168.1.50","port":9100}' \
  http://127.0.0.1:8765/api/v1/printers

# …or just print ad-hoc to any tcp:<host>:<port> (port never assumed):
printf '\x1b\x40Hello\x1d\x56\x00' | curl -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/octet-stream' --data-binary @- \
  'http://127.0.0.1:8765/api/v1/printers/tcp%3A192.168.1.50%3A9100/print'
```

No drivers needed for network printers — the agent opens a plain TCP
socket, writes your bytes, and reports the result.

## 10. USB printer configuration example

```bash
# 1. Meet the OS requirement (one-time):
#    Linux:   be in the lp group (see docs/USB.md)
#    Windows: install the printer in Settings → Printers
#    macOS:   lpadmin -p OCOM_USB -E -v "usb://..." -m raw

# 2. List — the printer appears with its real backend ID:
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8765/api/v1/printers
# → {"id":"usb:04B8:0202:ABC123", ...}            (Linux)
# → {"id":"usb:win:OCOM Printer", ...}            (Windows)
# → {"id":"usb:cups:OCOM_USB", ...}               (macOS)

# 3. Print exactly like TCP:
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"data":"G0BIaQ==..."}' \
  'http://127.0.0.1:8765/api/v1/printers/usb%3A04B8%3A0202%3AABC123/print'
```

Details + troubleshooting: [docs/USB.md](docs/USB.md).

## 11. Known platform limitations

Honest v1 boundaries (details in [docs/USB.md](docs/USB.md)):

- **Linux USB**: only printers claimed by the kernel `usblp` driver
  (`/dev/usb/lp*`) are visible; needs `lp` group / udev rule for access.
- **Windows USB**: only printers **installed in Windows on a USB port**
  are visible; identified by spooler name; VID/PID unknown (`0`).
- **macOS USB**: requires a manually created **Raw** CUPS queue; only
  `usb://` queues listed; VID/PID unknown (`0`); CUPS must be running.
- **Discovery**: LAN scan finds *open ports*, not models — naming stays a
  manual/explicit step. No Bluetooth, serial, cloud or driver-level APIs.
- **Scope**: no queue persistence, no database, no PDF/receipt rendering,
  no tray UI in v1 (headless + autostart by design).

## Verification status

Verified with a real Go toolchain (Linux), all green:

- `gofmt` clean, `go vet ./...` clean on **linux, windows and darwin**
  (cross-`vet` compiles every platform file + test).
- `go test ./...` passes, including with `-race`: TCP printing, exact-byte
  delivery, concurrent same/different-printer writes, timeouts, refused
  connections, auth, CORS, API errors, LAN scan, WebSocket events, USB
  mocks, Linux sysfs parsing and the macOS IPP codec.
- All 6 binaries (`windows/darwin/linux × amd64/arm64`) build with
  `CGO_ENABLED=0` — single static binaries (~7 MB).
- End-to-end: real binary + mock TCP printer + real SDK — byte-exact
  ESC/POS delivery over JSON and octet-stream, plus live WS events.
- TypeScript SDK: `tsc` strict build + 9/9 `node --test` tests pass.
- `ci/github-ci.yml` (copy to `.github/workflows/`) repeats all of this
  on Windows and macOS runners.
- Real-hardware USB printing is implemented against documented OS APIs but,
  like any v1, wants field reports per printer model — please open issues
  with make/model/OS when something misbehaves.

## Security model

Loopback-only bind (`127.0.0.1`, configurable port 8765), random per-install
bearer token, exact-match `trustedOrigins` CORS, 10 MiB payload cap, no
inbound ports beyond the one you configure. Never expose the agent to a LAN
or the internet — anyone with the token can print.
