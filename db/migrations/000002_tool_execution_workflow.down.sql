BEGIN;

DROP INDEX IF EXISTS public.idx_outbox_tool_call_requested;
DROP INDEX IF EXISTS public.idx_outbox_tool_group_completed;
DROP INDEX IF EXISTS public.idx_tool_calls_ready;
DROP INDEX IF EXISTS public.idx_tool_calls_lease;

ALTER TABLE public.turn_runs
    DROP CONSTRAINT turn_runs_status_check,
    ADD CONSTRAINT turn_runs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'awaiting_input'::text, 'cancel_requested'::text, 'completed'::text, 'failed'::text, 'cancelled'::text])));

ALTER TABLE public.tool_calls
    DROP CONSTRAINT tool_calls_status_check,
    DROP CONSTRAINT tool_calls_execution_group_check,
    DROP CONSTRAINT tool_calls_execution_ordinal_check,
    ADD CONSTRAINT tool_calls_status_check CHECK ((status = ANY (ARRAY['running'::text, 'awaiting_input'::text, 'completed'::text, 'failed'::text, 'cancelled'::text])));

ALTER TABLE public.tool_calls
    DROP COLUMN leased_at,
    DROP COLUMN lease_token,
    DROP COLUMN stable_operation_id,
    DROP COLUMN execution_ordinal,
    DROP COLUMN execution_group;

ALTER TABLE public.outbox_events
    DROP COLUMN execution_group,
    DROP COLUMN tool_call_id;

COMMIT;
