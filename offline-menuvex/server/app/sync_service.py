"""Core synchronization logic (protocol v2, matching the Go desktop client).

Guarantees implemented here:
- Idempotency: (tenant, operation_id) is processed at most once. Replays
  return the stored result — a lost HTTP response can never duplicate an
  order, payment, or inventory movement.
- No universal last-write-wins: catalog writes require base_version
  equality; orders go through the state machine; payments and inventory
  movements are append-only facts keyed by their own IDs; order line
  prices are snapshots and are never rewritten by later price changes.
- Every accepted mutation appends to the change log in the same
  transaction, so anything applied here is guaranteed to reach pull.
- Server re-validates everything: tenant comes from the device record,
  order arithmetic is recomputed, catalog fields/prices are checked.
"""
import re
from decimal import Decimal, InvalidOperation

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from .models import ChangeLog, ProcessedOperation, SyncConflict, SyncDevice, SyncEntity
from .schemas import OperationIn

PROTOCOL_VERSION = 2
MAX_BATCH = 100
MAX_PULL_LIMIT = 500

# Mirrors internal/store/domains.go allowedOrderTransitions — the server is
# the enforcing side; the client-side copy is only a UX convenience.
ORDER_TRANSITIONS = {
    "": {"pending", "accepted", "preparing", "ready", "completed", "cancelled"},
    "pending": {"accepted", "preparing", "ready", "completed", "cancelled"},
    "accepted": {"preparing", "ready", "completed", "cancelled"},
    "preparing": {"ready", "completed", "cancelled"},
    "ready": {"completed", "cancelled"},
    "completed": set(),
    "cancelled": set(),
    "canceled": set(),
}

CATALOG_FIELDS = {
    "category": {"title", "slug", "description"},
    "product": {"title", "slug", "description", "category_id", "price", "cost", "is_available"},
    "ingredient": {"name", "sku", "barcode", "base_unit"},
    "banner": {"title", "image", "link", "is_active"},
}
CATALOG_REQUIRED = {
    "category": {"title"},
    "product": {"title", "category_id", "price"},
    "ingredient": {"name", "base_unit"},
    "banner": {"title"},
}
BASE_UNITS = {"mg", "g", "kg", "ml", "cl", "l", "pcs", "pack", "portion", "bottle", "box", "shot", "sachet"}
UUID_RE = re.compile(r"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")

SECTION_TYPES = {
    "categories": "category",
    "products": "product",
    "ingredients": "ingredient",
    "banners": "banner",
    "recipes": "recipe",
    "orders": "order",
    "tables": "table",
    "customers": "customer",
    "debtors": "debtor",
    "cafe_profile": "cafe_profile",
}


class OpResult:
    def __init__(self, status: str, error: dict | None = None, server_seq: int | None = None):
        self.status = status
        self.error = error or {}
        self.server_seq = server_seq

    def wire(self, operation_id: str) -> dict:
        return {"operation_id": operation_id, "status": self.status, "error": self.error}


def _err(code: str, message: str) -> dict:
    return {"code": code, "message": message}


def get_entity(db: Session, tenant: str, etype: str, eid: str) -> SyncEntity | None:
    return db.scalars(
        select(SyncEntity).where(
            SyncEntity.tenant_id == tenant,
            SyncEntity.entity_type == etype,
            SyncEntity.entity_id == eid,
        )
    ).first()


def record_change(
    db: Session,
    tenant: str,
    etype: str,
    eid: str,
    operation: str,
    payload: dict,
    *,
    deleted: bool = False,
    origin_device_id: str | None = None,
    origin_operation_id: str | None = None,
) -> int:
    """Upsert the entity and append its change-log entry (same transaction).

    Any server-side mutation — including ones originating from the online
    web workflows — MUST go through this function inside the caller's
    transaction, otherwise offline clients will never see it.
    Returns the issued server_seq.
    """
    ent = get_entity(db, tenant, etype, eid)
    if ent is None:
        ent = SyncEntity(tenant_id=tenant, entity_type=etype, entity_id=eid, version=0)
        db.add(ent)
    ent.version += 1
    ent.deleted = deleted
    ent.payload = payload
    db.flush()
    change = ChangeLog(
        tenant_id=tenant,
        entity_type=etype,
        entity_id=eid,
        entity_version=ent.version,
        operation=operation,
        tombstone=deleted,
        payload=payload,
        origin_device_id=origin_device_id,
        origin_operation_id=origin_operation_id,
    )
    db.add(change)
    db.flush()
    ent.last_seq = change.server_seq
    db.flush()
    return change.server_seq


def _save_conflict(db, device, op: OperationIn, reason: str, server_payload: dict):
    db.add(
        SyncConflict(
            tenant_id=device.tenant_id,
            operation_id=op.operation_id,
            device_id=device.id,
            entity_type=op.entity_type,
            entity_id=op.entity_id,
            reason=reason,
            client_payload=op.payload or {},
            server_payload=server_payload or {},
        )
    )


def _valid_decimal(value) -> bool:
    try:
        d = Decimal(str(value))
    except (InvalidOperation, ValueError):
        return False
    return d >= 0


def _irr(value) -> int | None:
    """Parse an exact integer IRR amount; floats are rejected outright."""
    if isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value
    if isinstance(value, str) and re.fullmatch(r"-?[0-9]{1,18}", value.strip()):
        return int(value)
    return None


def _apply_catalog(db, device, op: OperationIn) -> OpResult:
    etype = op.entity_type
    payload = op.payload or {}
    if op.operation not in ("create", "update", "delete"):
        return OpResult("rejected", _err("bad_action", "unsupported catalog operation"))
    existing = get_entity(db, device.tenant_id, etype, op.entity_id)
    if op.operation == "create":
        if existing is not None and not existing.deleted:
            return OpResult("conflict", _err("already_exists", "record already exists on server"))
    else:
        if existing is None or existing.deleted:
            return OpResult("conflict", _err("missing", "record no longer exists on server"))
        if op.base_version is None or op.base_version != existing.version:
            _save_conflict(db, device, op, "base_version mismatch", existing.payload)
            return OpResult("conflict", _err("version_conflict", "record changed on server since it was read"))
    if op.operation == "delete":
        seq = record_change(db, device.tenant_id, etype, op.entity_id, "delete", existing.payload,
                              deleted=True, origin_device_id=device.id, origin_operation_id=op.operation_id)
        return OpResult("applied", server_seq=seq)
    # Field-level validation (never trust the client payload shape).
    allowed = CATALOG_FIELDS[etype]
    unknown = set(payload) - allowed
    if unknown:
        return OpResult("rejected", _err("bad_field", f"fields not editable via sync: {sorted(unknown)}"))
    if op.operation == "create" and not CATALOG_REQUIRED[etype].issubset(payload):
        return OpResult("rejected", _err("missing_field", "create is missing required fields"))
    for key in ("price", "cost"):
        if key in payload and not _valid_decimal(payload[key]):
            return OpResult("rejected", _err("bad_price", f"{key} must be a nonnegative decimal"))
    if etype == "product" and "category_id" in payload and not UUID_RE.match(str(payload["category_id"])):
        return OpResult("rejected", _err("bad_ref", "category_id must be a UUID"))
    if etype == "ingredient" and "base_unit" in payload:
        if payload["base_unit"] not in BASE_UNITS:
            return OpResult("rejected", _err("bad_unit", "unknown base unit"))
        if op.operation == "update" and existing.payload.get("base_unit") not in (None, payload["base_unit"]):
            return OpResult("rejected", _err("unit_change", "ingredient units cannot change via sync"))
    merged = dict(existing.payload) if existing else {}
    merged.update(payload)
    merged["id"] = op.entity_id
    seq = record_change(db, device.tenant_id, etype, op.entity_id, op.operation, merged,
                          origin_device_id=device.id, origin_operation_id=op.operation_id)
    return OpResult("applied", server_seq=seq)


def _apply_order_create(db, device, op: OperationIn) -> OpResult:
    p = op.payload or {}
    items = p.get("items")
    if not isinstance(items, list) or not items:
        return OpResult("rejected", _err("empty_order", "order must contain at least one item"))
    existing = get_entity(db, device.tenant_id, "order", op.entity_id)
    if existing is not None and not existing.deleted:
        # Same order id created twice under different operation ids: the
        # order itself is the identity — never duplicate it.
        return OpResult("applied", _err("duplicate_order", "order already recorded"), existing.last_seq)
    subtotal = 0
    for line in items:
        if not isinstance(line, dict):
            return OpResult("rejected", _err("bad_line", "order line must be an object"))
        qty = line.get("quantity")
        unit = _irr(line.get("unit_price_irr", line.get("unit_price")))
        total = _irr(line.get("line_total_irr", line.get("line_total")))
        if not isinstance(qty, int) or qty <= 0 or unit is None or unit < 0 or total is None:
            return OpResult("rejected", _err("bad_line", "order line has invalid quantity or price"))
        if unit * qty != total:
            return OpResult("rejected", _err("line_total_mismatch", "line total does not equal unit price × quantity"))
        subtotal += total
    declared = _irr(p.get("subtotal_irr", p.get("price")))
    if declared is not None and declared != subtotal and _irr(p.get("discount_irr", 0)) in (0, None):
        return OpResult("rejected", _err("total_mismatch", "order total does not match its lines"))
    status = str(p.get("status") or "pending").lower()
    if status not in ORDER_TRANSITIONS[""]:
        return OpResult("rejected", _err("bad_status", "unknown initial order status"))
    stored = dict(p)
    stored["id"] = op.entity_id
    stored["status"] = status
    stored.setdefault("subtotal_irr", str(subtotal))
    # Line prices are immutable snapshots from sale time; later catalog
    # price changes must never rewrite this order.
    seq = record_change(db, device.tenant_id, "order", op.entity_id, "create", stored,
                          origin_device_id=device.id, origin_operation_id=op.operation_id)
    return OpResult("applied", server_seq=seq)


def _apply_order_update(db, device, op: OperationIn) -> OpResult:
    """Offline order editing: items/totals replaced wholesale.

    Only while the server copy is still editable; totals are re-derived
    with the same exact arithmetic as create. The edit-time line prices
    captured on the device are kept as snapshots — the server never
    retro-prices an order from the current catalog."""
    existing = get_entity(db, device.tenant_id, "order", op.entity_id)
    if existing is None or existing.deleted:
        return OpResult("retry", _err("dependency", "order not on server yet; create must arrive first"))
    current_status = str(existing.payload.get("status") or "").lower()
    if current_status in ("completed", "cancelled", "canceled", "refunded"):
        _save_conflict(db, device, op, f"finished order ({current_status}) edited offline", existing.payload)
        return OpResult("conflict", _err("order_finished", "a finished order cannot be edited"))
    p = op.payload or {}
    items = p.get("items")
    if not isinstance(items, list) or not items:
        return OpResult("rejected", _err("empty_order", "order must contain at least one item"))
    subtotal = 0
    for line in items:
        if not isinstance(line, dict):
            return OpResult("rejected", _err("bad_line", "order line must be an object"))
        qty = line.get("quantity")
        unit = _irr(line.get("unit_price_irr", line.get("unit_price")))
        total = _irr(line.get("line_total_irr", line.get("line_total")))
        if not isinstance(qty, int) or qty <= 0 or unit is None or unit < 0 or total is None or unit * qty != total:
            return OpResult("rejected", _err("bad_line", "order line has invalid quantity or price"))
        subtotal += total
    discount = _irr(p.get("discount_irr", 0)) or 0
    extra = _irr(p.get("extra_irr", 0)) or 0
    if discount < 0 or extra < 0 or discount > subtotal:
        return OpResult("rejected", _err("total_mismatch", "discount/extra out of range"))
    total = max(subtotal - discount + extra, 0)
    declared = _irr(p.get("total_irr", p.get("price")))
    if declared is not None and declared != total:
        return OpResult("rejected", _err("total_mismatch", "order total does not match its lines"))
    updated = dict(existing.payload)
    for k in ("items", "extra_note", "note", "updated_at"):
        if k in p:
            updated[k] = p[k]
    updated["subtotal_irr"] = str(subtotal)
    updated["discount_irr"] = str(discount)
    updated["extra_irr"] = str(extra)
    updated["total_irr"] = str(total)
    updated["price"] = str(total)
    seq = record_change(db, device.tenant_id, "order", op.entity_id, "update", updated,
                          origin_device_id=device.id, origin_operation_id=op.operation_id)
    return OpResult("applied", server_seq=seq)


def _apply_order_transition(db, device, op: OperationIn) -> OpResult:
    p = op.payload or {}
    to_status = str(p.get("to_status") or p.get("status") or "").lower().strip()
    if not to_status:
        return OpResult("rejected", _err("bad_status", "to_status is required"))
    existing = get_entity(db, device.tenant_id, "order", op.entity_id)
    if existing is None or existing.deleted:
        return OpResult("retry", _err("dependency", "order not on server yet; create must arrive first"))
    current = str(existing.payload.get("status") or "").lower()
    if current == to_status:
        return OpResult("applied", _err("already_in_state", "order already in requested state"), existing.last_seq)
    from_status = str(p.get("from_status") or "").lower().strip()
    if from_status and from_status != current:
        _save_conflict(db, device, op, f"order is '{current}', device expected '{from_status}'", existing.payload)
        return OpResult("conflict", _err("state_conflict", "order state changed on another device"))
    if to_status not in ORDER_TRANSITIONS.get(current, set()):
        _save_conflict(db, device, op, f"illegal transition {current} -> {to_status}", existing.payload)
        return OpResult("conflict", _err("illegal_transition", f"cannot move order from {current} to {to_status}"))
    updated = dict(existing.payload)
    updated["status"] = to_status
    if p.get("reason"):
        updated["status_reason"] = str(p["reason"])[:500]
    seq = record_change(db, device.tenant_id, "order", op.entity_id, "transition", updated,
                          origin_device_id=device.id, origin_operation_id=op.operation_id)
    return OpResult("applied", server_seq=seq)


def _apply_payment(db, device, op: OperationIn) -> OpResult:
    p = op.payload or {}
    amount = _irr(p.get("amount_irr"))
    order_id = str(p.get("order_id") or "")
    if amount is None or amount <= 0:
        return OpResult("rejected", _err("bad_amount", "payment amount must be a positive IRR integer"))
    if not order_id:
        return OpResult("rejected", _err("bad_ref", "payment requires order_id"))
    existing = get_entity(db, device.tenant_id, "payment", op.entity_id)
    if existing is not None:
        return OpResult("applied", _err("duplicate_payment", "payment already recorded"), existing.last_seq)
    order = get_entity(db, device.tenant_id, "order", order_id)
    if order is None or order.deleted:
        return OpResult("retry", _err("dependency", "order not on server yet"))
    stored = {
        "id": op.entity_id,
        "order_id": order_id,
        "amount_irr": str(amount),
        "method": str(p.get("method") or "cash"),
        "kind": str(p.get("kind") or "settlement"),
        # A locally recorded payment is NOT proof a remote gateway settled;
        # gateway state is tracked separately and never inferred from sync.
        "gateway_status": str(p.get("gateway_status") or "local_record_only"),
        "client_created_at": op.client_created_at,
    }
    seq = record_change(db, device.tenant_id, "payment", op.entity_id, "record", stored,
                          origin_device_id=device.id, origin_operation_id=op.operation_id)
    return OpResult("applied", server_seq=seq)


def _apply_movement(db, device, op: OperationIn) -> OpResult:
    p = op.payload or {}
    ingredient_id = str(p.get("ingredient_id") or "")
    qty = p.get("quantity")
    if not ingredient_id or qty is None:
        return OpResult("rejected", _err("bad_movement", "movement requires ingredient_id and quantity"))
    try:
        qty_f = float(qty)
    except (TypeError, ValueError):
        return OpResult("rejected", _err("bad_movement", "quantity must be numeric"))
    if qty_f == 0:
        return OpResult("rejected", _err("bad_movement", "quantity must be nonzero"))
    existing = get_entity(db, device.tenant_id, "inventory_movement", op.entity_id)
    if existing is not None:
        return OpResult("applied", _err("duplicate_movement", "movement already recorded"), existing.last_seq)
    # Movements are append-only facts. Absolute stock levels are derived
    # (SUM of movements) and are never overwritten by a device snapshot.
    stored = {
        "id": op.entity_id,
        "ingredient_id": ingredient_id,
        "transaction_type": str(p.get("transaction_type") or "adjustment"),
        "quantity": str(qty),
        "unit_cost": str(p.get("unit_cost") or "0"),
        "note": str(p.get("note") or "")[:500],
        "client_created_at": op.client_created_at,
    }
    seq = record_change(db, device.tenant_id, "inventory_movement", op.entity_id, "record", stored,
                          origin_device_id=device.id, origin_operation_id=op.operation_id)
    return OpResult("applied", server_seq=seq)


def _apply_debtor_create(db, device, op: OperationIn) -> OpResult:
    existing = get_entity(db, device.tenant_id, "debtor", op.entity_id)
    if existing is not None and not existing.deleted:
        return OpResult("applied", _err("duplicate_debtor", "debtor already recorded"), existing.last_seq)
    p = dict(op.payload or {})
    total = _irr(p.get("total"))
    paid = _irr(p.get("paid", 0)) or 0
    if total is None or total < 0 or paid < 0 or paid > total:
        return OpResult("rejected", _err("bad_amount", "debtor amounts are invalid"))
    p["id"] = op.entity_id
    p.setdefault("remaining", str(total - paid))
    seq = record_change(db, device.tenant_id, "debtor", op.entity_id, "create", p,
                          origin_device_id=device.id, origin_operation_id=op.operation_id)
    return OpResult("applied", server_seq=seq)


def _apply_debtor_payment(db, device, op: OperationIn) -> OpResult:
    p = op.payload or {}
    amount = _irr(p.get("amount_irr"))
    if amount is None or amount <= 0:
        return OpResult("rejected", _err("bad_amount", "amount must be a positive IRR integer"))
    debtor = get_entity(db, device.tenant_id, "debtor", op.entity_id)
    if debtor is None or debtor.deleted:
        return OpResult("retry", _err("dependency", "debtor not on server yet"))
    data = dict(debtor.payload)
    remaining = _irr(data.get("remaining")) or 0
    paid = _irr(data.get("paid")) or 0
    if amount > remaining:
        return OpResult("rejected", _err("overpayment", "payment exceeds remaining balance"))
    data["remaining"] = str(remaining - amount)
    data["paid"] = str(paid + amount)
    if remaining - amount == 0:
        data["status"] = "paid"
    seq = record_change(db, device.tenant_id, "debtor", op.entity_id, "payment", data,
                          origin_device_id=device.id, origin_operation_id=op.operation_id)
    return OpResult("applied", server_seq=seq)


def apply_operation(db: Session, device: SyncDevice, op: OperationIn) -> OpResult:
    if not UUID_RE.match(op.operation_id or ""):
        return OpResult("rejected", _err("bad_operation_id", "operation_id must be a UUID"))
    if op.entity_type in CATALOG_FIELDS:
        return _apply_catalog(db, device, op)
    if op.entity_type == "order" and op.operation == "create":
        return _apply_order_create(db, device, op)
    if op.entity_type == "order" and op.operation == "update":
        return _apply_order_update(db, device, op)
    if op.entity_type == "order" and op.operation == "transition":
        return _apply_order_transition(db, device, op)
    if op.entity_type == "payment" and op.operation == "record":
        return _apply_payment(db, device, op)
    if op.entity_type == "inventory_movement" and op.operation == "record":
        return _apply_movement(db, device, op)
    if op.entity_type == "debtor" and op.operation == "create":
        return _apply_debtor_create(db, device, op)
    if op.entity_type == "debtor" and op.operation == "payment":
        return _apply_debtor_payment(db, device, op)
    return OpResult("rejected", _err("unsupported", f"no sync workflow for {op.entity_type}/{op.operation}"))


def push_batch(db: Session, device: SyncDevice, operations: list[OperationIn]) -> list[dict]:
    """Process one bounded batch. Each operation gets its own savepoint so a
    single bad operation never rolls back its neighbours; every outcome is
    persisted in the idempotency ledger."""
    results: list[dict] = []
    for op in operations:
        prior = db.scalars(
            select(ProcessedOperation).where(
                ProcessedOperation.tenant_id == device.tenant_id,
                ProcessedOperation.operation_id == op.operation_id,
            )
        ).first()
        if prior is not None:
            # Idempotent replay: original outcome, no re-application.
            results.append({"operation_id": op.operation_id, "status": prior.status, "error": prior.result.get("error", {})})
            continue
        with db.begin_nested():
            result = apply_operation(db, device, op)
            db.add(
                ProcessedOperation(
                    tenant_id=device.tenant_id,
                    operation_id=op.operation_id,
                    device_id=device.id,
                    client_sequence=op.client_sequence,
                    entity_type=op.entity_type,
                    entity_id=op.entity_id,
                    action=op.operation,
                    status=result.status,
                    result={"error": result.error},
                    server_seq=result.server_seq,
                )
            )
            db.flush()
        results.append(result.wire(op.operation_id))
    db.commit()
    return results


def max_seq(db: Session, tenant: str) -> int:
    return db.scalar(select(func.max(ChangeLog.server_seq)).where(ChangeLog.tenant_id == tenant)) or 0


def min_retained_seq(db: Session, tenant: str) -> int:
    return db.scalar(select(func.min(ChangeLog.server_seq)).where(ChangeLog.tenant_id == tenant)) or 0


def bootstrap(db: Session, device: SyncDevice, include: list[str]) -> dict:
    """Consistent snapshot: the cursor is taken first; rows are read as of
    that sequence inside one transaction, so nothing is lost between the
    snapshot and incremental sync."""
    seq = max_seq(db, device.tenant_id)
    data: dict[str, list[dict]] = {}
    for section in include:
        etype = SECTION_TYPES.get(section)
        if etype is None:
            continue
        rows = db.scalars(
            select(SyncEntity).where(
                SyncEntity.tenant_id == device.tenant_id,
                SyncEntity.entity_type == etype,
                SyncEntity.deleted.is_(False),
                SyncEntity.last_seq <= seq,
            )
        ).all()
        data[section] = [{**row.payload, "id": row.entity_id, "version": row.version} for row in rows]
    return {"snapshot_cursor": str(seq), "data": data, "has_more": False}


def pull(db: Session, device: SyncDevice, cursor: str, limit: int) -> dict:
    try:
        after = int(cursor) if cursor else 0
    except ValueError:
        after = -1
    floor = min_retained_seq(db, device.tenant_id)
    if after < 0 or (floor > 0 and 0 < after < floor - 1) or after > max_seq(db, device.tenant_id):
        # Cursor is unusable (expired/foreign): client must re-bootstrap.
        raise CursorExpired()
    limit = max(1, min(limit, MAX_PULL_LIMIT))
    rows = db.scalars(
        select(ChangeLog)
        .where(ChangeLog.tenant_id == device.tenant_id, ChangeLog.server_seq > after)
        .order_by(ChangeLog.server_seq.asc())
        .limit(limit + 1)
    ).all()
    has_more = len(rows) > limit
    rows = rows[:limit]
    next_cursor = str(rows[-1].server_seq) if rows else str(after)
    changes = [
        {
            "entity_type": r.entity_type,
            "entity_id": r.entity_id,
            "entity_version": r.entity_version,
            "tombstone": r.tombstone,
            "operation": r.operation,
            "payload": r.payload,
        }
        for r in rows
    ]
    return {"next_cursor": next_cursor, "has_more": has_more, "changes": changes}


class CursorExpired(Exception):
    pass
