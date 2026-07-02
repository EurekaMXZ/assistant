BEGIN;

DROP TABLE smtp_settings;
DROP TABLE account_action_tokens;
DROP TABLE audit_events;
DROP TABLE billing_usage_events;
DROP TABLE billing_transactions;
DROP TABLE billing_accounts;
DROP TABLE turn_stream_events;
DROP TABLE attachments;
DROP TABLE sandboxes;
DROP TABLE tool_calls;
DROP TABLE outbox_events;
DROP TABLE turn_runs;
DROP TABLE conversation_initial_turns;
DROP TABLE messages;
DROP TABLE turns;
DROP TABLE context_heads;
DROP TABLE conversations;
DROP TABLE model_settings;
DROP TABLE model_price_versions;
DROP TABLE models;
DROP TABLE provider_credentials;
DROP TABLE users;

DROP FUNCTION enforce_model_price_lifecycle();
DROP FUNCTION reject_immutable_row_change();
DROP FUNCTION set_updated_at();

COMMIT;
