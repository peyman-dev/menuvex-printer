# Frontend-agent prompt: integrate MenuVex printer support

Use this prompt when asking a coding agent to add printer-agent support to the MenuVex frontend. Keep the integration additive: the existing browser printing path and business/order logic must continue to work.

---

You are working on the MenuVex frontend. Integrate the local MenuVex Printer Agent using the official SDK already published from this repository. Do not implement a second WebSocket protocol, WebUSB transport, ESC/POS encoder, or print queue in the frontend.

## Required behavior

1. Use one long-lived `PrinterAgentClient` per application and the SDK's `MenuVexAgentPrinter`/routing helpers. Pair through the application's existing pairing UI and let the SDK store the secret as a non-extractable WebCrypto key. Never log, persist, or send the pairing secret to an application server.
2. The agent listens on the configured loopback port (default `8765`). In browser code, use the SDK's relative/local-agent behavior; do not point browser-facing code at a sandbox, development server, or another machine's `localhost`.
3. Check `spoolerQueueName(printer.connection)` **before** checking `connection.type`. A defined queue name means the descriptor is an installed OS print queue; show it as a spooler and never call `navigator.usb`, request a USB permission, or route it through the legacy WebUSB printer. This works for both `{ type: "spooler", queueName }` and the backward-compatible USB-shaped descriptor `{ type: "usb", vendorId: 0, productId: 0, serial: "queue:<queue name>", ... }`.
4. Continue to support real USB and private-LAN printers. Do not interpret a normal nonzero USB VID/PID descriptor as a spooler. `printers.installed` lists OS queues; `discover.network` returns unconfirmed candidates. A successful probe is not proof that paper printed—ask the operator to inspect a test print.
5. Treat `completed` as transport/spooler handoff, not proof of physical output. Use stable unique job IDs derived from the order/print purpose and never silently create a new ID to retry an uncertain result. On reconnect, refresh queue state and reconcile by job ID rather than automatically replaying.
6. Send semantic `invoice` or `receipt` documents only. Do not add raw ESC/POS bytes, arbitrary printer commands, cash-drawer pulses, or frontend-side paper-width overrides. Invoice totals, currency, dates, and other business data must come from the existing order adapter; never derive or invent them in the agent integration.

## Receipt text conventions

For free-form `receipt.lines`, ordinary text is aligned by writing direction. The renderer also supports these explicit layout hints:

- A line of at least three dash/equal/box-rule characters (for example `----------------`) becomes a full-width pixel rule; do not use a box-drawing glyph as a visual separator.
- A line beginning with `[center]` (or `[center]:`) is centered after the marker is removed.
- A line with two to four pipe-separated cells, such as `شرح کالا | تعداد | مبلغ`, is laid out as columns from right to left. The first cell occupies the rightmost column, the last occupies the leftmost, and intermediate cells are centered. The renderer wraps cells to the configured paper width.
- An empty line adds vertical spacing.

Prefer plain values from the order data and let the renderer handle Persian shaping, direction, wrapping, columns, and rasterization.

## SDK usage sketch

```ts
import { PrinterAgentClient, spoolerQueueName } from '@menuvex/printer-agent';

const agent = new PrinterAgentClient();
await agent.connect();
const printers = await agent.getPrinters();

for (const printer of printers) {
  const queueName = spoolerQueueName(printer.connection);
  if (queueName) {
    // Show “Windows/CUPS queue: <queueName>”; do not treat this as direct USB.
  }
}

await agent.print({
  jobId: `order:${order.id}:invoice`,
  printerId: selectedPrinterId,
  document: {
    type: 'invoice',
    data: {
      storeName: order.storeName,
      orderNumber: order.number,
      items: order.items.map((item) => ({
        name: item.name,
        quantity: item.quantity,
        unitPrice: item.unitPrice,
      })),
      total: order.total,
      // Include only values that the existing business adapter actually provides.
      currency: order.currency,
    },
  },
});
```

Adapt the import path and existing application types to the actual package version; do not copy this sketch verbatim if the project already has a provider or order adapter. Add tests for spooler and USB descriptors, an unavailable agent, duplicate job IDs, reconnect/reconciliation, and the existing legacy fallback policy.

## Paper-width test

The test ticket prints the configured paper profile in millimeters and dots, plus a sample column row and separator. ESC/POS and generic OS spooler APIs cannot reliably detect the physical roll width, so present this as the saved profile setting and ask the operator to compare the paper output with the printer manual. Never claim automatic hardware width detection.
