DROP INDEX idx_model_price_versions_cursor;

DROP INDEX idx_users_role_created_at_id;

CREATE INDEX idx_users_role_created_at
    ON users (role, created_at DESC);

DROP INDEX idx_users_created_at_id;
