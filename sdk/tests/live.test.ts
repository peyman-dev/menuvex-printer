import { it, expect } from 'vitest';
import { webcrypto } from 'node:crypto';
import WebSocket from 'ws';
import { PrinterAgentClient, type Socket } from '../src/client';
import { importPairingSecret } from '../src/credentials';
// Started only by the Rust integration test: real WS + auth + SQLite + renderer + test transport.
it.skipIf(!process.env.MENUVEX_TEST_PORT)(
  'SDK → real Rust Agent → test-only transport',
  async () => {
    Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true });
    const key = await importPairingSecret(process.env.MENUVEX_TEST_SECRET!);
    const client = new PrinterAgentClient({
      port: Number(process.env.MENUVEX_TEST_PORT),
      origin: 'https://menuvex.ir',
      credentials: { get: async () => key, save: async () => {}, clear: async () => {} },
      socketFactory: (url) =>
        new WebSocket(url, { origin: 'https://menuvex.ir' }) as unknown as Socket,
    });
    try {
      await client.connect();
      expect(await client.getPrinters()).toHaveLength(1);
      const request = {
        jobId: 'integration:invoice',
        printerId: 'integration-printer',
        document: { type: 'receipt' as const, lines: ['سلام MenuVex ۱۲۳', 'Integration test'] },
      };
      await client.print(request);
      await expect
        .poll(async () => (await client.getJob(request.jobId)).status, { timeout: 10000 })
        .toBe('completed');
      expect((await client.print(request)).status).toBe('completed');
      expect((await client.getQueue()).filter((j) => j.jobId === request.jobId)).toHaveLength(1);
    } finally {
      client.disconnect();
    }
  },
  20000,
);
