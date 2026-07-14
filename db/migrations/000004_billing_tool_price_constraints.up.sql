BEGIN;

ALTER TABLE billing_tool_prices
    ADD CONSTRAINT billing_tool_prices_enabled_price_check
        CHECK (NOT enabled OR price_per_call_nanos > 0),
    ADD CONSTRAINT billing_tool_prices_json_safe_price_check
        CHECK (price_per_call_nanos <= 9007199254740991);

COMMIT;
