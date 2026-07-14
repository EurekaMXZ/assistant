BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM billing_usage_events
        WHERE tool_amount_nanos <> 0 OR tool_usage <> '{}'::jsonb
    ) THEN
        RAISE EXCEPTION 'cannot remove tool pricing after billable tool usage has been recorded';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM billing_tool_prices
        WHERE enabled OR price_per_call_nanos <> 0
    ) THEN
        RAISE EXCEPTION 'cannot remove configured tool prices';
    END IF;
END $$;

ALTER TABLE billing_usage_events
    DROP COLUMN tool_pricing_snapshot,
    DROP COLUMN tool_usage,
    DROP COLUMN tool_amount_nanos;

DROP TABLE billing_tool_prices;

COMMIT;
