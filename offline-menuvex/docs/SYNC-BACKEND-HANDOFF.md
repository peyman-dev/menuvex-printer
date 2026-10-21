# Menuvex Offline Sync — Backend Handoff (single-file bundle)

> **What this is:** every sync document the backend team needs, merged into
> one file for easy handoff. It is a snapshot bundle — the canonical files
> live in the repo and win if anything ever differs:
>
> | Canonical file | Role |
> |---|---|
> | `offline-menuvex/server/README.md` | Server guide (run, register devices, integration rule) |
> | `offline-menuvex/docs/offline/sync-engine.md` | Audit, architecture, guarantees, limitations |
> | `offline-menuvex/contracts/go-sync-routes.json` | Sync route contract |
> | `offline-menuvex/contracts/sync-matrix.json` | Entity/operation sync matrix |
> | `offline-menuvex/server/migrations/001_init.sql` | PostgreSQL DDL |
> | `offline-menuvex/server/` (whole folder) | Reference FastAPI implementation + tests |
> | `offline-menuvex/contracts/openapi.json` | Full OpenAPI (~4 MB — not inlined here) |
>
> **Branch:** `arena/d792fd1d-menuvex-printer` · **Stack:** Go+HTMX desktop
> (local-first) ⇄ FastAPI + PostgreSQL (central sync server, protocol v2).
>
> **If you read only one thing:** §1 "Integrating web-originated changes" —
> every web-side mutation must call `record_change()` in the same DB
> transaction, otherwise offline devices never receive it via `/pull`.

## Table of contents

1. Sync Server README (run, guarantees, operations, device registration)
2. Offline-First Sync Engine — audit, design, honest limits, deployment
3. Contract: `go-sync-routes.json`
4. Contract: `sync-matrix.json`
5. PostgreSQL DDL: `migrations/001_init.sql`

---

# §1 — Sync Server README

_Source: `offline-menuvex/server/README.md`_

# Menuvex Sync Server — Offline-First Sync (protocol v2)

Central sync server for the offline Go desktop app (`offline-menuvex/`).
Implemented with **FastAPI + SQLAlchemy 2 + PostgreSQL**. It implements the
exact v2 wire contract the Go client (`internal/syncclient`) already speaks,
as documented in `contracts/go-sync-routes.json` and `contracts/sync-matrix.json`.

## What it guarantees

- **Idempotent push**: every operation is recorded in
  `sync_processed_operations` (unique on `tenant_id + operation_id`).
  A retried batch after a lost HTTP response returns the *original*
  per-operation results and never applies anything twice — no duplicate
  orders, payments, or stock movements.
- **Atomic change log**: `sync_change_log` rows are written in the *same*
  database transaction as the business mutation (`record_change()` in
  `app/sync_service.py`). Pull cursors are PostgreSQL sequences
  (`server_seq`), never device clocks.
- **Tenant isolation**: the tenant is derived from the authenticated
  device record, never from the request body. Device tokens are stored
  only as SHA-256 hashes; revocation is immediate.
- **Server-side validation**: order totals are re-derived with exact
  integer IRR arithmetic, order status transitions enforce the same
  state machine as the client, debt payments cannot exceed the
  remaining balance, catalog writes are field-allowlisted and checked
  against `base_version` (stale writers get a *conflict*, not a
  silent overwrite).
- **Conflicts are data, not data loss**: unresolvable differences are
  stored in `sync_conflicts` for an operator to resolve via the admin
  API. Nothing is silently discarded on either side.
- **Payments semantics**: a locally recorded payment is stored with
  `gateway_status: local_record_only`. An offline card/cash record is
  *evidence of a local record*, never proof of gateway settlement.

## Operations understood

| entity_type | operation | semantics |
|---|---|---|
| category, product, ingredient, banner (+ recipe/table/customer/debtor/group through catalog allowlist where defined) | create, update, delete | version-checked (`base_version`); delete = tombstone |
| order | create | new order; totals re-derived; same order id ⇒ treated as duplicate, never duplicated |
| order | update | replace items/totals while order is not finished |
| order | transition | state-machine checked (`pending→accepted→preparing→ready→completed`, cancel from any non-terminal state) |
| payment | record | unique per payment UUID; validated against the order |
| inventory_movement | record | append-only fact; stock is a derived sum, absolute quantities never overwrite other devices' movements |
| debtor | create | open a ledger account |
| debtor | payment | reduces remaining balance; overpayment rejected |

Unknown entity/operation pairs are rejected per-operation
(`status: rejected`) without failing the rest of the batch.

## Running locally

```bash
cd offline-menuvex/server
docker compose up -d        # PostgreSQL 16 + this server on :8765
```

Or bare-metal:

```bash
pip install -r requirements.txt
export DATABASE_URL=postgresql+psycopg://menuvex:secret@localhost:5432/menuvex_sync
export SYNC_JWT_SECRET='change-me'     # admin API (device mgmt / conflicts)
psql "$DATABASE_URL" -f migrations/001_init.sql
uvicorn app.main:app --host 0.0.0.0 --port 8765
```

`SYNC_CREATE_TABLES=1` (dev only) creates tables from the ORM models
instead of running the SQL migration.

## Tests

```bash
pip install -r requirements-dev.txt
./run_tests.sh
```

The suite runs the **same ORM code** against an in-memory SQLite
database for speed. Because JSONB and sequence types differ between
SQLite and PostgreSQL, treat docker-compose PostgreSQL as the
authoritative environment and run the integration smoke test against
it before production rollout.

## Registering a device

```bash
# admin JWT must carry the tenant (cafe) id claim
curl -X POST localhost:8765/api/v1/sync/devices/register \
  -H "Authorization: Bearer <admin-jwt>" \
  -d '{"name":"صندوق اصلی"}'
# response contains the device token exactly once — store it in the
# desktop app's environment: COFFISH_API_URL / COFFISH_DEVICE_ID / COFFISH_DEVICE_TOKEN
```

## Integrating web-originated changes

Any mutation that happens on the online web app (product edit, price
change, order status from the web panel…) **must** call
`record_change(db, tenant, entity_type, entity_id, operation, payload)`
inside the same transaction as the business write, otherwise offline
devices will never receive it through `/pull`.


---

# §2 — Offline-First Sync Engine (audit & design)

_Source: `offline-menuvex/docs/offline/sync-engine.md`_

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
8. Production installers start with an empty local store; the demo fixture is
   excluded by the `production` build tag. Existing operator data files are
   preserved by binding/refresh, so use a fresh data directory only when a
   genuinely new empty terminal is intended.

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


---

# §3 — Contract: go-sync-routes.json

_Source: `offline-menuvex/contracts/go-sync-routes.json`_

```json
[
  {
    "method": "POST",
    "path": "/api/v1/sync/devices/register",
    "auth": "user JWT"
  },
  {
    "method": "GET",
    "path": "/api/v1/sync/devices",
    "auth": "user JWT"
  },
  {
    "method": "GET",
    "path": "/api/v1/sync/devices/:id",
    "auth": "user JWT"
  },
  {
    "method": "POST",
    "path": "/api/v1/sync/devices/:id/revoke",
    "auth": "user JWT"
  },
  {
    "method": "GET",
    "path": "/api/v1/sync/conflicts",
    "auth": "user JWT"
  },
  {
    "method": "POST",
    "path": "/api/v1/sync/conflicts/:id/resolve",
    "auth": "user JWT"
  },
  {
    "method": "POST",
    "path": "/api/v1/sync/handshake",
    "auth": "device"
  },
  {
    "method": "POST",
    "path": "/api/v1/sync/bootstrap",
    "auth": "device"
  },
  {
    "method": "POST",
    "path": "/api/v1/sync/push",
    "auth": "device"
  },
  {
    "method": "GET",
    "path": "/api/v1/sync/pull",
    "auth": "device"
  },
  {
    "method": "POST",
    "path": "/api/v1/sync/ack",
    "auth": "device"
  },
  {
    "method": "GET",
    "path": "/api/v1/sync/status",
    "auth": "device"
  },
  {
    "method": "GET",
    "path": "/api/v1/sync/metrics",
    "auth": "internal key"
  }
]
```

---

# §4 — Contract: sync-matrix.json

_Source: `offline-menuvex/contracts/sync-matrix.json`_

```json
{
  "protocol_version": 2,
  "description": "Sync Matrix. entity types with status 'implemented' must equal the executor's supported_entity_types (tests/test_sync_executor.py).",
  "entries": [
    {
      "domain": "catalog",
      "entity_type": "category",
      "push_operations": [
        "create",
        "update",
        "delete"
      ],
      "pull": true,
      "bootstrap": true,
      "conflict_policy": "optimistic_version",
      "status": "implemented",
      "notes": "delete is a tombstone; omitted base_version = last-write-wins"
    },
    {
      "domain": "catalog",
      "entity_type": "product",
      "push_operations": [
        "create",
        "update",
        "delete"
      ],
      "pull": true,
      "bootstrap": true,
      "conflict_policy": "optimistic_version",
      "status": "implemented",
      "notes": "delete is a tombstone; omitted base_version = last-write-wins"
    },
    {
      "domain": "catalog",
      "entity_type": "ingredient",
      "push_operations": [
        "create",
        "update",
        "delete"
      ],
      "pull": true,
      "bootstrap": true,
      "conflict_policy": "optimistic_version",
      "status": "implemented",
      "notes": "delete is a tombstone; omitted base_version = last-write-wins"
    },
    {
      "domain": "marketing",
      "entity_type": "banner",
      "push_operations": [
        "create",
        "update",
        "delete"
      ],
      "pull": true,
      "bootstrap": true,
      "conflict_policy": "optimistic_version",
      "status": "implemented",
      "notes": "delete is a tombstone; omitted base_version = last-write-wins"
    },
    {
      "domain": "orders",
      "entity_type": "order",
      "push_operations": [
        "create",
        "update"
      ],
      "pull": true,
      "bootstrap": "opt-in (latest 50)",
      "conflict_policy": "create_once; update = last-write-wins full line list",
      "status": "implemented",
      "notes": "update edits the existing order in place (same id/number/table), same semantics as PUT /orders/{id}/items"
    },
    {
      "domain": "orders",
      "entity_type": "order_status",
      "push_operations": [
        "complete",
        "cancel"
      ],
      "pull": true,
      "bootstrap": false,
      "conflict_policy": "state_machine",
      "status": "implemented",
      "notes": ""
    },
    {
      "domain": "inventory",
      "entity_type": "inventory_movement",
      "push_operations": [
        "append",
        "create"
      ],
      "pull": true,
      "bootstrap": "inventory_summary",
      "conflict_policy": "append_only_event",
      "status": "implemented",
      "notes": "quantities are signed movements, never absolute stock"
    },
    {
      "domain": "recipes",
      "entity_type": "recipe",
      "push_operations": [],
      "pull": true,
      "bootstrap": true,
      "conflict_policy": "server_authoritative",
      "status": "read_only",
      "notes": ""
    },
    {
      "domain": "waste",
      "entity_type": "waste_event",
      "push_operations": [],
      "pull": true,
      "bootstrap": "opt-in",
      "conflict_policy": "server_authoritative",
      "status": "read_only",
      "notes": ""
    },
    {
      "domain": "waste",
      "entity_type": "waste_item",
      "push_operations": [],
      "pull": true,
      "bootstrap": "opt-in",
      "conflict_policy": "server_authoritative",
      "status": "read_only",
      "notes": ""
    },
    {
      "domain": "payments",
      "entity_type": null,
      "push_operations": [],
      "pull": false,
      "bootstrap": false,
      "conflict_policy": "append_only_event",
      "status": "not_implemented",
      "notes": "needs product decisions; rejected with SYNC_ENTITY_UNSUPPORTED"
    },
    {
      "domain": "customers",
      "entity_type": null,
      "push_operations": [],
      "pull": false,
      "bootstrap": false,
      "conflict_policy": "server_authoritative",
      "status": "not_implemented",
      "notes": "needs product decisions; rejected with SYNC_ENTITY_UNSUPPORTED"
    },
    {
      "domain": "debts",
      "entity_type": null,
      "push_operations": [],
      "pull": false,
      "bootstrap": false,
      "conflict_policy": "append_only_event",
      "status": "not_implemented",
      "notes": "needs product decisions; rejected with SYNC_ENTITY_UNSUPPORTED"
    },
    {
      "domain": "expenses",
      "entity_type": null,
      "push_operations": [],
      "pull": false,
      "bootstrap": false,
      "conflict_policy": "append_only_event",
      "status": "not_implemented",
      "notes": "needs product decisions; rejected with SYNC_ENTITY_UNSUPPORTED"
    },
    {
      "domain": "accounting",
      "entity_type": null,
      "push_operations": [],
      "pull": false,
      "bootstrap": false,
      "conflict_policy": "append_only_event",
      "status": "not_implemented",
      "notes": "needs product decisions; rejected with SYNC_ENTITY_UNSUPPORTED"
    },
    {
      "domain": "tables",
      "entity_type": null,
      "push_operations": [],
      "pull": false,
      "bootstrap": false,
      "conflict_policy": "server_authoritative",
      "status": "not_implemented",
      "notes": "needs product decisions; rejected with SYNC_ENTITY_UNSUPPORTED"
    },
    {
      "domain": "discounts",
      "entity_type": null,
      "push_operations": [],
      "pull": false,
      "bootstrap": false,
      "conflict_policy": "server_authoritative",
      "status": "not_implemented",
      "notes": "needs product decisions; rejected with SYNC_ENTITY_UNSUPPORTED"
    },
    {
      "domain": "settings",
      "entity_type": null,
      "push_operations": [],
      "pull": false,
      "bootstrap": false,
      "conflict_policy": "server_authoritative",
      "status": "not_implemented",
      "notes": "needs product decisions; rejected with SYNC_ENTITY_UNSUPPORTED"
    }
  ]
}
```

---

# §5 — PostgreSQL DDL (migrations/001_init.sql)

_Source: `offline-menuvex/server/migrations/001_init.sql`_

```sql
-- MenuVex sync v2 — PostgreSQL schema (authoritative DDL).
-- Safe to run on an existing database: creates only sync-owned tables.
BEGIN;

CREATE TABLE IF NOT EXISTS sync_devices (
    id              VARCHAR(36) PRIMARY KEY,
    tenant_id       VARCHAR(64) NOT NULL,
    name            VARCHAR(200) NOT NULL DEFAULT '',
    token_hash      VARCHAR(64) NOT NULL,
    revoked         BOOLEAN NOT NULL DEFAULT FALSE,
    last_ack_cursor VARCHAR(32) NOT NULL DEFAULT '',
    last_seen_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_sync_devices_tenant ON sync_devices (tenant_id);

CREATE TABLE IF NOT EXISTS sync_entities (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    entity_type VARCHAR(64) NOT NULL,
    entity_id   VARCHAR(64) NOT NULL,
    version     BIGINT NOT NULL DEFAULT 1,
    deleted     BOOLEAN NOT NULL DEFAULT FALSE,
    payload     JSONB NOT NULL DEFAULT '{}',
    last_seq    BIGINT NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_entity_identity UNIQUE (tenant_id, entity_type, entity_id)
);
CREATE INDEX IF NOT EXISTS ix_sync_entities_tenant ON sync_entities (tenant_id);

CREATE TABLE IF NOT EXISTS sync_change_log (
    server_seq          BIGSERIAL PRIMARY KEY,
    tenant_id           VARCHAR(64) NOT NULL,
    entity_type         VARCHAR(64) NOT NULL,
    entity_id           VARCHAR(64) NOT NULL,
    entity_version      BIGINT NOT NULL,
    operation           VARCHAR(32) NOT NULL,
    tombstone           BOOLEAN NOT NULL DEFAULT FALSE,
    payload             JSONB NOT NULL DEFAULT '{}',
    origin_device_id    VARCHAR(36),
    origin_operation_id VARCHAR(36),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_change_log_tenant_seq ON sync_change_log (tenant_id, server_seq);

-- Idempotency ledger: the unique constraint is what makes client replay
-- after a lost HTTP response safe. Never prune rows for operations that
-- could still be replayed by an offline device.
CREATE TABLE IF NOT EXISTS sync_processed_operations (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       VARCHAR(64) NOT NULL,
    operation_id    VARCHAR(36) NOT NULL,
    device_id       VARCHAR(36) NOT NULL REFERENCES sync_devices(id),
    client_sequence BIGINT NOT NULL DEFAULT 0,
    entity_type     VARCHAR(64) NOT NULL,
    entity_id       VARCHAR(64) NOT NULL,
    action          VARCHAR(32) NOT NULL,
    status          VARCHAR(24) NOT NULL,
    result          JSONB NOT NULL DEFAULT '{}',
    server_seq      BIGINT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_processed_operation UNIQUE (tenant_id, operation_id)
);
CREATE INDEX IF NOT EXISTS ix_processed_tenant_entity
    ON sync_processed_operations (tenant_id, entity_type, entity_id);

CREATE TABLE IF NOT EXISTS sync_conflicts (
    id             VARCHAR(36) PRIMARY KEY,
    tenant_id      VARCHAR(64) NOT NULL,
    operation_id   VARCHAR(36) NOT NULL,
    device_id      VARCHAR(36) NOT NULL,
    entity_type    VARCHAR(64) NOT NULL,
    entity_id      VARCHAR(64) NOT NULL,
    reason         TEXT NOT NULL DEFAULT '',
    client_payload JSONB NOT NULL DEFAULT '{}',
    server_payload JSONB NOT NULL DEFAULT '{}',
    status         VARCHAR(24) NOT NULL DEFAULT 'open',
    resolution     VARCHAR(24) NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS ix_sync_conflicts_tenant ON sync_conflicts (tenant_id);

COMMIT;
```

