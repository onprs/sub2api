-- 189 新增 allow_live 后，分组鉴权快照开始包含该权限。
-- 通过前向迁移扩展最终失效函数，确保权限变更能使主分组和动态候选分组的跨实例缓存失效。

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
       AND OLD.allow_image_generation IS NOT DISTINCT FROM NEW.allow_image_generation
       AND OLD.allow_live IS NOT DISTINCT FROM NEW.allow_live
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
