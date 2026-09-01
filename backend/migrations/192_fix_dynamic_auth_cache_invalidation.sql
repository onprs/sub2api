-- 补齐动态 API Key 路由对跨实例鉴权缓存的持久失效覆盖。
-- 189 已在部分环境执行，必须使用新迁移而不能仅修改旧文件。

CREATE OR REPLACE FUNCTION enqueue_api_key_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM enqueue_auth_cache_invalidation(OLD.key);
        RETURN OLD;
    END IF;

    IF OLD.key IS DISTINCT FROM NEW.key
       OR OLD.status IS DISTINCT FROM NEW.status
       OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at
       OR OLD.user_id IS DISTINCT FROM NEW.user_id
       OR OLD.group_id IS DISTINCT FROM NEW.group_id
       OR OLD.routing_platform IS DISTINCT FROM NEW.routing_platform
       OR OLD.routing_strategy IS DISTINCT FROM NEW.routing_strategy
       OR OLD.ip_whitelist IS DISTINCT FROM NEW.ip_whitelist
       OR OLD.ip_blacklist IS DISTINCT FROM NEW.ip_blacklist
       OR OLD.expires_at IS DISTINCT FROM NEW.expires_at THEN
        PERFORM enqueue_auth_cache_invalidation(OLD.key);
        IF NEW.deleted_at IS NULL AND NEW.key IS DISTINCT FROM OLD.key THEN
            PERFORM enqueue_auth_cache_invalidation(NEW.key);
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_api_key_auth_cache_invalidation_by_id(target_api_key_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    raw_key TEXT;
BEGIN
    SELECT key INTO raw_key
    FROM api_keys
    WHERE id = target_api_key_id;

    PERFORM enqueue_auth_cache_invalidation(raw_key);
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_api_key_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM enqueue_api_key_auth_cache_invalidation_by_id(OLD.api_key_id);
        RETURN OLD;
    END IF;

    IF TG_OP = 'INSERT' THEN
        PERFORM enqueue_api_key_auth_cache_invalidation_by_id(NEW.api_key_id);
        RETURN NEW;
    END IF;

    IF OLD.api_key_id IS DISTINCT FROM NEW.api_key_id
       OR OLD.group_id IS DISTINCT FROM NEW.group_id
       OR OLD.priority IS DISTINCT FROM NEW.priority THEN
        PERFORM enqueue_api_key_auth_cache_invalidation_by_id(OLD.api_key_id);
        IF NEW.api_key_id IS DISTINCT FROM OLD.api_key_id THEN
            PERFORM enqueue_api_key_auth_cache_invalidation_by_id(NEW.api_key_id);
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_api_key_groups_auth_cache_invalidation ON api_key_groups;
CREATE TRIGGER trg_api_key_groups_auth_cache_invalidation
AFTER INSERT OR UPDATE OR DELETE ON api_key_groups
FOR EACH ROW EXECUTE FUNCTION enqueue_api_key_group_auth_cache_invalidation();

CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_group_id BIGINT;
BEGIN
    target_group_id := OLD.id;
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.is_exclusive IS NOT DISTINCT FROM NEW.is_exclusive
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN
        RETURN NEW;
    END IF;

    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
    FROM api_keys AS k
    WHERE k.deleted_at IS NULL
      AND k.key <> ''
      AND (
          k.group_id = target_group_id
          OR EXISTS (
              SELECT 1
              FROM api_key_groups AS akg
              WHERE akg.api_key_id = k.id
                AND akg.group_id = target_group_id
          )
      );
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_user_group_auth_cache_invalidations(
    target_user_id BIGINT,
    target_group_id BIGINT
)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
    FROM api_keys AS k
    WHERE k.user_id = target_user_id
      AND k.deleted_at IS NULL
      AND k.key <> ''
      AND (
          k.group_id = target_group_id
          OR EXISTS (
              SELECT 1
              FROM api_key_groups AS akg
              WHERE akg.api_key_id = k.id
                AND akg.group_id = target_group_id
          )
      );
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_allowed_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.user_id IS NOT DISTINCT FROM NEW.user_id
       AND OLD.group_id IS NOT DISTINCT FROM NEW.group_id THEN
        RETURN NEW;
    END IF;

    IF TG_OP = 'DELETE' OR TG_OP = 'UPDATE' THEN
        PERFORM enqueue_user_group_auth_cache_invalidations(OLD.user_id, OLD.group_id);
    END IF;
    IF TG_OP = 'INSERT' OR TG_OP = 'UPDATE' THEN
        PERFORM enqueue_user_group_auth_cache_invalidations(NEW.user_id, NEW.group_id);
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
