BEGIN;

ALTER TABLE messages
    DROP COLUMN context_excluded;

DROP INDEX idx_turns_retry_source;
DROP INDEX idx_turns_retry_variant;

ALTER TABLE turns
    DROP CONSTRAINT turns_retry_source_fk,
    DROP CONSTRAINT turns_variant_shape,
    DROP COLUMN variant_index,
    DROP COLUMN retry_of_turn_id;

COMMIT;
