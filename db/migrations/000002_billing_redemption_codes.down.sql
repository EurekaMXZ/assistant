BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM billing_redemption_codes
    ) THEN
        RAISE EXCEPTION 'cannot remove redemption codes after codes have been issued';
    END IF;
END $$;

DROP INDEX billing_transactions_redemption_code_id_unique;

ALTER TABLE billing_transactions
    DROP CONSTRAINT billing_transactions_redemption_shape_check,
    DROP CONSTRAINT billing_transactions_kind_direction_check,
    DROP CONSTRAINT billing_transactions_kind_check,
    DROP COLUMN redemption_code_id,
    ADD CONSTRAINT billing_transactions_kind_check
        CHECK (kind IN ('manual_topup', 'manual_refund', 'model_usage_charge')),
    ADD CONSTRAINT billing_transactions_check CHECK (
        (kind = 'manual_topup' AND direction = 'credit')
        OR (kind IN ('manual_refund', 'model_usage_charge') AND direction = 'debit')
    );

DROP TABLE billing_redemption_code_disables;
DROP TABLE billing_redemption_codes;

COMMIT;
