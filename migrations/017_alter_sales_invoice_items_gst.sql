-- 017: Extend sales_invoice_items with tax snapshot columns
-- All tax columns are nullable to preserve pre-GST historical records
ALTER TABLE sales_invoice_items
    ADD COLUMN hsn_code      VARCHAR(20),
    ADD COLUMN gross_amount  NUMERIC(14,2),
    ADD COLUMN taxable_value NUMERIC(14,2),
    ADD COLUMN gst_rate      NUMERIC(5,2) DEFAULT NULL,
    ADD COLUMN cgst_rate     NUMERIC(5,2) DEFAULT NULL,
    ADD COLUMN cgst_amount   NUMERIC(14,2) DEFAULT NULL,
    ADD COLUMN sgst_rate     NUMERIC(5,2) DEFAULT NULL,
    ADD COLUMN sgst_amount   NUMERIC(14,2) DEFAULT NULL,
    ADD COLUMN igst_rate     NUMERIC(5,2) DEFAULT NULL,
    ADD COLUMN igst_amount   NUMERIC(14,2) DEFAULT NULL,
    ADD COLUMN cess_rate     NUMERIC(5,2) DEFAULT NULL,
    ADD COLUMN cess_amount   NUMERIC(14,2) DEFAULT NULL,
    ADD COLUMN line_total    NUMERIC(14,2);

CREATE INDEX idx_sales_invoice_items_hsn ON sales_invoice_items (hsn_code) WHERE hsn_code IS NOT NULL;
