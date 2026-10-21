"""User-authenticated administration: device lifecycle and conflict review."""
import secrets

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy import select
from sqlalchemy.orm import Session

from . import sync_service as svc
from .auth import get_tenant, hash_token
from .db import get_db
from .models import SyncConflict, SyncDevice, utcnow
from .schemas import ConflictResolveIn, DeviceRegisterIn

router = APIRouter(prefix="/api/v1/sync", tags=["sync-admin"])


def ok(data) -> dict:
    return {"success": True, "data": data, "error": None}


def device_view(d: SyncDevice) -> dict:
    return {
        "id": d.id,
        "name": d.name,
        "revoked": d.revoked,
        "last_ack_cursor": d.last_ack_cursor,
        "last_seen_at": d.last_seen_at.isoformat() if d.last_seen_at else None,
        "created_at": d.created_at.isoformat() if d.created_at else None,
    }


@router.post("/devices/register")
def register_device(body: DeviceRegisterIn, tenant: str = Depends(get_tenant), db: Session = Depends(get_db)):
    token = secrets.token_urlsafe(32)
    device = SyncDevice(tenant_id=tenant, name=(body.name or "")[:200], token_hash=hash_token(token))
    db.add(device)
    db.commit()
    # The raw token is returned exactly once; only its hash is stored.
    return ok({**device_view(device), "device_token": token})


@router.get("/devices")
def list_devices(tenant: str = Depends(get_tenant), db: Session = Depends(get_db)):
    rows = db.scalars(select(SyncDevice).where(SyncDevice.tenant_id == tenant).order_by(SyncDevice.created_at)).all()
    return ok([device_view(d) for d in rows])


@router.post("/devices/{device_id}/revoke")
def revoke_device(device_id: str, tenant: str = Depends(get_tenant), db: Session = Depends(get_db)):
    device = db.get(SyncDevice, device_id)
    if device is None or device.tenant_id != tenant:
        raise HTTPException(status_code=404, detail="device not found")
    device.revoked = True
    db.commit()
    return ok(device_view(device))


@router.get("/conflicts")
def list_conflicts(tenant: str = Depends(get_tenant), db: Session = Depends(get_db)):
    rows = db.scalars(
        select(SyncConflict)
        .where(SyncConflict.tenant_id == tenant, SyncConflict.status == "open")
        .order_by(SyncConflict.created_at.desc())
        .limit(200)
    ).all()
    return ok([
        {
            "id": c.id,
            "entity_type": c.entity_type,
            "entity_id": c.entity_id,
            "operation_id": c.operation_id,
            "reason": c.reason,
            "client_payload": c.client_payload,
            "server_payload": c.server_payload,
            "created_at": c.created_at.isoformat() if c.created_at else None,
        }
        for c in rows
    ])


@router.post("/conflicts/{conflict_id}/resolve")
def resolve_conflict(conflict_id: str, body: ConflictResolveIn, tenant: str = Depends(get_tenant), db: Session = Depends(get_db)):
    conflict = db.get(SyncConflict, conflict_id)
    if conflict is None or conflict.tenant_id != tenant or conflict.status != "open":
        raise HTTPException(status_code=404, detail="open conflict not found")
    if body.resolution not in ("keep_server", "keep_client"):
        raise HTTPException(status_code=422, detail="resolution must be keep_server or keep_client")
    if body.resolution == "keep_client":
        # Re-apply the client payload as a new server version so every
        # device converges through the normal pull feed.
        svc.record_change(
            db, tenant, conflict.entity_type, conflict.entity_id, "update",
            {**conflict.server_payload, **conflict.client_payload, "id": conflict.entity_id},
            origin_device_id=conflict.device_id, origin_operation_id=conflict.operation_id,
        )
    conflict.status = "resolved"
    conflict.resolution = body.resolution
    conflict.resolved_at = utcnow()
    db.commit()
    return ok({"id": conflict.id, "status": "resolved", "resolution": body.resolution})
