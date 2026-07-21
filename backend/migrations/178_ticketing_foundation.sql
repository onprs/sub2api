CREATE TABLE IF NOT EXISTS tickets (
    id BIGSERIAL PRIMARY KEY,
    ticket_no VARCHAR(32) NOT NULL,
    user_id BIGINT,
    requester_email VARCHAR(255) NOT NULL DEFAULT '',
    requester_username VARCHAR(100) NOT NULL DEFAULT '',
    subject VARCHAR(100) NOT NULL,
    category VARCHAR(32) NOT NULL,
    impact VARCHAR(24) NOT NULL,
    priority VARCHAR(16) NOT NULL DEFAULT 'normal',
    status VARCHAR(24) NOT NULL DEFAULT 'open',
    assignee_id BIGINT,
    request_id VARCHAR(128) NOT NULL DEFAULT '',
    usage_log_id BIGINT,
    api_key_id BIGINT,
    api_key_name VARCHAR(100) NOT NULL DEFAULT '',
    payment_order_id BIGINT,
    payment_order_no VARCHAR(100) NOT NULL DEFAULT '',
    user_subscription_id BIGINT,
    subscription_name VARCHAR(200) NOT NULL DEFAULT '',
    last_public_message_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    action_required_since TIMESTAMPTZ,
    user_notification_seq BIGINT NOT NULL DEFAULT 0,
    user_last_read_seq BIGINT NOT NULL DEFAULT 0,
    resolved_at TIMESTAMPTZ,
    reopen_deadline TIMESTAMPTZ,
    closed_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT tickets_ticket_no_unique UNIQUE (ticket_no),
    CONSTRAINT tickets_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL,
    CONSTRAINT tickets_assignee_id_fkey FOREIGN KEY (assignee_id) REFERENCES users(id) ON DELETE SET NULL,
    CONSTRAINT tickets_subject_check CHECK (char_length(btrim(subject)) BETWEEN 1 AND 100),
    CONSTRAINT tickets_category_check CHECK (category IN ('api_issue', 'subscription', 'payment', 'account', 'feature_request', 'other')),
    CONSTRAINT tickets_impact_check CHECK (impact IN ('blocked', 'degraded', 'general')),
    CONSTRAINT tickets_priority_check CHECK (priority IN ('urgent', 'high', 'normal', 'low')),
    CONSTRAINT tickets_status_check CHECK (status IN ('open', 'in_progress', 'waiting_user', 'resolved', 'closed')),
    CONSTRAINT tickets_notification_seq_check CHECK (
        user_notification_seq >= 0
        AND user_last_read_seq >= 0
        AND user_last_read_seq <= user_notification_seq
    ),
    CONSTRAINT tickets_version_check CHECK (version >= 1)
);

CREATE INDEX IF NOT EXISTS idx_tickets_user_public_activity
    ON tickets (user_id, last_public_message_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_tickets_user_status_public_activity
    ON tickets (user_id, status, last_public_message_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_tickets_admin_queue
    ON tickets (status, priority, action_required_since, id);

CREATE INDEX IF NOT EXISTS idx_tickets_assignee_queue
    ON tickets (assignee_id, status, action_required_since, id);

CREATE INDEX IF NOT EXISTS idx_tickets_category_activity
    ON tickets (category, status, last_activity_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_tickets_request_id
    ON tickets (request_id)
    WHERE request_id <> '';

CREATE INDEX IF NOT EXISTS idx_tickets_payment_order_id
    ON tickets (payment_order_id)
    WHERE payment_order_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tickets_user_subscription_id
    ON tickets (user_subscription_id)
    WHERE user_subscription_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tickets_resolved_auto_close
    ON tickets (reopen_deadline, id)
    WHERE status = 'resolved';

CREATE TABLE IF NOT EXISTS ticket_messages (
    id BIGSERIAL PRIMARY KEY,
    ticket_id BIGINT NOT NULL,
    author_id BIGINT,
    author_role VARCHAR(16) NOT NULL,
    visibility VARCHAR(16) NOT NULL,
    author_name VARCHAR(100) NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ticket_messages_ticket_id_fkey FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE,
    CONSTRAINT ticket_messages_author_id_fkey FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE SET NULL,
    CONSTRAINT ticket_messages_author_role_check CHECK (author_role IN ('user', 'admin', 'system')),
    CONSTRAINT ticket_messages_visibility_check CHECK (visibility IN ('public', 'internal')),
    CONSTRAINT ticket_messages_user_visibility_check CHECK (author_role <> 'user' OR visibility = 'public'),
    CONSTRAINT ticket_messages_body_check CHECK (char_length(btrim(body)) BETWEEN 1 AND 5000)
);

CREATE INDEX IF NOT EXISTS idx_ticket_messages_ticket_id_id
    ON ticket_messages (ticket_id, id);

CREATE INDEX IF NOT EXISTS idx_ticket_messages_ticket_visibility_id
    ON ticket_messages (ticket_id, visibility, id);

CREATE TABLE IF NOT EXISTS ticket_events (
    id BIGSERIAL PRIMARY KEY,
    ticket_id BIGINT NOT NULL,
    actor_id BIGINT,
    actor_role VARCHAR(16) NOT NULL,
    event_type VARCHAR(40) NOT NULL,
    from_status VARCHAR(24),
    to_status VARCHAR(24),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    visibility VARCHAR(16) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ticket_events_ticket_id_fkey FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE,
    CONSTRAINT ticket_events_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES users(id) ON DELETE SET NULL,
    CONSTRAINT ticket_events_actor_role_check CHECK (actor_role IN ('user', 'admin', 'system')),
    CONSTRAINT ticket_events_visibility_check CHECK (visibility IN ('public', 'internal')),
    CONSTRAINT ticket_events_from_status_check CHECK (from_status IS NULL OR from_status IN ('open', 'in_progress', 'waiting_user', 'resolved', 'closed')),
    CONSTRAINT ticket_events_to_status_check CHECK (to_status IS NULL OR to_status IN ('open', 'in_progress', 'waiting_user', 'resolved', 'closed'))
);

CREATE INDEX IF NOT EXISTS idx_ticket_events_ticket_id_id
    ON ticket_events (ticket_id, id);

CREATE INDEX IF NOT EXISTS idx_ticket_events_ticket_visibility_id
    ON ticket_events (ticket_id, visibility, id);
