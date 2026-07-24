BEGIN;


CREATE FUNCTION public.enforce_model_price_lifecycle() RETURNS trigger
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



CREATE FUNCTION public.enforce_user_attachment_storage() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    delta bigint;
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE users
        SET storage_used_bytes = storage_used_bytes + NEW.size_bytes
        WHERE id = NEW.uploaded_by_user_id
          AND storage_used_bytes + NEW.size_bytes <= storage_quota_bytes;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'storage quota exceeded' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'DELETE' THEN
        UPDATE users
        SET storage_used_bytes = storage_used_bytes - OLD.size_bytes
        WHERE id = OLD.uploaded_by_user_id;
        RETURN OLD;
    END IF;

    delta = NEW.size_bytes - OLD.size_bytes;
    IF NEW.uploaded_by_user_id IS DISTINCT FROM OLD.uploaded_by_user_id THEN
        UPDATE users
        SET storage_used_bytes = storage_used_bytes - OLD.size_bytes
        WHERE id = OLD.uploaded_by_user_id;
        UPDATE users
        SET storage_used_bytes = storage_used_bytes + NEW.size_bytes
        WHERE id = NEW.uploaded_by_user_id
          AND storage_used_bytes + NEW.size_bytes <= storage_quota_bytes;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'storage quota exceeded' USING ERRCODE = '23514';
        END IF;
    ELSIF delta > 0 THEN
        UPDATE users
        SET storage_used_bytes = storage_used_bytes + delta
        WHERE id = NEW.uploaded_by_user_id
          AND storage_used_bytes + delta <= storage_quota_bytes;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'storage quota exceeded' USING ERRCODE = '23514';
        END IF;
    ELSIF delta < 0 THEN
        UPDATE users
        SET storage_used_bytes = storage_used_bytes + delta
        WHERE id = NEW.uploaded_by_user_id;
    END IF;

    RETURN NEW;
END;
$$;



CREATE FUNCTION public.reject_immutable_row_change() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$;



CREATE FUNCTION public.set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;


CREATE TABLE public.account_action_tokens (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    purpose text NOT NULL,
    token_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    sent_at timestamp with time zone,
    used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT account_action_tokens_check CHECK ((expires_at > created_at)),
    CONSTRAINT account_action_tokens_check1 CHECK (((sent_at IS NULL) OR (sent_at >= created_at))),
    CONSTRAINT account_action_tokens_check2 CHECK (((used_at IS NULL) OR (used_at >= created_at))),
    CONSTRAINT account_action_tokens_purpose_check CHECK ((purpose = ANY (ARRAY['email_verification'::text, 'password_reset'::text]))),
    CONSTRAINT account_action_tokens_token_hash_check CHECK ((octet_length(token_hash) = 32))
);



CREATE TABLE public.attachments (
    id uuid DEFAULT uuidv7() NOT NULL,
    conversation_id uuid NOT NULL,
    uploaded_by_user_id uuid NOT NULL,
    idempotency_key text,
    filename text NOT NULL,
    content_type text NOT NULL,
    category text NOT NULL,
    size_bytes bigint NOT NULL,
    sha256 text NOT NULL,
    object_key text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    status text DEFAULT 'ready'::text NOT NULL,
    content_md5 text DEFAULT ''::text NOT NULL,
    upload_completed_at timestamp with time zone,
    CONSTRAINT attachments_category_check CHECK ((category = ANY (ARRAY['image'::text, 'text'::text, 'document'::text, 'binary'::text]))),
    CONSTRAINT attachments_idempotency_key_check CHECK (((idempotency_key IS NULL) OR ((length(btrim(idempotency_key)) > 0) AND (length(idempotency_key) <= 128)))),
    CONSTRAINT attachments_metadata_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT attachments_size_bytes_check CHECK ((size_bytes > 0)),
    CONSTRAINT attachments_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'ready'::text, 'deleting'::text])))
);



CREATE TABLE public.audit_events (
    id uuid DEFAULT uuidv7() NOT NULL,
    actor_user_id uuid,
    actor_role text DEFAULT ''::text NOT NULL,
    subject_user_id uuid,
    action text NOT NULL,
    resource_type text DEFAULT ''::text NOT NULL,
    resource_id text DEFAULT ''::text NOT NULL,
    outcome text NOT NULL,
    request_id text DEFAULT ''::text NOT NULL,
    client_ip inet,
    user_agent text DEFAULT ''::text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    visible_to_subject boolean DEFAULT false NOT NULL,
    required_role text DEFAULT 'user'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT audit_events_action_check CHECK ((length(btrim(action)) > 0)),
    CONSTRAINT audit_events_metadata_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT audit_events_outcome_check CHECK ((outcome = ANY (ARRAY['succeeded'::text, 'failed'::text, 'denied'::text]))),
    CONSTRAINT audit_events_required_role_check CHECK ((required_role = ANY (ARRAY['user'::text, 'admin'::text, 'system'::text])))
);



CREATE TABLE public.billing_accounts (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    currency text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    balance_nanos bigint DEFAULT 0 NOT NULL,
    version bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT billing_accounts_balance_nanos_check CHECK ((balance_nanos >= 0)),
    CONSTRAINT billing_accounts_currency_check CHECK ((currency ~ '^[A-Z]{3}$'::text)),
    CONSTRAINT billing_accounts_status_check CHECK ((status = ANY (ARRAY['active'::text, 'frozen'::text]))),
    CONSTRAINT billing_accounts_version_check CHECK ((version >= 0))
);



CREATE TABLE public.billing_redemption_code_disables (
    redemption_code_id uuid NOT NULL,
    disabled_by_user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);



CREATE TABLE public.billing_redemption_codes (
    id uuid DEFAULT uuidv7() NOT NULL,
    code_hash bytea NOT NULL,
    code_hint text NOT NULL,
    currency text NOT NULL,
    amount_nanos bigint NOT NULL,
    created_by_user_id uuid NOT NULL,
    expires_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT billing_redemption_codes_amount_nanos_check CHECK ((amount_nanos > 0)),
    CONSTRAINT billing_redemption_codes_check CHECK (((expires_at IS NULL) OR (expires_at > created_at))),
    CONSTRAINT billing_redemption_codes_code_hash_check CHECK ((octet_length(code_hash) = 32)),
    CONSTRAINT billing_redemption_codes_code_hint_check CHECK ((length(btrim(code_hint)) > 0)),
    CONSTRAINT billing_redemption_codes_currency_check CHECK ((currency ~ '^[A-Z]{3}$'::text))
);



CREATE TABLE public.billing_tool_prices (
    tool_key text NOT NULL,
    currency text NOT NULL,
    price_per_call_nanos bigint DEFAULT 0 NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    updated_by_user_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT billing_tool_prices_currency_check CHECK ((currency ~ '^[A-Z]{3}$'::text)),
    CONSTRAINT billing_tool_prices_enabled_price_check CHECK (((NOT enabled) OR (price_per_call_nanos > 0))),
    CONSTRAINT billing_tool_prices_json_safe_price_check CHECK ((price_per_call_nanos <= '9007199254740991'::bigint)),
    CONSTRAINT billing_tool_prices_price_per_call_nanos_check CHECK ((price_per_call_nanos >= 0)),
    CONSTRAINT billing_tool_prices_tool_key_check CHECK ((tool_key ~ '^[a-z0-9_]+(?:\.[a-z0-9_]+)*$'::text)),
    CONSTRAINT billing_tool_prices_version_check CHECK ((version > 0))
);



CREATE TABLE public.billing_transactions (
    id uuid DEFAULT uuidv7() NOT NULL,
    account_id uuid NOT NULL,
    user_id uuid NOT NULL,
    currency text NOT NULL,
    account_sequence bigint NOT NULL,
    kind text NOT NULL,
    direction text NOT NULL,
    amount_nanos bigint NOT NULL,
    balance_after_nanos bigint NOT NULL,
    actor_user_id uuid,
    reason text DEFAULT ''::text NOT NULL,
    reference text DEFAULT ''::text NOT NULL,
    idempotency_key text,
    request_hash bytea,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    redemption_code_id uuid,
    CONSTRAINT billing_transactions_account_sequence_check CHECK ((account_sequence > 0)),
    CONSTRAINT billing_transactions_amount_nanos_check CHECK ((amount_nanos > 0)),
    CONSTRAINT billing_transactions_balance_after_nanos_check CHECK ((balance_after_nanos >= 0)),
    CONSTRAINT billing_transactions_check1 CHECK (((kind <> ALL (ARRAY['manual_topup'::text, 'manual_refund'::text])) OR ((actor_user_id IS NOT NULL) AND (length(btrim(reason)) > 0) AND (idempotency_key IS NOT NULL) AND (request_hash IS NOT NULL)))),
    CONSTRAINT billing_transactions_currency_check CHECK ((currency ~ '^[A-Z]{3}$'::text)),
    CONSTRAINT billing_transactions_direction_check CHECK ((direction = ANY (ARRAY['credit'::text, 'debit'::text]))),
    CONSTRAINT billing_transactions_kind_check CHECK ((kind = ANY (ARRAY['manual_topup'::text, 'manual_refund'::text, 'model_usage_charge'::text, 'redemption_credit'::text]))),
    CONSTRAINT billing_transactions_kind_direction_check CHECK ((((kind = ANY (ARRAY['manual_topup'::text, 'redemption_credit'::text])) AND (direction = 'credit'::text)) OR ((kind = ANY (ARRAY['manual_refund'::text, 'model_usage_charge'::text])) AND (direction = 'debit'::text)))),
    CONSTRAINT billing_transactions_redemption_shape_check CHECK ((((kind = 'redemption_credit'::text) AND (redemption_code_id IS NOT NULL) AND (actor_user_id IS NOT NULL) AND (actor_user_id = user_id)) OR ((kind <> 'redemption_credit'::text) AND (redemption_code_id IS NULL)))),
    CONSTRAINT billing_transactions_request_hash_check CHECK (((request_hash IS NULL) OR (octet_length(request_hash) = 32)))
);



CREATE TABLE public.billing_usage_events (
    id uuid DEFAULT uuidv7() NOT NULL,
    request_key text NOT NULL,
    owner_user_id uuid,
    conversation_id uuid,
    turn_id uuid,
    turn_run_id uuid,
    workflow text NOT NULL,
    attempt integer DEFAULT 1 NOT NULL,
    provider text NOT NULL,
    model_id uuid,
    model_revision bigint,
    model_price_id uuid,
    upstream_model text NOT NULL,
    provider_response_id text DEFAULT ''::text NOT NULL,
    status text NOT NULL,
    currency text,
    amount_nanos bigint,
    input_tokens integer DEFAULT 0 NOT NULL,
    cache_read_input_tokens integer DEFAULT 0 NOT NULL,
    cache_creation_input_tokens integer DEFAULT 0 NOT NULL,
    output_tokens integer DEFAULT 0 NOT NULL,
    reasoning_output_tokens integer DEFAULT 0 NOT NULL,
    total_tokens integer DEFAULT 0 NOT NULL,
    pricing_snapshot jsonb DEFAULT '{}'::jsonb NOT NULL,
    usage jsonb DEFAULT '{}'::jsonb NOT NULL,
    billing_transaction_id uuid,
    error_code text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    tool_amount_nanos bigint DEFAULT 0 NOT NULL,
    tool_usage jsonb DEFAULT '{}'::jsonb NOT NULL,
    tool_pricing_snapshot jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT billing_usage_events_amount_nanos_check CHECK (((amount_nanos IS NULL) OR (amount_nanos >= 0))),
    CONSTRAINT billing_usage_events_attempt_check CHECK ((attempt > 0)),
    CONSTRAINT billing_usage_events_cache_creation_input_tokens_check CHECK ((cache_creation_input_tokens >= 0)),
    CONSTRAINT billing_usage_events_cache_read_input_tokens_check CHECK ((cache_read_input_tokens >= 0)),
    CONSTRAINT billing_usage_events_currency_check CHECK (((currency IS NULL) OR (currency ~ '^[A-Z]{3}$'::text))),
    CONSTRAINT billing_usage_events_input_tokens_check CHECK ((input_tokens >= 0)),
    CONSTRAINT billing_usage_events_output_tokens_check CHECK ((output_tokens >= 0)),
    CONSTRAINT billing_usage_events_pricing_snapshot_check CHECK ((jsonb_typeof(pricing_snapshot) = 'object'::text)),
    CONSTRAINT billing_usage_events_reasoning_output_tokens_check CHECK ((reasoning_output_tokens >= 0)),
    CONSTRAINT billing_usage_events_status_check CHECK ((status = ANY (ARRAY['completed'::text, 'failed'::text]))),
    CONSTRAINT billing_usage_events_tool_amount_nanos_check CHECK ((tool_amount_nanos >= 0)),
    CONSTRAINT billing_usage_events_tool_pricing_snapshot_check CHECK ((jsonb_typeof(tool_pricing_snapshot) = 'object'::text)),
    CONSTRAINT billing_usage_events_tool_usage_check CHECK ((jsonb_typeof(tool_usage) = 'object'::text)),
    CONSTRAINT billing_usage_events_total_tokens_check CHECK ((total_tokens >= 0)),
    CONSTRAINT billing_usage_events_usage_check CHECK ((jsonb_typeof(usage) = 'object'::text)),
    CONSTRAINT billing_usage_events_workflow_check CHECK ((workflow = ANY (ARRAY['turn'::text, 'compaction'::text])))
);



CREATE TABLE public.context_heads (
    conversation_id uuid NOT NULL,
    anchor_generation bigint DEFAULT 0 NOT NULL,
    anchor_key text,
    covered_until_seq bigint DEFAULT 0 NOT NULL,
    raw_tail_start_seq bigint DEFAULT 1 NOT NULL,
    last_seq bigint DEFAULT 0 NOT NULL,
    active_context_tokens integer DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    version bigint DEFAULT 0 NOT NULL,
    latest_request_run_id uuid,
    latest_successful_run_id uuid,
    latest_checkpoint_key text,
    checkpoint_covered_event_seq bigint DEFAULT 0 NOT NULL,
    last_context_event_seq bigint DEFAULT 0 NOT NULL,
    context_schema_version integer DEFAULT 1 NOT NULL,
    latest_checkpoint_checksum text,
    CONSTRAINT context_heads_active_context_tokens_check CHECK ((active_context_tokens >= 0)),
    CONSTRAINT context_heads_anchor_generation_check CHECK ((anchor_generation >= 0)),
    CONSTRAINT context_heads_check CHECK ((covered_until_seq <= last_seq)),
    CONSTRAINT context_heads_check1 CHECK ((raw_tail_start_seq = (covered_until_seq + 1))),
    CONSTRAINT context_heads_check2 CHECK ((((anchor_generation = 0) AND (anchor_key IS NULL) AND (covered_until_seq = 0)) OR ((anchor_generation > 0) AND (anchor_key IS NOT NULL) AND (covered_until_seq > 0)))),
    CONSTRAINT context_heads_checkpoint_covered_event_seq_check CHECK ((checkpoint_covered_event_seq >= 0)),
    CONSTRAINT context_heads_context_schema_version_check CHECK ((context_schema_version > 0)),
    CONSTRAINT context_heads_covered_until_seq_check CHECK ((covered_until_seq >= 0)),
    CONSTRAINT context_heads_last_context_event_seq_check CHECK ((last_context_event_seq >= 0)),
    CONSTRAINT context_heads_last_seq_check CHECK ((last_seq >= 0)),
    CONSTRAINT context_heads_raw_tail_start_seq_check CHECK ((raw_tail_start_seq >= 1)),
    CONSTRAINT context_heads_version_check CHECK ((version >= 0))
);



CREATE TABLE public.conversation_events (
    id uuid DEFAULT uuidv7() NOT NULL,
    conversation_id uuid NOT NULL,
    turn_id uuid,
    turn_run_id uuid,
    event_seq bigint NOT NULL,
    event_key text NOT NULL,
    schema_version integer DEFAULT 1 NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    context_included boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT conversation_events_event_key_check CHECK ((length(btrim(event_key)) > 0)),
    CONSTRAINT conversation_events_event_seq_check CHECK ((event_seq > 0)),
    CONSTRAINT conversation_events_event_type_check CHECK ((length(btrim(event_type)) > 0)),
    CONSTRAINT conversation_events_payload_check CHECK ((jsonb_typeof(payload) = 'object'::text)),
    CONSTRAINT conversation_events_schema_version_check CHECK ((schema_version > 0))
);



CREATE TABLE public.conversation_initial_turns (
    owner_user_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    conversation_id uuid NOT NULL,
    turn_id uuid,
    prepare_fingerprint text NOT NULL,
    commit_fingerprint text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT conversation_initial_turns_idempotency_key_check CHECK (((length(btrim(idempotency_key)) >= 1) AND (length(btrim(idempotency_key)) <= 128)))
);



CREATE TABLE public.conversation_shares (
    id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    created_by_user_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    last_message_seq bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT conversation_shares_idempotency_key_check CHECK ((((octet_length(idempotency_key) >= 1) AND (octet_length(idempotency_key) <= 128)) AND (idempotency_key = btrim(idempotency_key)))),
    CONSTRAINT conversation_shares_last_message_seq_check CHECK ((last_message_seq >= 0))
);



CREATE TABLE public.conversations (
    id uuid DEFAULT uuidv7() NOT NULL,
    owner_user_id uuid NOT NULL,
    title text,
    status text DEFAULT 'active'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    deleted_at timestamp with time zone,
    CONSTRAINT conversations_lifecycle_check CHECK ((((status = 'active'::text) AND (archived_at IS NULL) AND (deleted_at IS NULL)) OR ((status = 'archived'::text) AND (archived_at IS NOT NULL) AND (deleted_at IS NULL)) OR ((status = 'deleted'::text) AND (deleted_at IS NOT NULL)))),
    CONSTRAINT conversations_metadata_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT conversations_status_check CHECK ((status = ANY (ARRAY['active'::text, 'archived'::text, 'deleted'::text])))
);



CREATE TABLE public.generated_image_assets (
    id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    turn_id uuid NOT NULL,
    turn_run_id uuid NOT NULL,
    response_id text DEFAULT ''::text NOT NULL,
    item_id text NOT NULL,
    kind text NOT NULL,
    revision integer NOT NULL,
    status text DEFAULT 'ready'::text NOT NULL,
    object_key text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    sha256 text NOT NULL,
    width integer NOT NULL,
    height integer NOT NULL,
    attachment_id uuid,
    expires_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT generated_image_assets_check CHECK ((((kind = 'partial'::text) AND (attachment_id IS NULL) AND (expires_at IS NOT NULL)) OR ((kind = 'final'::text) AND (attachment_id IS NOT NULL) AND (expires_at IS NULL)))),
    CONSTRAINT generated_image_assets_content_type_check CHECK ((content_type ~~ 'image/%'::text)),
    CONSTRAINT generated_image_assets_height_check CHECK ((height > 0)),
    CONSTRAINT generated_image_assets_item_id_check CHECK ((length(btrim(item_id)) > 0)),
    CONSTRAINT generated_image_assets_kind_check CHECK ((kind = ANY (ARRAY['partial'::text, 'final'::text]))),
    CONSTRAINT generated_image_assets_revision_check CHECK (((revision >= 0) AND (revision <= 3))),
    CONSTRAINT generated_image_assets_sha256_check CHECK ((sha256 ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT generated_image_assets_size_bytes_check CHECK ((size_bytes > 0)),
    CONSTRAINT generated_image_assets_status_check CHECK ((status = ANY (ARRAY['ready'::text, 'deleting'::text]))),
    CONSTRAINT generated_image_assets_width_check CHECK ((width > 0))
);



CREATE TABLE public.messages (
    id uuid DEFAULT uuidv7() NOT NULL,
    conversation_id uuid NOT NULL,
    turn_id uuid,
    seq bigint NOT NULL,
    role text NOT NULL,
    content_text text,
    token_count integer,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    context_excluded boolean DEFAULT false NOT NULL,
    CONSTRAINT messages_metadata_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT messages_role_check CHECK ((role = ANY (ARRAY['system'::text, 'developer'::text, 'user'::text, 'assistant'::text, 'tool'::text]))),
    CONSTRAINT messages_seq_check CHECK ((seq > 0)),
    CONSTRAINT messages_token_count_check CHECK (((token_count IS NULL) OR (token_count >= 0)))
);



CREATE TABLE public.model_price_versions (
    id uuid DEFAULT uuidv7() NOT NULL,
    model_id uuid NOT NULL,
    version bigint NOT NULL,
    currency text NOT NULL,
    input_per_million_nanos bigint DEFAULT 0 NOT NULL,
    cache_read_input_per_million_nanos bigint DEFAULT 0 CONSTRAINT model_price_versions_cache_read_input_per_million_nano_not_null NOT NULL,
    cache_creation_input_per_million_nanos bigint DEFAULT 0 CONSTRAINT model_price_versions_cache_creation_input_per_million__not_null NOT NULL,
    output_per_million_nanos bigint DEFAULT 0 NOT NULL,
    image_input_per_million_nanos bigint,
    image_output_per_image_nanos bigint,
    status text DEFAULT 'draft'::text NOT NULL,
    effective_from timestamp with time zone,
    pricing_snapshot jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by_user_id uuid NOT NULL,
    published_by_user_id uuid,
    published_at timestamp with time zone,
    archived_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT model_price_versions_cache_creation_input_per_million_nan_check CHECK ((cache_creation_input_per_million_nanos >= 0)),
    CONSTRAINT model_price_versions_cache_read_input_per_million_nanos_check CHECK ((cache_read_input_per_million_nanos >= 0)),
    CONSTRAINT model_price_versions_check CHECK ((((status = 'draft'::text) AND (effective_from IS NULL) AND (published_by_user_id IS NULL) AND (published_at IS NULL) AND (archived_at IS NULL)) OR ((status = 'published'::text) AND (effective_from IS NOT NULL) AND (published_by_user_id IS NOT NULL) AND (published_at IS NOT NULL) AND (archived_at IS NULL)) OR ((status = 'archived'::text) AND (effective_from IS NOT NULL) AND (published_by_user_id IS NOT NULL) AND (published_at IS NOT NULL) AND (archived_at IS NOT NULL) AND (archived_at >= published_at)))),
    CONSTRAINT model_price_versions_currency_check CHECK ((currency ~ '^[A-Z]{3}$'::text)),
    CONSTRAINT model_price_versions_image_input_per_million_nanos_check CHECK (((image_input_per_million_nanos IS NULL) OR (image_input_per_million_nanos >= 0))),
    CONSTRAINT model_price_versions_image_output_per_image_nanos_check CHECK (((image_output_per_image_nanos IS NULL) OR (image_output_per_image_nanos >= 0))),
    CONSTRAINT model_price_versions_input_per_million_nanos_check CHECK ((input_per_million_nanos >= 0)),
    CONSTRAINT model_price_versions_output_per_million_nanos_check CHECK ((output_per_million_nanos >= 0)),
    CONSTRAINT model_price_versions_pricing_snapshot_check CHECK ((jsonb_typeof(pricing_snapshot) = 'object'::text)),
    CONSTRAINT model_price_versions_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'published'::text, 'archived'::text]))),
    CONSTRAINT model_price_versions_version_check CHECK ((version > 0))
);



CREATE TABLE public.model_settings (
    singleton boolean DEFAULT true NOT NULL,
    default_chat_model_id uuid,
    compaction_model_id uuid,
    updated_by_user_id uuid,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT model_settings_singleton_check CHECK (singleton)
);



CREATE TABLE public.models (
    id uuid DEFAULT uuidv7() NOT NULL,
    provider text NOT NULL,
    credential_id uuid NOT NULL,
    slug text NOT NULL,
    upstream_model text NOT NULL,
    display_name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    input_modalities text[] DEFAULT ARRAY['text'::text] NOT NULL,
    output_modalities text[] DEFAULT ARRAY['text'::text] NOT NULL,
    supports_tools boolean DEFAULT true NOT NULL,
    supports_parallel_tools boolean DEFAULT true NOT NULL,
    supported_reasoning_efforts text[] DEFAULT ARRAY[]::text[] NOT NULL,
    context_window_tokens integer NOT NULL,
    max_output_tokens integer NOT NULL,
    default_parameters jsonb DEFAULT '{}'::jsonb NOT NULL,
    status text DEFAULT 'enabled'::text NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    created_by_user_id uuid NOT NULL,
    updated_by_user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT models_check CHECK (((max_output_tokens > 0) AND (max_output_tokens <= context_window_tokens))),
    CONSTRAINT models_context_window_tokens_check CHECK ((context_window_tokens > 0)),
    CONSTRAINT models_default_parameters_check CHECK ((jsonb_typeof(default_parameters) = 'object'::text)),
    CONSTRAINT models_default_reasoning_effort_supported CHECK (((NOT (default_parameters ? 'reasoning_effort'::text)) OR ((jsonb_typeof((default_parameters -> 'reasoning_effort'::text)) = 'string'::text) AND ((default_parameters ->> 'reasoning_effort'::text) = ANY (supported_reasoning_efforts))))),
    CONSTRAINT models_display_name_check CHECK (((length(btrim(display_name)) >= 1) AND (length(btrim(display_name)) <= 120))),
    CONSTRAINT models_provider_check CHECK ((provider = 'openai'::text)),
    CONSTRAINT models_revision_check CHECK ((revision > 0)),
    CONSTRAINT models_slug_check CHECK ((slug ~ '^[a-z0-9][a-z0-9._-]{0,99}$'::text)),
    CONSTRAINT models_status_check CHECK ((status = ANY (ARRAY['enabled'::text, 'disabled'::text]))),
    CONSTRAINT models_supported_reasoning_efforts_valid CHECK ((supported_reasoning_efforts <@ ARRAY['low'::text, 'medium'::text, 'high'::text, 'xhigh'::text])),
    CONSTRAINT models_upstream_model_check CHECK ((length(btrim(upstream_model)) > 0))
);



CREATE TABLE public.outbox_events (
    id uuid DEFAULT uuidv7() NOT NULL,
    event_type text NOT NULL,
    conversation_id uuid,
    turn_id uuid,
    turn_run_id uuid,
    published_at timestamp with time zone,
    claim_token uuid,
    claimed_at timestamp with time zone,
    error_message text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);



CREATE TABLE public.provider_credentials (
    id uuid DEFAULT uuidv7() NOT NULL,
    provider text NOT NULL,
    name text NOT NULL,
    base_url text NOT NULL,
    encrypted_api_key bytea NOT NULL,
    nonce bytea NOT NULL,
    key_version integer DEFAULT 1 NOT NULL,
    key_hint text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'enabled'::text NOT NULL,
    last_validated_at timestamp with time zone,
    last_validation_error text,
    created_by_user_id uuid NOT NULL,
    updated_by_user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT provider_credentials_base_url_check CHECK ((length(btrim(base_url)) > 0)),
    CONSTRAINT provider_credentials_key_version_check CHECK ((key_version > 0)),
    CONSTRAINT provider_credentials_name_check CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 120))),
    CONSTRAINT provider_credentials_nonce_check CHECK ((octet_length(nonce) = 12)),
    CONSTRAINT provider_credentials_provider_check CHECK ((provider = 'openai'::text)),
    CONSTRAINT provider_credentials_status_check CHECK ((status = ANY (ARRAY['enabled'::text, 'disabled'::text, 'revoked'::text])))
);



CREATE TABLE public.sandboxes (
    id uuid DEFAULT uuidv7() NOT NULL,
    conversation_id uuid NOT NULL,
    provider text NOT NULL,
    runtime_id text NOT NULL,
    status text NOT NULL,
    runtime_metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    destroyed_at timestamp with time zone,
    last_activity_at timestamp with time zone DEFAULT now() NOT NULL,
    stopped_at timestamp with time zone,
    execution_token uuid,
    execution_lease_until timestamp with time zone,
    release_previous_status text,
    release_token uuid,
    release_lease_until timestamp with time zone,
    CONSTRAINT sandboxes_execution_lease_check CHECK (((execution_token IS NULL) = (execution_lease_until IS NULL))),
    CONSTRAINT sandboxes_lifecycle_check CHECK ((((status = 'active'::text) AND (stopped_at IS NULL) AND (destroyed_at IS NULL) AND (release_previous_status IS NULL) AND (release_token IS NULL)) OR ((status = 'stopped'::text) AND (stopped_at IS NOT NULL) AND (destroyed_at IS NULL) AND (release_previous_status IS NULL) AND (execution_token IS NULL) AND (release_token IS NULL)) OR ((status = 'releasing'::text) AND (destroyed_at IS NULL) AND (execution_token IS NULL) AND (release_previous_status = ANY (ARRAY['active'::text, 'stopped'::text])) AND (((release_previous_status = 'active'::text) AND (stopped_at IS NULL)) OR ((release_previous_status = 'stopped'::text) AND (stopped_at IS NOT NULL)))) OR ((status = 'destroyed'::text) AND (destroyed_at IS NOT NULL) AND (destroyed_at >= created_at) AND (release_previous_status IS NULL) AND (execution_token IS NULL) AND (release_token IS NULL)))),
    CONSTRAINT sandboxes_provider_check CHECK ((provider = ANY (ARRAY['firecracker'::text, 'cubesandbox'::text]))),
    CONSTRAINT sandboxes_release_lease_check CHECK (((release_token IS NULL) = (release_lease_until IS NULL))),
    CONSTRAINT sandboxes_runtime_metadata_check CHECK ((jsonb_typeof(runtime_metadata) = 'object'::text)),
    CONSTRAINT sandboxes_status_check CHECK ((status = ANY (ARRAY['active'::text, 'stopped'::text, 'releasing'::text, 'destroyed'::text])))
);



CREATE TABLE public.smtp_settings (
    singleton boolean DEFAULT true NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    host text DEFAULT ''::text NOT NULL,
    port integer DEFAULT 587 NOT NULL,
    security text DEFAULT 'starttls'::text NOT NULL,
    username text DEFAULT ''::text NOT NULL,
    encrypted_password bytea,
    password_nonce bytea,
    key_version integer DEFAULT 1 NOT NULL,
    from_email text DEFAULT ''::text NOT NULL,
    from_name text DEFAULT ''::text NOT NULL,
    updated_by_user_id uuid,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT smtp_settings_check CHECK ((((encrypted_password IS NULL) AND (password_nonce IS NULL)) OR ((encrypted_password IS NOT NULL) AND (password_nonce IS NOT NULL) AND (octet_length(password_nonce) = 12)))),
    CONSTRAINT smtp_settings_check1 CHECK (((NOT enabled) OR ((length(host) > 0) AND (length(btrim(from_email)) > 0)))),
    CONSTRAINT smtp_settings_from_email_check CHECK (((POSITION((chr(10)) IN (from_email)) = 0) AND (POSITION((chr(13)) IN (from_email)) = 0))),
    CONSTRAINT smtp_settings_from_name_check CHECK (((POSITION((chr(10)) IN (from_name)) = 0) AND (POSITION((chr(13)) IN (from_name)) = 0))),
    CONSTRAINT smtp_settings_host_check CHECK (((host = btrim(host)) AND (POSITION((chr(10)) IN (host)) = 0) AND (POSITION((chr(13)) IN (host)) = 0))),
    CONSTRAINT smtp_settings_key_version_check CHECK ((key_version > 0)),
    CONSTRAINT smtp_settings_port_check CHECK (((port >= 1) AND (port <= 65535))),
    CONSTRAINT smtp_settings_security_check CHECK ((security = ANY (ARRAY['starttls'::text, 'tls'::text, 'none'::text]))),
    CONSTRAINT smtp_settings_singleton_check CHECK (singleton),
    CONSTRAINT smtp_settings_username_check CHECK (((POSITION((chr(10)) IN (username)) = 0) AND (POSITION((chr(13)) IN (username)) = 0)))
);



CREATE TABLE public.tool_calls (
    id uuid DEFAULT uuidv7() NOT NULL,
    turn_id uuid NOT NULL,
    turn_run_id uuid NOT NULL,
    call_id text NOT NULL,
    tool_type text NOT NULL,
    namespace text,
    tool_name text NOT NULL,
    status text NOT NULL,
    execution_attempt integer NOT NULL,
    arguments_blob_key text NOT NULL,
    output_blob_key text,
    error_message text,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    failed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    answer_idempotency_key text,
    answer_fingerprint text,
    answer_option_id text,
    answer_output_pending boolean DEFAULT false NOT NULL,
    cancelled_at timestamp with time zone,
    CONSTRAINT tool_calls_answer_declaration_check CHECK ((((answer_idempotency_key IS NULL) AND (answer_fingerprint IS NULL) AND (answer_option_id IS NULL) AND (answer_output_pending = false)) OR ((answer_idempotency_key IS NOT NULL) AND (answer_fingerprint IS NOT NULL) AND (answer_option_id IS NOT NULL) AND (output_blob_key IS NOT NULL)))),
    CONSTRAINT tool_calls_answer_fingerprint_check CHECK (((answer_fingerprint IS NULL) OR (answer_fingerprint ~ '^[0-9a-f]{64}$'::text))),
    CONSTRAINT tool_calls_answer_idempotency_key_check CHECK (((answer_idempotency_key IS NULL) OR ((length(btrim(answer_idempotency_key)) >= 1) AND (length(btrim(answer_idempotency_key)) <= 128)))),
    CONSTRAINT tool_calls_answer_option_id_check CHECK (((answer_option_id IS NULL) OR (answer_option_id ~ '^[A-Za-z0-9_-]{1,64}$'::text))),
    CONSTRAINT tool_calls_check CHECK ((NOT ((completed_at IS NOT NULL) AND (failed_at IS NOT NULL)))),
    CONSTRAINT tool_calls_execution_attempt_check CHECK ((execution_attempt > 0)),
    CONSTRAINT tool_calls_status_check CHECK ((status = ANY (ARRAY['running'::text, 'awaiting_input'::text, 'completed'::text, 'failed'::text, 'cancelled'::text])))
);



CREATE TABLE public.turn_runs (
    id uuid DEFAULT uuidv7() NOT NULL,
    turn_id uuid NOT NULL,
    step_index integer NOT NULL,
    provider text NOT NULL,
    model text NOT NULL,
    status text NOT NULL,
    request_blob_key text NOT NULL,
    response_blob_key text,
    response_id text,
    input_tokens integer DEFAULT 0 NOT NULL,
    cache_read_input_tokens integer DEFAULT 0 NOT NULL,
    cache_creation_input_tokens integer DEFAULT 0 NOT NULL,
    output_tokens integer DEFAULT 0 NOT NULL,
    reasoning_output_tokens integer DEFAULT 0 NOT NULL,
    total_tokens integer DEFAULT 0 NOT NULL,
    billing_currency text,
    billing_amount_nanos bigint,
    error_message text,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    failed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    attempt integer DEFAULT 1 NOT NULL,
    state_blob_key text DEFAULT ''::text NOT NULL,
    result_blob_key text,
    lease_token uuid,
    heartbeat_at timestamp with time zone,
    checkpoint_blob_key text,
    cancelled_at timestamp with time zone,
    failure_blob_key text,
    artifact_metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    billing_settled_at timestamp with time zone,
    CONSTRAINT turn_runs_artifact_metadata_check CHECK ((jsonb_typeof(artifact_metadata) = 'object'::text)),
    CONSTRAINT turn_runs_attempt_check CHECK ((attempt >= 0)),
    CONSTRAINT turn_runs_billing_amount_nanos_check CHECK (((billing_amount_nanos IS NULL) OR (billing_amount_nanos >= 0))),
    CONSTRAINT turn_runs_billing_currency_check CHECK (((billing_currency IS NULL) OR (billing_currency ~ '^[A-Z]{3}$'::text))),
    CONSTRAINT turn_runs_cache_creation_input_tokens_check CHECK ((cache_creation_input_tokens >= 0)),
    CONSTRAINT turn_runs_cache_read_input_tokens_check CHECK ((cache_read_input_tokens >= 0)),
    CONSTRAINT turn_runs_check CHECK ((NOT ((completed_at IS NOT NULL) AND (failed_at IS NOT NULL)))),
    CONSTRAINT turn_runs_input_tokens_check CHECK ((input_tokens >= 0)),
    CONSTRAINT turn_runs_model_check CHECK ((length(btrim(model)) > 0)),
    CONSTRAINT turn_runs_output_tokens_check CHECK ((output_tokens >= 0)),
    CONSTRAINT turn_runs_provider_check CHECK ((provider = 'openai.responses'::text)),
    CONSTRAINT turn_runs_reasoning_output_tokens_check CHECK ((reasoning_output_tokens >= 0)),
    CONSTRAINT turn_runs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'awaiting_input'::text, 'cancel_requested'::text, 'completed'::text, 'failed'::text, 'cancelled'::text]))),
    CONSTRAINT turn_runs_step_index_check CHECK ((step_index > 0)),
    CONSTRAINT turn_runs_total_tokens_check CHECK ((total_tokens >= 0))
);



CREATE TABLE public.turns (
    id uuid DEFAULT uuidv7() NOT NULL,
    conversation_id uuid NOT NULL,
    seq bigint NOT NULL,
    status text NOT NULL,
    request_blob_key text,
    response_blob_key text,
    openai_response_id text,
    error_code text,
    error_message text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    model_id uuid NOT NULL,
    model_revision bigint NOT NULL,
    model_price_id uuid NOT NULL,
    model_snapshot jsonb DEFAULT '{}'::jsonb NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    failed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    retry_of_turn_id uuid,
    variant_index integer DEFAULT 1 NOT NULL,
    cancel_requested_at timestamp with time zone,
    cancelled_at timestamp with time zone,
    CONSTRAINT turns_check CHECK ((NOT ((completed_at IS NOT NULL) AND (failed_at IS NOT NULL)))),
    CONSTRAINT turns_metadata_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT turns_model_revision_check CHECK ((model_revision > 0)),
    CONSTRAINT turns_model_snapshot_check CHECK ((jsonb_typeof(model_snapshot) = 'object'::text)),
    CONSTRAINT turns_seq_check CHECK ((seq > 0)),
    CONSTRAINT turns_status_check CHECK ((status = ANY (ARRAY['accepted'::text, 'context_ready'::text, 'processing'::text, 'awaiting_input'::text, 'cancel_requested'::text, 'completed'::text, 'failed'::text, 'cancelled'::text]))),
    CONSTRAINT turns_variant_index_check CHECK ((variant_index > 0)),
    CONSTRAINT turns_variant_shape CHECK ((((retry_of_turn_id IS NULL) AND (variant_index = 1)) OR ((retry_of_turn_id IS NOT NULL) AND (variant_index > 1))))
);



CREATE TABLE public.user_locations (
    user_id uuid NOT NULL,
    latitude double precision NOT NULL,
    longitude double precision NOT NULL,
    coordinate_system text DEFAULT 'gcj02'::text NOT NULL,
    formatted_address text DEFAULT ''::text NOT NULL,
    province text DEFAULT ''::text NOT NULL,
    city text DEFAULT ''::text NOT NULL,
    district text DEFAULT ''::text NOT NULL,
    adcode text DEFAULT ''::text NOT NULL,
    poi_id text DEFAULT ''::text NOT NULL,
    poi_name text DEFAULT ''::text NOT NULL,
    source text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_locations_adcode_check CHECK (((adcode = ''::text) OR (adcode ~ '^[0-9]{6}$'::text))),
    CONSTRAINT user_locations_city_check CHECK ((length(city) <= 100)),
    CONSTRAINT user_locations_coordinate_system_check CHECK ((coordinate_system = 'gcj02'::text)),
    CONSTRAINT user_locations_district_check CHECK ((length(district) <= 100)),
    CONSTRAINT user_locations_formatted_address_check CHECK ((length(formatted_address) <= 500)),
    CONSTRAINT user_locations_latitude_check CHECK (((latitude <> 'NaN'::double precision) AND (latitude >= ('-90'::integer)::double precision) AND (latitude <= (90)::double precision))),
    CONSTRAINT user_locations_longitude_check CHECK (((longitude <> 'NaN'::double precision) AND (longitude >= ('-180'::integer)::double precision) AND (longitude <= (180)::double precision))),
    CONSTRAINT user_locations_poi_id_check CHECK ((length(poi_id) <= 128)),
    CONSTRAINT user_locations_poi_name_check CHECK ((length(poi_name) <= 200)),
    CONSTRAINT user_locations_province_check CHECK ((length(province) <= 100)),
    CONSTRAINT user_locations_source_check CHECK ((source = ANY (ARRAY['map'::text, 'search'::text, 'geolocation'::text])))
);



CREATE TABLE public.user_mcp_servers (
    id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    endpoint_url text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    encrypted_parameters bytea NOT NULL,
    parameters_nonce bytea NOT NULL,
    encrypted_headers bytea NOT NULL,
    headers_nonce bytea NOT NULL,
    last_validation_status text DEFAULT 'untested'::text NOT NULL,
    last_validation_error text,
    last_validated_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_mcp_servers_check CHECK ((((last_validation_status = 'untested'::text) AND (last_validated_at IS NULL) AND (last_validation_error IS NULL)) OR ((last_validation_status = 'valid'::text) AND (last_validated_at IS NOT NULL) AND (last_validation_error IS NULL)) OR ((last_validation_status = 'invalid'::text) AND (last_validated_at IS NOT NULL) AND (last_validation_error IS NOT NULL)))),
    CONSTRAINT user_mcp_servers_encrypted_headers_check CHECK ((octet_length(encrypted_headers) >= 16)),
    CONSTRAINT user_mcp_servers_encrypted_parameters_check CHECK ((octet_length(encrypted_parameters) >= 16)),
    CONSTRAINT user_mcp_servers_endpoint_url_check CHECK (((endpoint_url = btrim(endpoint_url)) AND ((length(endpoint_url) >= 1) AND (length(endpoint_url) <= 2048)) AND (endpoint_url ~ '^https?://[^/?#@]+(/[^?#]*)?$'::text))),
    CONSTRAINT user_mcp_servers_headers_nonce_check CHECK ((octet_length(headers_nonce) = 12)),
    CONSTRAINT user_mcp_servers_last_validation_error_check CHECK (((last_validation_error IS NULL) OR ((length(last_validation_error) >= 1) AND (length(last_validation_error) <= 500)))),
    CONSTRAINT user_mcp_servers_last_validation_status_check CHECK ((last_validation_status = ANY (ARRAY['untested'::text, 'valid'::text, 'invalid'::text]))),
    CONSTRAINT user_mcp_servers_name_check CHECK (((name = btrim(name)) AND ((length(name) >= 1) AND (length(name) <= 100)))),
    CONSTRAINT user_mcp_servers_parameters_nonce_check CHECK ((octet_length(parameters_nonce) = 12)),
    CONSTRAINT user_mcp_servers_revision_check CHECK ((revision > 0)),
    CONSTRAINT user_mcp_servers_slug_check CHECK ((((length(slug) >= 1) AND (length(slug) <= 64)) AND (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'::text)))
);



CREATE TABLE public.user_mcp_tools (
    server_id uuid NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    input_schema jsonb DEFAULT '{}'::jsonb NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_mcp_tools_description_check CHECK ((length(description) <= 4000)),
    CONSTRAINT user_mcp_tools_input_schema_check CHECK ((jsonb_typeof(input_schema) = 'object'::text)),
    CONSTRAINT user_mcp_tools_name_check CHECK (((name = btrim(name)) AND ((length(name) >= 1) AND (length(name) <= 255))))
);



CREATE TABLE public.user_preferences (
    user_id uuid NOT NULL,
    preferences_text text DEFAULT ''::text NOT NULL,
    location_enabled_for_model boolean DEFAULT false NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_preferences_preferences_text_check CHECK ((length(preferences_text) <= 8000)),
    CONSTRAINT user_preferences_version_check CHECK ((version > 0))
);



CREATE TABLE public.users (
    id uuid DEFAULT uuidv7() NOT NULL,
    email text NOT NULL,
    username text NOT NULL,
    password_hash text NOT NULL,
    role text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    last_login_at timestamp with time zone,
    email_verified_at timestamp with time zone,
    auth_version bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    storage_quota_bytes bigint DEFAULT 536870912 NOT NULL,
    storage_used_bytes bigint DEFAULT 0 NOT NULL,
    deleted_at timestamp with time zone,
    sandbox_quota integer DEFAULT 3 NOT NULL,
    CONSTRAINT users_auth_version_check CHECK ((auth_version > 0)),
    CONSTRAINT users_role_check CHECK ((role = ANY (ARRAY['system'::text, 'admin'::text, 'user'::text]))),
    CONSTRAINT users_sandbox_quota_check CHECK ((sandbox_quota >= 0)),
    CONSTRAINT users_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text]))),
    CONSTRAINT users_storage_quota_bytes_check CHECK ((storage_quota_bytes >= 0)),
    CONSTRAINT users_storage_used_bytes_check CHECK ((storage_used_bytes >= 0))
);



ALTER TABLE ONLY public.account_action_tokens
    ADD CONSTRAINT account_action_tokens_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.account_action_tokens
    ADD CONSTRAINT account_action_tokens_token_hash_key UNIQUE (token_hash);



ALTER TABLE ONLY public.attachments
    ADD CONSTRAINT attachments_conversation_id_uploaded_by_user_id_idempotency_key UNIQUE (conversation_id, uploaded_by_user_id, idempotency_key);



ALTER TABLE ONLY public.attachments
    ADD CONSTRAINT attachments_object_key_key UNIQUE (object_key);



ALTER TABLE ONLY public.attachments
    ADD CONSTRAINT attachments_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.audit_events
    ADD CONSTRAINT audit_events_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.billing_accounts
    ADD CONSTRAINT billing_accounts_id_user_id_currency_key UNIQUE (id, user_id, currency);



ALTER TABLE ONLY public.billing_accounts
    ADD CONSTRAINT billing_accounts_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.billing_accounts
    ADD CONSTRAINT billing_accounts_user_id_currency_key UNIQUE (user_id, currency);



ALTER TABLE ONLY public.billing_redemption_code_disables
    ADD CONSTRAINT billing_redemption_code_disables_pkey PRIMARY KEY (redemption_code_id);



ALTER TABLE ONLY public.billing_redemption_codes
    ADD CONSTRAINT billing_redemption_codes_code_hash_key UNIQUE (code_hash);



ALTER TABLE ONLY public.billing_redemption_codes
    ADD CONSTRAINT billing_redemption_codes_id_currency_amount_nanos_key UNIQUE (id, currency, amount_nanos);



ALTER TABLE ONLY public.billing_redemption_codes
    ADD CONSTRAINT billing_redemption_codes_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.billing_tool_prices
    ADD CONSTRAINT billing_tool_prices_pkey PRIMARY KEY (tool_key, currency);



ALTER TABLE ONLY public.billing_transactions
    ADD CONSTRAINT billing_transactions_account_id_account_sequence_key UNIQUE (account_id, account_sequence);



ALTER TABLE ONLY public.billing_transactions
    ADD CONSTRAINT billing_transactions_actor_user_id_idempotency_key_key UNIQUE (actor_user_id, idempotency_key);



ALTER TABLE ONLY public.billing_transactions
    ADD CONSTRAINT billing_transactions_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.billing_usage_events
    ADD CONSTRAINT billing_usage_events_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.billing_usage_events
    ADD CONSTRAINT billing_usage_events_request_key_key UNIQUE (request_key);



ALTER TABLE ONLY public.context_heads
    ADD CONSTRAINT context_heads_pkey PRIMARY KEY (conversation_id);



ALTER TABLE ONLY public.conversation_events
    ADD CONSTRAINT conversation_events_conversation_id_event_key_key UNIQUE (conversation_id, event_key);



ALTER TABLE ONLY public.conversation_events
    ADD CONSTRAINT conversation_events_conversation_id_event_seq_key UNIQUE (conversation_id, event_seq);



ALTER TABLE ONLY public.conversation_events
    ADD CONSTRAINT conversation_events_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.conversation_initial_turns
    ADD CONSTRAINT conversation_initial_turns_conversation_id_key UNIQUE (conversation_id);



ALTER TABLE ONLY public.conversation_initial_turns
    ADD CONSTRAINT conversation_initial_turns_pkey PRIMARY KEY (owner_user_id, idempotency_key);



ALTER TABLE ONLY public.conversation_shares
    ADD CONSTRAINT conversation_shares_conversation_id_created_by_user_id_idem_key UNIQUE (conversation_id, created_by_user_id, idempotency_key);



ALTER TABLE ONLY public.conversation_shares
    ADD CONSTRAINT conversation_shares_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.conversations
    ADD CONSTRAINT conversations_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.generated_image_assets
    ADD CONSTRAINT generated_image_assets_object_key_key UNIQUE (object_key);



ALTER TABLE ONLY public.generated_image_assets
    ADD CONSTRAINT generated_image_assets_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.generated_image_assets
    ADD CONSTRAINT generated_image_assets_turn_run_id_item_id_kind_revision_key UNIQUE (turn_run_id, item_id, kind, revision);



ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_conversation_id_seq_key UNIQUE (conversation_id, seq);



ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.model_price_versions
    ADD CONSTRAINT model_price_versions_model_id_id_key UNIQUE (model_id, id);



ALTER TABLE ONLY public.model_price_versions
    ADD CONSTRAINT model_price_versions_model_id_version_key UNIQUE (model_id, version);



ALTER TABLE ONLY public.model_price_versions
    ADD CONSTRAINT model_price_versions_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.model_settings
    ADD CONSTRAINT model_settings_pkey PRIMARY KEY (singleton);



ALTER TABLE ONLY public.models
    ADD CONSTRAINT models_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.models
    ADD CONSTRAINT models_provider_upstream_model_credential_id_key UNIQUE (provider, upstream_model, credential_id);



ALTER TABLE ONLY public.models
    ADD CONSTRAINT models_slug_key UNIQUE (slug);



ALTER TABLE ONLY public.outbox_events
    ADD CONSTRAINT outbox_events_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.provider_credentials
    ADD CONSTRAINT provider_credentials_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.provider_credentials
    ADD CONSTRAINT provider_credentials_provider_name_key UNIQUE (provider, name);



ALTER TABLE ONLY public.sandboxes
    ADD CONSTRAINT sandboxes_conversation_id_runtime_id_key UNIQUE (conversation_id, runtime_id);



ALTER TABLE ONLY public.sandboxes
    ADD CONSTRAINT sandboxes_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.smtp_settings
    ADD CONSTRAINT smtp_settings_pkey PRIMARY KEY (singleton);



ALTER TABLE ONLY public.tool_calls
    ADD CONSTRAINT tool_calls_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.tool_calls
    ADD CONSTRAINT tool_calls_turn_run_id_call_id_key UNIQUE (turn_run_id, call_id);



ALTER TABLE ONLY public.turn_runs
    ADD CONSTRAINT turn_runs_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.turn_runs
    ADD CONSTRAINT turn_runs_turn_id_id_key UNIQUE (turn_id, id);



ALTER TABLE ONLY public.turn_runs
    ADD CONSTRAINT turn_runs_turn_id_step_index_attempt_key UNIQUE (turn_id, step_index, attempt);



ALTER TABLE ONLY public.turns
    ADD CONSTRAINT turns_conversation_id_id_key UNIQUE (conversation_id, id);



ALTER TABLE ONLY public.turns
    ADD CONSTRAINT turns_conversation_id_seq_key UNIQUE (conversation_id, seq);



ALTER TABLE ONLY public.turns
    ADD CONSTRAINT turns_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.user_locations
    ADD CONSTRAINT user_locations_pkey PRIMARY KEY (user_id);



ALTER TABLE ONLY public.user_mcp_servers
    ADD CONSTRAINT user_mcp_servers_owner_user_id_slug_key UNIQUE (owner_user_id, slug);



ALTER TABLE ONLY public.user_mcp_servers
    ADD CONSTRAINT user_mcp_servers_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.user_mcp_tools
    ADD CONSTRAINT user_mcp_tools_pkey PRIMARY KEY (server_id, name);



ALTER TABLE ONLY public.user_preferences
    ADD CONSTRAINT user_preferences_pkey PRIMARY KEY (user_id);



ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);



CREATE INDEX attachments_conversation_id_created_at_idx ON public.attachments USING btree (conversation_id, created_at DESC);



CREATE UNIQUE INDEX billing_transactions_redemption_code_id_unique ON public.billing_transactions USING btree (redemption_code_id) WHERE (redemption_code_id IS NOT NULL);



CREATE INDEX conversation_events_context_tail_idx ON public.conversation_events USING btree (conversation_id, event_seq) WHERE (context_included = true);



CREATE INDEX conversation_events_conversation_seq_idx ON public.conversation_events USING btree (conversation_id, event_seq DESC);



CREATE INDEX conversation_events_turn_seq_idx ON public.conversation_events USING btree (turn_id, event_seq);



CREATE INDEX generated_image_assets_cleanup_idx ON public.generated_image_assets USING btree (expires_at) WHERE (kind = 'partial'::text);



CREATE INDEX generated_image_assets_turn_created_idx ON public.generated_image_assets USING btree (turn_id, created_at, revision);



CREATE UNIQUE INDEX idx_account_action_tokens_active ON public.account_action_tokens USING btree (user_id, purpose) WHERE (used_at IS NULL);



CREATE INDEX idx_account_action_tokens_cooldown ON public.account_action_tokens USING btree (user_id, purpose, sent_at DESC) WHERE (sent_at IS NOT NULL);



CREATE INDEX idx_attachments_upload_cleanup_updated_at ON public.attachments USING btree (updated_at) WHERE (status = ANY (ARRAY['pending'::text, 'deleting'::text]));



CREATE INDEX idx_audit_actor_cursor ON public.audit_events USING btree (actor_user_id, created_at DESC, id DESC);



CREATE INDEX idx_audit_admin_cursor ON public.audit_events USING btree (created_at DESC, id DESC);



CREATE INDEX idx_audit_events_required_role_cursor ON public.audit_events USING btree (required_role, created_at DESC, id DESC);



CREATE INDEX idx_audit_subject_cursor ON public.audit_events USING btree (subject_user_id, created_at DESC, id DESC) WHERE visible_to_subject;



CREATE INDEX idx_billing_accounts_cursor ON public.billing_accounts USING btree (created_at DESC, id DESC);



CREATE INDEX idx_billing_redemption_codes_cursor ON public.billing_redemption_codes USING btree (created_at DESC, id DESC);



CREATE INDEX idx_billing_transactions_admin_cursor ON public.billing_transactions USING btree (created_at DESC, id DESC);



CREATE INDEX idx_billing_transactions_user_cursor ON public.billing_transactions USING btree (user_id, created_at DESC, id DESC);



CREATE INDEX idx_billing_usage_admin_cursor ON public.billing_usage_events USING btree (created_at DESC, id DESC);



CREATE UNIQUE INDEX idx_billing_usage_turn_run ON public.billing_usage_events USING btree (turn_run_id) WHERE (turn_run_id IS NOT NULL);



CREATE INDEX idx_billing_usage_user_cursor ON public.billing_usage_events USING btree (owner_user_id, created_at DESC, id DESC);



CREATE INDEX idx_conversation_shares_conversation_created_at ON public.conversation_shares USING btree (conversation_id, created_at DESC, id DESC);



CREATE INDEX idx_conversations_owner_created_at ON public.conversations USING btree (owner_user_id, created_at DESC);



CREATE INDEX idx_messages_turn_id ON public.messages USING btree (turn_id);



CREATE INDEX idx_model_price_versions_cursor ON public.model_price_versions USING btree (model_id, created_at DESC, id DESC);



CREATE INDEX idx_model_prices_effective ON public.model_price_versions USING btree (model_id, effective_from DESC, version DESC) WHERE (status = 'published'::text);



CREATE INDEX idx_models_admin_cursor ON public.models USING btree (created_at DESC, id DESC);



CREATE INDEX idx_models_credential ON public.models USING btree (credential_id);



CREATE INDEX idx_models_status_cursor ON public.models USING btree (status, created_at DESC, id DESC);



CREATE INDEX idx_outbox_events_claim_expiry ON public.outbox_events USING btree (claimed_at) WHERE ((published_at IS NULL) AND (claim_token IS NOT NULL));



CREATE INDEX idx_outbox_events_pending ON public.outbox_events USING btree (created_at) WHERE (published_at IS NULL);



CREATE INDEX idx_provider_credentials_cursor ON public.provider_credentials USING btree (created_at DESC, id DESC);



CREATE UNIQUE INDEX idx_sandboxes_conversation_usable ON public.sandboxes USING btree (conversation_id) WHERE ((status = ANY (ARRAY['active'::text, 'stopped'::text, 'releasing'::text])) AND (destroyed_at IS NULL));



CREATE INDEX idx_sandboxes_idle_active ON public.sandboxes USING btree (last_activity_at, id) WHERE ((status = 'active'::text) AND (destroyed_at IS NULL));



CREATE INDEX idx_sandboxes_releasing ON public.sandboxes USING btree (updated_at, id) WHERE ((status = 'releasing'::text) AND (destroyed_at IS NULL));



CREATE INDEX idx_sandboxes_stopped_retention ON public.sandboxes USING btree (stopped_at, id) WHERE ((status = 'stopped'::text) AND (destroyed_at IS NULL));



CREATE INDEX idx_tool_calls_turn ON public.tool_calls USING btree (turn_id, created_at);



CREATE INDEX idx_turn_runs_active_lease ON public.turn_runs USING btree (heartbeat_at) WHERE ((status = 'running'::text) AND (lease_token IS NOT NULL));



CREATE INDEX idx_turns_retry_source ON public.turns USING btree (retry_of_turn_id) WHERE (retry_of_turn_id IS NOT NULL);



CREATE UNIQUE INDEX idx_turns_retry_variant ON public.turns USING btree (retry_of_turn_id, variant_index) WHERE (retry_of_turn_id IS NOT NULL);



CREATE INDEX idx_turns_status_updated_at ON public.turns USING btree (status, updated_at);



CREATE INDEX idx_users_created_at_id ON public.users USING btree (created_at DESC, id DESC);



CREATE UNIQUE INDEX idx_users_email_unique ON public.users USING btree (lower(email));



CREATE INDEX idx_users_role_created_at_id ON public.users USING btree (role, created_at DESC, id DESC);



CREATE UNIQUE INDEX idx_users_unique_system_role ON public.users USING btree (role) WHERE (role = 'system'::text);



CREATE UNIQUE INDEX idx_users_username_unique ON public.users USING btree (lower(username));



CREATE INDEX user_mcp_servers_owner_updated_idx ON public.user_mcp_servers USING btree (owner_user_id, updated_at DESC, id DESC);



CREATE TRIGGER attachments_enforce_user_storage BEFORE INSERT OR DELETE OR UPDATE ON public.attachments FOR EACH ROW EXECUTE FUNCTION public.enforce_user_attachment_storage();



CREATE TRIGGER attachments_set_updated_at BEFORE UPDATE ON public.attachments FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER audit_events_immutable BEFORE DELETE OR UPDATE ON public.audit_events FOR EACH ROW EXECUTE FUNCTION public.reject_immutable_row_change();



CREATE TRIGGER billing_accounts_set_updated_at BEFORE UPDATE ON public.billing_accounts FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER billing_redemption_code_disables_immutable BEFORE DELETE OR UPDATE ON public.billing_redemption_code_disables FOR EACH ROW EXECUTE FUNCTION public.reject_immutable_row_change();



CREATE TRIGGER billing_redemption_codes_immutable BEFORE DELETE OR UPDATE ON public.billing_redemption_codes FOR EACH ROW EXECUTE FUNCTION public.reject_immutable_row_change();



CREATE TRIGGER billing_tool_prices_set_updated_at BEFORE UPDATE ON public.billing_tool_prices FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER billing_transactions_immutable BEFORE DELETE OR UPDATE ON public.billing_transactions FOR EACH ROW EXECUTE FUNCTION public.reject_immutable_row_change();



CREATE TRIGGER billing_usage_events_immutable BEFORE DELETE OR UPDATE ON public.billing_usage_events FOR EACH ROW EXECUTE FUNCTION public.reject_immutable_row_change();



CREATE TRIGGER context_heads_set_updated_at BEFORE UPDATE ON public.context_heads FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER conversation_initial_turns_set_updated_at BEFORE UPDATE ON public.conversation_initial_turns FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER conversations_set_updated_at BEFORE UPDATE ON public.conversations FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER generated_image_assets_set_updated_at BEFORE UPDATE ON public.generated_image_assets FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER model_price_versions_lifecycle BEFORE DELETE OR UPDATE ON public.model_price_versions FOR EACH ROW EXECUTE FUNCTION public.enforce_model_price_lifecycle();



CREATE TRIGGER model_settings_set_updated_at BEFORE UPDATE ON public.model_settings FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER models_set_updated_at BEFORE UPDATE ON public.models FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER provider_credentials_set_updated_at BEFORE UPDATE ON public.provider_credentials FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER sandboxes_set_updated_at BEFORE UPDATE ON public.sandboxes FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER smtp_settings_set_updated_at BEFORE UPDATE ON public.smtp_settings FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER tool_calls_set_updated_at BEFORE UPDATE ON public.tool_calls FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER turn_runs_set_updated_at BEFORE UPDATE ON public.turn_runs FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER turns_set_updated_at BEFORE UPDATE ON public.turns FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER user_locations_set_updated_at BEFORE UPDATE ON public.user_locations FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER user_mcp_servers_set_updated_at BEFORE UPDATE ON public.user_mcp_servers FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER user_mcp_tools_set_updated_at BEFORE UPDATE ON public.user_mcp_tools FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER user_preferences_set_updated_at BEFORE UPDATE ON public.user_preferences FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



CREATE TRIGGER users_set_updated_at BEFORE UPDATE ON public.users FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();



ALTER TABLE ONLY public.account_action_tokens
    ADD CONSTRAINT account_action_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.attachments
    ADD CONSTRAINT attachments_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.attachments
    ADD CONSTRAINT attachments_uploaded_by_user_id_fkey FOREIGN KEY (uploaded_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.audit_events
    ADD CONSTRAINT audit_events_actor_user_id_fkey FOREIGN KEY (actor_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.audit_events
    ADD CONSTRAINT audit_events_subject_user_id_fkey FOREIGN KEY (subject_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_accounts
    ADD CONSTRAINT billing_accounts_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_redemption_code_disables
    ADD CONSTRAINT billing_redemption_code_disables_disabled_by_user_id_fkey FOREIGN KEY (disabled_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_redemption_code_disables
    ADD CONSTRAINT billing_redemption_code_disables_redemption_code_id_fkey FOREIGN KEY (redemption_code_id) REFERENCES public.billing_redemption_codes(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_redemption_codes
    ADD CONSTRAINT billing_redemption_codes_created_by_user_id_fkey FOREIGN KEY (created_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_tool_prices
    ADD CONSTRAINT billing_tool_prices_updated_by_user_id_fkey FOREIGN KEY (updated_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_transactions
    ADD CONSTRAINT billing_transactions_account_id_user_id_currency_fkey FOREIGN KEY (account_id, user_id, currency) REFERENCES public.billing_accounts(id, user_id, currency) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_transactions
    ADD CONSTRAINT billing_transactions_actor_user_id_fkey FOREIGN KEY (actor_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_transactions
    ADD CONSTRAINT billing_transactions_redemption_value_fkey FOREIGN KEY (redemption_code_id, currency, amount_nanos) REFERENCES public.billing_redemption_codes(id, currency, amount_nanos) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_usage_events
    ADD CONSTRAINT billing_usage_events_billing_transaction_id_fkey FOREIGN KEY (billing_transaction_id) REFERENCES public.billing_transactions(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_usage_events
    ADD CONSTRAINT billing_usage_events_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_usage_events
    ADD CONSTRAINT billing_usage_events_model_id_fkey FOREIGN KEY (model_id) REFERENCES public.models(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_usage_events
    ADD CONSTRAINT billing_usage_events_model_price_id_fkey FOREIGN KEY (model_price_id) REFERENCES public.model_price_versions(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_usage_events
    ADD CONSTRAINT billing_usage_events_owner_user_id_fkey FOREIGN KEY (owner_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_usage_events
    ADD CONSTRAINT billing_usage_events_turn_id_fkey FOREIGN KEY (turn_id) REFERENCES public.turns(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.billing_usage_events
    ADD CONSTRAINT billing_usage_events_turn_run_id_fkey FOREIGN KEY (turn_run_id) REFERENCES public.turn_runs(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.context_heads
    ADD CONSTRAINT context_heads_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.context_heads
    ADD CONSTRAINT context_heads_latest_request_run_id_fkey FOREIGN KEY (latest_request_run_id) REFERENCES public.turn_runs(id) ON DELETE SET NULL;



ALTER TABLE ONLY public.context_heads
    ADD CONSTRAINT context_heads_latest_successful_run_id_fkey FOREIGN KEY (latest_successful_run_id) REFERENCES public.turn_runs(id) ON DELETE SET NULL;



ALTER TABLE ONLY public.conversation_events
    ADD CONSTRAINT conversation_events_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.conversation_events
    ADD CONSTRAINT conversation_events_conversation_id_turn_id_fkey FOREIGN KEY (conversation_id, turn_id) REFERENCES public.turns(conversation_id, id) ON DELETE CASCADE;



ALTER TABLE ONLY public.conversation_events
    ADD CONSTRAINT conversation_events_turn_id_turn_run_id_fkey FOREIGN KEY (turn_id, turn_run_id) REFERENCES public.turn_runs(turn_id, id) ON DELETE CASCADE;



ALTER TABLE ONLY public.conversation_initial_turns
    ADD CONSTRAINT conversation_initial_turns_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.conversation_initial_turns
    ADD CONSTRAINT conversation_initial_turns_conversation_id_turn_id_fkey FOREIGN KEY (conversation_id, turn_id) REFERENCES public.turns(conversation_id, id) ON DELETE CASCADE;



ALTER TABLE ONLY public.conversation_initial_turns
    ADD CONSTRAINT conversation_initial_turns_owner_user_id_fkey FOREIGN KEY (owner_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.conversation_shares
    ADD CONSTRAINT conversation_shares_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.conversation_shares
    ADD CONSTRAINT conversation_shares_created_by_user_id_fkey FOREIGN KEY (created_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.conversations
    ADD CONSTRAINT conversations_owner_user_id_fkey FOREIGN KEY (owner_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.generated_image_assets
    ADD CONSTRAINT generated_image_assets_attachment_id_fkey FOREIGN KEY (attachment_id) REFERENCES public.attachments(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.generated_image_assets
    ADD CONSTRAINT generated_image_assets_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.generated_image_assets
    ADD CONSTRAINT generated_image_assets_conversation_id_turn_id_fkey FOREIGN KEY (conversation_id, turn_id) REFERENCES public.turns(conversation_id, id) ON DELETE CASCADE;



ALTER TABLE ONLY public.generated_image_assets
    ADD CONSTRAINT generated_image_assets_turn_id_turn_run_id_fkey FOREIGN KEY (turn_id, turn_run_id) REFERENCES public.turn_runs(turn_id, id) ON DELETE CASCADE;



ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_conversation_id_turn_id_fkey FOREIGN KEY (conversation_id, turn_id) REFERENCES public.turns(conversation_id, id) ON DELETE SET NULL (turn_id);



ALTER TABLE ONLY public.model_price_versions
    ADD CONSTRAINT model_price_versions_created_by_user_id_fkey FOREIGN KEY (created_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.model_price_versions
    ADD CONSTRAINT model_price_versions_model_id_fkey FOREIGN KEY (model_id) REFERENCES public.models(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.model_price_versions
    ADD CONSTRAINT model_price_versions_published_by_user_id_fkey FOREIGN KEY (published_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.model_settings
    ADD CONSTRAINT model_settings_compaction_model_id_fkey FOREIGN KEY (compaction_model_id) REFERENCES public.models(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.model_settings
    ADD CONSTRAINT model_settings_default_chat_model_id_fkey FOREIGN KEY (default_chat_model_id) REFERENCES public.models(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.model_settings
    ADD CONSTRAINT model_settings_updated_by_user_id_fkey FOREIGN KEY (updated_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.models
    ADD CONSTRAINT models_created_by_user_id_fkey FOREIGN KEY (created_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.models
    ADD CONSTRAINT models_credential_id_fkey FOREIGN KEY (credential_id) REFERENCES public.provider_credentials(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.models
    ADD CONSTRAINT models_updated_by_user_id_fkey FOREIGN KEY (updated_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.outbox_events
    ADD CONSTRAINT outbox_events_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.outbox_events
    ADD CONSTRAINT outbox_events_conversation_id_turn_id_fkey FOREIGN KEY (conversation_id, turn_id) REFERENCES public.turns(conversation_id, id) ON DELETE CASCADE;



ALTER TABLE ONLY public.outbox_events
    ADD CONSTRAINT outbox_events_turn_id_turn_run_id_fkey FOREIGN KEY (turn_id, turn_run_id) REFERENCES public.turn_runs(turn_id, id) ON DELETE CASCADE;



ALTER TABLE ONLY public.provider_credentials
    ADD CONSTRAINT provider_credentials_created_by_user_id_fkey FOREIGN KEY (created_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.provider_credentials
    ADD CONSTRAINT provider_credentials_updated_by_user_id_fkey FOREIGN KEY (updated_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.sandboxes
    ADD CONSTRAINT sandboxes_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.smtp_settings
    ADD CONSTRAINT smtp_settings_updated_by_user_id_fkey FOREIGN KEY (updated_by_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.tool_calls
    ADD CONSTRAINT tool_calls_turn_id_turn_run_id_fkey FOREIGN KEY (turn_id, turn_run_id) REFERENCES public.turn_runs(turn_id, id) ON DELETE CASCADE;



ALTER TABLE ONLY public.turn_runs
    ADD CONSTRAINT turn_runs_turn_id_fkey FOREIGN KEY (turn_id) REFERENCES public.turns(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.turns
    ADD CONSTRAINT turns_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.turns
    ADD CONSTRAINT turns_model_id_fkey FOREIGN KEY (model_id) REFERENCES public.models(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.turns
    ADD CONSTRAINT turns_model_id_model_price_id_fkey FOREIGN KEY (model_id, model_price_id) REFERENCES public.model_price_versions(model_id, id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.turns
    ADD CONSTRAINT turns_retry_source_fk FOREIGN KEY (conversation_id, retry_of_turn_id) REFERENCES public.turns(conversation_id, id) ON DELETE CASCADE;



ALTER TABLE ONLY public.user_locations
    ADD CONSTRAINT user_locations_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.user_mcp_servers
    ADD CONSTRAINT user_mcp_servers_owner_user_id_fkey FOREIGN KEY (owner_user_id) REFERENCES public.users(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.user_mcp_tools
    ADD CONSTRAINT user_mcp_tools_server_id_fkey FOREIGN KEY (server_id) REFERENCES public.user_mcp_servers(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.user_preferences
    ADD CONSTRAINT user_preferences_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;



INSERT INTO public.model_settings (singleton) VALUES (true);
INSERT INTO public.smtp_settings (singleton) VALUES (true);

COMMIT;
