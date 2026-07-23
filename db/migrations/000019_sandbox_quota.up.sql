ALTER TABLE users
    ADD COLUMN sandbox_quota integer NOT NULL DEFAULT 3,
    ADD CONSTRAINT users_sandbox_quota_check CHECK (sandbox_quota >= 0);
