"""End-to-end contract tests for the sync v2 server.

These run against SQLite through the same SQLAlchemy code paths used with
PostgreSQL in production (see README for the exact deltas: BIGSERIAL/JSONB).
"""
import uuid

from conftest import TestingSession, dev_headers, make_device, op, pull, push, uid, user_headers
from sqlalchemy import select

from app.models import ChangeLog, SyncEntity


def seed_catalog(client, device):
    cat = uid()
    prod = uid()
    results = push(client, device, [
        op("category", cat, "create", {"title": "نوشیدنی گرم"}, seq=1),
        op("product", prod, "create", {"title": "اسپرسو", "category_id": cat, "price": "500000"}, seq=2),
    ])
    assert [r["status"] for r in results] == ["applied", "applied"]
    return cat, prod


def order_payload(prod, qty=2, unit=500000):
    return {
        "status": "pending",
        "payment_method": "cash",
        "client_token": uid(),
        "items": [{
            "product_id": prod, "product_name_snapshot": "اسپرسو", "quantity": qty,
            "unit_price_irr": str(unit), "line_total_irr": str(unit * qty),
        }],
        "subtotal_irr": str(unit * qty),
        "price": str(unit * qty),
    }


# ── handshake / auth ────────────────────────────────────────────────────────

def test_handshake_requires_bootstrap_without_cursor(client):
    d = make_device(client, "cafe-1")
    r = client.post("/api/v1/sync/handshake",
                    json={"sync_protocol_version": 2, "last_known_cursor": ""},
                    headers=dev_headers(d))
    data = r.json()["data"]
    assert data["bootstrap_required"] is True
    assert data["supported_protocol_version"] == 2
    assert data["limits"]["max_batch_size"] == 100


def test_device_auth_rejected(client):
    r = client.post("/api/v1/sync/handshake", json={"sync_protocol_version": 2},
                    headers={"Authorization": "Device nope:bad"})
    assert r.status_code == 401
    d = make_device(client, "cafe-1")
    r = client.post(f"/api/v1/sync/devices/{d['id']}/revoke", headers=user_headers("cafe-1"))
    assert r.status_code == 200
    r = client.post("/api/v1/sync/handshake", json={"sync_protocol_version": 2}, headers=dev_headers(d))
    assert r.status_code == 401


def test_tenant_isolation(client):
    a = make_device(client, "cafe-A")
    b = make_device(client, "cafe-B")
    cat = uid()
    push(client, a, [op("category", cat, "create", {"title": "A-only"})])
    assert pull(client, b)["changes"] == []
    changes = pull(client, a)["changes"]
    assert len(changes) == 1 and changes[0]["entity_id"] == cat
    # B may not address A's device or conflicts.
    r = client.post(f"/api/v1/sync/devices/{a['id']}/revoke", headers=user_headers("cafe-B"))
    assert r.status_code == 404


# ── critical workflow: order → transition → payment → pull ─────────────────

def test_order_lifecycle_and_pull(client):
    d = make_device(client, "cafe-1")
    _, prod = seed_catalog(client, d)
    order = uid()
    results = push(client, d, [op("order", order, "create", order_payload(prod), seq=3)])
    assert results[0]["status"] == "applied"
    results = push(client, d, [op("order", order, "transition", {"to_status": "completed", "from_status": "pending"}, seq=4)])
    assert results[0]["status"] == "applied"
    payment = uid()
    results = push(client, d, [op("payment", payment, "record",
                                   {"order_id": order, "amount_irr": "1000000", "method": "cash"}, seq=5)])
    assert results[0]["status"] == "applied"
    page = pull(client, d)
    kinds = [(c["entity_type"], c["operation"]) for c in page["changes"]]
    assert ("order", "create") in kinds and ("order", "transition") in kinds and ("payment", "record") in kinds
    pay_change = [c for c in page["changes"] if c["entity_type"] == "payment"][0]
    assert pay_change["payload"]["gateway_status"] == "local_record_only"


def test_order_arithmetic_revalidated(client):
    d = make_device(client, "cafe-1")
    _, prod = seed_catalog(client, d)
    bad = order_payload(prod)
    bad["items"][0]["line_total_irr"] = "1"
    results = push(client, d, [op("order", uid(), "create", bad)])
    assert results[0]["status"] == "rejected"
    assert results[0]["error"]["code"] == "line_total_mismatch"


def test_illegal_order_transition_conflicts(client):
    d = make_device(client, "cafe-1")
    _, prod = seed_catalog(client, d)
    order = uid()
    push(client, d, [op("order", order, "create", order_payload(prod), seq=3)])
    push(client, d, [op("order", order, "transition", {"to_status": "completed"}, seq=4)])
    results = push(client, d, [op("order", order, "transition", {"to_status": "pending"}, seq=5)])
    assert results[0]["status"] == "conflict"
    rows = client.get("/api/v1/sync/conflicts", headers=user_headers("cafe-1")).json()["data"]
    assert len(rows) == 1 and rows[0]["entity_id"] == order


# ── idempotency: lost response after server commit ─────────────────────────

def test_replay_after_lost_response_does_not_duplicate(client):
    d = make_device(client, "cafe-1")
    _, prod = seed_catalog(client, d)
    order = uid()
    create = op("order", order, "create", order_payload(prod), seq=3)
    first = push(client, d, [create])
    assert first[0]["status"] == "applied"
    # Client never saw the response; it resends the SAME operation id.
    second = push(client, d, [create])
    assert second[0]["status"] == "applied"
    db = TestingSession()
    orders = db.scalars(select(SyncEntity).where(SyncEntity.entity_type == "order")).all()
    assert len(orders) == 1
    assert orders[0].version == 1  # not re-applied
    # Payment replay under a different operation id but same payment id.
    pay_id = uid()
    payload = {"order_id": order, "amount_irr": "1000000", "method": "card"}
    r1 = push(client, d, [op("payment", pay_id, "record", payload, seq=4)])
    r2 = push(client, d, [op("payment", pay_id, "record", payload, seq=5)])
    assert r1[0]["status"] == "applied" and r2[0]["status"] == "applied"
    payments = db.scalars(select(SyncEntity).where(SyncEntity.entity_type == "payment")).all()
    assert len(payments) == 1
    # Movement replay likewise.
    mv = uid()
    mp = {"ingredient_id": uid(), "transaction_type": "purchase", "quantity": "5000", "unit_cost": "1200"}
    push(client, d, [op("inventory_movement", mv, "record", mp, seq=6)])
    push(client, d, [op("inventory_movement", mv, "record", mp, seq=7)])
    moves = db.scalars(select(ChangeLog).where(ChangeLog.entity_type == "inventory_movement")).all()
    assert len(moves) == 1
    db.close()


def test_payment_before_order_retries_not_rejected(client):
    d = make_device(client, "cafe-1")
    results = push(client, d, [op("payment", uid(), "record", {"order_id": uid(), "amount_irr": "10"})])
    assert results[0]["status"] == "retry"  # dependency, not a permanent failure


# ── catalog versioning / conflict ───────────────────────────────────────────

def test_catalog_base_version_conflict_and_resolution(client):
    d1 = make_device(client, "cafe-1", "POS-1")
    d2 = make_device(client, "cafe-1", "POS-2")
    prod = uid()
    cat = uid()
    push(client, d1, [op("category", cat, "create", {"title": "x"}),
                      op("product", prod, "create", {"title": "لاته", "category_id": cat, "price": "10"}, seq=2)])
    # Both devices read version 1; d1 updates first.
    assert push(client, d1, [op("product", prod, "update", {"price": "12"}, base_version=1, seq=3)])[0]["status"] == "applied"
    # d2's stale base_version must conflict, not silently overwrite.
    assert push(client, d2, [op("product", prod, "update", {"price": "99"}, base_version=1, seq=3)])[0]["status"] == "conflict"
    entity = [c for c in pull(client, d2)["changes"] if c["entity_id"] == prod][-1]
    assert entity["payload"]["price"] == "12"
    rows = client.get("/api/v1/sync/conflicts", headers=user_headers("cafe-1")).json()["data"]
    assert len(rows) == 1
    r = client.post(f"/api/v1/sync/conflicts/{rows[0]['id']}/resolve",
                    json={"resolution": "keep_client"}, headers=user_headers("cafe-1"))
    assert r.status_code == 200
    entity = [c for c in pull(client, d1)["changes"] if c["entity_id"] == prod][-1]
    assert entity["payload"]["price"] == "99"


def test_price_snapshot_not_rewritten(client):
    d = make_device(client, "cafe-1")
    cat, prod = seed_catalog(client, d)
    order = uid()
    push(client, d, [op("order", order, "create", order_payload(prod, unit=500000), seq=3)])
    push(client, d, [op("product", prod, "update", {"price": "900000"}, base_version=1, seq=4)])
    order_change = [c for c in pull(client, d)["changes"] if c["entity_type"] == "order"][-1]
    assert order_change["payload"]["items"][0]["unit_price_irr"] == "500000"


def test_delete_propagates_as_tombstone(client):
    d = make_device(client, "cafe-1")
    cat, prod = seed_catalog(client, d)
    results = push(client, d, [op("product", prod, "delete", {}, base_version=1, seq=3)])
    assert results[0]["status"] == "applied"
    change = [c for c in pull(client, d)["changes"] if c["entity_id"] == prod][-1]
    assert change["tombstone"] is True and change["operation"] == "delete"


# ── bootstrap / pull pagination / cursor ────────────────────────────────────

def test_bootstrap_snapshot_and_cursor_continuity(client):
    d = make_device(client, "cafe-1")
    cat, prod = seed_catalog(client, d)
    r = client.post("/api/v1/sync/bootstrap", json={"include": ["categories", "products", "orders"]},
                    headers=dev_headers(d))
    data = r.json()["data"]
    assert data["has_more"] is False
    assert [p["id"] for p in data["data"]["products"]] == [prod]
    assert data["data"]["orders"] == []
    cursor = data["snapshot_cursor"]
    assert pull(client, d, cursor)["changes"] == []
    # A change after the snapshot must appear incrementally from the cursor.
    push(client, d, [op("product", prod, "update", {"price": "11"}, base_version=1, seq=9)])
    page = pull(client, d, cursor)
    assert len(page["changes"]) == 1 and page["changes"][0]["payload"]["price"] == "11"


def test_pull_pagination(client):
    d = make_device(client, "cafe-1")
    for i in range(5):
        push(client, d, [op("category", uid(), "create", {"title": f"c{i}"}, seq=i + 1)])
    p1 = pull(client, d, "", limit=2)
    assert len(p1["changes"]) == 2 and p1["has_more"] is True
    p2 = pull(client, d, p1["next_cursor"], limit=2)
    p3 = pull(client, d, p2["next_cursor"], limit=2)
    assert len(p3["changes"]) == 1 and p3["has_more"] is False
    seen = [c["entity_id"] for c in p1["changes"] + p2["changes"] + p3["changes"]]
    assert len(set(seen)) == 5  # ordered, no duplicates across pages


def test_invalid_cursor_requests_bootstrap(client):
    d = make_device(client, "cafe-1")
    r = client.get("/api/v1/sync/pull", params={"cursor": "999999"}, headers=dev_headers(d))
    assert r.status_code == 409
    assert r.json()["error"]["retry_class"] == "bootstrap"


def test_ack_records_cursor(client):
    d = make_device(client, "cafe-1")
    seed_catalog(client, d)
    page = pull(client, d)
    r = client.post("/api/v1/sync/ack", json={"cursor": page["next_cursor"]}, headers=dev_headers(d))
    assert r.status_code == 200
    rows = client.get("/api/v1/sync/devices", headers=user_headers("cafe-1")).json()["data"]
    assert rows[0]["last_ack_cursor"] == page["next_cursor"]


def test_batch_too_large_rejected(client):
    d = make_device(client, "cafe-1")
    ops = [op("category", uid(), "create", {"title": "x"}, seq=i) for i in range(101)]
    r = client.post("/api/v1/sync/push", json={"operations": ops}, headers=dev_headers(d))
    assert r.status_code == 413


def test_order_update_and_debtor_chain(client):
    auth = make_device(client, "cafe-1")
    oid = str(uuid.uuid4())
    push(client, auth, [op("order", oid, "create", order_payload(oid))])
    updated = order_payload(oid)
    updated["items"] = [{"product_id": "p-1", "quantity": 1, "unit_price_irr": "50000", "line_total_irr": "50000"}]
    updated["subtotal_irr"] = "50000"
    updated["total_irr"] = "50000"
    updated["price"] = "50000"
    r = push(client, auth, [op("order", oid, "update", updated)])
    assert r[0]["status"] == "applied", r
    # finish the order; an offline edit afterwards must surface as conflict
    push(client, auth, [op("order", oid, "transition", {"from_status": "pending", "to_status": "accepted"})])
    push(client, auth, [op("order", oid, "transition", {"from_status": "accepted", "to_status": "preparing"})])
    push(client, auth, [op("order", oid, "transition", {"from_status": "preparing", "to_status": "ready"})])
    push(client, auth, [op("order", oid, "transition", {"from_status": "ready", "to_status": "completed"})])
    r = push(client, auth, [op("order", oid, "update", updated)])
    assert r[0]["status"] == "conflict"
    # debtor create -> payment chain settles the account
    did = str(uuid.uuid4())
    r = push(client, auth, [op("debtor", did, "create",
                                {"id": did, "full_name": "الف", "total": "70000", "paid": "0", "status": "open"})])
    assert r[0]["status"] == "applied"
    r = push(client, auth, [op("debtor", did, "payment",
                                {"payment_id": str(uuid.uuid4()), "amount_irr": "70000"})])
    assert r[0]["status"] == "applied"
    data = pull(client, auth)
    debt = [c for c in data["changes"] if c["entity_type"] == "debtor"][-1]
    assert debt["payload"]["remaining"] == "0"
    assert debt["payload"]["status"] == "paid"
