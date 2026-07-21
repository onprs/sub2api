CREATE TABLE IF NOT EXISTS ticket_attachments (
    id BIGSERIAL PRIMARY KEY,
    upload_token VARCHAR(64) NOT NULL,
    uploaded_by BIGINT,
    uploader_role VARCHAR(16) NOT NULL,
    ticket_id BIGINT,
    message_id BIGINT,
    state VARCHAR(16) NOT NULL DEFAULT 'pending',
    storage_provider VARCHAR(16) NOT NULL,
    object_key TEXT NOT NULL,
    original_name VARCHAR(255) NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    byte_size BIGINT NOT NULL,
    sha256 VARCHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ticket_attachments_upload_token_unique UNIQUE (upload_token),
    CONSTRAINT ticket_attachments_storage_object_unique UNIQUE (storage_provider, object_key),
    CONSTRAINT ticket_attachments_uploaded_by_fkey FOREIGN KEY (uploaded_by) REFERENCES users(id) ON DELETE SET NULL,
    CONSTRAINT ticket_attachments_ticket_id_fkey FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE,
    CONSTRAINT ticket_attachments_message_id_fkey FOREIGN KEY (message_id) REFERENCES ticket_messages(id) ON DELETE CASCADE,
    CONSTRAINT ticket_attachments_uploader_role_check CHECK (uploader_role IN ('user', 'admin')),
    CONSTRAINT ticket_attachments_state_check CHECK (state IN ('pending', 'attached', 'deleting')),
    CONSTRAINT ticket_attachments_storage_provider_check CHECK (storage_provider IN ('local', 's3')),
    CONSTRAINT ticket_attachments_byte_size_check CHECK (byte_size > 0),
    CONSTRAINT ticket_attachments_sha256_check CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ticket_attachments_lifecycle_check CHECK (
        (state = 'pending' AND ticket_id IS NULL AND message_id IS NULL AND expires_at IS NOT NULL)
        OR (state = 'attached' AND ticket_id IS NOT NULL AND message_id IS NOT NULL AND expires_at IS NULL)
        OR state = 'deleting'
    )
);

CREATE INDEX IF NOT EXISTS idx_ticket_attachments_pending_expiry
    ON ticket_attachments (expires_at, id)
    WHERE state IN ('pending', 'deleting');

CREATE INDEX IF NOT EXISTS idx_ticket_attachments_ticket_message
    ON ticket_attachments (ticket_id, message_id, id)
    WHERE ticket_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ticket_attachments_uploader_activity
    ON ticket_attachments (uploaded_by, uploader_role, created_at DESC)
    WHERE uploaded_by IS NOT NULL;
