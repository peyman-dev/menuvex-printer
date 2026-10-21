"""Authentication.

Devices authenticate with `Authorization: Device <device_id>:<token>`
(the scheme the Go sync client already sends). Only token hashes are stored.
Tenant identity ALWAYS comes from the authenticated device/user record —
never from a client-supplied body field.

Admin endpoints (device registration, conflict review) use a bearer JWT
with a `tenant_id` claim, verified with SYNC_JWT_SECRET. In the production
MenuVex deployment this hook is where the web application's existing JWT
verification plugs in.
"""
import hashlib
import hmac
import os

import jwt
from fastapi import Depends, Header, HTTPException
from sqlalchemy.orm import Session

from .db import get_db
from .models import SyncDevice, utcnow

JWT_SECRET = os.environ.get("SYNC_JWT_SECRET", "")


def hash_token(token: str) -> str:
    return hashlib.sha256(token.encode("utf-8")).hexdigest()


def get_device(authorization: str = Header(default=""), db: Session = Depends(get_db)) -> SyncDevice:
    if not authorization.startswith("Device "):
        raise HTTPException(status_code=401, detail="device authentication required")
    cred = authorization[len("Device "):].strip()
    device_id, sep, token = cred.partition(":")
    if not sep or not device_id or not token:
        raise HTTPException(status_code=401, detail="malformed device credentials")
    device = db.get(SyncDevice, device_id)
    if device is None or device.revoked:
        raise HTTPException(status_code=401, detail="unknown or revoked device")
    if not hmac.compare_digest(device.token_hash, hash_token(token)):
        raise HTTPException(status_code=401, detail="invalid device token")
    device.last_seen_at = utcnow()
    db.commit()
    return device


def get_tenant(authorization: str = Header(default="")) -> str:
    if not authorization.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="user authentication required")
    if not JWT_SECRET:
        raise HTTPException(status_code=503, detail="SYNC_JWT_SECRET is not configured")
    token = authorization[len("Bearer "):].strip()
    try:
        claims = jwt.decode(token, JWT_SECRET, algorithms=["HS256"])
    except jwt.PyJWTError:
        raise HTTPException(status_code=401, detail="invalid user token") from None
    tenant_id = str(claims.get("tenant_id") or claims.get("cafe_id") or "").strip()
    if not tenant_id:
        raise HTTPException(status_code=403, detail="token has no tenant scope")
    return tenant_id
