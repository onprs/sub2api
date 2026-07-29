ALTER TABLE account_observer_tokens
    DROP CONSTRAINT IF EXISTS account_observer_tokens_scope_check;

ALTER TABLE account_observer_tokens
    ADD CONSTRAINT account_observer_tokens_scope_check CHECK (
        scope IN (
            'account_observer:read',
            'account_observer:read account_observer:delete'
        )
    );
