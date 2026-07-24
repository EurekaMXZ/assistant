BEGIN;

CREATE TABLE generated_image_assets (
    id uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    turn_id uuid NOT NULL,
    turn_run_id uuid NOT NULL,
    response_id text NOT NULL DEFAULT '',
    item_id text NOT NULL CHECK (length(btrim(item_id)) > 0),
    kind text NOT NULL CHECK (kind IN ('partial', 'final')),
    revision integer NOT NULL CHECK (revision >= 0 AND revision <= 3),
    status text NOT NULL DEFAULT 'ready' CHECK (status IN ('ready', 'deleting')),
    object_key text NOT NULL UNIQUE,
    content_type text NOT NULL CHECK (content_type LIKE 'image/%'),
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    sha256 text NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    width integer NOT NULL CHECK (width > 0),
    height integer NOT NULL CHECK (height > 0),
    attachment_id uuid REFERENCES attachments (id) ON DELETE CASCADE,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (turn_run_id, item_id, kind, revision),
    FOREIGN KEY (conversation_id, turn_id)
        REFERENCES turns (conversation_id, id) ON DELETE CASCADE,
    FOREIGN KEY (turn_id, turn_run_id)
        REFERENCES turn_runs (turn_id, id) ON DELETE CASCADE,
    CHECK (
        (kind = 'partial' AND attachment_id IS NULL AND expires_at IS NOT NULL)
        OR (kind = 'final' AND attachment_id IS NOT NULL AND expires_at IS NULL)
    )
);

CREATE INDEX generated_image_assets_turn_created_idx
    ON generated_image_assets (turn_id, created_at, revision);

CREATE INDEX generated_image_assets_cleanup_idx
    ON generated_image_assets (expires_at)
    WHERE kind = 'partial';

CREATE TRIGGER generated_image_assets_set_updated_at
BEFORE UPDATE ON generated_image_assets
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
