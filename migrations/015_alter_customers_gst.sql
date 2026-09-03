-- 015: Extend customers with GST-related fields
-- All columns are nullable with defaults to preserve existing records
ALTER TABLE customers
    ADD COLUMN gstin            VARCHAR(15),
    ADD COLUMN customer_type    TEXT NOT NULL DEFAULT 'B2C',
    ADD COLUMN billing_address  TEXT,
    ADD COLUMN shipping_address TEXT,
    ADD COLUMN state            TEXT,
    ADD COLUMN state_code       VARCHAR(2);

CREATE INDEX idx_customers_type ON customers (customer_type);
CREATE INDEX idx_customers_gstin ON customers (gstin) WHERE gstin IS NOT NULL;
CREATE INDEX idx_customers_state_code ON customers (state_code) WHERE state_code IS NOT NULL;
