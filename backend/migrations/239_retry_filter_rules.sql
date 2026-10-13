CREATE TABLE IF NOT EXISTS retry_filter_presets (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS retry_filter_rules (
    id BIGSERIAL PRIMARY KEY,
    preset_id BIGINT NOT NULL REFERENCES retry_filter_presets(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    priority INTEGER NOT NULL DEFAULT 0,
    status_codes JSONB NOT NULL DEFAULT '[]'::jsonb,
    keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
    upstream_codes JSONB NOT NULL DEFAULT '[]'::jsonb,
    upstream_code_paths JSONB NOT NULL DEFAULT '["error.type","error.code","code","type"]'::jsonb,
    models JSONB NOT NULL DEFAULT '[]'::jsonb,
    match_mode VARCHAR(10) NOT NULL DEFAULT 'any',
    action VARCHAR(32) NOT NULL DEFAULT 'same_account_retry',
    retry_count_override INTEGER,
    terminate_status_code INTEGER,
    terminate_message TEXT,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_retry_filter_rules_preset_enabled_priority
    ON retry_filter_rules (preset_id, enabled, priority);
