-- Error requests are part of the user usage history by default.
-- Administrators can still disable the feature after this one-time rollout.
INSERT INTO settings (key, value, updated_at)
VALUES ('allow_user_view_error_requests', 'true', NOW())
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_at = EXCLUDED.updated_at;
