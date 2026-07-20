BEGIN;

ALTER TABLE turns
    DROP CONSTRAINT turns_status_check,
    ADD CONSTRAINT turns_status_check
        CHECK (status IN ('accepted', 'context_ready', 'processing', 'completed', 'failed', 'cancelled')),
    ADD COLUMN cancel_requested_at timestamptz,
    ADD COLUMN cancelled_at timestamptz;

ALTER TABLE turn_runs
    DROP CONSTRAINT turn_runs_status_check,
    ADD CONSTRAINT turn_runs_status_check
        CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
    ADD COLUMN output_items_blob_key text,
    ADD COLUMN tool_results_blob_key text,
    ADD COLUMN presentation_events_blob_key text,
    ADD COLUMN checkpoint_blob_key text,
    ADD COLUMN request_checksum text,
    ADD COLUMN response_checksum text,
    ADD COLUMN request_size_bytes bigint,
    ADD COLUMN response_size_bytes bigint,
    ADD COLUMN request_schema_version integer NOT NULL DEFAULT 1,
    ADD COLUMN response_schema_version integer NOT NULL DEFAULT 1,
    ADD COLUMN cancelled_at timestamptz;

ALTER TABLE turn_runs
    DROP CONSTRAINT turn_runs_turn_id_step_index_key,
    ADD CONSTRAINT turn_runs_turn_id_step_index_attempt_key
        UNIQUE (turn_id, step_index, attempt);

ALTER TABLE context_heads
    ADD COLUMN version bigint NOT NULL DEFAULT 0 CHECK (version >= 0),
    ADD COLUMN latest_request_run_id uuid REFERENCES turn_runs (id) ON DELETE SET NULL,
    ADD COLUMN latest_successful_run_id uuid REFERENCES turn_runs (id) ON DELETE SET NULL,
    ADD COLUMN latest_checkpoint_key text,
    ADD COLUMN checkpoint_covered_event_seq bigint NOT NULL DEFAULT 0 CHECK (checkpoint_covered_event_seq >= 0),
    ADD COLUMN last_context_event_seq bigint NOT NULL DEFAULT 0 CHECK (last_context_event_seq >= 0),
    ADD COLUMN context_schema_version integer NOT NULL DEFAULT 1 CHECK (context_schema_version > 0);

CREATE TABLE conversation_events (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    turn_id uuid,
    turn_run_id uuid,
    event_seq bigint NOT NULL CHECK (event_seq > 0),
    event_key text NOT NULL CHECK (length(btrim(event_key)) > 0),
    schema_version integer NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    event_type text NOT NULL CHECK (length(btrim(event_type)) > 0),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    context_included boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, event_seq),
    UNIQUE (conversation_id, event_key),
    FOREIGN KEY (conversation_id, turn_id)
        REFERENCES turns (conversation_id, id) ON DELETE CASCADE,
    FOREIGN KEY (turn_id, turn_run_id)
        REFERENCES turn_runs (turn_id, id) ON DELETE CASCADE
);

CREATE INDEX conversation_events_conversation_seq_idx
    ON conversation_events (conversation_id, event_seq DESC);

CREATE INDEX conversation_events_turn_seq_idx
    ON conversation_events (turn_id, event_seq);

CREATE INDEX conversation_events_context_tail_idx
    ON conversation_events (conversation_id, event_seq)
    WHERE context_included = true;

WITH legacy_messages AS (
    SELECT
        m.*,
        row_number() OVER (PARTITION BY m.conversation_id ORDER BY m.seq, m.id)::bigint AS event_seq
    FROM messages m
)
INSERT INTO conversation_events (
    conversation_id, turn_id, event_seq, event_key, schema_version,
    event_type, payload, context_included, created_at
)
SELECT
    m.conversation_id,
    m.turn_id,
    m.event_seq,
    'message:' || m.id::text,
    1,
    'message.completed',
    jsonb_build_object('message', jsonb_strip_nulls(jsonb_build_object(
        'id', m.id::text,
        'conversation_id', m.conversation_id::text,
        'turn_id', m.turn_id::text,
        'seq', m.seq,
        'role', m.role,
        'content_text', m.content_text,
        'token_count', m.token_count,
        'metadata', m.metadata,
        'created_at', m.created_at
    ))),
    NOT m.context_excluded,
    m.created_at
FROM legacy_messages m
ON CONFLICT (conversation_id, event_key) DO NOTHING;

UPDATE context_heads ch
SET
    version = CASE WHEN events.last_context_event_seq > 0 THEN 1 ELSE ch.version END,
    last_context_event_seq = events.last_context_event_seq
FROM (
    SELECT conversation_id, COALESCE(MAX(event_seq) FILTER (WHERE context_included), 0) AS last_context_event_seq
    FROM conversation_events
    GROUP BY conversation_id
) events
WHERE events.conversation_id = ch.conversation_id;

COMMIT;
