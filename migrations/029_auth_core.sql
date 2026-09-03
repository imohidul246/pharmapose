-- 029: Auth core — users, store memberships, sessions, audit log, employee limits.
-- Browsers hold a random opaque token in an HttpOnly cookie; the database stores
-- only its SHA-256 hash, so a leaked cookie jar never leaks a password-equivalent.
-- Every request re-validates users.is_active AND store_memberships.is_active, so
-- deactivation (logout-all + disable employee) takes effect on the next request.

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    phone         VARCHAR(20) NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    is_active     BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE store_memberships (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id   UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       VARCHAR(20) NOT NULL DEFAULT 'EMPLOYEE',
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_store_memberships_user UNIQUE (store_id, user_id),
    CONSTRAINT chk_membership_role CHECK (role IN ('STORE_OWNER', 'EMPLOYEE'))
);

CREATE INDEX idx_memberships_user ON store_memberships (user_id);
CREATE INDEX idx_memberships_store ON store_memberships (store_id);

-- A store can have exactly one active owner at a time.
CREATE UNIQUE INDEX uq_active_owner_per_store
    ON store_memberships (store_id)
    WHERE role = 'STORE_OWNER' AND is_active = true;

-- Employee seat cap is enforced in application code; the CHECK keeps the cap
-- from ever drifting negative as defense-in-depth.
ALTER TABLE stores
    ADD COLUMN max_employees INT NOT NULL DEFAULT 2,
    ADD CONSTRAINT chk_stores_max_employees CHECK (max_employees >= 0);

CREATE TABLE sessions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    ip         VARCHAR(45) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_sessions_user ON sessions (user_id);
CREATE INDEX idx_sessions_expires ON sessions (expires_at);

CREATE TABLE audit_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id   UUID REFERENCES stores(id) ON DELETE CASCADE,
    user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    action     TEXT NOT NULL,
    entity     TEXT NOT NULL DEFAULT '',
    entity_id  TEXT NOT NULL DEFAULT '',
    details    JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_store_time ON audit_logs (store_id, created_at DESC);
CREATE INDEX idx_audit_logs_user_time ON audit_logs (user_id, created_at DESC);