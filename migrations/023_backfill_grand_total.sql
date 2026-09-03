-- 023: Backfill NULL grand_total for pre-GST records and make NOT NULL

-- Backfill sales_invoices
UPDATE sales_invoices SET grand_total = total_amount WHERE grand_total IS NULL;
ALTER TABLE sales_invoices
    ALTER COLUMN grand_total SET DEFAULT 0.00,
    ALTER COLUMN grand_total SET NOT NULL;

-- Backfill purchase_orders
UPDATE purchase_orders SET grand_total = total_amount WHERE grand_total IS NULL;
ALTER TABLE purchase_orders
    ALTER COLUMN grand_total SET DEFAULT 0.00,
    ALTER COLUMN grand_total SET NOT NULL;
