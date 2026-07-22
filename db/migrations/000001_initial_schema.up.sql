BEGIN;

CREATE FUNCTION set_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

CREATE FUNCTION reject_immutable_row_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$;

CREATE FUNCTION enforce_model_price_lifecycle()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'model_price_versions is immutable' USING ERRCODE = '55000';
    END IF;

    IF ROW(
        NEW.id,
        NEW.model_id,
        NEW.version,
        NEW.currency,
        NEW.input_per_million_nanos,
        NEW.cache_read_input_per_million_nanos,
        NEW.cache_creation_input_per_million_nanos,
        NEW.output_per_million_nanos,
        NEW.image_input_per_million_nanos,
        NEW.image_output_per_image_nanos,
        NEW.pricing_snapshot,
        NEW.created_by_user_id,
        NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.id,
        OLD.model_id,
        OLD.version,
        OLD.currency,
        OLD.input_per_million_nanos,
        OLD.cache_read_input_per_million_nanos,
        OLD.cache_creation_input_per_million_nanos,
        OLD.output_per_million_nanos,
        OLD.image_input_per_million_nanos,
        OLD.image_output_per_image_nanos,
        OLD.pricing_snapshot,
        OLD.created_by_user_id,
        OLD.created_at
    ) THEN
        RAISE EXCEPTION 'model price terms are immutable' USING ERRCODE = '55000';
    END IF;

    IF OLD.status = 'draft' AND NEW.status = 'published' THEN
        IF NEW.published_by_user_id IS NULL
            OR NEW.published_at IS NULL
            OR NEW.effective_from IS NULL
            OR NEW.archived_at IS NOT NULL THEN
            RAISE EXCEPTION 'invalid model price publication' USING ERRCODE = '23514';
        END IF;
    ELSIF OLD.status = 'published' AND NEW.status = 'archived' THEN
        IF NEW.published_by_user_id IS DISTINCT FROM OLD.published_by_user_id
            OR NEW.published_at IS DISTINCT FROM OLD.published_at
            OR NEW.effective_from IS DISTINCT FROM OLD.effective_from
            OR NEW.archived_at IS NULL THEN
            RAISE EXCEPTION 'invalid model price archival' USING ERRCODE = '23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'invalid model price status transition from % to %', OLD.status, NEW.status
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    email text NOT NULL,
    username text NOT NULL,
    password_hash text NOT NULL,
    role text NOT NULL CHECK (role IN ('system', 'admin', 'user')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    last_login_at timestamptz,
    email_verified_at timestamptz,
    auth_version bigint NOT NULL DEFAULT 1 CHECK (auth_version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_users_email_unique
    ON users (lower(email));

CREATE UNIQUE INDEX idx_users_username_unique
    ON users (lower(username));

CREATE UNIQUE INDEX idx_users_unique_system_role
    ON users (role)
    WHERE role = 'system';

CREATE INDEX idx_users_role_created_at
    ON users (role, created_at DESC);

CREATE TRIGGER users_set_updated_at
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE provider_credentials (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    provider text NOT NULL CHECK (provider IN ('openai')),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    base_url text NOT NULL CHECK (length(btrim(base_url)) > 0),
    encrypted_api_key bytea NOT NULL,
    nonce bytea NOT NULL CHECK (octet_length(nonce) = 12),
    key_version integer NOT NULL DEFAULT 1 CHECK (key_version > 0),
    key_hint text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled', 'revoked')),
    last_validated_at timestamptz,
    last_validation_error text,
    created_by_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    updated_by_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, name)
);

CREATE INDEX idx_provider_credentials_cursor
    ON provider_credentials (created_at DESC, id DESC);

CREATE TRIGGER provider_credentials_set_updated_at
BEFORE UPDATE ON provider_credentials
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE models (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    provider text NOT NULL CHECK (provider IN ('openai')),
    credential_id uuid NOT NULL REFERENCES provider_credentials (id) ON DELETE RESTRICT,
    slug text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9._-]{0,99}$'),
    upstream_model text NOT NULL CHECK (length(btrim(upstream_model)) > 0),
    display_name text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 120),
    description text NOT NULL DEFAULT '',
    input_modalities text[] NOT NULL DEFAULT ARRAY['text']::text[],
    output_modalities text[] NOT NULL DEFAULT ARRAY['text']::text[],
    supports_tools boolean NOT NULL DEFAULT true,
    supports_parallel_tools boolean NOT NULL DEFAULT true,
    supported_reasoning_efforts text[] NOT NULL DEFAULT ARRAY[]::text[],
    context_window_tokens integer NOT NULL CHECK (context_window_tokens > 0),
    max_output_tokens integer NOT NULL CHECK (max_output_tokens > 0 AND max_output_tokens <= context_window_tokens),
    default_parameters jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(default_parameters) = 'object'),
    status text NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_by_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    updated_by_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, upstream_model, credential_id),
    CONSTRAINT models_supported_reasoning_efforts_valid
        CHECK (supported_reasoning_efforts <@ ARRAY['low', 'medium', 'high', 'xhigh']::text[]),
    CONSTRAINT models_default_reasoning_effort_supported
        CHECK (
            NOT (default_parameters ? 'reasoning_effort')
            OR (
                jsonb_typeof(default_parameters -> 'reasoning_effort') = 'string'
                AND (default_parameters ->> 'reasoning_effort') = ANY(supported_reasoning_efforts)
            )
        )
);

CREATE INDEX idx_models_status_cursor
    ON models (status, created_at DESC, id DESC);

CREATE INDEX idx_models_admin_cursor
    ON models (created_at DESC, id DESC);

CREATE INDEX idx_models_credential
    ON models (credential_id);

CREATE TRIGGER models_set_updated_at
BEFORE UPDATE ON models
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE model_price_versions (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    model_id uuid NOT NULL REFERENCES models (id) ON DELETE RESTRICT,
    version bigint NOT NULL CHECK (version > 0),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    input_per_million_nanos bigint NOT NULL DEFAULT 0 CHECK (input_per_million_nanos >= 0),
    cache_read_input_per_million_nanos bigint NOT NULL DEFAULT 0 CHECK (cache_read_input_per_million_nanos >= 0),
    cache_creation_input_per_million_nanos bigint NOT NULL DEFAULT 0 CHECK (cache_creation_input_per_million_nanos >= 0),
    output_per_million_nanos bigint NOT NULL DEFAULT 0 CHECK (output_per_million_nanos >= 0),
    image_input_per_million_nanos bigint CHECK (image_input_per_million_nanos IS NULL OR image_input_per_million_nanos >= 0),
    image_output_per_image_nanos bigint CHECK (image_output_per_image_nanos IS NULL OR image_output_per_image_nanos >= 0),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')),
    effective_from timestamptz,
    pricing_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(pricing_snapshot) = 'object'),
    created_by_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    published_by_user_id uuid REFERENCES users (id) ON DELETE RESTRICT,
    published_at timestamptz,
    archived_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (model_id, version),
    UNIQUE (model_id, id),
    CHECK (
        (status = 'draft'
            AND effective_from IS NULL
            AND published_by_user_id IS NULL
            AND published_at IS NULL
            AND archived_at IS NULL)
        OR
        (status = 'published'
            AND effective_from IS NOT NULL
            AND published_by_user_id IS NOT NULL
            AND published_at IS NOT NULL
            AND archived_at IS NULL)
        OR
        (status = 'archived'
            AND effective_from IS NOT NULL
            AND published_by_user_id IS NOT NULL
            AND published_at IS NOT NULL
            AND archived_at IS NOT NULL
            AND archived_at >= published_at)
    )
);

CREATE INDEX idx_model_prices_effective
    ON model_price_versions (model_id, effective_from DESC, version DESC)
    WHERE status = 'published';

CREATE TRIGGER model_price_versions_lifecycle
BEFORE UPDATE OR DELETE ON model_price_versions
FOR EACH ROW EXECUTE FUNCTION enforce_model_price_lifecycle();

CREATE TABLE model_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    default_chat_model_id uuid REFERENCES models (id) ON DELETE RESTRICT,
    compaction_model_id uuid REFERENCES models (id) ON DELETE RESTRICT,
    updated_by_user_id uuid REFERENCES users (id) ON DELETE RESTRICT,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO model_settings (singleton) VALUES (true);

CREATE TRIGGER model_settings_set_updated_at
BEFORE UPDATE ON model_settings
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE conversations (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    owner_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    title text,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CHECK (
        (status = 'active' AND archived_at IS NULL)
        OR (status = 'archived' AND archived_at IS NOT NULL AND archived_at >= created_at)
    )
);

CREATE INDEX idx_conversations_owner_created_at
    ON conversations (owner_user_id, created_at DESC);

CREATE TRIGGER conversations_set_updated_at
BEFORE UPDATE ON conversations
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE context_heads (
    conversation_id uuid PRIMARY KEY REFERENCES conversations (id) ON DELETE CASCADE,
    anchor_generation bigint NOT NULL DEFAULT 0 CHECK (anchor_generation >= 0),
    anchor_key text,
    covered_until_seq bigint NOT NULL DEFAULT 0 CHECK (covered_until_seq >= 0),
    raw_tail_start_seq bigint NOT NULL DEFAULT 1 CHECK (raw_tail_start_seq >= 1),
    last_seq bigint NOT NULL DEFAULT 0 CHECK (last_seq >= 0),
    active_context_tokens integer NOT NULL DEFAULT 0 CHECK (active_context_tokens >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (covered_until_seq <= last_seq),
    CHECK (raw_tail_start_seq = covered_until_seq + 1),
    CHECK (
        (anchor_generation = 0 AND anchor_key IS NULL AND covered_until_seq = 0)
        OR (anchor_generation > 0 AND anchor_key IS NOT NULL AND covered_until_seq > 0)
    )
);

CREATE TRIGGER context_heads_set_updated_at
BEFORE UPDATE ON context_heads
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE turns (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    seq bigint NOT NULL CHECK (seq > 0),
    status text NOT NULL CHECK (status IN ('accepted', 'context_ready', 'processing', 'completed', 'failed')),
    request_blob_key text,
    response_blob_key text,
    stream_blob_key text,
    openai_response_id text,
    error_code text,
    error_message text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    model_id uuid NOT NULL REFERENCES models (id) ON DELETE RESTRICT,
    model_revision bigint NOT NULL CHECK (model_revision > 0),
    model_price_id uuid NOT NULL,
    model_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(model_snapshot) = 'object'),
    started_at timestamptz,
    completed_at timestamptz,
    failed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, seq),
    UNIQUE (conversation_id, id),
    FOREIGN KEY (model_id, model_price_id)
        REFERENCES model_price_versions (model_id, id) ON DELETE RESTRICT,
    CHECK (NOT (completed_at IS NOT NULL AND failed_at IS NOT NULL))
);

CREATE INDEX idx_turns_status_updated_at
    ON turns (status, updated_at ASC);

CREATE TRIGGER turns_set_updated_at
BEFORE UPDATE ON turns
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE messages (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    turn_id uuid,
    seq bigint NOT NULL CHECK (seq > 0),
    role text NOT NULL CHECK (role IN ('system', 'developer', 'user', 'assistant', 'tool')),
    content_text text,
    token_count integer CHECK (token_count IS NULL OR token_count >= 0),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, seq),
    FOREIGN KEY (conversation_id, turn_id)
        REFERENCES turns (conversation_id, id) ON DELETE SET NULL (turn_id)
);

CREATE INDEX idx_messages_turn_id
    ON messages (turn_id);

CREATE TABLE conversation_initial_turns (
    owner_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    idempotency_key text NOT NULL CHECK (length(btrim(idempotency_key)) BETWEEN 1 AND 128),
    conversation_id uuid NOT NULL UNIQUE REFERENCES conversations (id) ON DELETE CASCADE,
    turn_id uuid,
    prepare_fingerprint text NOT NULL,
    commit_fingerprint text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_user_id, idempotency_key),
    FOREIGN KEY (conversation_id, turn_id)
        REFERENCES turns (conversation_id, id) ON DELETE CASCADE
);

CREATE TRIGGER conversation_initial_turns_set_updated_at
BEFORE UPDATE ON conversation_initial_turns
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE outbox_events (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    event_type text NOT NULL,
    conversation_id uuid REFERENCES conversations (id) ON DELETE CASCADE,
    turn_id uuid,
    turn_run_id uuid,
    published_at timestamptz,
    claim_token uuid,
    claimed_at timestamptz,
    error_message text,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (conversation_id, turn_id)
        REFERENCES turns (conversation_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_outbox_events_pending
    ON outbox_events (created_at)
    WHERE published_at IS NULL;

CREATE INDEX idx_outbox_events_claim_expiry
    ON outbox_events (claimed_at)
    WHERE published_at IS NULL AND claim_token IS NOT NULL;

CREATE TABLE turn_runs (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    turn_id uuid NOT NULL REFERENCES turns (id) ON DELETE CASCADE,
    step_index integer NOT NULL CHECK (step_index > 0),
    provider text NOT NULL CHECK (provider = 'openai.responses'),
    model text NOT NULL CHECK (length(btrim(model)) > 0),
    status text NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    request_blob_key text NOT NULL,
    response_blob_key text,
    response_id text,
    input_tokens integer NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    cache_read_input_tokens integer NOT NULL DEFAULT 0 CHECK (cache_read_input_tokens >= 0),
    cache_creation_input_tokens integer NOT NULL DEFAULT 0 CHECK (cache_creation_input_tokens >= 0),
    output_tokens integer NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    reasoning_output_tokens integer NOT NULL DEFAULT 0 CHECK (reasoning_output_tokens >= 0),
    total_tokens integer NOT NULL DEFAULT 0 CHECK (total_tokens >= 0),
    billing_currency text CHECK (billing_currency IS NULL OR billing_currency ~ '^[A-Z]{3}$'),
    billing_amount_nanos bigint CHECK (billing_amount_nanos IS NULL OR billing_amount_nanos >= 0),
    error_message text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    failed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    state_blob_key text NOT NULL DEFAULT '',
    result_blob_key text,
    lease_token uuid,
    heartbeat_at timestamptz,
    UNIQUE (turn_id, step_index),
    UNIQUE (turn_id, id),
    CHECK (NOT (completed_at IS NOT NULL AND failed_at IS NOT NULL))
);

CREATE INDEX idx_turn_runs_active_lease
    ON turn_runs (heartbeat_at)
    WHERE status = 'running' AND lease_token IS NOT NULL;

CREATE TRIGGER turn_runs_set_updated_at
BEFORE UPDATE ON turn_runs
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE outbox_events
    ADD FOREIGN KEY (turn_id, turn_run_id)
    REFERENCES turn_runs (turn_id, id) ON DELETE CASCADE;

CREATE TABLE tool_calls (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    turn_id uuid NOT NULL,
    turn_run_id uuid NOT NULL,
    call_id text NOT NULL,
    tool_type text NOT NULL,
    namespace text,
    tool_name text NOT NULL,
    status text NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    execution_attempt integer NOT NULL CHECK (execution_attempt > 0),
    arguments_blob_key text NOT NULL,
    output_blob_key text,
    error_message text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    failed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (turn_run_id, call_id),
    FOREIGN KEY (turn_id, turn_run_id)
        REFERENCES turn_runs (turn_id, id) ON DELETE CASCADE,
    CHECK (NOT (completed_at IS NOT NULL AND failed_at IS NOT NULL))
);

CREATE INDEX idx_tool_calls_turn
    ON tool_calls (turn_id, created_at);

CREATE TRIGGER tool_calls_set_updated_at
BEFORE UPDATE ON tool_calls
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE sandboxes (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    provider text NOT NULL,
    runtime_id text NOT NULL,
    status text NOT NULL CHECK (status IN ('active', 'destroyed')),
    runtime_metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(runtime_metadata) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    destroyed_at timestamptz,
    UNIQUE (conversation_id, runtime_id),
    CHECK (
        (status = 'active' AND destroyed_at IS NULL)
        OR (status = 'destroyed' AND destroyed_at IS NOT NULL AND destroyed_at >= created_at)
    )
);

CREATE UNIQUE INDEX idx_sandboxes_conversation_active
    ON sandboxes (conversation_id)
    WHERE status = 'active' AND destroyed_at IS NULL;

CREATE TRIGGER sandboxes_set_updated_at
BEFORE UPDATE ON sandboxes
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE attachments (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    uploaded_by_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    idempotency_key text,
    filename text NOT NULL,
    content_type text NOT NULL,
    category text NOT NULL CHECK (category IN ('image', 'text', 'document', 'binary')),
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    sha256 text NOT NULL,
    object_key text NOT NULL UNIQUE,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, uploaded_by_user_id, idempotency_key),
    CHECK (idempotency_key IS NULL OR (length(btrim(idempotency_key)) > 0 AND length(idempotency_key) <= 128))
);

CREATE INDEX attachments_conversation_id_created_at_idx
    ON attachments (conversation_id, created_at DESC);

CREATE TRIGGER attachments_set_updated_at
BEFORE UPDATE ON attachments
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE turn_stream_events (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    turn_id uuid NOT NULL,
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    event_index bigint GENERATED ALWAYS AS IDENTITY,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (conversation_id, turn_id)
        REFERENCES turns (conversation_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_turn_stream_events_turn
    ON turn_stream_events (turn_id, event_index);

CREATE TABLE billing_accounts (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'frozen')),
    balance_nanos bigint NOT NULL DEFAULT 0 CHECK (balance_nanos >= 0),
    version bigint NOT NULL DEFAULT 0 CHECK (version >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, currency),
    UNIQUE (id, user_id, currency)
);

CREATE INDEX idx_billing_accounts_cursor
    ON billing_accounts (created_at DESC, id DESC);

CREATE TRIGGER billing_accounts_set_updated_at
BEFORE UPDATE ON billing_accounts
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE billing_transactions (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    account_id uuid NOT NULL,
    user_id uuid NOT NULL,
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    account_sequence bigint NOT NULL CHECK (account_sequence > 0),
    kind text NOT NULL CHECK (kind IN ('manual_topup', 'manual_refund', 'model_usage_charge')),
    direction text NOT NULL CHECK (direction IN ('credit', 'debit')),
    amount_nanos bigint NOT NULL CHECK (amount_nanos > 0),
    balance_after_nanos bigint NOT NULL CHECK (balance_after_nanos >= 0),
    actor_user_id uuid REFERENCES users (id) ON DELETE RESTRICT,
    reason text NOT NULL DEFAULT '',
    reference text NOT NULL DEFAULT '',
    idempotency_key text,
    request_hash bytea CHECK (request_hash IS NULL OR octet_length(request_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (account_id, account_sequence),
    UNIQUE (actor_user_id, idempotency_key),
    FOREIGN KEY (account_id, user_id, currency)
        REFERENCES billing_accounts (id, user_id, currency) ON DELETE RESTRICT,
    CHECK (
        (kind = 'manual_topup' AND direction = 'credit')
        OR (kind IN ('manual_refund', 'model_usage_charge') AND direction = 'debit')
    ),
    CHECK (
        kind NOT IN ('manual_topup', 'manual_refund')
        OR (
            actor_user_id IS NOT NULL
            AND length(btrim(reason)) > 0
            AND idempotency_key IS NOT NULL
            AND request_hash IS NOT NULL
        )
    )
);

CREATE INDEX idx_billing_transactions_user_cursor
    ON billing_transactions (user_id, created_at DESC, id DESC);

CREATE INDEX idx_billing_transactions_admin_cursor
    ON billing_transactions (created_at DESC, id DESC);

CREATE TRIGGER billing_transactions_immutable
BEFORE UPDATE OR DELETE ON billing_transactions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_row_change();

CREATE TABLE billing_usage_events (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    request_key text NOT NULL UNIQUE,
    owner_user_id uuid REFERENCES users (id) ON DELETE RESTRICT,
    conversation_id uuid REFERENCES conversations (id) ON DELETE RESTRICT,
    turn_id uuid REFERENCES turns (id) ON DELETE RESTRICT,
    turn_run_id uuid REFERENCES turn_runs (id) ON DELETE RESTRICT,
    workflow text NOT NULL CHECK (workflow IN ('turn', 'compaction')),
    attempt integer NOT NULL DEFAULT 1 CHECK (attempt > 0),
    provider text NOT NULL,
    model_id uuid REFERENCES models (id) ON DELETE RESTRICT,
    model_revision bigint,
    model_price_id uuid REFERENCES model_price_versions (id) ON DELETE RESTRICT,
    upstream_model text NOT NULL,
    provider_response_id text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('completed', 'failed')),
    currency text CHECK (currency IS NULL OR currency ~ '^[A-Z]{3}$'),
    amount_nanos bigint CHECK (amount_nanos IS NULL OR amount_nanos >= 0),
    input_tokens integer NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    cache_read_input_tokens integer NOT NULL DEFAULT 0 CHECK (cache_read_input_tokens >= 0),
    cache_creation_input_tokens integer NOT NULL DEFAULT 0 CHECK (cache_creation_input_tokens >= 0),
    output_tokens integer NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    reasoning_output_tokens integer NOT NULL DEFAULT 0 CHECK (reasoning_output_tokens >= 0),
    total_tokens integer NOT NULL DEFAULT 0 CHECK (total_tokens >= 0),
    pricing_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(pricing_snapshot) = 'object'),
    usage jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(usage) = 'object'),
    billing_transaction_id uuid REFERENCES billing_transactions (id) ON DELETE RESTRICT,
    error_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_billing_usage_turn_run
    ON billing_usage_events (turn_run_id)
    WHERE turn_run_id IS NOT NULL;

CREATE INDEX idx_billing_usage_user_cursor
    ON billing_usage_events (owner_user_id, created_at DESC, id DESC);

CREATE INDEX idx_billing_usage_admin_cursor
    ON billing_usage_events (created_at DESC, id DESC);

CREATE TRIGGER billing_usage_events_immutable
BEFORE UPDATE OR DELETE ON billing_usage_events
FOR EACH ROW EXECUTE FUNCTION reject_immutable_row_change();

CREATE TABLE audit_events (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    actor_user_id uuid REFERENCES users (id) ON DELETE RESTRICT,
    actor_role text NOT NULL DEFAULT '',
    subject_user_id uuid REFERENCES users (id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (length(btrim(action)) > 0),
    resource_type text NOT NULL DEFAULT '',
    resource_id text NOT NULL DEFAULT '',
    outcome text NOT NULL CHECK (outcome IN ('succeeded', 'failed', 'denied')),
    request_id text NOT NULL DEFAULT '',
    client_ip inet,
    user_agent text NOT NULL DEFAULT '',
    reason text NOT NULL DEFAULT '',
    visible_to_subject boolean NOT NULL DEFAULT false,
    required_role text NOT NULL DEFAULT 'user' CHECK (required_role IN ('user', 'admin', 'system')),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_actor_cursor
    ON audit_events (actor_user_id, created_at DESC, id DESC);

CREATE INDEX idx_audit_subject_cursor
    ON audit_events (subject_user_id, created_at DESC, id DESC)
    WHERE visible_to_subject;

CREATE INDEX idx_audit_admin_cursor
    ON audit_events (created_at DESC, id DESC);

CREATE INDEX idx_audit_events_required_role_cursor
    ON audit_events (required_role, created_at DESC, id DESC);

CREATE TRIGGER audit_events_immutable
BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW EXECUTE FUNCTION reject_immutable_row_change();

CREATE TABLE account_action_tokens (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose text NOT NULL CHECK (purpose IN ('email_verification', 'password_reset')),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz NOT NULL,
    sent_at timestamptz,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (sent_at IS NULL OR sent_at >= created_at),
    CHECK (used_at IS NULL OR used_at >= created_at)
);

CREATE INDEX idx_account_action_tokens_cooldown
    ON account_action_tokens (user_id, purpose, sent_at DESC)
    WHERE sent_at IS NOT NULL;

CREATE UNIQUE INDEX idx_account_action_tokens_active
    ON account_action_tokens (user_id, purpose)
    WHERE used_at IS NULL;

CREATE TABLE smtp_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled boolean NOT NULL DEFAULT false,
    host text NOT NULL DEFAULT ''
        CHECK (host = btrim(host) AND position(chr(10) in host) = 0 AND position(chr(13) in host) = 0),
    port integer NOT NULL DEFAULT 587 CHECK (port BETWEEN 1 AND 65535),
    security text NOT NULL DEFAULT 'starttls' CHECK (security IN ('starttls', 'tls', 'none')),
    username text NOT NULL DEFAULT ''
        CHECK (position(chr(10) in username) = 0 AND position(chr(13) in username) = 0),
    encrypted_password bytea,
    password_nonce bytea,
    key_version integer NOT NULL DEFAULT 1 CHECK (key_version > 0),
    from_email text NOT NULL DEFAULT ''
        CHECK (position(chr(10) in from_email) = 0 AND position(chr(13) in from_email) = 0),
    from_name text NOT NULL DEFAULT ''
        CHECK (position(chr(10) in from_name) = 0 AND position(chr(13) in from_name) = 0),
    updated_by_user_id uuid REFERENCES users (id) ON DELETE RESTRICT,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (encrypted_password IS NULL AND password_nonce IS NULL)
        OR (
            encrypted_password IS NOT NULL
            AND password_nonce IS NOT NULL
            AND octet_length(password_nonce) = 12
        )
    ),
    CHECK (NOT enabled OR (length(host) > 0 AND length(btrim(from_email)) > 0))
);

INSERT INTO smtp_settings (singleton) VALUES (true);

CREATE TRIGGER smtp_settings_set_updated_at
BEFORE UPDATE ON smtp_settings
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
