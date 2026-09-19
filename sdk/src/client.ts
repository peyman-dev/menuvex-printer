import { z } from 'zod';
import {
  AgentError,
  type AgentStatus,
  type ConnectionState,
  type Printer,
  type PrinterStatusEvent,
  type PrintJob,
  type PrintRequest,
  jobSchema,
  printerSchema,
  printRequestSchema,
  statusSchema,
  id,
} from './types';
import { VERSION, serverMessageSchema, stableStringify } from './protocol';
import {
  BrowserCredentialStore,
  type CredentialStore,
  importPairingSecret,
  signChallenge,
} from './credentials';
export interface Socket {
  onmessage: ((e: { data: unknown }) => void) | null;
  onclose: (() => void) | null;
  onerror: (() => void) | null;
  send(data: string): void;
  close(): void;
}
interface Options {
  port?: number;
  credentials?: CredentialStore;
  socketFactory?: (url: string) => Socket;
  origin?: string;
  timeoutMs?: number;
  reconnectMaxMs?: number;
}
type Pending = {
  resolve: (v: unknown) => void;
  reject: (e: Error) => void;
  timer: ReturnType<typeof setTimeout>;
};
export class PrinterAgentClient {
  private socket?: Socket;
  private credentials: CredentialStore;
  private factory: (url: string) => Socket;
  private state: ConnectionState = 'disconnected';
  private authenticated = false;
  private stopped = true;
  private failures = 0;
  private connecting?: Promise<void>;
  private reconnectTimer?: ReturnType<typeof setTimeout>;
  private handshakeTimer?: ReturnType<typeof setTimeout>;
  private pending = new Map<string, Pending>();
  private prints = new Map<string, { fingerprint: string; promise: Promise<PrintJob> }>();
  private statusListeners = new Set<(s: ConnectionState) => void>();
  private printerListeners = new Set<(s: PrinterStatusEvent) => void>();
  private jobListeners = new Set<(j: PrintJob) => void>();
  private resolveConnect?: () => void;
  private rejectConnect?: (e: Error) => void;
  private url: string;
  private origin: string;
  private timeoutMs: number;
  private reconnectMaxMs: number;
  constructor(options: Options = {}) {
    const port = options.port ?? 8765;
    if (!Number.isInteger(port) || port < 1024 || port > 65535)
      throw new AgentError('INVALID_PORT', 'Port must be 1024–65535');
    this.url = `ws://127.0.0.1:${port}`;
    this.origin = options.origin ?? globalThis.location?.origin ?? '';
    this.credentials = options.credentials ?? new BrowserCredentialStore();
    this.factory = options.socketFactory ?? ((url) => new WebSocket(url) as unknown as Socket);
    this.timeoutMs = options.timeoutMs ?? 10000;
    this.reconnectMaxMs = options.reconnectMaxMs ?? 30000;
  }
  private emit<T>(listeners: Set<(v: T) => void>, value: T) {
    for (const cb of listeners) {
      try {
        cb(value);
      } catch {
        /* Consumer exceptions must not break the printing connection. */
      }
    }
  }
  private setState(s: ConnectionState) {
    this.state = s;
    this.emit(this.statusListeners, s);
  }
  getConnectionState() {
    return this.state;
  }
  isConnected() {
    return this.authenticated && this.state === 'connected';
  }
  onAgentStatus(cb: (s: ConnectionState) => void) {
    this.statusListeners.add(cb);
    cb(this.state);
    return () => {
      this.statusListeners.delete(cb);
    };
  }
  onPrinterStatus(cb: (s: PrinterStatusEvent) => void) {
    this.printerListeners.add(cb);
    return () => {
      this.printerListeners.delete(cb);
    };
  }
  onPrintJob(cb: (j: PrintJob) => void) {
    this.jobListeners.add(cb);
    return () => {
      this.jobListeners.delete(cb);
    };
  }
  async pair(secret: string) {
    const key = await importPairingSecret(secret);
    this.disconnect();
    await this.credentials.save(key);
    await this.connect();
  }
  async forgetPairing() {
    this.disconnect();
    await this.credentials.clear();
  }
  connect(): Promise<void> {
    if (this.isConnected()) return Promise.resolve();
    if (this.connecting) return this.connecting;
    this.stopped = false;
    clearTimeout(this.reconnectTimer);
    this.reconnectTimer = undefined;
    this.setState('connecting');
    this.connecting = new Promise<void>((resolve, reject) => {
      this.resolveConnect = resolve;
      this.rejectConnect = reject;
    });
    const promise = this.connecting;
    try {
      const ws = this.factory(this.url);
      this.socket = ws;
      let chain = Promise.resolve();
      ws.onmessage = (e) => {
        chain = chain
          .then(async () => {
            if (this.socket === ws) await this.message(e.data);
          })
          .catch((e) => {
            if (this.socket === ws)
              this.fail(
                e instanceof AgentError
                  ? e
                  : new AgentError('INVALID_RESPONSE', 'Agent returned an invalid message'),
              );
          });
      };
      ws.onclose = () => {
        if (this.socket === ws)
          this.fail(
            new AgentError(
              'AGENT_UNAVAILABLE',
              'Connection closed; installation, permission, port or firewall may be responsible',
              true,
            ),
          );
      };
      ws.onerror = () => {
        if (this.socket === ws)
          this.fail(new AgentError('AGENT_UNAVAILABLE', 'Cannot reach the local agent', true));
      };
      this.handshakeTimer = setTimeout(
        () => this.fail(new AgentError('AGENT_UNAVAILABLE', 'Agent handshake timed out')),
        this.timeoutMs,
      );
    } catch {
      queueMicrotask(() =>
        this.fail(
          new AgentError(
            'AGENT_UNAVAILABLE',
            'Browser blocked or could not open the local connection',
          ),
        ),
      );
    }
    return promise;
  }
  disconnect() {
    this.stopped = true;
    clearTimeout(this.reconnectTimer);
    this.reconnectTimer = undefined;
    this.fail(new AgentError('DISCONNECTED', 'Client disconnected', true));
  }
  private fail(error: AgentError) {
    const ws = this.socket;
    this.socket = undefined;
    if (ws) {
      ws.onclose = null;
      ws.onerror = null;
      ws.onmessage = null;
      ws.close();
    }
    this.authenticated = false;
    clearTimeout(this.handshakeTimer);
    this.rejectConnect?.(error);
    this.connecting = undefined;
    this.resolveConnect = undefined;
    this.rejectConnect = undefined;
    for (const p of this.pending.values()) {
      clearTimeout(p.timer);
      p.reject(error);
    }
    this.pending.clear();
    const auth = ['AUTH_FAILED', 'PAIRING_REQUIRED', 'INVALID_PAIRING_SECRET'].includes(error.code);
    this.setState(
      auth ? 'unauthorized' : error.code === 'INVALID_RESPONSE' ? 'error' : 'disconnected',
    );
    if (!this.stopped && !auth && !this.reconnectTimer) {
      const delay = Math.min(1000 * 2 ** Math.min(this.failures++, 10), this.reconnectMaxMs);
      this.reconnectTimer = setTimeout(() => {
        this.reconnectTimer = undefined;
        void this.connect().catch(() => {});
      }, delay);
    }
  }
  private async message(raw: unknown) {
    if (typeof raw !== 'string' || raw.length > 512 * 1024)
      throw new AgentError('INVALID_RESPONSE', 'Unexpected message encoding/size');
    const msg = serverMessageSchema.parse(JSON.parse(raw));
    if (msg.type === 'hello') {
      const sessionSocket = this.socket;
      if (this.authenticated) throw new AgentError('INVALID_RESPONSE', 'Repeated hello');
      const key = await this.credentials.get();
      if (!key)
        throw new AgentError(
          'PAIRING_REQUIRED',
          'Pair once using the secret shown in the desktop agent',
        );
      const ws = sessionSocket;
      if ((await signChallenge(key, msg.nonce, this.origin, true)) !== msg.serverProof)
        throw new AgentError(
          'AUTH_FAILED',
          'The local listener could not prove its identity. Check pairing and port.',
        );
      const proof = await signChallenge(key, msg.nonce, this.origin);
      if (this.socket !== ws) return;
      // Authentication has its own handshake deadline; no command is sent before it succeeds.
      this.authRequestId = crypto.randomUUID();
      ws?.send(
        JSON.stringify({
          version: VERSION,
          type: 'authenticate',
          requestId: this.authRequestId,
          proof,
        }),
      );
      return;
    }
    if (msg.type === 'authenticated') {
      if (msg.requestId !== this.authRequestId || this.authenticated)
        throw new AgentError('INVALID_RESPONSE', 'Unexpected authentication response');
      this.authenticated = true;
      const sessionSocket = this.socket;
      // Do not await RPCs inside the message chain: their responses must be able to run.
      void this.refresh()
        .then(() => {
          if (!this.authenticated || this.socket !== sessionSocket) return;
          clearTimeout(this.handshakeTimer);
          this.failures = 0;
          this.setState('connected');
          this.resolveConnect?.();
          this.connecting = undefined;
          this.resolveConnect = undefined;
          this.rejectConnect = undefined;
        })
        .catch((e) => {
          if (this.socket === sessionSocket) this.fail(e);
        });
      return;
    }
    if (msg.type === 'error' && (!this.authenticated || !msg.requestId))
      throw new AgentError(
        msg.error.code,
        msg.error.message,
        msg.error.uncertain,
        msg.error.retryable,
      );
    if (!this.authenticated) throw new AgentError('INVALID_RESPONSE', 'Unauthenticated response');
    if (msg.type === 'response' || msg.type === 'error') {
      const p = msg.requestId ? this.pending.get(msg.requestId) : undefined;
      if (!p) return;
      clearTimeout(p.timer);
      this.pending.delete(msg.requestId!);
      if (msg.type === 'error')
        p.reject(
          new AgentError(
            msg.error.code,
            msg.error.message,
            msg.error.uncertain,
            msg.error.retryable,
          ),
        );
      else p.resolve(msg.data);
      return;
    }
    if (msg.type === 'printer.status')
      this.emit(this.printerListeners, { printerId: msg.printerId, status: msg.status });
    else if ('job' in msg) this.emit(this.jobListeners, msg.job);
    else if (msg.type === 'resync') void this.refresh().catch((e) => this.fail(e));
  }
  private authRequestId?: string;
  private async refresh() {
    const [printers, jobs] = await Promise.all([this.getPrinters(), this.getQueue()]);
    for (const p of printers)
      this.emit(this.printerListeners, { printerId: p.id, status: p.status });
    for (const j of jobs) this.emit(this.jobListeners, j);
  }
  private rpc<T>(type: string, fields: Record<string, unknown>, schema: z.ZodType<T>): Promise<T> {
    if (!this.authenticated || !this.socket)
      return Promise.reject(new AgentError('AGENT_NOT_READY', 'Connect and authenticate first'));
    const requestId = crypto.randomUUID();
    return new Promise<unknown>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(requestId);
        reject(
          new AgentError(
            'REQUEST_TIMEOUT',
            'Outcome may be unknown. Query the same job ID; do not use WebUSB fallback.',
            type === 'print' || type === 'printer.test',
          ),
        );
      }, this.timeoutMs);
      this.pending.set(requestId, { resolve, reject, timer });
      try {
        this.socket!.send(JSON.stringify({ version: VERSION, requestId, type, ...fields }));
      } catch {
        clearTimeout(timer);
        this.pending.delete(requestId);
        reject(new AgentError('CONNECTION_LOST', 'Query the same job ID after reconnecting', true));
      }
    }).then((value) => {
      try {
        return schema.parse(value);
      } catch (error) {
        if (
          error instanceof z.ZodError &&
          error.issues.some((issue) => issue.path.includes('connection'))
        ) {
          throw new AgentError(
            'CONNECTION_SCHEMA_MISMATCH',
            'نوع اتصال پرینتر با SDK سازگار نیست. SDK و اعتبارسنجی سایت را هماهنگ کنید: usb، network و spooler. نام lan در پروتکل معتبر نیست. این خطا مربوط به مجوز USB نیست.',
          );
        }
        throw error;
      }
    });
  }
  getStatus(): Promise<AgentStatus> {
    return this.rpc('agent.status', {}, statusSchema);
  }
  getPrinters(): Promise<Printer[]> {
    return this.rpc('printers.list', {}, z.array(printerSchema));
  }
  getPrinter(printerId: string): Promise<Printer> {
    return this.rpc('printer.get', { printerId: id.parse(printerId) }, printerSchema);
  }
  async testPrinter(printerId: string): Promise<void> {
    await this.rpc(
      'printer.test',
      { printerId: id.parse(printerId), jobId: `test:${crypto.randomUUID()}` },
      jobSchema,
    );
  }
  print(request: PrintRequest): Promise<PrintJob> {
    const parsed = printRequestSchema.parse(request);
    const fingerprint = stableStringify(parsed);
    const existing = this.prints.get(parsed.jobId);
    if (existing)
      return existing.fingerprint === fingerprint
        ? existing.promise
        : Promise.reject(
            new AgentError('JOB_ID_CONFLICT', 'Different request uses the same job ID'),
          );
    const promise = this.rpc('print', parsed, jobSchema).finally(() =>
      this.prints.delete(parsed.jobId),
    );
    this.prints.set(parsed.jobId, { fingerprint, promise });
    return promise;
  }
  getJob(jobId: string) {
    return this.rpc('print.status', { jobId: id.parse(jobId) }, jobSchema);
  }
  getQueue(): Promise<PrintJob[]> {
    return this.rpc('queue.list', {}, z.array(jobSchema));
  }
  async cancelJob(jobId: string): Promise<void> {
    await this.rpc('queue.cancel', { jobId: id.parse(jobId) }, jobSchema);
  }
  ping() {
    return this.rpc('ping', {}, z.object({ version: z.literal(1), agentVersion: z.string() }));
  }
}
