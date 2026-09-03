ALTER TABLE sales_invoices
    ADD COLUMN discount_total NUMERIC(14,2) NOT NULL DEFAULT 0.00;

ALTER TABLE sales_invoice_items
    ADD COLUMN discount_type     TEXT         NOT NULL DEFAULT 'NONE',
    ADD COLUMN discount_value    NUMERIC(12,2) NOT NULL DEFAULT 0.00,
    ADD COLUMN discount_amount   NUMERIC(14,2) NOT NULL DEFAULT 0.00;

CREATE INDEX idx_sales_invoices_created_at_disc ON sales_invoices (created_at);
