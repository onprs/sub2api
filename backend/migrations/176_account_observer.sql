CREATE TABLE IF NOT EXISTS account_observer_instances (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    instance_id UUID NOT NULL DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (singleton = TRUE)
);

INSERT INTO account_observer_instances (singleton)
VALUES (TRUE)
ON CONFLICT (singleton) DO NOTHING;

CREATE TABLE IF NOT EXISTS account_observer_tokens (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    token_prefix VARCHAR(32) NOT NULL,
    token_hash CHAR(64) NOT NULL,
    scope VARCHAR(64) NOT NULL DEFAULT 'account_observer:read',
    allowed_cidrs CIDR[] NOT NULL DEFAULT '{}',
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_observer_tokens_scope_check CHECK (scope = 'account_observer:read'),
    CONSTRAINT account_observer_tokens_hash_unique UNIQUE (token_hash)
);

CREATE INDEX IF NOT EXISTS idx_account_observer_tokens_prefix
    ON account_observer_tokens (token_prefix);

CREATE INDEX IF NOT EXISTS idx_account_observer_tokens_active
    ON account_observer_tokens (id)
    WHERE revoked_at IS NULL;
