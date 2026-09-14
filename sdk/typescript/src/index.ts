/**
 * novex-printer-agent — tiny TypeScript client for the Novex Printer Agent.
 *
 * The agent is a localhost bridge: browser/Node app -> agent -> LAN or USB
 * printer. This SDK speaks the agent's HTTP API (and optionally its
 * WebSocket event feed). Zero dependencies, works in browsers and Node 18+.
 *
 * ```ts
 * const agent = new NovexPrinterAgent({ host: "127.0.0.1", port: 8765, token: "..." });
 * await agent.connect();
 * const printers = await agent.getPrinters();
 * await agent.print({ printerId: "tcp:192.168.1.50:9100", data: escPosBytes });
 * ```
 */

declare const Buffer: any | undefined;

export interface NovexAgentOptions {
  host?: string;
  port?: number;
  /** API token printed by the agent on startup / `--print-token`. */
  token: string;
  /** Per-request timeout in ms (default 15000). */
  timeoutMs?: number;
  /** Set true only if you put a TLS reverse proxy in front of the agent. */
  tls?: boolean;
}

export interface PrinterInfo {
  id: string;
  name: string;
  type: "network" | "usb" | string;
  status: string;
  address?: string;
  port?: number;
  vendorId?: number;
  productId?: number;
  serial?: string;
  manufacturer?: string;
  product?: string;
  detail?: string;
}

export interface PrinterStatus {
  printerId: string;
  connected: boolean;
  status: string;
  lastError?: string;
  lastPrintAt?: string;
}

export interface AgentInfo {
  version: string;
  platform: string;
  arch: string;
  host: string;
  port: number;
  usbSupported: boolean;
  usbDetail: string;
  time: string;
}

export interface PrinterEvent {
  event: "printer.connected" | "printer.disconnected" | "printer.error" | string;
  printerId?: string;
  message?: string;
}

export interface ListOptions {
  /** Run a LAN scan for open printer ports (slower). */
  scan?: boolean;
  /** Ports to probe when scanning (default [9100]). */
  ports?: number[];
  /** Scan budget in ms. */
  scanTimeoutMs?: number;
}

/** Print payload: raw bytes in any common shape, or a base64 string. */
export type PrintData = Uint8Array | ArrayBuffer | number[] | string;

export interface PrintOptions {
  printerId: string;
  data: PrintData;
  /** Set when `data` is a string: "base64" (default) or "utf8". */
  stringEncoding?: "base64" | "utf8";
}

/** Stable error codes returned by the agent (see docs/API.md). */
export class NovexError extends Error {
  code: string;
  status: number;
  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = "NovexError";
    this.code = code;
    this.status = status;
  }
}

function bytesToBase64(data: Uint8Array): string {
  if (typeof Buffer !== "undefined" && Buffer && typeof Buffer.from === "function") {
    return Buffer.from(data).toString("base64");
  }
  // Browser path: chunked binary string -> btoa (avoids call-stack blowup).
  let binary = "";
  const chunk = 0x8000;
  for (let i = 0; i < data.length; i += chunk) {
    binary += String.fromCharCode.apply(null, data.subarray(i, i + chunk) as unknown as number[]);
  }
  return btoa(binary);
}

function utf8ToBytes(text: string): Uint8Array {
  if (typeof TextEncoder !== "undefined") {
    return new TextEncoder().encode(text);
  }
  // Minimal UTF-8 fallback (ASCII + latin-1 range is enough for ESC/POS text).
  const out: number[] = [];
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i);
    if (c < 0x80) out.push(c);
    else if (c < 0x800) out.push(0xc0 | (c >> 6), 0x80 | (c & 0x3f));
    else out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 0x3f), 0x80 | (c & 0x3f));
  }
  return new Uint8Array(out);
}

export function toUint8Array(data: PrintData, stringEncoding: "base64" | "utf8" = "base64"): Uint8Array {
  if (data instanceof Uint8Array) return data;
  if (data instanceof ArrayBuffer) return new Uint8Array(data);
  if (Array.isArray(data)) return new Uint8Array(data);
  if (typeof data === "string") {
    if (stringEncoding === "utf8") return utf8ToBytes(data);
    if (typeof Buffer !== "undefined" && Buffer && typeof Buffer.from === "function") {
      return new Uint8Array(Buffer.from(data, "base64"));
    }
    const binary = atob(data);
    const out = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
    return out;
  }
  throw new NovexError("INVALID_REQUEST", "unsupported print data type", 0);
}

export class NovexPrinterAgent {
  private baseUrl: string;
  private wsUrl: string;
  private token: string;
  private timeoutMs: number;
  private socket: WebSocket | null = null;

  constructor(options: NovexAgentOptions) {
    const host = options.host || "127.0.0.1";
    const port = options.port || 8765;
    const scheme = options.tls ? "https" : "http";
    const wsScheme = options.tls ? "wss" : "ws";
    this.baseUrl = `${scheme}://${host}:${port}`;
    this.wsUrl = `${wsScheme}://${host}:${port}/ws`;
    this.token = options.token;
    this.timeoutMs = options.timeoutMs || 15000;
  }

  /** Check the agent is reachable and the token is valid. */
  async connect(): Promise<AgentInfo> {
    await this.request("GET", "/health", undefined, false);
    return (await this.request("GET", "/api/v1/info")) as AgentInfo;
  }

  /** Close the event socket (HTTP needs no teardown). */
  async disconnect(): Promise<void> {
    if (this.socket) {
      const s = this.socket;
      this.socket = null;
      try {
        s.close();
      } catch {
        /* already closed */
      }
    }
  }

  async getInfo(): Promise<AgentInfo> {
    return (await this.request("GET", "/api/v1/info")) as AgentInfo;
  }

  async getPrinters(options?: ListOptions): Promise<PrinterInfo[]> {
    let path = "/api/v1/printers";
    if (options) {
      const q = new URLSearchParams();
      if (options.scan) q.set("scan", "true");
      if (options.ports && options.ports.length) q.set("ports", options.ports.join(","));
      if (options.scanTimeoutMs) q.set("scanTimeoutMs", String(options.scanTimeoutMs));
      const qs = q.toString();
      if (qs) path += "?" + qs;
    }
    const res = (await this.request("GET", path)) as { printers: PrinterInfo[] };
    return res.printers || [];
  }

  async getPrinter(printerId: string): Promise<PrinterInfo> {
    const res = (await this.request("GET", `/api/v1/printers/${encodeURIComponent(printerId)}`)) as {
      printer: PrinterInfo;
    };
    return res.printer;
  }

  async connectPrinter(printerId: string): Promise<void> {
    await this.request("POST", `/api/v1/printers/${encodeURIComponent(printerId)}/connect`, {});
  }

  async disconnectPrinter(printerId: string): Promise<void> {
    await this.request("POST", `/api/v1/printers/${encodeURIComponent(printerId)}/disconnect`, {});
  }

  async getStatus(printerId: string): Promise<PrinterStatus> {
    return (await this.request(
      "GET",
      `/api/v1/printers/${encodeURIComponent(printerId)}/status`
    )) as PrinterStatus;
  }

  /**
   * Send raw bytes to a printer. The bytes are delivered untouched
   * (ESC/POS, ZPL, EPL, CPCL, ...). No prior connectPrinter() needed.
   */
  async print(options: PrintOptions): Promise<void> {
    const bytes = toUint8Array(options.data, options.stringEncoding || "base64");
    if (bytes.length === 0) {
      throw new NovexError("INVALID_REQUEST", "print payload must not be empty", 0);
    }
    await this.request(
      "POST",
      `/api/v1/printers/${encodeURIComponent(options.printerId)}/print`,
      { data: bytesToBase64(bytes) }
    );
  }

  /** Register a LAN printer by friendly name (persisted in agent config). */
  async registerPrinter(name: string, address: string, port: number): Promise<PrinterInfo> {
    const res = (await this.request("POST", "/api/v1/printers", { name, address, port })) as {
      printerId: string;
      printer: PrinterInfo;
    };
    return res.printer;
  }

  /** Remove a printer previously added with registerPrinter(). */
  async removePrinter(printerId: string): Promise<void> {
    await this.request("DELETE", `/api/v1/printers/${encodeURIComponent(printerId)}`);
  }

  /**
   * Subscribe to real-time events (printer.connected/disconnected/error).
   * Optional: plain HTTP polling of getStatus() works without it.
   * Returns an unsubscribe function.
   */
  async subscribe(listener: (event: PrinterEvent) => void): Promise<() => void> {
    await this.disconnect();
    const url = `${this.wsUrl}?token=${encodeURIComponent(this.token)}`;
    const socket = new WebSocket(url);
    this.socket = socket;
    socket.onmessage = (msg: MessageEvent) => {
      try {
        const event = JSON.parse(String(msg.data)) as PrinterEvent;
        if (event && event.event && event.event !== "agent.hello") {
          listener(event);
        }
      } catch {
        /* ignore malformed frames */
      }
    };
    return () => {
      if (this.socket === socket) this.socket = null;
      try {
        socket.close();
      } catch {
        /* already closed */
      }
    };
  }

  private async request(
    method: string,
    path: string,
    body?: unknown,
    auth = true
  ): Promise<unknown> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    try {
      const headers: Record<string, string> = {};
      if (auth) headers["Authorization"] = `Bearer ${this.token}`;
      let payload: string | undefined;
      if (body !== undefined) {
        headers["Content-Type"] = "application/json";
        payload = JSON.stringify(body);
      }
      const res = await fetch(this.baseUrl + path, {
        method,
        headers,
        body: payload,
        signal: controller.signal,
      });
      const text = await res.text();
      let json: any = null;
      try {
        json = text ? JSON.parse(text) : null;
      } catch {
        json = null;
      }
      if (!res.ok) {
        const code = (json && json.error && json.error.code) || "INTERNAL_ERROR";
        const message = (json && json.error && json.error.message) || `HTTP ${res.status}`;
        throw new NovexError(code, message, res.status);
      }
      return json;
    } catch (err) {
      if (err instanceof NovexError) throw err;
      if (err instanceof Error && err.name === "AbortError") {
        throw new NovexError("PRINTER_TIMEOUT", `request to ${path} timed out`, 0);
      }
      throw new NovexError(
        "PRINTER_CONNECTION_FAILED",
        err instanceof Error ? `cannot reach agent: ${err.message}` : "cannot reach agent",
        0
      );
    } finally {
      clearTimeout(timer);
    }
  }
}

export default NovexPrinterAgent;
