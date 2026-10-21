import os
import sys
import uuid

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
os.environ.setdefault("DATABASE_URL", "sqlite://")
os.environ.setdefault("SYNC_JWT_SECRET", "test-secret")
os.environ.setdefault("SYNC_CREATE_TABLES", "1")

import jwt
import pytest
from fastapi.testclient import TestClient
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker
from sqlalchemy.pool import StaticPool

from app.db import Base, get_db
from app.main import app

ENGINE = create_engine(
    "sqlite://", connect_args={"check_same_thread": False}, poolclass=StaticPool
)
TestingSession = sessionmaker(bind=ENGINE, autoflush=False, expire_on_commit=False)


def override_db():
    db = TestingSession()
    try:
        yield db
    finally:
        db.close()


app.dependency_overrides[get_db] = override_db


@pytest.fixture(autouse=True)
def fresh_db():
    Base.metadata.drop_all(ENGINE)
    Base.metadata.create_all(ENGINE)
    yield


@pytest.fixture()
def client():
    return TestClient(app)


def uid() -> str:
    return str(uuid.uuid4())


def user_headers(tenant: str) -> dict:
    token = jwt.encode({"tenant_id": tenant, "sub": "owner"}, "test-secret", algorithm="HS256")
    return {"Authorization": f"Bearer {token}"}


def make_device(client: TestClient, tenant: str, name: str = "POS-1") -> dict:
    r = client.post("/api/v1/sync/devices/register", json={"name": name}, headers=user_headers(tenant))
    assert r.status_code == 200, r.text
    data = r.json()["data"]
    return {"id": data["id"], "token": data["device_token"], "tenant": tenant}


def dev_headers(device: dict) -> dict:
    return {"Authorization": f"Device {device['id']}:{device['token']}"}


def op(entity_type, entity_id, operation, payload=None, base_version=None, seq=1, op_id=None):
    return {
        "operation_id": op_id or uid(),
        "client_sequence": seq,
        "entity_type": entity_type,
        "entity_id": entity_id,
        "operation": operation,
        "base_version": base_version,
        "payload": payload or {},
        "client_created_at": "2026-10-21T10:00:00Z",
    }


def push(client: TestClient, device: dict, operations: list) -> list:
    r = client.post("/api/v1/sync/push", json={"operations": operations}, headers=dev_headers(device))
    assert r.status_code == 200, r.text
    body = r.json()
    assert body["success"] is True
    return body["data"]["results"]


def pull(client: TestClient, device: dict, cursor: str = "", limit: int = 500) -> dict:
    r = client.get("/api/v1/sync/pull", params={"cursor": cursor, "limit": limit}, headers=dev_headers(device))
    assert r.status_code == 200, r.text
    return r.json()["data"]
