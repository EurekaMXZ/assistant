CREATE INDEX idx_users_created_at_id
    ON users (created_at DESC, id DESC);

DROP INDEX idx_users_role_created_at;

CREATE INDEX idx_users_role_created_at_id
    ON users (role, created_at DESC, id DESC);

CREATE INDEX idx_model_price_versions_cursor
    ON model_price_versions (model_id, created_at DESC, id DESC);
