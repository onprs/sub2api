-- Prefer end-to-end ClinePass checks through the local gateway while keeping direct upstream checks available.

UPDATE channel_monitor_request_templates
SET description = 'Checks ClinePass through POST /chat/completions. Prefer the current Sub2API service with a local ClinePass group key; use https://api.cline.bot/api/v1 only with a Cline-issued API key.'
WHERE provider = 'clinepass'
  AND name = 'ClinePass Chat Completions default check';
