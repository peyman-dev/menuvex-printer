# Offline-First Sync Engine — Audit, Design, and Honest Limits

## 1. What the audit found (before this change)

The desktop app was **offline-capable but not offline-syncing**:

1. **Persistence was a JSON file, not SQLite** (`internal/store/store.go`).
   Transactions clone the whole DB, mutate, then write tmp-file + fsync +
   rename. That is crash-atomic on POSIX, but every transaction rewrites
   the entire database (O(n)), the parent directory was not fsynced, and
   the Windows fallback used a non-atomic `os.WriteFile` that could
   corrupt the file on power loss.
2. **The outbox was catalog-only.** Orders, order transitions, split
   payments, debtor settles and stock movements were written to the
   local store but *never queued*. The critical
   “take order → pay → print → queue → sync” path silently stopped at
   “queue”. A restaurant could trade all day offline and the server
   would learn nothing.
3. **Sync was manual-only** (one button → one `Refresh` call). No
   scheduler, no retry/backoff, no connectivity triggers, no status UI.
4. **Data-loss bug:** `store.Bootstrap` replaced **all** entities with
   the server snapshot despite a comment claiming it only touched
   server-owned data. Any re-bootstrap (e.g. after cursor expiry)
   destroyed unsynced locally created orders.
5. **No server existed in the repo** — only contracts. The device token
   is stored in plaintext in the local data file (0600).
6. Sound foundations worth keeping: the v2 wire protocol, stable
   operation IDs + per-device sequence, strict result bijection in
   `SaveResults`, cursor committed in the same transaction as pulled
   changes, ack-after-commit, version-gated `applyChange`, and matching
   client/server order state machines.

## 2. What was built

### 2.1 Central server — `offline-menuvex/server/` (FastAPI + PostgreSQL)

Implements the exact v2 contract the Go client already speaks:
handshake, consistent bootstrap snapshot, bounded idempotent push with
per-operation results (`applied/conflict/rejected/retry`), sequence
cursor pull with pagination, ack, plus JWT-admin device management and
conflict resolution routes from `contracts/go-sync-routes.json`.

Key tables (`migrations/001_init.sql`):

- `sync_devices` — device registry, token stored as SHA-256 hash only.
- `sync_entities` — current server copy per (tenant, type, id) + version.
- `sync_change_log` — append-only; `server_seq` (BIGSERIAL) is the pull
  cursor. Written in the same transaction as every business mutation.
- `sync_processed_operations` — idempotency ledger; replayed operations
  return their original result.
- `sync_conflicts` — operator-resolvable conflict records.

Domain rules enforced server-side (client prices/permissions are never
trusted): exact IRR order arithmetic re-derived from lines; order
state machine (`pending→accepted→preparing→ready→completed`, cancel
from any non-terminal state); finished orders immutable; line prices
are sale-time snapshots, never retro-priced; payments unique per UUID
and stored as `gateway_status: local_record_only`; inventory is
append-only movements (stock = Σ movements); debtor overpayment
rejected; catalog writes are field-allowlisted and `base_version`
checked (stale write ⇒ conflict, never silent overwrite); deletions
propagate as tombstones; tenant always comes from the authenticated
device, never the payload.

### 2.2 Client sync engine — `internal/syncengine/`

A background scheduler around the existing v2 `Refresh` cycle with the
required triggers:

- **on startup** (3 s + per-device jitter up to ~1 min, so a
  neighbourhood power restore doesn’t stampede the server),
- **every 10 minutes** — a real bidirectional cycle, not a ping,
- **~2 s after the last local change** (debounced; the store wakes the
  engine only when a transaction actually queued a new operation),
- **when the server becomes reachable again** (light `/healthz` probe
  every 20 s *only while unhealthy*; the probe accelerates recovery
  but never gates the scheduled cycles),
- **after failures** with bounded exponential backoff (30 s → 10 min)
  plus jitter.

Cycles are single-flight, context-cancellable, capped at 2 minutes
each, and the engine **drains**: after a long outage it repeats cycles
until the outbox is empty instead of sending one 100-op batch per
10 minutes. Sync runs **only while the app is open** — there is no OS
background service, and the UI/docs do not claim otherwise.

Structured `slog` logging covers cycle start/success/failure with
pending/conflict counts; device tokens are never logged.

### 2.3 Transactional outbox wiring (the core defect, fixed)

Business mutation + outbox operation now commit in the **same** local
transaction (`queueOpTx` in `internal/store/outbox.go`), so a crash
between “order saved” and “request sent” can never lose work:

| local action | queued operation |
|---|---|
| POS order created | `order/create` (full snapshot payload) |
| order items edited | `order/update` |
| order status changed | `order/transition` {from, to, reason} |
| split payment recorded | `payment/record` (unique payment UUID) |
| manual stock movement / waste | `inventory_movement/record` (signed delta) |
| debtor account opened (dialog, credit sale, defer) | `debtor/create` |
| debtor payment settled | `debtor/payment` |

Queueing happens **only when a device is bound to a server**; pure
standalone installs behave exactly as before and grow no queue.
Operations carry stable UUIDs and a monotone per-device
`client_sequence`, so server-side ordering follows business order and
dependencies (order → its payments/transitions) are preserved; the
server additionally answers `retry` (not failure) when a dependency
has not arrived yet, so one blocked entity never blocks unrelated
entities. Failures record `attempts`, `next_retry_at`, `last_error`
locally; operations are never silently dropped. Retry *pacing* is
enforced by the engine's cycle-level backoff (automatic triggers wait
out the backoff window), while an explicit `Refresh` always replays
pending operations immediately with their original IDs — a lost
response must never turn into a stuck or duplicated operation.

### 2.4 Data-safety fixes in the local store

- `Origin` tracking (`local` vs `server`) on every stored entity;
  local writes are auto-marked in the transaction path.
  **Bootstrap now replaces only server-origin rows** — locally created
  or edited data survives re-bootstrap/cursor expiry. Legacy rows with
  no origin are treated as local (safe direction).
- Parent-directory fsync after rename; Windows replace now uses a
  backup-swap (`.bak`) instead of non-atomic overwrite, and `Open`
  recovers from `.bak` if the main file is torn by a power cut.
- `schema_version` stamped in the meta area for future migrations.
- Sync metadata (`last_sync_success_at`, last error) persisted in the
  store so status survives restarts.

### 2.5 Operator-visible sync status

The app shell header now shows a live chip (HTMX-polled every 15 s
from `GET /sync/status`): standalone-offline, syncing, synced,
N pending, server unreachable (data safe on device), N conflicts,
device not accepted by server, or “initial data not fully received”.
The UI therefore distinguishes *“saved on this device”* from
*“arrived at the server”* honestly, including after failed cycles.

## 3. Scenario coverage (honest status)

| Required scenario | Status |
|---|---|
| Order created with healthy server syncs end-to-end | Server side **executed** (pytest). Client side: Go test written (`syncengine`), **not yet executed** — no Go toolchain in the authoring sandbox; must run in CI. |
| 15-minute outage: orders/payments keep working locally | By construction (local store is primary; sync never blocks HTMX handlers). Go test `TestEngineDrainsOfflineOrder` written, unexecuted. |
| Automatic recovery after outage without restart/click | Engine triggers (probe edge + backoff + 10-min ticker) implemented; Go test written, unexecuted. |
| Lost push response ⇒ no duplicates | **Executed** on server (operation replay returns stored result; order/payment/movement replays tested). Client keeps same operation IDs on retry. |
| Crash after local transaction | Outbox op commits atomically with the order (same tx). Go test `TestPOSOrderQueuesSyncOpAndSurvivesRestart` written, unexecuted. |
| Crash during pull apply | Pre-existing design: page + cursor commit in one tx, ack afterwards. |
| Server product update reaches POS | Pull + version-gated apply (pre-existing, kept). |
| Updates & deletions propagate | Tombstones in change log (**executed** server-side test). |
| Real conflicts visible, not auto-merged | **Executed**: catalog base-version conflict + admin resolve; order state conflicts recorded. Client shows conflict count in the status chip. |
| Disk/store failure | Startup fails closed with a clear error (pre-existing); `.bak` recovery added for torn writes. Full-disk behaviour still depends on the OS error surface — not simulated. |
| Unhydrated client must not claim completeness | Status chip shows “initial data not fully received” until first successful bootstrap/cycle. |
| No overlapping cycles | Single-flight engine + client mutex. Go test `TestNoOverlappingCycles` written, unexecuted. |
| Tenant isolation | **Executed** (device A cannot read/write tenant B; tenant never taken from payload). |
| Bootstrap preserves unsynced local work | Go test `TestBootstrapPreservesLocalUnsyncedOrder` written, unexecuted. |

Executed in this session: **17 pytest tests, all passing**
(`cd server && ./run_tests.sh`). Go code is syntax-checked
(tree-sitter) only — `go build`/`go test` **have not been run** because
no Go toolchain is available in the sandbox. Do not ship before CI runs
the Go suite.

## 4. Known limitations (deliberate, documented)

1. **Local storage is still the JSON file store, not SQLite.**
   Migrating to SQLite (e.g. `modernc.org/sqlite`) changes the module
   from zero-dependency to a large vendored driver and cannot be
   fetched or compile-verified in this sandbox; shipping an unverified
   storage rewrite into a POS would be reckless. The staged plan:
   (a) add the driver behind the existing `store.Store` API,
   (b) schema below, (c) one-shot importer that copies the JSON file
   into SQLite **without deleting it**, verifies row counts + outbox
   integrity, then flips a marker, (d) keep the JSON path as fallback
   for one release. Until then, durability relies on
   tmp+fsync+rename+dir-fsync+`.bak` recovery, and write cost grows
   with DB size.

   Target SQLite schema (WAL, `synchronous=FULL`):
   ```sql
   CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
   CREATE TABLE entities (
     entity_type TEXT NOT NULL, entity_id TEXT NOT NULL,
     version INTEGER NOT NULL, deleted INTEGER NOT NULL DEFAULT 0,
     origin TEXT NOT NULL DEFAULT 'local',  -- local | server
     payload TEXT NOT NULL, updated_at TEXT NOT NULL,
     PRIMARY KEY (entity_type, entity_id));
   CREATE TABLE outbox (
     operation_id TEXT PRIMARY KEY, client_sequence INTEGER NOT NULL UNIQUE,
     entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, operation TEXT NOT NULL,
     base_version INTEGER, payload TEXT NOT NULL,
     state TEXT NOT NULL DEFAULT 'queued',  -- queued|applied|conflict|rejected|retry
     attempts INTEGER NOT NULL DEFAULT 0, next_retry_at TEXT, last_error TEXT,
     result TEXT, created_at TEXT NOT NULL);
   CREATE INDEX idx_outbox_pending ON outbox(state, client_sequence);
   ```
2. **Multiple terminals do not share live state during an outage.**
   Each PC has its own local DB; two cashiers on different devices
   will not see each other’s offline orders until both sync through
   the central server. LAN leader/peer coordination was assessed and
   intentionally **not** built: it would add election/merge complexity
   disproportionate to the current single-counter deployments. If a
   venue needs multi-cashier offline operation, that is a separate
   project (one designated always-on local hub device running this
   same server is the pragmatic path).
3. **No sync while the app is closed** (no OS service installed).
4. **Conflict resolution UI lives server-side** (admin API); the
   desktop shows the conflict *count* but resolving requires the web
   admin or API. A desktop conflicts page is future work.
5. **Server tests run on SQLite via the ORM**; PostgreSQL DDL ships in
   `migrations/001_init.sql` and is exercised by docker-compose, but a
   full PG integration run remains a pre-production step.
6. **Offline authentication** is the existing local session system;
   the device token sits in the local data file (0600). OS keychain
   storage is future work. Server-side, tokens are hashed and
   revocable.
7. Order completion BOM consumption movements are recorded locally as
   ledger entities; only *manual* movements are queued to the server,
   because the server derives consumption from the order itself —
   queuing both would double-deduct centrally. (Web-side recipe
   deduction must use the same rule.)
8. Binding a server device onto a database that was seeded with demo
   standalone data will keep the demo rows (they are local-origin).
   Bind devices on a fresh data directory.

## 5. Deployment / migration checklist

1. `cd server && docker compose up -d` (PostgreSQL + sync server), set
   `SYNC_JWT_SECRET`, apply `migrations/001_init.sql`.
2. Register each desktop as a device (admin API); token shown once.
3. On each desktop set `COFFISH_API_URL`, `COFFISH_DEVICE_ID`,
   `COFFISH_DEVICE_TOKEN`; start the app — the engine bootstraps and
   begins 10-minute cycles automatically. Existing local data files
   need no migration (origin/schema metadata is added lazily and
   non-destructively).
4. Wire every web-side mutation through `record_change()` (see server
   README) so pull stays truthful.
5. CI must run `go test ./...` (including the new store/engine tests)
   and the pytest suite against PostgreSQL before release.
