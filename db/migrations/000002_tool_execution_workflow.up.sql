BEGIN;

ALTER TABLE public.outbox_events
    ADD COLUMN tool_call_id uuid,
    ADD COLUMN execution_group integer DEFAULT 0 NOT NULL;

ALTER TABLE public.tool_calls
    ADD COLUMN execution_group integer DEFAULT 0 NOT NULL,
    ADD COLUMN execution_ordinal integer DEFAULT 0 NOT NULL,
    ADD COLUMN stable_operation_id text DEFAULT ''::text NOT NULL,
    ADD COLUMN lease_token uuid,
    ADD COLUMN leased_at timestamp with time zone;

ALTER TABLE public.tool_calls
    DROP CONSTRAINT tool_calls_status_check,
    ADD CONSTRAINT tool_calls_execution_group_check CHECK ((execution_group >= 0)),
    ADD CONSTRAINT tool_calls_execution_ordinal_check CHECK ((execution_ordinal >= 0)),
    ADD CONSTRAINT tool_calls_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'awaiting_input'::text, 'completed'::text, 'failed'::text, 'cancelled'::text])));

ALTER TABLE public.turn_runs
    DROP CONSTRAINT turn_runs_status_check,
    ADD CONSTRAINT turn_runs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'waiting_tools'::text, 'awaiting_input'::text, 'cancel_requested'::text, 'completed'::text, 'failed'::text, 'cancelled'::text])));

CREATE UNIQUE INDEX idx_outbox_tool_call_requested ON public.outbox_events USING btree (tool_call_id) WHERE ((event_type = 'tool_call.requested'::text) AND (tool_call_id IS NOT NULL));
CREATE UNIQUE INDEX idx_outbox_tool_group_completed ON public.outbox_events USING btree (turn_run_id, execution_group) WHERE ((event_type = 'tool_group.completed'::text) AND (turn_run_id IS NOT NULL));
CREATE INDEX idx_tool_calls_ready ON public.tool_calls USING btree (turn_run_id, execution_group, execution_ordinal) WHERE (status = 'queued'::text);
CREATE INDEX idx_tool_calls_lease ON public.tool_calls USING btree (leased_at) WHERE ((status = 'running'::text) AND (lease_token IS NOT NULL));

COMMIT;
