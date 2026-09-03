-- 026: B2B Wholesale Sale support
-- Adds sale_type, buyer info to sales_invoices
-- Adds MRP and bonus_quantity to sales_invoice_items

-- 1. Sale type and buyer info on invoice header
ALTER TABLE sales_invoices
    ADD COLUMN sale_type    VARCHAR(10) NOT NULL DEFAULT 'RETAIL',
    ADD COLUMN buyer_name   VARCHAR(255),
    ADD COLUMN buyer_gstin  VARCHAR(15),
    ADD COLUMN buyer_address TEXT;

CREATE INDEX idx_sales_invoices_sale_type ON sales_invoices (sale_type);

-- 2. MRP reference and bonus quantity on line items
ALTER TABLE sales_invoice_items
    ADD COLUMN mrp             NUMERIC(12,2),
    ADD COLUMN bonus_quantity  INT NOT NULL DEFAULT 0;
