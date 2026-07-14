BEGIN;

CREATE TABLE billing_redemption_codes (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    code_hash bytea NOT NULL UNIQUE CHECK (octet_length(code_hash) = 32),
    code_hint text NOT NULL CHECK (length(btrim(code_hint)) > 0),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    amount_nanos bigint NOT NULL CHECK (amount_nanos > 0),
    created_by_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, currency, amount_nanos),
    CHECK (expires_at IS NULL OR expires_at > created_at)
);

CREATE INDEX idx_billing_redemption_codes_cursor
    ON billing_redemption_codes (created_at DESC, id DESC);

CREATE TRIGGER billing_redemption_codes_immutable
BEFORE UPDATE OR DELETE ON billing_redemption_codes
FOR EACH ROW EXECUTE FUNCTION reject_immutable_row_change();

CREATE TABLE billing_redemption_code_disables (
    redemption_code_id uuid PRIMARY KEY REFERENCES billing_redemption_codes (id) ON DELETE RESTRICT,
    disabled_by_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER billing_redemption_code_disables_immutable
BEFORE UPDATE OR DELETE ON billing_redemption_code_disables
FOR EACH ROW EXECUTE FUNCTION reject_immutable_row_change();

ALTER TABLE billing_transactions
    ADD COLUMN redemption_code_id uuid;

ALTER TABLE billing_transactions
    ADD CONSTRAINT billing_transactions_redemption_value_fkey
    FOREIGN KEY (redemption_code_id, currency, amount_nanos)
    REFERENCES billing_redemption_codes (id, currency, amount_nanos) ON DELETE RESTRICT;

CREATE UNIQUE INDEX billing_transactions_redemption_code_id_unique
    ON billing_transactions (redemption_code_id)
    WHERE redemption_code_id IS NOT NULL;

ALTER TABLE billing_transactions
    DROP CONSTRAINT billing_transactions_kind_check,
    DROP CONSTRAINT billing_transactions_check,
    ADD CONSTRAINT billing_transactions_kind_check
        CHECK (kind IN ('manual_topup', 'manual_refund', 'model_usage_charge', 'redemption_credit')),
    ADD CONSTRAINT billing_transactions_kind_direction_check CHECK (
        (kind IN ('manual_topup', 'redemption_credit') AND direction = 'credit')
        OR (kind IN ('manual_refund', 'model_usage_charge') AND direction = 'debit')
    ),
    ADD CONSTRAINT billing_transactions_redemption_shape_check CHECK (
        (
            kind = 'redemption_credit'
            AND redemption_code_id IS NOT NULL
            AND actor_user_id IS NOT NULL
            AND actor_user_id = user_id
        )
        OR (kind <> 'redemption_credit' AND redemption_code_id IS NULL)
    );

COMMIT;
