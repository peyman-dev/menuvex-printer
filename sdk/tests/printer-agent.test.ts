/**
 * Regression suite for the two outages this change fixes:
 *
 * 1. an unsupported `connection.type` used to surface as Zod's anonymous "Invalid input";
 * 2. one failing printer used to be indistinguishable from a dead agent.
 *
 * Everything here runs against the real SDK client and the real Zod schemas — no re-implemented
 * validators — so a schema or error-mapping regression fails a test rather than reaching a
 * cashier's screen.
 */
import { afterEach, beforeEach, describe, it, expect, vi } from 'vitest';
import { webcrypto, createHmac } from 'node:crypto';
import 'fake-indexeddb/auto';
import { PrinterAgentClient, type Socket } from '../src/client';
import { importPairingSecret, type CredentialStore } from '../src/credentials';
import {
  AgentError,
  CONNECTION_TYPES,
  connectionSchema,
  documentSchema,
  errorSchema,
  escposDocument,
  printerSchema,
  usbFallbackTargetSchema,
  type Printer,
  type PrintRequest,
} from '../src/types';
import { spoolerQueueName } from '../src/compat';
import { z } from 'zod';
vi.stubGlobal('crypto', webcrypto);

const secret = btoa(String.fromCharCode(...new Uint8Array(32).fill(7)));

const basePrinter = {
  id: 'printer:1',
  name: 'پرینتر',
  connection: { type: 'network' as const, host: '192.168.1.50', port: 9100 },
  paperMm: 80 as const,
  widthDots: 576,
  copies: 1,
  cut: true,
  fontFamily: 'Noto Sans Arabic',
  fontSize: 24,
  status: 'unknown' as const,
};

class TestSocket implements Socket {
  onmessage: Socket['onmessage'] = null;
  onclose: Socket['onclose'] = null;
  onerror: Socket['onerror'] = null;
  sent: Record<string, unknown>[] = [];
  closed = false;
  printers: unknown[] = [];
  /** Answer `print` with this error instead of a job, to model a broken printer. */
  printError: { code: string; message: string; printer?: string; actionRequired?: string } | null = null;
  /** Answer `printer.save` with this error instead of a profile. */
  saveError: { code: string; message: string } | null = null;
  constructor() {
    queueMicrotask(() =>
      this.emit({
        type: 'hello',
        version: 1,
        nonce: secret,
        serverProof: createHmac('sha256', Buffer.alloc(32, 7))
          .update(`menuvex-print-agent:server:v1\n${secret}\nhttps://menuvex.ir`)
          .digest('base64'),
        agentVersion: '1.0.0',
        authentication: 'hmac-sha256',
      }),
    );
  }
  emit(v: unknown) {
    this.onmessage?.({ data: JSON.stringify(v) });
  }
  send(raw: string) {
    const msg = JSON.parse(raw);
    this.sent.push(msg);
    queueMicrotask(() => {
      if (msg.type === 'authenticate') {
        this.emit({
          type: 'authenticated',
          version: 1,
          requestId: msg.requestId,
          agentVersion: '1.0.0',
        });
        return;
      }
      if (msg.type === 'print' && this.printError) {
        this.emit({
          type: 'error',
          version: 1,
          requestId: msg.requestId,
          error: { uncertain: false, retryable: false, ...this.printError },
        });
        return;
      }
      if (msg.type === 'printer.save' && this.saveError) {
        this.emit({
          type: 'error',
          version: 1,
          requestId: msg.requestId,
          error: { uncertain: false, retryable: false, ...this.saveError },
        });
        return;
      }
      const data =
        msg.type === 'printers.list'
          ? this.printers
          : msg.type === 'queue.list'
            ? []
            : msg.type === 'print' || msg.type === 'printer.test'
              ? {
                  jobId: msg.jobId,
                  printerId: msg.printerId,
                  status: 'queued',
                  attempts: 0,
                  createdAt: 1,
                  nextAt: 1,
                  error: null,
                }
              : msg.type === 'agent.status'
                ? {
                    ready: true,
                    agentVersion: '1.0.0',
                    port: 8765,
                    routes: [],
                    serverError: null,
                  }
                : { version: 1, agentVersion: '1.0.0' };
      this.emit({ type: 'response', version: 1, requestId: msg.requestId, data });
    });
  }
  close() {
    this.closed = true;
  }
}

let clients: PrinterAgentClient[] = [];
let key: CryptoKey;
beforeEach(async () => {
  key = await importPairingSecret(secret);
});
afterEach(() => {
  clients.forEach((c) => c.disconnect());
  clients = [];
});

function make() {
  const sockets: TestSocket[] = [];
  const credentials: CredentialStore = {
    get: async () => key,
    save: async (k) => {
      key = k;
    },
    clear: async () => {},
  };
  const c = new PrinterAgentClient({
    credentials,
    origin: 'https://menuvex.ir',
    timeoutMs: 500,
    socketFactory: () => {
      const s = new TestSocket();
      sockets.push(s);
      return s;
    },
  });
  clients.push(c);
  return { c, sockets };
}

describe('printer connection schema', () => {
  it('accepts exactly the supported connection types', () => {
    for (const connection of [
      { type: 'network', host: '192.168.1.50', port: 9100 },
      {
        type: 'usb',
        vendorId: 1,
        productId: 2,
        serial: null,
        bus: 1,
        ports: [1],
        interface: 0,
        endpoint: 1,
        alternate: 0,
      },
      { type: 'spooler', queueName: 'POS-80' },
    ]) {
      expect(connectionSchema.parse(connection).type).toBe(connection.type);
    }
    expect([...CONNECTION_TYPES]).toEqual(['spooler', 'network', 'usb']);
  });

  it('names the value it rejected instead of saying "Invalid input"', () => {
    for (const [received, expected] of [
      ['lan', '"lan"'],
      ['bluetooth', '"bluetooth"'],
      ['USB', '"USB"'],
      ['', '<missing>'],
    ] as const) {
      const result = connectionSchema.safeParse({ type: received });
      expect(result.success).toBe(false);
      if (result.success) continue;
      const message = result.error.issues[0].message;
      expect(message).toContain('Unsupported printer connection type');
      expect(message).toContain(expected);
      expect(message).not.toBe('Invalid input');
    }
  });

  it('names a missing or non-string discriminator', () => {
    for (const connection of [{ host: '192.168.1.50' }, { type: null }, { type: 7 }]) {
      const result = connectionSchema.safeParse(connection);
      expect(result.success).toBe(false);
    }
  });

  it('validates the USB-shaped descriptor the agent publishes for an OS queue', () => {
    // `Connection::api_value` projects `spooler` onto this reserved form, so a website that
    // only implements network + usb still parses every `printers.list` payload.
    const wire = {
      type: 'usb',
      vendorId: 0,
      productId: 0,
      serial: 'queue:POS-80',
      bus: 0,
      ports: [],
      interface: 0,
      endpoint: 0,
      alternate: 0,
    };
    const printer = printerSchema.parse({ ...basePrinter, connection: wire });
    expect(spoolerQueueName(printer.connection)).toBe('POS-80');
    // A real USB device must never be mistaken for a queue.
    const real = printerSchema.parse({
      ...basePrinter,
      connection: { ...wire, vendorId: 1155, productId: 22339, serial: 'ABC123' },
    });
    expect(spoolerQueueName(real.connection)).toBeUndefined();
  });

  it('treats the legacy per-printer raw flag and USB fallback as optional for old profiles', () => {
    expect(printerSchema.parse(basePrinter).rawPassthrough).toBeUndefined();
    expect(printerSchema.parse(basePrinter).usbFallbackTarget).toBeUndefined();
    expect(printerSchema.parse({ ...basePrinter, rawPassthrough: true }).rawPassthrough).toBe(true);
  });

  it('preserves optional structured printer/action context on errors', () => {
    const error = errorSchema.parse({
      code: 'RAW_PASSTHROUGH_DISABLED',
      message: 'Raw ESC/POS is disabled.',
      retryable: false,
      uncertain: false,
      printer: 'POS-80C copy 2',
      actionRequired: 'Enable both local switches in Settings.',
    });
    expect(error.printer).toBe('POS-80C copy 2');
    expect(error.actionRequired).toContain('local switches');
  });

  it('allows only spooler/network USB fallback targets', () => {
    expect(usbFallbackTargetSchema.parse({ type: 'spooler', queueName: 'POS-80' })).toEqual({
      type: 'spooler',
      queueName: 'POS-80',
    });
    expect(usbFallbackTargetSchema.parse({ type: 'network', host: '192.168.1.50', port: 9100 })).toMatchObject({
      type: 'network',
      host: '192.168.1.50',
    });
    expect(() =>
      usbFallbackTargetSchema.parse({
        type: 'usb',
        vendorId: 1,
        productId: 2,
        serial: null,
        bus: 1,
        ports: [1],
        interface: 0,
        endpoint: 1,
        alternate: 0,
      }),
    ).toThrow(/USB fallback must be a spooler queue or network printer/);
  });
});

describe('agent responses the website cannot validate', () => {
  it('reports the unsupported type by name and keeps the socket alive', async () => {
    const { c, sockets } = make();
    await c.connect();
    sockets[0].printers = [
      { ...basePrinter, connection: { type: 'lan', host: '192.168.1.50', port: 9100 } },
    ];
    const error = await c.getPrinters().catch((e: unknown) => e);
    expect(error).toBeInstanceOf(AgentError);
    expect((error as AgentError).code).toBe('CONNECTION_SCHEMA_MISMATCH');
    expect((error as AgentError).message).toContain('Unsupported printer connection type: "lan"');
    expect((error as AgentError).message).toContain('0.connection.type');
    // The agent is still there: this is a data problem, not a dead connection.
    expect(c.isConnected()).toBe(true);
    expect(sockets[0].closed).toBe(false);
    sockets[0].printers = [];
    await expect(c.getPrinters()).resolves.toEqual([]);
  });
});

describe('printer failures stay contained', () => {
  const request: PrintRequest = {
    jobId: 'order:42:invoice',
    printerId: 'printer:1',
    document: { type: 'receipt', lines: ['test'] },
  };

  it('survives a printer.save rejection', async () => {
    const { c, sockets } = make();
    await c.connect();
    sockets[0].saveError = {
      code: 'UNSUPPORTED_CONNECTION_TYPE',
      message: 'Unsupported printer connection type: "lan".',
    };
    const { status: _status, ...input } = basePrinter;
    void _status;
    // Defence in depth: the SDK refuses the bad descriptor before it is ever sent, and the
    // message names the value instead of saying "Invalid input". `savePrinter` validates
    // synchronously (like `print`), so this is a throw, not a rejection.
    expect(() =>
      c.savePrinter({
        ...input,
        connection: { type: 'lan', host: '1.2.3.4', port: 9100 } as never,
      }),
    ).toThrowError(z.ZodError);
    try {
      c.savePrinter({ ...input, connection: { type: 'lan' } as never });
      throw new Error('an unsupported connection type must not be sent to the agent');
    } catch (error) {
      expect(error).toBeInstanceOf(z.ZodError);
      expect((error as z.ZodError).issues[0].message).toContain(
        'Unsupported printer connection type: "lan"',
      );
    }
    expect(sockets[0].sent.some((m) => m.type === 'printer.save')).toBe(false);
    // An agent that rejects it (older SDK, direct WebSocket) reports the same by name.
    sockets[0].saveError = {
      code: 'UNSUPPORTED_CONNECTION_TYPE',
      message:
        'Unsupported printer connection type: "lan". Supported: "network", "usb", "spooler".',
    };
    const rejected = await c.savePrinter(input).catch((e: unknown) => e);
    expect((rejected as AgentError).code).toBe('UNSUPPORTED_CONNECTION_TYPE');
    expect((rejected as AgentError).message).toContain('"lan"');
    expect(c.isConnected()).toBe(true);
    // Printer management keeps working afterwards.
    await expect(c.getStatus()).resolves.toMatchObject({ agentVersion: '1.0.0' });
    await expect(c.getQueue()).resolves.toEqual([]);
  });

  it('survives a failed print job and accepts the next one', async () => {
    const { c, sockets } = make();
    await c.connect();
    sockets[0].printError = { code: 'USB_ACCESS_DENIED', message: 'Install the USB permissions' };
    const error = await c.print(request).catch((e: unknown) => e);
    expect((error as AgentError).code).toBe('USB_ACCESS_DENIED');
    expect(c.isConnected()).toBe(true);
    expect(sockets[0].closed).toBe(false);
    // The same client prints again once the operator fixed the printer.
    sockets[0].printError = null;
    await expect(c.print({ ...request, jobId: 'order:43:invoice' })).resolves.toMatchObject({
      status: 'queued',
    });
    // Exactly one authenticated socket was used for all of it.
    expect(sockets).toHaveLength(1);
  });

  it('keeps the queue alive through repeated job failures', async () => {
    const { c, sockets } = make();
    await c.connect();
    sockets[0].printError = { code: 'PRINTER_OFFLINE', message: 'unavailable' };
    for (let i = 0; i < 5; i++) {
      await expect(c.print({ ...request, jobId: `order:${i}:invoice` })).rejects.toBeInstanceOf(
        AgentError,
      );
    }
    expect(c.isConnected()).toBe(true);
    await expect(c.getStatus()).resolves.toMatchObject({ agentVersion: '1.0.0' });
  });
});

describe('frontend-owned print design', () => {
  it('sends raw ESC/POS to the agent untouched', async () => {
    const { c, sockets } = make();
    await c.connect();
    const bytes = new Uint8Array([0x1b, 0x40, 0x1d, 0x56, 0x00]);
    const request: PrintRequest = {
      jobId: 'order:9:raw',
      printerId: 'printer:1',
      document: escposDocument(bytes),
    };
    await c.print(request);
    const sent = sockets[0].sent.find((m) => m.type === 'print') as {
      document: { type: string; data: string; commands?: number[] };
    };
    expect(sent.document.type).toBe('escpos');
    // The bytes on the wire are exactly the bytes the frontend produced.
    expect(Buffer.from(sent.document.data, 'base64')).toEqual(Buffer.from(bytes));
  });

  it('accepts inline command bytes as well as base64', () => {
    const parsed = documentSchema.parse({ type: 'escpos', commands: [27, 64] });
    expect(parsed).toMatchObject({ type: 'escpos', commands: [27, 64], data: '' });
    expect(() => documentSchema.parse({ type: 'escpos', commands: [256] })).toThrow();
    expect(() => documentSchema.parse({ type: 'escpos', drawer: true })).toThrow();
  });

  it('rejects a raw document the operator has not enabled', async () => {
    const { c, sockets } = make();
    await c.connect();
    sockets[0].printError = {
      code: 'RAW_PASSTHROUGH_DISABLED',
      message: 'Raw ESC/POS documents are disabled.',
      printer: 'POS-80C copy 2',
      actionRequired: 'Enable both local switches in Settings.',
    };
    const error = await c
      .print({
        jobId: 'order:9:raw',
        printerId: 'printer:1',
        document: { type: 'escpos', commands: [27, 64] },
      })
      .catch((e: unknown) => e);
    expect((error as AgentError).code).toBe('RAW_PASSTHROUGH_DISABLED');
    expect((error as AgentError).printer).toBe('POS-80C copy 2');
    expect((error as AgentError).actionRequired).toContain('local switches');
    expect(c.isConnected()).toBe(true);
  });

  it('still carries semantic documents for agent-rendered receipts', () => {
    expect(documentSchema.parse({ type: 'receipt', lines: ['test'] }).type).toBe('receipt');
    expect(
      documentSchema.parse({
        type: 'invoice',
        data: {
          storeName: 'کافه',
          orderNumber: '1',
          items: [{ name: 'چای', quantity: 1, unitPrice: 100 }],
          total: 100,
        },
      }).type,
    ).toBe('invoice');
  });
});

/** Guards the exact issue shape that produced the reported production error. */
describe('reported failure shape', () => {
  it('reproduces path [0, connection, type] for a printer list and now names the value', () => {
    const list = z.array(printerSchema);
    const result = list.safeParse([
      { ...basePrinter, connection: { type: 'lan', host: '192.168.1.50', port: 9100 } },
    ]);
    expect(result.success).toBe(false);
    if (result.success) return;
    const issue = result.error.issues[0];
    expect(issue.path).toEqual([0, 'connection', 'type']);
    expect(issue.message).toContain('Unsupported printer connection type: "lan"');
  });
});

/** Compile-time guard: a `Printer` built by the desktop window must satisfy the SDK type. */
const _typedPrinter: Printer = { ...basePrinter, connection: { type: 'spooler', queueName: 'Q' } };
void _typedPrinter;
