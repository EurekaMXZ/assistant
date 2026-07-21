BEGIN;

CREATE TABLE user_mcp_servers (
    id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name text NOT NULL
        CHECK (name = btrim(name) AND length(name) BETWEEN 1 AND 100),
    slug text NOT NULL
        CHECK (length(slug) BETWEEN 1 AND 64 AND slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    endpoint_url text NOT NULL
        CHECK (
            endpoint_url = btrim(endpoint_url)
            AND length(endpoint_url) BETWEEN 1 AND 2048
            AND endpoint_url ~ '^https?://[^/?#@]+(/[^?#]*)?$'
        ),
    enabled boolean NOT NULL DEFAULT true,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    encrypted_parameters bytea NOT NULL CHECK (octet_length(encrypted_parameters) >= 16),
    parameters_nonce bytea NOT NULL CHECK (octet_length(parameters_nonce) = 12),
    encrypted_headers bytea NOT NULL CHECK (octet_length(encrypted_headers) >= 16),
    headers_nonce bytea NOT NULL CHECK (octet_length(headers_nonce) = 12),
    last_validation_status text NOT NULL DEFAULT 'untested'
        CHECK (last_validation_status IN ('untested', 'valid', 'invalid')),
    last_validation_error text
        CHECK (last_validation_error IS NULL OR length(last_validation_error) BETWEEN 1 AND 500),
    last_validated_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, slug),
    CHECK (
        (last_validation_status = 'untested' AND last_validated_at IS NULL AND last_validation_error IS NULL)
        OR (last_validation_status = 'valid' AND last_validated_at IS NOT NULL AND last_validation_error IS NULL)
        OR (last_validation_status = 'invalid' AND last_validated_at IS NOT NULL AND last_validation_error IS NOT NULL)
    )
);

CREATE INDEX user_mcp_servers_owner_updated_idx
    ON user_mcp_servers (owner_user_id, updated_at DESC, id DESC);

CREATE TRIGGER user_mcp_servers_set_updated_at
BEFORE UPDATE ON user_mcp_servers
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE user_mcp_tools (
    server_id uuid NOT NULL REFERENCES user_mcp_servers (id) ON DELETE CASCADE,
    name text NOT NULL
        CHECK (name = btrim(name) AND length(name) BETWEEN 1 AND 255),
    description text NOT NULL DEFAULT '' CHECK (length(description) <= 4000),
    input_schema jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(input_schema) = 'object'),
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, name)
);

CREATE TRIGGER user_mcp_tools_set_updated_at
BEFORE UPDATE ON user_mcp_tools
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
