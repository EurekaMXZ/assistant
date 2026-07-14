BEGIN;

ALTER TABLE turns
    ADD COLUMN retry_of_turn_id uuid,
    ADD COLUMN variant_index integer NOT NULL DEFAULT 1 CHECK (variant_index > 0),
    ADD CONSTRAINT turns_variant_shape CHECK (
        (retry_of_turn_id IS NULL AND variant_index = 1)
        OR (retry_of_turn_id IS NOT NULL AND variant_index > 1)
    ),
    ADD CONSTRAINT turns_retry_source_fk
        FOREIGN KEY (conversation_id, retry_of_turn_id)
        REFERENCES turns (conversation_id, id) ON DELETE CASCADE;

CREATE UNIQUE INDEX idx_turns_retry_variant
    ON turns (retry_of_turn_id, variant_index)
    WHERE retry_of_turn_id IS NOT NULL;

CREATE INDEX idx_turns_retry_source
    ON turns (retry_of_turn_id)
    WHERE retry_of_turn_id IS NOT NULL;

ALTER TABLE messages
    ADD COLUMN context_excluded boolean NOT NULL DEFAULT false;

COMMIT;
