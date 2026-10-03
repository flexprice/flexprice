-- migrate:up
-- Deploy: apply this migration before rolling the binary, or set
-- FLEXPRICE_ACTIVITY_ENABLED=false for the first roll. Activity rows are
-- written inside the business transaction, so a missing table fails every
-- write to a registered entity.
CREATE TABLE IF NOT EXISTS activity_logs (
    id             VARCHAR(50)  NOT NULL,
    tenant_id      VARCHAR(50)  NOT NULL,
    environment_id VARCHAR(50)  NOT NULL,
    category       VARCHAR(20)  NOT NULL DEFAULT 'business',
    entity_type    VARCHAR(64)  NOT NULL,
    entity_id      VARCHAR(50)  NOT NULL,
    entity_label   VARCHAR(255) NOT NULL DEFAULT '',
    action         VARCHAR(128) NOT NULL,
    actor_type     VARCHAR(20)  NOT NULL,
    actor_id       VARCHAR(128) NOT NULL,
    actor_label    VARCHAR(255) NOT NULL DEFAULT '',
    actor_user_id  VARCHAR(50)  NULL,
    source         VARCHAR(20)  NOT NULL,
    customer_id    VARCHAR(50)  NULL,
    subscription_id VARCHAR(50) NULL,
    request_id     VARCHAR(64)  NULL,
    outcome        VARCHAR(20)  NOT NULL DEFAULT 'success',
    error_code     VARCHAR(64)  NULL,
    changes        JSONB        NULL,
    snapshot       JSONB        NULL,
    metadata       JSONB        NULL,
    occurred_at    TIMESTAMPTZ  NOT NULL,
    PRIMARY KEY (occurred_at, id)
) PARTITION BY RANGE (occurred_at);

CREATE INDEX IF NOT EXISTS activity_logs_entity_idx
    ON activity_logs (tenant_id, environment_id, entity_type, entity_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS activity_logs_feed_idx
    ON activity_logs (tenant_id, environment_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS activity_logs_customer_idx
    ON activity_logs (tenant_id, environment_id, customer_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS activity_logs_subscription_idx
    ON activity_logs (tenant_id, environment_id, subscription_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS activity_logs_actor_idx
    ON activity_logs (tenant_id, environment_id, actor_type, actor_id, occurred_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS activity_logs_2026_10 PARTITION OF activity_logs
    FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE IF NOT EXISTS activity_logs_2026_11 PARTITION OF activity_logs
    FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
CREATE TABLE IF NOT EXISTS activity_logs_2026_12 PARTITION OF activity_logs
    FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');

-- Safety net: a row whose month has no partition yet (the worker that runs
-- MaintainPartitionsActivity was down) lands here instead of failing the
-- business write. The archiver never exports or drops it. Rows here must be
-- moved out before the matching monthly partition can be created.
CREATE TABLE IF NOT EXISTS activity_logs_default PARTITION OF activity_logs DEFAULT;

-- migrate:down
DROP TABLE IF EXISTS activity_logs;
