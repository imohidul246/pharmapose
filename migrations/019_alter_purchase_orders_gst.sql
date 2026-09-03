-- 019: Extend purchase_orders with supplier FK and GST columns
-- supplier_id is nullable so existing POs with free-text supplier_name remain valid
ALTER TABLE purchase_orders
    ADD COLUMN supplier_id        UUID REFERENCES suppliers(id) ON DELETE SET NULL,
    ADD COLUMN supplier_gstin     VARCHAR(15),
    ADD COLUMN supplier_state_code VARCHAR(2),
    ADD COLUMN store_id           UUID REFERENCES stores(id) ON DELETE SET NULL,
    ADD COLUMN gst_registration_id UUID REFERENCES gst_registrations(id) ON DELETE SET NULL,
    ADD COLUMN supply_type        TEXT DEFAULT NULL,
    ADD COLUMN gross_amount       NUMERIC(14,2),
    ADD COLUMN taxable_amount     NUMERIC(14,2),
    ADD COLUMN cgst_total         NUMERIC(14,2) DEFAULT 0.00,
    ADD COLUMN sgst_total         NUMERIC(14,2) DEFAULT 0.00,
    ADD COLUMN igst_total         NUMERIC(14,2) DEFAULT 0.00,
    ADD COLUMN cess_total         NUMERIC(14,2) DEFAULT 0.00,
    ADD COLUMN tax_total          NUMERIC(14,2) DEFAULT 0.00,
    ADD COLUMN grand_total        NUMERIC(14,2),
    ADD COLUMN price_includes_tax BOOLEAN DEFAULT NULL;

CREATE INDEX idx_purchase_orders_supplier ON purchase_orders (supplier_id) WHERE supplier_id IS NOT NULL;
CREATE INDEX idx_purchase_orders_gst_reg ON purchase_orders (gst_registration_id) WHERE gst_registration_id IS NOT NULL;
