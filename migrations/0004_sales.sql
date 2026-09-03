DO $$ BEGIN
    CREATE TYPE payment_type AS ENUM ('CASH', 'CREDIT');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE sales_invoices (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_no   BIGSERIAL UNIQUE,
    customer_id  UUID REFERENCES customers(id) ON DELETE SET NULL,
    payment_type payment_type NOT NULL DEFAULT 'CASH',
    total_amount NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sales_invoices_customer_id ON sales_invoices (customer_id);
CREATE INDEX idx_sales_invoices_created_at ON sales_invoices (created_at);
CREATE INDEX idx_sales_invoices_payment_type ON sales_invoices (payment_type);

CREATE TABLE sales_invoice_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id      UUID NOT NULL REFERENCES sales_invoices(id) ON DELETE RESTRICT,
    medicine_id     UUID NOT NULL REFERENCES medicines(id),
    batch_id        UUID NOT NULL REFERENCES batches(id),
    quantity        INT NOT NULL CHECK (quantity > 0),
    unit_sale_price NUMERIC(12,2) NOT NULL,
    subtotal        NUMERIC(14,2) NOT NULL
);

CREATE INDEX idx_sales_invoice_items_invoice_id ON sales_invoice_items (invoice_id);
CREATE INDEX idx_sales_invoice_items_batch_id ON sales_invoice_items (batch_id);
CREATE INDEX idx_sales_invoice_items_medicine_id ON sales_invoice_items (medicine_id);
