BEGIN;

CREATE TABLE conversation_shares (
    id uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    created_by_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    idempotency_key text NOT NULL CHECK (
        octet_length(idempotency_key) BETWEEN 1 AND 128
        AND idempotency_key = btrim(idempotency_key)
    ),
    title text NOT NULL DEFAULT '',
    last_message_seq bigint NOT NULL CHECK (last_message_seq >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, created_by_user_id, idempotency_key)
);

CREATE INDEX idx_conversation_shares_conversation_created_at
    ON conversation_shares (conversation_id, created_at DESC, id DESC);

COMMIT;
