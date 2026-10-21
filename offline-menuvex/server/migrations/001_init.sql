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
