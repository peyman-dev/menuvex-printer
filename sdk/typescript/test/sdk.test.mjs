// SDK tests against a mock agent (no Go binary needed).
// Run: npm test   (builds dist/ with tsc, then node --test)
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import crypto from "node:crypto";

const { NovexPrinterAgent, NovexError, toUint8Array } = await import("../dist/index.js");

const TOKEN = "unit-test-token";
const requests = [];
let mockPrintPayload = null;

function json(res, status, obj) {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(obj));
}

const server = http.createServer((req, res) => {
  let body = "";
  req.on("data", (c) => (body += c));
  req.on("end", () => {
    requests.push({ method: req.method, url: req.url, auth: req.headers["authorization"] });
    const u = new URL(req.url, "http://x");
    if (u.pathname === "/health") {
      return json(res, 200, { status: "ok", version: "test", platform: "test", arch: "test" });
    }
    if (req.headers["authorization"] !== `Bearer ${TOKEN}`) {
      return json(res, 401, { success: false, error: { code: "UNAUTHORIZED", message: "nope" } });
    }
    if (u.pathname === "/api/v1/info") {
      return json(res, 200, { version: "test", platform: "test", arch: "a", host: "h", port: 1, usbSupported: true, usbDetail: "mock", time: "t" });
    }
    if (u.pathname === "/api/v1/printers" && req.method === "GET") {
      return json(res, 200, { printers: [{ id: "tcp:1.2.3.4:9100", name: "P", type: "network", status: "available", address: "1.2.3.4", port: 9100 }], scanned: u.searchParams.get("scan") === "true" });
    }
    const m = u.pathname.match(/^\/api\/v1\/printers\/([^/]+)(\/(\w+))?$/);
    if (m) {
      const id = decodeURIComponent(m[1]);
      const action = m[3] || "";
      if (action === "print" && req.method === "POST") {
        mockPrintPayload = JSON.parse(body || "{}").data;
        return json(res, 200, { success: true, printerId: id });
      }
      if (action === "status") {
        return json(res, 200, { printerId: id, connected: false, status: "available" });
      }
      if ((action === "connect" || action === "disconnect") && req.method === "POST") {
        return json(res, 200, { success: true, printerId: id, connected: action === "connect" });
      }
      if (action === "") {
        return json(res, 200, { printer: { id, name: id, type: "network", status: "available" } });
      }
    }
    return json(res, 404, { success: false, error: { code: "NOT_FOUND", message: "nope" } });
  });
});

// Minimal WS endpoint: handshake + one event frame, then close.
server.on("upgrade", (req, socket) => {
  const u = new URL(req.url, "http://x");
  const key = req.headers["sec-websocket-key"];
  if (u.pathname !== "/ws" || u.searchParams.get("token") !== TOKEN || !key) {
    socket.write("HTTP/1.1 401 Unauthorized\r\nContent-Length: 0\r\n\r\n");
    socket.destroy();
    return;
  }
  const accept = crypto.createHash("sha1").update(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").digest("base64");
  socket.write(
    "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
      `Sec-WebSocket-Accept: ${accept}\r\n\r\n`
  );
  const payload = Buffer.from(JSON.stringify({ event: "printer.connected", printerId: "tcp:1.2.3.4:9100" }));
  socket.write(Buffer.concat([Buffer.from([0x81, payload.length]), payload]));
});

let base;
const liveSockets = new Set();
server.on("connection", (s) => {
  liveSockets.add(s);
  s.on("close", () => liveSockets.delete(s));
});
before(async () => {
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const a = server.address();
  base = { host: "127.0.0.1", port: a.port };
});
after(async () => {
  for (const s of liveSockets) s.destroy();
  await new Promise((r) => server.close(r));
});

function agent(token = TOKEN) {
  return new NovexPrinterAgent({ ...base, token });
}

test("connect() checks health + info with auth", async () => {
  const a = agent();
  const info = await a.connect();
  assert.equal(info.usbSupported, true);
  assert.ok(requests.some((r) => r.url === "/health" && !r.auth));
  assert.ok(requests.some((r) => r.url === "/api/v1/info" && r.auth === `Bearer ${TOKEN}`));
});

test("connect() with bad token throws UNAUTHORIZED", async () => {
  const a = agent("wrong");
  await assert.rejects(() => a.connect(), (e) => e instanceof NovexError && e.code === "UNAUTHORIZED");
});

test("getPrinters() passes scan options", async () => {
  const a = agent();
  const list = await a.getPrinters({ scan: true, ports: [9100, 515], scanTimeoutMs: 1000 });
  assert.equal(list.length, 1);
  assert.equal(list[0].id, "tcp:1.2.3.4:9100");
  const last = requests[requests.length - 1];
  assert.match(last.url, /scan=true/);
  assert.match(last.url, /ports=9100%2C515|ports=9100,515/);
});

test("print() sends exact bytes as base64", async () => {
  const a = agent();
  const data = new Uint8Array([0x1b, 0x40, 0x00, 0xff, 72, 105]);
  await a.print({ printerId: "tcp:1.2.3.4:9100", data });
  assert.equal(mockPrintPayload, Buffer.from(data).toString("base64"));
  const last = requests[requests.length - 1];
  assert.equal(last.method, "POST");
  assert.match(last.url, /printers\/tcp%3A1.2.3.4%3A9100\/print/);
});

test("print() accepts ArrayBuffer, number[] and base64 string", async () => {
  const a = agent();
  await a.print({ printerId: "tcp:1.2.3.4:9100", data: new Uint8Array([1, 2]).buffer });
  assert.equal(mockPrintPayload, Buffer.from([1, 2]).toString("base64"));
  await a.print({ printerId: "tcp:1.2.3.4:9100", data: [3, 4] });
  assert.equal(mockPrintPayload, Buffer.from([3, 4]).toString("base64"));
  await a.print({ printerId: "tcp:1.2.3.4:9100", data: Buffer.from([5]).toString("base64") });
  assert.equal(mockPrintPayload, Buffer.from([5]).toString("base64"));
  await a.print({ printerId: "tcp:1.2.3.4:9100", data: "Hi", stringEncoding: "utf8" });
  assert.equal(mockPrintPayload, Buffer.from("Hi").toString("base64"));
});

test("print() rejects empty payload locally", async () => {
  const a = agent();
  await assert.rejects(() => a.print({ printerId: "x", data: new Uint8Array(0) }), /must not be empty/);
});

test("printer lifecycle methods hit the right endpoints", async () => {
  const a = agent();
  const p = await a.getPrinter("tcp:1.2.3.4:9100");
  assert.equal(p.id, "tcp:1.2.3.4:9100");
  await a.connectPrinter("tcp:1.2.3.4:9100");
  await a.disconnectPrinter("tcp:1.2.3.4:9100");
  const st = await a.getStatus("tcp:1.2.3.4:9100");
  assert.equal(st.status, "available");
  const urls = requests.slice(-4).map((r) => `${r.method} ${r.url}`);
  assert.ok(urls[0].endsWith("printers/tcp%3A1.2.3.4%3A9100"));
  assert.ok(urls[1].endsWith("/connect"));
  assert.ok(urls[2].endsWith("/disconnect"));
  assert.ok(urls[3].endsWith("/status"));
});

test("subscribe() receives events over WebSocket", async () => {
  const a = agent();
  const events = [];
  const unsub = await a.subscribe((e) => events.push(e));
  const deadline = Date.now() + 3000;
  while (events.length === 0 && Date.now() < deadline) await new Promise((r) => setTimeout(r, 25));
  unsub();
  await a.disconnect();
  assert.equal(events.length, 1);
  assert.equal(events[0].event, "printer.connected");
  assert.equal(events[0].printerId, "tcp:1.2.3.4:9100");
});

test("toUint8Array conversions", async () => {
  assert.deepEqual([...toUint8Array(new Uint8Array([9]))], [9]);
  assert.deepEqual([...toUint8Array(new Uint8Array([9]).buffer)], [9]);
  assert.deepEqual([...toUint8Array([9])], [9]);
  assert.deepEqual([...toUint8Array(Buffer.from([9]).toString("base64"))], [9]);
});
