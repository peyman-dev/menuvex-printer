import { z } from 'zod';
export const id = z
  .string()
  .min(1)
  .max(128)
  .regex(/^[a-zA-Z0-9_:.-]+$/);
const text = (max: number) =>
  z
    .string()
    .max(max)
    .refine((s) => !/[\x00-\x09\x0b-\x1f\x7f-\x9f]/.test(s), 'Control characters are prohibited');
export const documentSchema = z.discriminatedUnion('type', [
  z.strictObject({
    type: z.literal('invoice'),
    data: z.strictObject({
      storeName: text(300),
      orderNumber: text(128),
      items: z
        .array(
          z.strictObject({
            name: text(300).min(1),
            quantity: z.number().int().min(1).max(9999),
            unitPrice: z.number().int().min(0).max(900_000_000),
          }),
        )
        .min(1)
        .max(100),
      total: z.number().int().min(0).max(9_000_000_000_000),
      footer: text(1000).default(''),
      // Optional app-template fields. The agent prints them only when supplied and never
      // invents business data (no local currency word, subtotal or date).
      title: text(128).optional(),
      address: text(500).optional(),
      phone: text(64).optional(),
      date: text(64).optional(),
      status: text(128).optional(),
      orderType: text(128).optional(),
      table: text(64).optional(),
      note: text(500).optional(),
      currency: text(32).optional(),
      subtotal: z.number().int().min(0).max(9_000_000_000_000).optional(),
    }),
  }),
  z.strictObject({ type: z.literal('receipt'), lines: z.array(text(500)).min(1).max(100) }),
  /**
   * Frontend-authored raw ESC/POS. The agent forwards these bytes **verbatim** — no rendering,
   * no font substitution, no added initialize/feed/cut — so the frontend stays the source of
   * truth for the printed design. Gated behind the operator-owned `rawPassthrough` printer flag;
   * without it the agent answers `RAW_PASSTHROUGH_DISABLED`.
   *
   * Use `commands` for short sequences and `data` (base64) for full raster receipts: a JSON
   * number array for a raster image would exceed the 128 KiB message limit.
   */
  z.strictObject({
    type: z.literal('escpos'),
    commands: z
      .array(z.number().int().min(0).max(255))
      .max(16 * 1024)
      .default([]),
    data: z
      .string()
      .max(128 * 1024)
      .default(''),
  }),
]);
/**
 * The only connection types the protocol carries.
 *
 * `spooler` is stored locally and is *never* published on the wire by a current agent — see
 * `Connection::api_value` in `src-tauri/src/printers/mod.rs`, which projects it onto the reserved
 * USB-shaped descriptor. It stays in this schema so the desktop window and older builds can round
 * trip it. A website that only implements `network` and `usb` therefore still validates every
 * `printers.list` payload; use `spoolerQueueName()` before reading `type`.
 */
export const CONNECTION_TYPES = ['spooler', 'network', 'usb'] as const;
export type ConnectionType = (typeof CONNECTION_TYPES)[number];
/** Name the value that was rejected. Zod's default `invalid_union` message ("Invalid input" /
 * "Invalid discriminator value") hides it, which is how an unsupported type reached production
 * with no actionable message. */
function unsupportedConnectionType(received: unknown): string {
  const shown =
    typeof received === 'string' && received.length > 0
      ? `"${received.slice(0, 64)}"`
      : '<missing>';
  return `Unsupported printer connection type: ${shown}. Supported: ${CONNECTION_TYPES.map(
    (t) => `"${t}"`,
  ).join(', ')}.`;
}
export const connectionSchema = z.discriminatedUnion(
  'type',
  [
    z.strictObject({ type: z.literal('spooler'), queueName: z.string().min(1).max(512) }),
    z.strictObject({
      type: z.literal('network'),
      host: z.string(),
      port: z.number().int().min(1).max(65535),
    }),
    z.strictObject({
      type: z.literal('usb'),
      vendorId: z.number().int(),
      productId: z.number().int(),
      serial: z.string().nullable(),
      bus: z.number().int(),
      ports: z.array(z.number().int()),
      interface: z.number().int(),
      endpoint: z.number().int(),
      alternate: z.number().int(),
    }),
  ],
  {
    error: (issue) =>
      unsupportedConnectionType((issue.input as { type?: unknown } | undefined)?.type),
  },
);
export type Connection = z.infer<typeof connectionSchema>;

export const printerStatusSchema = z.enum(['online', 'offline', 'unknown', 'busy', 'error']);
export const printerSchema = z.object({
  id,
  name: z.string(),
  connection: connectionSchema,
  paperMm: z.union([z.literal(58), z.literal(80)]),
  widthDots: z.number().int(),
  copies: z.number().int(),
  cut: z.boolean(),
  fontFamily: z.string(),
  fontSize: z.number().int(),
  /** Operator-owned switch for frontend-authored raw ESC/POS. Optional so profiles written by
   * older agents (which never set it) still parse; it is `false` when absent. */
  rawPassthrough: z.boolean().optional(),
  status: printerStatusSchema,
});
export const errorSchema = z.object({
  code: z.string(),
  message: z.string(),
  retryable: z.boolean(),
  uncertain: z.boolean(),
});
export const jobSchema = z.object({
  jobId: id,
  printerId: id,
  status: z.enum(['queued', 'printing', 'completed', 'failed', 'cancelled']),
  attempts: z.number().int(),
  createdAt: z.number(),
  nextAt: z.number(),
  error: errorSchema.nullable(),
});
export const printRequestSchema = z.strictObject({
  jobId: id,
  printerId: id,
  document: documentSchema,
});
export const routeSchema = z.object({ role: id, printerId: id, autoPrint: z.boolean() });
export const statusSchema = z.object({
  ready: z.boolean(),
  agentVersion: z.string(),
  port: z.number(),
  routes: z.array(routeSchema),
  serverError: z.string().nullable(),
});
/** A network printer candidate returned by the agent's LAN scan (discover.network). */
export const discoveredNetworkPrinterSchema = z.object({
  host: z.string(),
  port: z.number().int().min(1).max(65535),
});
/** A printer profile to create/update via printer.save (no server-computed status). */
export const printerInputSchema = printerSchema.omit({ status: true });
/** An installed OS print queue (Windows spooler / CUPS) returned by printers.installed. */
export const installedPrinterSchema = z.object({ queueName: z.string() });
/** Result of queue.clear: cancelled queued jobs and deleted history rows. */
export const queueClearResultSchema = z.object({
  cancelled: z.number().int(),
  removed: z.number().int(),
});
export type PrintDocument = z.input<typeof documentSchema>;
export type PrintRequest = z.input<typeof printRequestSchema>;
export type PrintJob = z.infer<typeof jobSchema>;
export type Printer = z.infer<typeof printerSchema>;
export type PrinterStatus = z.infer<typeof printerStatusSchema>;
export type AgentStatus = z.infer<typeof statusSchema>;
export type DiscoveredNetworkPrinter = z.infer<typeof discoveredNetworkPrinterSchema>;
export type PrinterInput = z.infer<typeof printerInputSchema>;
export type InstalledPrinter = z.infer<typeof installedPrinterSchema>;
export type QueueClearResult = z.infer<typeof queueClearResultSchema>;
/** Raw ESC/POS document: the frontend owns the design, the agent only transports the bytes. */
export function escposDocument(bytes: Uint8Array): PrintDocument {
  let binary = '';
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return { type: 'escpos', data: btoa(binary) };
}
export type ConnectionState =
  'connected' | 'disconnected' | 'connecting' | 'unauthorized' | 'error';
export type PrinterStatusEvent = { printerId: string; status: PrinterStatus };
export class AgentError extends Error {
  constructor(
    public code: string,
    message: string,
    public uncertain = false,
    public retryable = false,
  ) {
    super(message);
    this.name = 'AgentError';
  }
}
