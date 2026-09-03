-- 010: Create suppliers table (replaces free-text supplier_name on purchase_orders)
CREATE TABLE suppliers (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    legal_name TEXT NOT NULL,
    trade_name TEXT NOT NULL DEFAULT '',
    gstin      VARCHAR(15),
    pan        VARCHAR(10),
    address    TEXT NOT NULL DEFAULT '',
    state      TEXT NOT NULL DEFAULT '',
    state_code VARCHAR(2) NOT NULL DEFAULT '',
    phone      TEXT NOT NULL DEFAULT '',
    email      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_suppliers_gstin ON suppliers (gstin) WHERE gstin IS NOT NULL;
CREATE INDEX idx_suppliers_state_code ON suppliers (state_code);
CREATE INDEX idx_suppliers_name ON suppliers (legal_name);
