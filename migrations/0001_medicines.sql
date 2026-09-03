CREATE TABLE medicines (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              VARCHAR(255) NOT NULL,
    salt_composition  TEXT NOT NULL DEFAULT '',
    manufacturer      VARCHAR(255) NOT NULL DEFAULT '',
    min_reorder_level INT NOT NULL DEFAULT 0,
    packing           VARCHAR(100) NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ
);

CREATE INDEX idx_medicines_name ON medicines (name);
CREATE INDEX idx_medicines_salt_composition ON medicines USING gin (to_tsvector('simple', salt_composition));
CREATE INDEX idx_medicines_deleted_at ON medicines (deleted_at);
