-- 022: Add CHECK constraints for data integrity (defense-in-depth)

-- Bonus quantity non-negative
ALTER TABLE purchase_order_items
    ADD CONSTRAINT chk_poi_bonus_quantity CHECK (bonus_quantity >= 0);

-- Purchase order item prices non-negative
ALTER TABLE purchase_order_items
    ADD CONSTRAINT chk_poi_purchase_price CHECK (purchase_price >= 0),
    ADD CONSTRAINT chk_poi_sale_price CHECK (sale_price >= 0);

-- Sales invoice item non-negative values
ALTER TABLE sales_invoice_items
    ADD CONSTRAINT chk_sii_unit_sale_price CHECK (unit_sale_price >= 0),
    ADD CONSTRAINT chk_sii_subtotal CHECK (subtotal >= 0);

-- Tax rate bounds
ALTER TABLE tax_rates
    ADD CONSTRAINT chk_tr_gst_rate CHECK (gst_rate >= 0 AND gst_rate <= 100),
    ADD CONSTRAINT chk_tr_cess_rate CHECK (cess_rate >= 0 AND cess_rate <= 100);

-- Effective date ordering
ALTER TABLE tax_rates
    ADD CONSTRAINT chk_tr_effective CHECK (effective_to IS NULL OR effective_to > effective_from);
ALTER TABLE medicine_tax_config
    ADD CONSTRAINT chk_mtc_effective CHECK (effective_to IS NULL OR effective_to > effective_from);

-- Customer type: backfill empty values then add constraint
UPDATE customers SET customer_type = 'B2C' WHERE customer_type = '' OR customer_type IS NULL;
ALTER TABLE customers
    ADD CONSTRAINT chk_customer_type CHECK (customer_type IN ('B2C', 'B2B'));

-- Medicine reorder level non-negative
ALTER TABLE medicines
    ADD CONSTRAINT chk_medicine_reorder CHECK (min_reorder_level >= 0);
