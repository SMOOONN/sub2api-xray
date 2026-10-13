ALTER TABLE retry_filter_rules
    ADD COLUMN IF NOT EXISTS retry_delay_ms INTEGER,
    ADD COLUMN IF NOT EXISTS empty_request_mode VARCHAR(16) NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS empty_response_mode VARCHAR(16) NOT NULL DEFAULT 'none';

ALTER TABLE retry_filter_rules
    DROP CONSTRAINT IF EXISTS retry_filter_rules_retry_delay_ms_check;
ALTER TABLE retry_filter_rules
    ADD CONSTRAINT retry_filter_rules_retry_delay_ms_check
    CHECK (retry_delay_ms IS NULL OR (retry_delay_ms >= 0 AND retry_delay_ms <= 60000));

ALTER TABLE retry_filter_rules
    DROP CONSTRAINT IF EXISTS retry_filter_rules_retry_count_override_check;
ALTER TABLE retry_filter_rules
    ADD CONSTRAINT retry_filter_rules_retry_count_override_check
    CHECK (retry_count_override IS NULL OR (retry_count_override >= 0 AND retry_count_override <= 50));

ALTER TABLE retry_filter_rules
    DROP CONSTRAINT IF EXISTS retry_filter_rules_empty_request_mode_check;
ALTER TABLE retry_filter_rules
    ADD CONSTRAINT retry_filter_rules_empty_request_mode_check
    CHECK (empty_request_mode IN ('none', 'body', 'structured', 'both'));

ALTER TABLE retry_filter_rules
    DROP CONSTRAINT IF EXISTS retry_filter_rules_empty_response_mode_check;
ALTER TABLE retry_filter_rules
    ADD CONSTRAINT retry_filter_rules_empty_response_mode_check
    CHECK (empty_response_mode IN ('none', 'completed', 'interrupted', 'both'));
