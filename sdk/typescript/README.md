# novex-printer-agent (TypeScript SDK)

Tiny zero-dependency client for the [Novex Printer Agent](../..) — the
localhost bridge between web apps and printers. Works in browsers and
Node.js 18+.

## Install

```bash
npm install novex-printer-agent
```

## Usage

```ts
import { NovexPrinterAgent } from "novex-printer-agent";

const agent = new NovexPrinterAgent({
  host: "127.0.0.1",
  port: 8765,
  token: "...", // agent startup log or: novex-printer-agent --print-token
});

await agent.connect();

const printers = await agent.getPrinters();
// → [{ id: "tcp:192.168.1.50:9100", name: "OCOM", type: "network", ... }]

// Raw ESC/POS bytes, delivered untouched:
const data = new Uint8Array([0x1b, 0x40, /* ... */ 0x1d, 0x56, 0x00]);
await agent.print({ printerId: "tcp:192.168.1.50:9100", data });
```

With a LAN scan to discover printers:

```ts
const printers = await agent.getPrinters({ scan: true, ports: [9100] });
```

## API

| Method | Description |
|---|---|
| `connect()` | Verify agent + token, returns `AgentInfo` |
| `disconnect()` | Close the event socket |
| `getPrinters(opts?)` | List printers (`{scan?, ports?, scanTimeoutMs?}`) |
| `getPrinter(id)` | Single printer detail |
| `connectPrinter(id)` | Open a cached connection (optional) |
| `disconnectPrinter(id)` | Close the cached connection |
| `print({printerId, data, stringEncoding?})` | Send raw bytes (`Uint8Array` / `ArrayBuffer` / `number[]` / base64 `string` / `utf8` string) |
| `getStatus(id)` | Connection status + last error |
| `registerPrinter(name, address, port)` | Save a LAN printer by name |
| `removePrinter(id)` | Remove a registered printer |
| `subscribe(listener)` | WebSocket events (`printer.connected` / `disconnected` / `error`), returns unsubscribe |

Errors are `NovexError` with stable `code` (see
[docs/API.md](../../docs/API.md#error-codes)), `message` and HTTP `status`:

```ts
import { NovexError } from "novex-printer-agent";
try {
  await agent.print({ printerId, data });
} catch (err) {
  if (err instanceof NovexError && err.code === "PRINTER_CONNECTION_FAILED") {
    // printer offline — tell the user
  }
}
```

## Next.js example

```tsx
"use client";
import { NovexPrinterAgent } from "novex-printer-agent";

const agent = new NovexPrinterAgent({
  host: "127.0.0.1",
  port: 8765,
  token: process.env.NEXT_PUBLIC_NOVEX_TOKEN!,
});

export async function printReceipt(printerId: string, escPos: Uint8Array) {
  await agent.print({ printerId, data: escPos });
}
```

> The page origin (e.g. `http://localhost:3000`, `https://menuvex.ir`) must
> be listed in the agent's `trustedOrigins`, otherwise the browser request
> is rejected with `FORBIDDEN_ORIGIN`.

## Development

```bash
npm install
npm test    # tsc build + node --test against a mock agent
```
