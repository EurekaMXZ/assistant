BEGIN;

ALTER TABLE billing_tool_prices
    DROP CONSTRAINT billing_tool_prices_json_safe_price_check,
    DROP CONSTRAINT billing_tool_prices_enabled_price_check;

COMMIT;
