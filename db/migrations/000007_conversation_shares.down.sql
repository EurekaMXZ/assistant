BEGIN;

LOCK TABLE conversation_shares IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM conversation_shares) THEN
        RAISE EXCEPTION 'cannot remove conversation shares while share records exist';
    END IF;
END $$;

DROP TABLE conversation_shares;

COMMIT;
