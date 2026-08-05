BEGIN;

ALTER TABLE public.billing_tool_prices
    DROP COLUMN IF EXISTS tool_enabled;

COMMIT;
