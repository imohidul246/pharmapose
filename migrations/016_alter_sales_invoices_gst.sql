-- 016: Extend sales_invoices with GST totals, store reference, and supply type
-- All GST columns are nullable to preserve pre-GST historical records
ALTER TABLE sales_invoices
    ADD COLUMN store_id            UUID REFERENCES stores(id) ON DELETE SET NULL,
    ADD COLUMN gst_registration_id UUID REFERENCES gst_registrations(id) ON DELETE SET NULL,
    ADD COLUMN customer_gstin      VARCHAR(15),
    ADD COLUMN customer_state_code VARCHAR(2),
    ADD COLUMN supply_type         TEXT DEFAULT NULL,
    ADD COLUMN gross_amount        NUMERIC(14,2),
    ADD COLUMN taxable_amount      NUMERIC(14,2),
    ADD COLUMN cgst_total          NUMERIC(14,2) DEFAULT 0.00,
    ADD COLUMN sgst_total          NUMERIC(14,2) DEFAULT 0.00,
    ADD COLUMN igst_total          NUMERIC(14,2) DEFAULT 0.00,
    ADD COLUMN cess_total          NUMERIC(14,2) DEFAULT 0.00,
    ADD COLUMN tax_total           NUMERIC(14,2) DEFAULT 0.00,
    ADD COLUMN round_off           NUMERIC(6,2) DEFAULT 0.00,
    ADD COLUMN grand_total         NUMERIC(14,2),
    ADD COLUMN price_includes_tax  BOOLEAN DEFAULT NULL;

CREATE INDEX idx_sales_invoices_store ON sales_invoices (store_id) WHERE store_id IS NOT NULL;
CREATE INDEX idx_sales_invoices_gst_reg ON sales_invoices (gst_registration_id) WHERE gst_registration_id IS NOT NULL;
CREATE INDEX idx_sales_invoices_supply_type ON sales_invoices (supply_type) WHERE supply_type IS NOT NULL;
