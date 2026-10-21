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
