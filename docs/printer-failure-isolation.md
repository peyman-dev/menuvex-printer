# Printer failure isolation and USB stability

This is the debugging record for the report _"adding a USB printer kills the agent"_. It states
what was actually wrong (file, function, line), what changed, and how to prove it on real
hardware. Line numbers marked **before** refer to commit `c2c3d5f`.

## Symptoms and what they really were

| Reported symptom                                              | Actual mechanism                                                                                                                                                                                                                   |
| ------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| "Website cannot connect to Agent anymore"                     | The WebSocket stayed up, but every command started returning `AGENT_NOT_READY` because the queue worker had permanently stopped and the state mutexes were poisoned. From the browser that is indistinguishable from a dead agent. |
| "Agent becomes unresponsive / printer management stops"       | `state::worker` `break`-ed out of its loop on the first internal failure and set `worker_done = true` forever. Nothing restarts it.                                                                                                |
| "Agent may crash"                                             | No panic hook was installed and the `worker`/`monitor`/`server` task handles were discarded, so a panic was silent rather than fatal — except inside a WebSocket session, where it closed the socket.                              |
| Validation error `invalid_union … options: ["network","usb"]` | A frontend-side validator that knows only `network` and `usb`. See [Connection types](protocol.md#connection-types).                                                                                                               |

## Root cause 1 — the queue worker died permanently

`src-tauri/src/state.rs`, `worker()`, **before** lines 278–291:

```rust
match result {
    Ok(Ok(true)) => { continue; }
    Ok(Ok(false)) => (),
    _ => {                                   // ← every failure landed here
        tracing::error!("queue worker stopped after internal failure");
        s.ready.store(false, Ordering::SeqCst);
        *s.server_error.lock().unwrap() = Some("QUEUE_ERROR: restart and inspect queue".into());
        break;                               // ← never returned
    }
}
```

`result` is `Err` in two cases, and **both** ended the worker for the lifetime of the process:

1. `Ok(Err(AgentError))` — the closure returned an error. Any poisoned lock (`map_err(|_| lock_error())`) or any transient SQLite failure in `store.finish()` did this.
2. `Err(JoinError)` — the blocking task **panicked**, e.g. inside `Renderer::encode` (before line 264) or inside a transport.

After that, `State::enqueue` answered `AGENT_NOT_READY` to every `print` and `printer.test`, and `ready` stayed `false`. Adding a USB printer did not cause this directly — it caused the first job failure, and the first job failure was fatal by design.

**Fix:** `worker()` now wraps `worker_loop()` and the loop **never** breaks on failure. It logs `QUEUE_WORKER_ERROR` / `QUEUE_WORKER_PANIC` with a backtrace, backs off (`1 s → 15 s`, capped) and continues. Sustained failures are reported as `serverError: "QUEUE_DEGRADED: …"` while jobs are still accepted and retried. `worker_done` is only set on shutdown.

## Root cause 2 — one panic poisoned every mutex

**Before**, `state.rs` lines 286, 305, 309 (and `server.rs`, `main.rs`) used `lock().unwrap()`. A panic while any of those guards was alive poisoned the mutex, and from then on:

- `monitor()` panicked at `state.active.lock().unwrap()` on every 10 s tick;
- every `map_err(|_| lock_error())` path returned `AGENT_NOT_READY`, so `agent.status`, `printers.list` and `queue.list` all failed for the website.

That is the exact "printer management stops working" symptom, and it was permanent until the process was restarted.

**Fix:** one helper, `state::lock()`, recovers from poisoning and logs `MUTEX_POISON_RECOVERED` with a backtrace. It is safe here because nothing behind those mutexes has an invariant that unwinding can corrupt — the SQLite `Transaction` objects are locals that roll back during the unwind, `Renderer` owns only a font database and a glyph cache (the mutable `Paper` is a local), and the rest is plain data. No `.lock().unwrap()` remains in `state.rs`, `server.rs` or `main.rs`.

## Root cause 3 — a panicking command closed the website's socket

`src-tauri/src/server.rs`, **before** line 157:

```rust
let result = tokio::task::spawn_blocking(move || s.dispatch(req.command)).await?;
```

The `?` propagated the `JoinError`, `session()` returned, and the WebSocket was closed. One bad printer command therefore disconnected the cashier's browser, and nothing logged it.

**Fix:** `dispatch_isolated()` runs the handler under `catch_unwind`, converts a panic into an `AGENT_INTERNAL_ERROR` response frame, and never propagates the `JoinError`. `main.rs` does the same for the Tauri IPC path, installs a `set_hook` panic logger, forces `RUST_BACKTRACE=1`, and logs `TASK_STOPPED` if a background task ever returns.

## Root cause 4 — USB operations were unbounded, uncleaned and chatty

`src-tauri/src/printers/usb.rs`, **before**:

| Line               | Problem                                                                                                                                                                  |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 37, 114, 127       | `Context::new()` per call. A libusb context spawns an event thread and re-enumerates the whole bus; the status poll created one every 10 s per USB printer.              |
| 93–108 (`matches`) | The **status poll** called `dev.open()` to read the serial, competing with an in-flight print job for the device and requiring USB permissions just to report `offline`. |
| 44                 | `discover()` opens **every** device on the bus with no deadline; one wedged device hung the desktop "جستجوی USB" button forever.                                         |
| 169                | `claim_interface()` with **no** `release_interface()` anywhere in the file, and no cleanup on the `?` early-returns or the 30 s timeout path.                            |
| 166–180            | No overall wall-clock bound: enumeration, `open()` and string descriptors can block indefinitely.                                                                        |

**Fix** (`printers/usb.rs`):

- One process-wide context in a `OnceLock` (`context()`); init failures are _not_ cached so they stay retryable.
- `guarded(stage, limit, op)` runs every USB operation on its own thread with a hard deadline — 10 s presence, 25 s discovery, 35 s print — and returns `USB_TIMEOUT` instead of hanging. A wedged device can no longer pin the agent's blocking pool or the UI.
- `struct Claim` is RAII: `release_interface()` runs on success, on every `?` early-return and on the abandoned-thread path, and the handle is closed afterwards so the OS re-attaches its kernel driver.
- `matches()` prefers bus/port identity and only opens the device when the same VID/PID sits on a different socket, so the poll cannot fight a print job.
- Non-USB profiles are rejected before libusb is touched.

## Root cause 5 — the design was the agent's, not the frontend's

`src-tauri/src/print/layout.rs` (whole module, invoked from `Renderer::encode` in
`src-tauri/src/print/mod.rs`) builds the invoice layout inside the agent: centered store header,
`تلفن:` line, `فاکتور فروش | فیش N` slip row, the four-column items table, Persian-digit conversion
(`fa_digits`), thousands grouping (`money`), the dashed note frame and the `POWERED BY MENUVEX.IR`
brand line. The protocol only carried `invoice` and `receipt`, both `deny_unknown_fields`, so
`{"format":"html"}` or `{"commands":[…]}` could not even be parsed — the frontend had **no** way to
send its own layout. That is where the design "changed".

**Fix:** a third document type, `escpos`, forwards frontend-authored bytes verbatim (no rendering,
no font substitution, no added `ESC @`/feed/cut), gated behind the operator-owned per-printer
`rawPassthrough` flag. See [Who owns the design](escpos.md#who-owns-the-design).

## Connection schema

`connection.type` is `network | usb | spooler`. Rejections now name the value:

```
Unsupported printer connection type: "lan". Supported: "network", "usb", "spooler".
```

raised as `UNSUPPORTED_CONNECTION_TYPE` by the agent (`printer.save`, local `save_config`), by the
SDK before it sends anything, and by the Legacy Windows agent. The website-facing wire only ever
carries `network` or `usb` (`Connection::api_value`), asserted by
`wire_descriptor_never_exposes_a_third_connection_type`.

## Architecture

```
Browser (Menuvex PWA)
   │  ws://127.0.0.1:8765
   ▼
server::run ── session() ── dispatch_isolated()  ← catch_unwind, error frame, socket survives
   │
   ├── State (poison-recovering locks) ── SQLite queue (authoritative)
   │
   ├── state::worker   ← never terminates; logs + backs off on any failure
   │      └── Renderer::encode  (invoice/receipt)  or  verbatim bytes (escpos)
   │             └── HardwareTransport::send  ← one printer's failure is one job's failure
   │                    ├── network  TCP/9100, bounded
   │                    ├── spooler  winspool RAW / lp -o raw
   │                    └── usb      guarded(deadline) + Claim(RAII)
   │
   └── state::monitor  ← per-printer probe; a dead printer reports "unknown", never stops the loop
```

The rule this enforces: **a printer error fails one job; it can never fail a socket, the queue or
the process.**

## Test matrix

Run on the target OS with the daily log open (`app_log_dir()/agent-*.log`).

USB:

| Case                                         | Expected                                                                                                                                                                 |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| libusb thermal printer, permissions set      | `PRINTER_FOUND` → `USB_DEVICE_OPENED` → `USB_INTERFACE_CLAIMED` → `USB_INTERFACE_RELEASED` → `PRINT_JOB_COMPLETED`; the agent keeps answering `agent.status` throughout. |
| Wrong permissions (no udev rule / no WinUSB) | `PRINTER_FOUND` with `accessible: false` and `USB_ACCESS_DENIED` in the UI; `printer.save` still succeeds; the website stays connected.                                  |
| Printer unplugged while queued               | `PRINTER_OFFLINE` (retryable), retried at 2/4/8…s up to `maxAttempts`, then `PRINT_JOB_FAILED`. The worker keeps running.                                                |
| Device that stops answering                  | `USB_TIMEOUT` after the deadline; the desktop window is not frozen; the queue keeps serving other printers.                                                              |
| Two printers, one broken                     | The broken one's jobs fail; the healthy one still prints in the same minute.                                                                                             |

Network:

| Case                    | Expected                                                                                                                  |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| TCP/9100 printer online | `PRINT_JOB_COMPLETED`.                                                                                                    |
| Offline / wrong IP      | `NETWORK_TIMEOUT` or `NETWORK_CONNECTION_FAILED`, retryable then `PRINT_JOB_FAILED`; `printer.status` flips to `offline`. |

Application:

| Case                                | Expected                                                                                                    |
| ----------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| Agent restart with pending jobs     | Jobs still `printing` at crash time become `failed / PRINT_OUTCOME_UNKNOWN` and are **not** auto-reprinted. |
| Website reconnect                   | Re-authenticates, refreshes printers + queue; no duplicate prints.                                          |
| Multiple printers, mixed transports | Independent statuses and queues.                                                                            |
| Failed print job recovery           | Cancel/clear from the queue tab; a new job ID is required to reprint an uncertain outcome.                  |
| `escpos` with either local raw gate off | `RAW_PASSTHROUGH_DISABLED` with `printer`/`actionRequired`; the socket stays open, and a website cannot enable the setting. |
| Invalid ESC/POS prefix / configured size exceeded | `RAW_ESC_POS_INVALID` / `RAW_PAYLOAD_TOO_LARGE`; no print job is queued. |
| Valid opt-in ESC/POS | No rendering is applied. Windows uses `DOC_INFO_1W` datatype `RAW`; verify the bytes at the printer because vendor drivers may still transform them. |

## What is still not verified here

`cargo`/`rustc` and the native libraries were unavailable in the environment where this change was
written (crates.io and rustup are unreachable), so `npm run test:rust` has **not** been executed
against it. The Rust sources parse cleanly, and the SDK/TypeScript half is covered by
`npm test` (39 tests) and `npm run build`. Run `npm run test:rust` and the matrix above before
shipping.
