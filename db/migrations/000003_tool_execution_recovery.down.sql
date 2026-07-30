BEGIN;

UPDATE public.tool_calls
SET status = 'failed'
WHERE status = 'uncertain';

ALTER TABLE public.tool_calls
    DROP CONSTRAINT tool_calls_status_check,
    ADD CONSTRAINT tool_calls_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'awaiting_input'::text, 'completed'::text, 'failed'::text, 'cancelled'::text])));

COMMIT;
