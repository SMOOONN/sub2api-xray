-- A source may remove a proxy while an account editor still holds its ID.
-- Serialize reference creation with soft deletion, without changing ownership
-- rules or deleting existing data. SHARE also conflicts with non-key updates.
CREATE OR REPLACE FUNCTION enforce_live_proxy_reference()
RETURNS TRIGGER AS $$
DECLARE
    target_id BIGINT;
    target_deleted_at TIMESTAMPTZ;
BEGIN
    IF NEW.deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;
    target_id := (to_jsonb(NEW)->>TG_ARGV[0])::BIGINT;
    IF target_id IS NULL THEN
        RETURN NEW;
    END IF;
    -- Ordinary edits of an already-bound account do not introduce a reference.
    -- Avoid taking the opposite lock order for a name-only edit.
    IF TG_OP = 'UPDATE' AND OLD.deleted_at IS NULL
       AND (to_jsonb(OLD)->>TG_ARGV[0])::BIGINT IS NOT DISTINCT FROM target_id THEN
        RETURN NEW;
    END IF;
    SELECT deleted_at INTO target_deleted_at
    FROM proxies WHERE id = target_id FOR SHARE;
    IF NOT FOUND OR target_deleted_at IS NOT NULL THEN
        RAISE EXCEPTION 'proxy reference is no longer available' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_account_live_proxy_reference ON accounts;
CREATE TRIGGER trg_account_live_proxy_reference
BEFORE INSERT OR UPDATE OF proxy_id, proxy_fallback_origin_id, deleted_at ON accounts
FOR EACH ROW EXECUTE FUNCTION enforce_live_proxy_reference('proxy_id');

DROP TRIGGER IF EXISTS trg_account_live_proxy_origin ON accounts;
CREATE TRIGGER trg_account_live_proxy_origin
BEFORE INSERT OR UPDATE OF proxy_fallback_origin_id, deleted_at ON accounts
FOR EACH ROW EXECUTE FUNCTION enforce_live_proxy_reference('proxy_fallback_origin_id');

DROP TRIGGER IF EXISTS trg_proxy_live_backup_reference ON proxies;
CREATE TRIGGER trg_proxy_live_backup_reference
BEFORE INSERT OR UPDATE OF backup_proxy_id, deleted_at ON proxies
FOR EACH ROW EXECUTE FUNCTION enforce_live_proxy_reference('backup_proxy_id');
