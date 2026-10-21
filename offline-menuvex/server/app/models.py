"""Sync data model.

PostgreSQL is authoritative for shared data. Every accepted mutation writes
three things in ONE transaction: the entity row, a change_log entry with a
server-issued monotonic sequence (the pull cursor), and — for client
operations — a processed_operations row that makes replay idempotent.
"""
import uuid
from datetime import datetime, timezone

from sqlalchemy import (
    BigInteger,
    Boolean,
    DateTime,
    ForeignKey,
    Index,
    String,
    Text,
    UniqueConstraint,
)
from sqlalchemy.orm import Mapped, mapped_column

from .db import Base, BigIntPK, JSONType


def utcnow():
    return datetime.now(timezone.utc)


def new_uuid() -> str:
    return str(uuid.uuid4())


class SyncDevice(Base):
    __tablename__ = "sync_devices"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_uuid)
    tenant_id: Mapped[str] = mapped_column(String(64), index=True, nullable=False)
    name: Mapped[str] = mapped_column(String(200), default="")
    # Only the SHA-256 hash of the device token is stored; the raw token is
    # returned exactly once at registration time.
    token_hash: Mapped[str] = mapped_column(String(64), nullable=False)
    revoked: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    last_ack_cursor: Mapped[str] = mapped_column(String(32), default="")
    last_seen_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)


class SyncEntity(Base):
    """Current server-side state of one synchronized record."""

    __tablename__ = "sync_entities"
    __table_args__ = (
        UniqueConstraint("tenant_id", "entity_type", "entity_id", name="uq_entity_identity"),
    )

    id: Mapped[int] = mapped_column(BigIntPK, primary_key=True, autoincrement=True)
    tenant_id: Mapped[str] = mapped_column(String(64), index=True, nullable=False)
    entity_type: Mapped[str] = mapped_column(String(64), nullable=False)
    entity_id: Mapped[str] = mapped_column(String(64), nullable=False)
    version: Mapped[int] = mapped_column(BigInteger, default=1, nullable=False)
    deleted: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    payload: Mapped[dict] = mapped_column(JSONType, default=dict, nullable=False)
    last_seq: Mapped[int] = mapped_column(BigInteger, default=0, nullable=False)
    updated_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow, onupdate=utcnow)


class ChangeLog(Base):
    """Durable, ordered feed of every server-side change (the pull source).

    server_seq is issued by PostgreSQL (BIGSERIAL) — never derived from a
    device clock. Rows are append-only; deletions are tombstones.
    """

    __tablename__ = "sync_change_log"
    __table_args__ = (Index("ix_change_log_tenant_seq", "tenant_id", "server_seq"),)

    server_seq: Mapped[int] = mapped_column(BigIntPK, primary_key=True, autoincrement=True)
    tenant_id: Mapped[str] = mapped_column(String(64), nullable=False)
    entity_type: Mapped[str] = mapped_column(String(64), nullable=False)
    entity_id: Mapped[str] = mapped_column(String(64), nullable=False)
    entity_version: Mapped[int] = mapped_column(BigInteger, nullable=False)
    operation: Mapped[str] = mapped_column(String(32), nullable=False)  # create|update|delete|record|...
    tombstone: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    payload: Mapped[dict] = mapped_column(JSONType, default=dict, nullable=False)
    origin_device_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    origin_operation_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)


class ProcessedOperation(Base):
    """Idempotency ledger: one row per (tenant, operation_id), forever.

    A retried upload (e.g. after the HTTP response was lost) finds its row
    here and receives the original result instead of being applied twice.
    """

    __tablename__ = "sync_processed_operations"
    __table_args__ = (
        UniqueConstraint("tenant_id", "operation_id", name="uq_processed_operation"),
        Index("ix_processed_tenant_entity", "tenant_id", "entity_type", "entity_id"),
    )

    id: Mapped[int] = mapped_column(BigIntPK, primary_key=True, autoincrement=True)
    tenant_id: Mapped[str] = mapped_column(String(64), nullable=False)
    operation_id: Mapped[str] = mapped_column(String(36), nullable=False)
    device_id: Mapped[str] = mapped_column(String(36), ForeignKey("sync_devices.id"), nullable=False)
    client_sequence: Mapped[int] = mapped_column(BigInteger, default=0)
    entity_type: Mapped[str] = mapped_column(String(64), nullable=False)
    entity_id: Mapped[str] = mapped_column(String(64), nullable=False)
    action: Mapped[str] = mapped_column(String(32), nullable=False)
    status: Mapped[str] = mapped_column(String(24), nullable=False)  # applied|conflict|rejected|retry|skipped
    result: Mapped[dict] = mapped_column(JSONType, default=dict, nullable=False)
    server_seq: Mapped[int | None] = mapped_column(BigInteger, nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)


class SyncConflict(Base):
    """Conflicts stay visible until a human resolves them; never auto-merged."""

    __tablename__ = "sync_conflicts"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_uuid)
    tenant_id: Mapped[str] = mapped_column(String(64), index=True, nullable=False)
    operation_id: Mapped[str] = mapped_column(String(36), nullable=False)
    device_id: Mapped[str] = mapped_column(String(36), nullable=False)
    entity_type: Mapped[str] = mapped_column(String(64), nullable=False)
    entity_id: Mapped[str] = mapped_column(String(64), nullable=False)
    reason: Mapped[str] = mapped_column(Text, default="")
    client_payload: Mapped[dict] = mapped_column(JSONType, default=dict, nullable=False)
    server_payload: Mapped[dict] = mapped_column(JSONType, default=dict, nullable=False)
    status: Mapped[str] = mapped_column(String(24), default="open", nullable=False)  # open|resolved
    resolution: Mapped[str] = mapped_column(String(24), default="")  # keep_server|keep_client
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    resolved_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
