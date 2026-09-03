CREATE TABLE purchase_orders (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_no   VARCHAR(50) NOT NULL UNIQUE,
    supplier_name VARCHAR(255) NOT NULL,
    total_amount NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_purchase_orders_created_at ON purchase_orders (created_at);

CREATE TABLE purchase_order_items (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    purchase_id    UUID NOT NULL REFERENCES purchase_orders(id) ON DELETE CASCADE,
    medicine_id    UUID NOT NULL REFERENCES medicines(id),
    batch_number   VARCHAR(100) NOT NULL,
    expiry_date    DATE NOT NULL,
    quantity       INT NOT NULL CHECK (quantity > 0),
    purchase_price NUMERIC(12,2) NOT NULL DEFAULT 0.00,
    sale_price     NUMERIC(12,2) NOT NULL DEFAULT 0.00
);

CREATE INDEX idx_purchase_order_items_purchase_id ON purchase_order_items (purchase_id);
CREATE INDEX idx_purchase_order_items_medicine_id ON purchase_order_items (medicine_id);
