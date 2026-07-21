BEGIN;

CREATE TABLE user_preferences (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    preferences_text text NOT NULL DEFAULT ''
        CHECK (length(preferences_text) <= 8000),
    location_enabled_for_model boolean NOT NULL DEFAULT false,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER user_preferences_set_updated_at
BEFORE UPDATE ON user_preferences
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE user_locations (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    latitude double precision NOT NULL
        CHECK (latitude <> 'NaN'::double precision AND latitude >= -90 AND latitude <= 90),
    longitude double precision NOT NULL
        CHECK (longitude <> 'NaN'::double precision AND longitude >= -180 AND longitude <= 180),
    coordinate_system text NOT NULL DEFAULT 'gcj02' CHECK (coordinate_system = 'gcj02'),
    formatted_address text NOT NULL DEFAULT '' CHECK (length(formatted_address) <= 500),
    province text NOT NULL DEFAULT '' CHECK (length(province) <= 100),
    city text NOT NULL DEFAULT '' CHECK (length(city) <= 100),
    district text NOT NULL DEFAULT '' CHECK (length(district) <= 100),
    adcode text NOT NULL DEFAULT '' CHECK (adcode = '' OR adcode ~ '^[0-9]{6}$'),
    poi_id text NOT NULL DEFAULT '' CHECK (length(poi_id) <= 128),
    poi_name text NOT NULL DEFAULT '' CHECK (length(poi_name) <= 200),
    source text NOT NULL CHECK (source IN ('map', 'search', 'geolocation')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER user_locations_set_updated_at
BEFORE UPDATE ON user_locations
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
