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
    }),
  }),
  z.strictObject({ type: z.literal('receipt'), lines: z.array(text(500)).min(1).max(100) }),
]);
export const connectionSchema = z.discriminatedUnion('type', [
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
]);
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
export type PrintDocument = z.input<typeof documentSchema>;
export type PrintRequest = z.input<typeof printRequestSchema>;
export type PrintJob = z.infer<typeof jobSchema>;
export type Printer = z.infer<typeof printerSchema>;
export type PrinterStatus = z.infer<typeof printerStatusSchema>;
export type AgentStatus = z.infer<typeof statusSchema>;
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
