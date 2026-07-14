BEGIN;

CREATE TABLE billing_tool_prices (
    tool_key text NOT NULL CHECK (tool_key ~ '^[a-z0-9_]+(?:\.[a-z0-9_]+)*$'),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    price_per_call_nanos bigint NOT NULL DEFAULT 0 CHECK (price_per_call_nanos >= 0),
    enabled boolean NOT NULL DEFAULT false,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_by_user_id uuid REFERENCES users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tool_key, currency)
);

CREATE TRIGGER billing_tool_prices_set_updated_at
BEFORE UPDATE ON billing_tool_prices
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE billing_usage_events
    ADD COLUMN tool_amount_nanos bigint NOT NULL DEFAULT 0 CHECK (tool_amount_nanos >= 0),
    ADD COLUMN tool_usage jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(tool_usage) = 'object'),
    ADD COLUMN tool_pricing_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(tool_pricing_snapshot) = 'object');

COMMIT;
