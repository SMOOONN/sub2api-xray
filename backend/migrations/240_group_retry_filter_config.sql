ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS retry_filter_config JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN groups.retry_filter_config IS
    'Retry filter preset reference for this group, e.g. {"preset_id":123}';
