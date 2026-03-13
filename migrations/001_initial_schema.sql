-- 001_initial_schema.sql
-- Run with: psql $DATABASE_URL -f migrations/001_initial_schema.sql

BEGIN;

-- Tenants (multi-tenancy)
CREATE TABLE IF NOT EXISTS tenants (
    id            UUID PRIMARY KEY,
    name          TEXT NOT NULL,
    api_key_hash  TEXT NOT NULL UNIQUE,
    rate_limit    INTEGER NOT NULL DEFAULT 60,      -- requests per minute
    byte_limit    BIGINT NOT NULL DEFAULT 524288000, -- 500MB per hour
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tenants_api_key_hash ON tenants(api_key_hash);

-- Jobs
CREATE TABLE IF NOT EXISTS jobs (
    id                  UUID PRIMARY KEY,
    tenant_id           UUID NOT NULL REFERENCES tenants(id),
    status              TEXT NOT NULL DEFAULT 'pending',
    document_url        TEXT,
    document_format     TEXT,
    document_size_bytes BIGINT NOT NULL DEFAULT 0,
    schema              JSONB NOT NULL,
    result              JSONB,
    confidence_scores   JSONB,
    model_used          TEXT,
    cost_usd            DOUBLE PRECISION NOT NULL DEFAULT 0,
    error_message       TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at        TIMESTAMPTZ
);

-- Index for worker polling: pending jobs ordered by creation time
CREATE INDEX IF NOT EXISTS idx_jobs_pending ON jobs(created_at ASC) WHERE status = 'pending';
-- Index for tenant job listing
CREATE INDEX IF NOT EXISTS idx_jobs_tenant ON jobs(tenant_id, created_at DESC);

-- Chunks (for tracking per-chunk extraction status)
CREATE TABLE IF NOT EXISTS chunks (
    id          UUID PRIMARY KEY,
    job_id      UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    sequence    INTEGER NOT NULL,
    content     TEXT NOT NULL,
    page_start  INTEGER,
    page_end    INTEGER,
    status      TEXT NOT NULL DEFAULT 'pending',
    UNIQUE(job_id, sequence)
);

-- Webhooks
CREATE TABLE IF NOT EXISTS webhooks (
    id          UUID PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    url         TEXT NOT NULL,
    secret      TEXT NOT NULL,
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhooks_tenant ON webhooks(tenant_id) WHERE active = TRUE;

-- Webhook deliveries (audit trail + retry queue)
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id      UUID NOT NULL REFERENCES webhooks(id),
    job_id          UUID NOT NULL REFERENCES jobs(id),
    attempt         INTEGER NOT NULL DEFAULT 1,
    status          TEXT NOT NULL DEFAULT 'pending',
    response_code   INTEGER,
    next_retry_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_pending ON webhook_deliveries(next_retry_at ASC)
    WHERE status = 'pending';

COMMIT;
