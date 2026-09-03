-- 011: Create businesses and gst_registrations for multi-store GST support
CREATE TABLE businesses (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    legal_name TEXT NOT NULL,
    trade_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE gst_registrations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    gstin       VARCHAR(15),
    legal_name  TEXT NOT NULL DEFAULT '',
    trade_name  TEXT NOT NULL DEFAULT '',
    pan         VARCHAR(10),
    state_code  VARCHAR(2) NOT NULL DEFAULT '',
    state_name  TEXT NOT NULL DEFAULT '',
    address     TEXT NOT NULL DEFAULT '',
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_gst_registrations_business ON gst_registrations (business_id);
CREATE INDEX idx_gst_registrations_gstin ON gst_registrations (gstin) WHERE gstin IS NOT NULL;
CREATE INDEX idx_gst_registrations_state ON gst_registrations (state_code);

CREATE TABLE stores (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    gst_registration_id  UUID REFERENCES gst_registrations(id) ON DELETE SET NULL,
    name                 TEXT NOT NULL,
    address              TEXT NOT NULL DEFAULT '',
    is_active            BOOLEAN NOT NULL DEFAULT true,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_stores_gst_registration ON stores (gst_registration_id);
