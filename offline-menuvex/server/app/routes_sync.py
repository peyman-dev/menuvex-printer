"""Device sync endpoints — exact wire contract of the Go desktop client
(internal/syncclient): envelope {success, data, error{code,message,retry_class}}.
"""
import os

from fastapi import APIRouter, Depends, Query
from fastapi.responses import JSONResponse
from sqlalchemy.orm import Session

from . import sync_service as svc
from .auth import get_device
from .db import get_db
from .models import SyncDevice
from .schemas import AckIn, BootstrapIn, HandshakeIn, PushIn

router = APIRouter(prefix="/api/v1/sync", tags=["sync"])

BOOTSTRAP_AFTER_DAYS = int(os.environ.get("SYNC_BOOTSTRAP_CURSOR_MAX_AGE_DAYS", "0"))


def ok(data) -> dict:
    return {"success": True, "data": data, "error": None}


def fail(status: int, code: str, message: str, retry_class: str) -> JSONResponse:
    return JSONResponse(
        status_code=status,
        content={"success": False, "data": None,
                 "error": {"code": code, "message": message, "retry_class": retry_class}},
    )


@router.post("/handshake")
def handshake(body: HandshakeIn, device: SyncDevice = Depends(get_device), db: Session = Depends(get_db)):
    if body.sync_protocol_version != svc.PROTOCOL_VERSION:
        return fail(409, "protocol", "unsupported sync protocol version", "fatal")
    bootstrap_required = not body.last_known_cursor
    if body.last_known_cursor:
        try:
            after = int(body.last_known_cursor)
            floor = svc.min_retained_seq(db, device.tenant_id)
            if after < 0 or (floor > 0 and 0 < after < floor - 1) or after > svc.max_seq(db, device.tenant_id):
                bootstrap_required = True
        except ValueError:
            bootstrap_required = True
    return ok({
        "bootstrap_required": bootstrap_required,
        "supported_protocol_version": svc.PROTOCOL_VERSION,
        "limits": {"max_batch_size": svc.MAX_BATCH},
    })


@router.post("/bootstrap")
def bootstrap(body: BootstrapIn, device: SyncDevice = Depends(get_device), db: Session = Depends(get_db)):
    include = body.include or list(svc.SECTION_TYPES.keys())
    return ok(svc.bootstrap(db, device, include))


@router.post("/push")
def push(body: PushIn, device: SyncDevice = Depends(get_device), db: Session = Depends(get_db)):
    if len(body.operations) > svc.MAX_BATCH:
        return fail(413, "batch_too_large", f"batch exceeds {svc.MAX_BATCH} operations", "retry")
    results = svc.push_batch(db, device, body.operations)
    return ok({"results": results})


@router.get("/pull")
def pull(
    cursor: str = Query(default=""),
    limit: int = Query(default=500),
    device: SyncDevice = Depends(get_device),
    db: Session = Depends(get_db),
):
    try:
        return ok(svc.pull(db, device, cursor, limit))
    except svc.CursorExpired:
        return fail(409, "cursor_expired", "cursor is no longer retained; bootstrap required", "bootstrap")


@router.post("/ack")
def ack(body: AckIn, device: SyncDevice = Depends(get_device), db: Session = Depends(get_db)):
    # The client only acks after the page and its cursor were committed
    # locally; this is bookkeeping for observability, never a gate.
    device.last_ack_cursor = body.cursor
    db.commit()
    return ok({})
