BEGIN;

CREATE INDEX idx_messages_turn_role_seq ON public.messages USING btree (turn_id, role, seq);

COMMIT;
