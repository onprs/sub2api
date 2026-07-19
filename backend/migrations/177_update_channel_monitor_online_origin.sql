-- Update internal channel monitor targets after the public API origin migration.
-- Account upstream base URLs are intentionally unchanged.

UPDATE channel_monitors
SET endpoint = CASE
        WHEN provider = 'opencode_go' THEN 'https://cdn-api.onprs.online/v1'
        ELSE 'https://cdn-api.onprs.online'
    END,
    updated_at = NOW()
WHERE (
        provider = 'opencode_go'
        AND REGEXP_REPLACE(BTRIM(endpoint), '/+$', '') IN (
            'https://api.onprs.top',
            'https://api.onprs.top/v1'
        )
    )
    OR (
        provider IN ('antigravity_claude', 'antigravity_gemini')
        AND REGEXP_REPLACE(BTRIM(endpoint), '/+$', '') = 'https://api.onprs.top'
    );

UPDATE channel_monitor_request_templates
SET description = REPLACE(
        description,
        'https://api.onprs.top',
        'https://cdn-api.onprs.online'
    ),
    updated_at = NOW()
WHERE provider IN ('antigravity_claude', 'antigravity_gemini')
  AND description LIKE '%https://api.onprs.top%';
