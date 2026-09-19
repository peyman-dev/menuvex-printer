import { afterEach, beforeEach, describe, it, expect, vi } from 'vitest';
import { webcrypto, createHmac } from 'node:crypto';
import 'fake-indexeddb/auto';
import { PrinterAgentClient, type Socket } from '../src/client';
import {
  BrowserCredentialStore,
  importPairingSecret,
  signChallenge,
  type CredentialStore,
} from '../src/credentials';
import { AgentError, type PrintRequest } from '../src/types';
import { MigratingPrinterProvider } from '../src/providers';
import { BrowserRoutingStore } from '../src/routing';
vi.stubGlobal('crypto', webcrypto);
const secret = btoa(String.fromCharCode(...new Uint8Array(32).fill(7)));
const request: PrintRequest = {
  jobId: 'order:42:invoice',
  printerId: 'p1',
  document: { type: 'receipt', lines: ['test'] },
};
const job = {
  jobId: request.jobId,
  printerId: 'p1',
  status: 'queued',
  attempts: 0,
  createdAt: 1,
  nextAt: 1,
  error: null,
};
class TestSocket implements Socket {
  onmessage: Socket['onmessage'] = null;
  onclose: Socket['onclose'] = null;
  onerror: Socket['onerror'] = null;
  sent: Record<string, unknown>[] = [];
  closed = false;
  rejectAuth = false;
  dropPrint = false;
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
        this.emit(
          this.rejectAuth
            ? {
                type: 'error',
                version: 1,
                requestId: msg.requestId,
                error: {
                  code: 'AUTH_FAILED',
                  message: 'Denied',
                  uncertain: false,
                  retryable: false,
                },
              }
            : {
                type: 'authenticated',
                version: 1,
                requestId: msg.requestId,
                agentVersion: '1.0.0',
              },
        );
        return;
      }
      if (msg.type === 'print' && this.dropPrint) {
        this.onclose?.();
        return;
      }
      const data =
        msg.type === 'printers.list' || msg.type === 'queue.list'
          ? []
          : msg.type === 'print'
            ? job
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
  vi.useRealTimers();
});
function make(options: { rejectAuth?: boolean; missingKey?: boolean } = {}) {
  const sockets: TestSocket[] = [];
  const credentials: CredentialStore = {
    get: async () => (options.missingKey ? null : key),
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
      s.rejectAuth = !!options.rejectAuth;
      sockets.push(s);
      return s;
    },
  });
  clients.push(c);
  return { c, sockets };
}
describe('PrinterAgentClient', () => {
  it('uses one authenticated connection and refreshes subscriptions', async () => {
    const { c, sockets } = make();
    const a = c.connect(),
      b = c.connect();
    expect(a).toBe(b);
    await a;
    expect(c.isConnected()).toBe(true);
    expect(sockets).toHaveLength(1);
    expect(sockets[0].sent.map((m) => m.type)).toEqual([
      'authenticate',
      'printers.list',
      'queue.list',
    ]);
    expect(sockets[0].sent[0].proof).toBe(await signChallenge(key, secret, 'https://menuvex.ir'));
    expect(sockets[0].sent[0]).not.toHaveProperty('token');
  });
  it('rejects missing credentials without issuing printer commands', async () => {
    const { c, sockets } = make({ missingKey: true });
    await expect(c.connect()).rejects.toMatchObject({ code: 'PAIRING_REQUIRED' });
    expect(c.getConnectionState()).toBe('unauthorized');
    expect(sockets[0].sent).toHaveLength(0);
  });
  it('stops reconnecting after authentication failure', async () => {
    const { c, sockets } = make({ rejectAuth: true });
    await expect(c.connect()).rejects.toMatchObject({ code: 'AUTH_FAILED' });
    expect(c.getConnectionState()).toBe('unauthorized');
    expect(sockets).toHaveLength(1);
  });
  it('coalesces duplicates and rejects conflicting in-flight IDs', async () => {
    const { c, sockets } = make();
    await c.connect();
    const first = c.print(request);
    expect(c.print(request)).toBe(first);
    await expect(
      c.print({ ...request, document: { type: 'receipt', lines: ['different'] } }),
    ).rejects.toMatchObject({ code: 'JOB_ID_CONFLICT' });
    expect(await first).toMatchObject({ status: 'queued' });
    expect(sockets[0].sent.filter((m) => m.type === 'print')).toHaveLength(1);
  });
  it('automatically reconnects and refreshes queue after an outage', async () => {
    const { c, sockets } = make();
    await c.connect();
    vi.useFakeTimers();
    sockets[0].onclose?.();
    await vi.advanceTimersByTimeAsync(1000);
    await vi.waitFor(() => expect(c.isConnected()).toBe(true));
    expect(sockets).toHaveLength(2);
    expect(sockets[1].sent.map((m) => m.type)).toEqual([
      'authenticate',
      'printers.list',
      'queue.list',
    ]);
  });
  it('does not replay a print after an ambiguous connection loss', async () => {
    const { c, sockets } = make();
    await c.connect();
    sockets[0].dropPrint = true;
    await expect(c.print(request)).rejects.toMatchObject({ uncertain: true });
    expect(c.isConnected()).toBe(false);
  });
  it('unsubscribes event handlers', async () => {
    const { c, sockets } = make();
    await c.connect();
    const cb = vi.fn();
    const remove = c.onPrintJob(cb);
    sockets[0].emit({ type: 'print.completed', version: 1, job: { ...job, status: 'completed' } });
    await new Promise((r) => setTimeout(r, 0));
    expect(cb).toHaveBeenCalledOnce();
    remove();
    sockets[0].emit({ type: 'print.completed', version: 1, job });
    await new Promise((r) => setTimeout(r, 0));
    expect(cb).toHaveBeenCalledOnce();
  });
  it('rejects invalid semantic control bytes', () => {
    const { c } = make();
    expect(() =>
      c.print({ ...request, document: { type: 'receipt', lines: ['\x1b@'] } }),
    ).toThrow();
  });
});
describe('additional security and lifecycle checks', () => {
  it('rejects a rogue listener server proof before exposing commands', async () => {
    const { c, sockets } = make();
    key = await importPairingSecret(btoa(String.fromCharCode(...new Uint8Array(32).fill(8))));
    await expect(c.connect()).rejects.toMatchObject({ code: 'AUTH_FAILED' });
    expect(sockets[0].sent).toHaveLength(0);
  });
  it('never uses legacy to bypass failed authentication', async () => {
    const { c } = make({ rejectAuth: true });
    const legacy = { print: vi.fn() };
    const provider = new MigratingPrinterProvider(c, legacy, () => true);
    await expect(
      provider.print({ ...request, jobId: `auth:${crypto.randomUUID()}` }),
    ).rejects.toMatchObject({ code: 'AUTH_FAILED' });
    expect(legacy.print).not.toHaveBeenCalled();
  });
  it('explicit disconnect cancels scheduled reconnect', async () => {
    const { c, sockets } = make();
    await c.connect();
    vi.useFakeTimers();
    sockets[0].onclose?.();
    c.disconnect();
    await vi.advanceTimersByTimeAsync(60000);
    expect(sockets).toHaveLength(1);
  });
});

describe('browser pairing', () => {
  it('persists a non-extractable key across store instances', async () => {
    const name = `test-${crypto.randomUUID()}`;
    const store = new BrowserCredentialStore(name);
    await store.save(key);
    const loaded = await new BrowserCredentialStore(name).get();
    expect(loaded?.extractable).toBe(false);
    await expect(crypto.subtle.exportKey('raw', loaded!)).rejects.toThrow();
    expect(await signChallenge(loaded!, secret, 'https://menuvex.ir')).toBe(
      await signChallenge(key, secret, 'https://menuvex.ir'),
    );
    await store.clear();
    expect(await store.get()).toBeNull();
  });
  it('binds signatures to origin and nonce', async () => {
    expect(await signChallenge(key, secret, 'https://menuvex.ir')).not.toBe(
      await signChallenge(key, secret, 'https://evil.test'),
    );
  });
});
describe('migration safety', () => {
  it('uses legacy only before first submission, with persistent ownership', async () => {
    const agent = {
      isConnected: () => false,
      connect: vi.fn().mockRejectedValue(new AgentError('AGENT_UNAVAILABLE', 'offline')),
      print: vi.fn(),
    } as unknown as PrinterAgentClient;
    const legacy = { print: vi.fn().mockResolvedValue(undefined) };
    const provider = new MigratingPrinterProvider(agent, legacy, () => true);
    const r = { ...request, jobId: `legacy:${crypto.randomUUID()}` };
    await provider.print(r);
    expect(legacy.print).toHaveBeenCalledOnce();
    await expect(provider.print(r)).rejects.toMatchObject({ code: 'LEGACY_OUTCOME_UNKNOWN' });
  });
  it('does not fall back for an already Agent-owned ID', async () => {
    const routes = new BrowserRoutingStore();
    const r = { ...request, jobId: `agent:${crypto.randomUUID()}` };
    await routes.claim(r.jobId, 'agent');
    const agent = {
      isConnected: () => false,
      connect: vi.fn().mockRejectedValue(new AgentError('AGENT_UNAVAILABLE', 'offline')),
    } as unknown as PrinterAgentClient;
    const legacy = { print: vi.fn() };
    await expect(
      new MigratingPrinterProvider(agent, legacy, () => true, routes).print(r),
    ).rejects.toMatchObject({ code: 'AGENT_UNAVAILABLE' });
    expect(legacy.print).not.toHaveBeenCalled();
  });
  it('does not fall back on authentication or print failure', async () => {
    const legacy = { print: vi.fn() };
    const agent = {
      isConnected: () => true,
      print: vi.fn().mockRejectedValue(new AgentError('REQUEST_TIMEOUT', 'unknown', true)),
    } as unknown as PrinterAgentClient;
    await expect(
      new MigratingPrinterProvider(agent, legacy, () => true).print({
        ...request,
        jobId: `timeout:${crypto.randomUUID()}`,
      }),
    ).rejects.toMatchObject({ code: 'REQUEST_TIMEOUT' });
    expect(legacy.print).not.toHaveBeenCalled();
  });
  it('allows only one concurrent legacy claim', async () => {
    const store = new BrowserRoutingStore();
    const id = crypto.randomUUID();
    const results = await Promise.all([store.claim(id, 'legacy'), store.claim(id, 'legacy')]);
    expect(results.filter((r) => r.created)).toHaveLength(1);
  });
});
