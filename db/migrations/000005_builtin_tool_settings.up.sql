BEGIN;

ALTER TABLE public.billing_tool_prices
    ADD COLUMN tool_enabled boolean DEFAULT true NOT NULL;

COMMIT;
