from typing import Any

from pydantic import BaseModel, Field


class OperationIn(BaseModel):
    operation_id: str
    client_sequence: int = 0
    entity_type: str
    entity_id: str
    operation: str
    base_version: int | None = None
    payload: dict[str, Any] = Field(default_factory=dict)
    client_created_at: str = ""


class PushIn(BaseModel):
    operations: list[OperationIn] = Field(default_factory=list, max_length=500)


class HandshakeIn(BaseModel):
    sync_protocol_version: int = 0
    app_version: str = ""
    last_known_cursor: str = ""


class BootstrapIn(BaseModel):
    include: list[str] = Field(default_factory=list)


class AckIn(BaseModel):
    cursor: str = ""


class DeviceRegisterIn(BaseModel):
    name: str = ""


class ConflictResolveIn(BaseModel):
    resolution: str  # keep_server | keep_client
