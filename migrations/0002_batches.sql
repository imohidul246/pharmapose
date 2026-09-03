CREATE TABLE batches (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    medicine_id    UUID NOT NULL REFERENCES medicines(id) ON DELETE CASCADE,
    batch_number   VARCHAR(100) NOT NULL,
    expiry_date    DATE NOT NULL,
    purchase_price NUMERIC(12,2) NOT NULL DEFAULT 0.00 CHECK (purchase_price >= 0),
    sale_price     NUMERIC(12,2) NOT NULL DEFAULT 0.00 CHECK (sale_price >= 0),
    current_stock  INT NOT NULL DEFAULT 0 CHECK (current_stock >= 0),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_batches_medicine_id ON batches (medicine_id);
CREATE INDEX idx_batches_batch_number ON batches (batch_number);
CREATE INDEX idx_batches_expiry_date ON batches (expiry_date);
CREATE UNIQUE INDEX uq_batches_medicine_batch ON batches (medicine_id, batch_number);
