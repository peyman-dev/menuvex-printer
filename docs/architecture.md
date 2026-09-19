# Architecture and initial inspection

## Baseline

Inspected all tracked files, working-tree files, history and installed tooling before any file change. Baseline `612aed9` contained only `README.md` (`# menuvex-printer`). No package manager, existing printer implementation, WebUSB, order creation, order WebSocket, invoice renderer, UI components or API contracts existed. Nothing in the MenuVex PWA was changed. The user selected standalone Agent + SDK delivery.

The proposed folder nesting was unnecessary in this empty dedicated repository: the root is the Agent, with `src/`, `src-tauri/`, `sdk/`, `docs/`, and CI. Rust is a library plus optional desktop binary; tests use `--no-default-features` to avoid GTK while retaining real core behavior.

## Flow

```text
Existing order backend/WebSocket (external repository, unchanged)
  → PWA invoice adapter → singleton PrinterAgentClient
  → localhost WebSocket + Origin/Host check + HMAC challenge
  → strict semantic document validation
  → SQLite INSERT/deduplicate (transaction)
  → worker claim persisted before any transport IO
  → pinned printer profile + bundled font + Rustybuzz/BiDi/Swash
  → ESC/POS raster → USB bulk / private LAN TCP
  → durable outcome → broadcast event → UI/SDK
```

No PWA bytes, filesystem paths, DNS names, arbitrary hosts or printer configuration mutations are accepted over WebSocket. Configuration and pairing secret disclosure require the bundled local Tauri UI. `agent.shutdown` is recognized but returns `LOCAL_CONFIRMATION_REQUIRED`; a remote browser cannot terminate the POS agent.

## State and persistence

`Storage` owns SQLite through a mutex. WAL, FULL synchronous mode, busy timeout, unique primary-key job IDs and transactional claims protect concurrent submissions. `printing` on restart becomes a failed ambiguous job. Queued jobs survive restart. Printer profiles are snapshotted into jobs so later configuration edits do not silently redirect pending jobs.

Same ID + same printer/document returns existing state including failed/cancelled/completed. Different document/printer is `JOB_ID_CONFLICT`. Failed IDs are not implicitly restarted. Paper-width/copy configuration is not rewritten for existing jobs. Queue has at most 256 active jobs, 16 configured printers; list returns active jobs first plus recent history up to 500. Dedup rows and documents are retained indefinitely in v1; administrators must plan data retention and backups. Do not delete rows merely to recover space: that removes the dedup guarantee. A future migration can retain hashes/tombstones while expiring receipt content.

One worker serializes hardware writes, including copies. FIFO is maintained per printer even while an earlier job backs off; another printer can proceed while that job waits. This conservative implementation is not a thread per printer; a slow send can delay other printers for its bounded timeout. Retry is only for known pre-send errors. Failure after an earlier copy becomes ambiguous even if a later copy fails before writing.

## Lifecycle

Single-instance desktop plugin; one runtime/server; bounded eight sockets; five-second upgrade deadline; ten-second authentication deadline; 30 commands/second per socket. WS writes time out. Event broadcast is bounded; lagging clients receive `resync`. Epoch rotation revokes sessions on their next loop/heartbeat (at most about 15 seconds idle). Workers are notified on new jobs, otherwise check once/second. Status probing is every ten seconds only for configured devices, with no subnet scanning.

Window close hides; tray Quit stops accepting work and waits for the current bounded worker operation. OS termination/power loss uses persisted crash recovery. Port collision never silently picks a different port: local Settings can change it, then tray Restart applies it. Tauri UI requests use native IPC, so the UI remains usable if the WS port cannot bind.

## Extension seams

`Transport` is independent of Print Manager; Serial or Windows RAW spooler can implement it later. Document variants are strict and versioned. `Renderer` owns font/cache lifetime. Automatic LAN discovery belongs in `printers/discovery.rs`, not the print loop. Database schema `user_version=1` must receive explicit forward migrations before upgrades; version incompatibility is rejected rather than downgraded.

Updater is deliberately not installed with a placeholder key/URL. Add Tauri updater only with reviewed signed metadata, public key, verified HTTPS endpoint, atomic update policy and graceful queue drain. Keep old protocol clients functional during a staged rollout. See release gates.
