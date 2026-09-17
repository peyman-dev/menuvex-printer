import { z } from 'zod';
import { errorSchema, id, jobSchema, printerStatusSchema } from './types';
export const VERSION = 1;
export const serverMessageSchema = z.discriminatedUnion('type', [
  z.object({
    type: z.literal('hello'),
    version: z.literal(1),
    nonce: z.string().length(44),
    serverProof: z.string().length(44),
    agentVersion: z.string(),
    authentication: z.literal('hmac-sha256'),
  }),
  z.object({
    type: z.literal('authenticated'),
    version: z.literal(1),
    requestId: id,
    agentVersion: z.string(),
  }),
  z.object({
    type: z.literal('response'),
    version: z.literal(1),
    requestId: id,
    data: z.unknown(),
  }),
  z.object({
    type: z.literal('error'),
    version: z.literal(1),
    requestId: id.optional(),
    error: errorSchema,
  }),
  z.object({
    type: z.literal('printer.status'),
    version: z.literal(1),
    printerId: id,
    status: printerStatusSchema,
  }),
  z.object({
    type: z.enum([
      'print.queued',
      'print.printing',
      'print.completed',
      'print.failed',
      'print.cancelled',
    ]),
    version: z.literal(1),
    job: jobSchema,
  }),
  z.object({ type: z.literal('resync'), version: z.literal(1) }),
]);
export function stableStringify(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(',')}]`;
  if (value !== null && typeof value === 'object')
    return `{${Object.entries(value)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([k, v]) => `${JSON.stringify(k)}:${stableStringify(v)}`)
      .join(',')}}`;
  return JSON.stringify(value);
}
