-- 025: GST Return Generation Support (GSTR-1, GSTR-3B)
-- Adds invoice sequencing, UQC, credit note enhancements, and performance indexes.

-- 1. Sequential Invoice Numbers per Financial Year
CREATE TABLE invoice_sequences (
    store_id       UUID REFERENCES stores(id) ON DELETE CASCADE,
    financial_year VARCHAR(7) NOT NULL,  -- e.g. '2026-27'
    prefix         VARCHAR(20) NOT NULL DEFAULT 'INV/',
    last_value     INT NOT NULL DEFAULT 0
);

-- Unique constraint: each (store_id, financial_year, prefix) combination
-- NULL store_id is allowed for stores without GST registration
CREATE UNIQUE INDEX uq_invoice_sequences ON invoice_sequences (store_id, financial_year, prefix);

-- Convert sales_invoices.invoice_no from BIGSERIAL to VARCHAR(16)
ALTER TABLE sales_invoices DROP CONSTRAINT IF EXISTS sales_invoices_invoice_no_key;
ALTER TABLE sales_invoices ALTER COLUMN invoice_no DROP DEFAULT;
ALTER TABLE sales_invoices ALTER COLUMN invoice_no TYPE VARCHAR(16) USING invoice_no::text;

-- Backfill existing rows with sequential numbers using a CTE
WITH numbered AS (
    SELECT id, ROW_NUMBER() OVER (ORDER BY created_at, id) AS rn
    FROM sales_invoices
)
UPDATE sales_invoices si
SET invoice_no = 'INV/' || LPAD(n.rn::text, 5, '0')
FROM numbered n
WHERE si.id = n.id;

ALTER TABLE sales_invoices ALTER COLUMN invoice_no SET NOT NULL;

-- Add new columns to sales_invoices
ALTER TABLE sales_invoices ADD COLUMN invoice_date DATE NOT NULL DEFAULT CURRENT_DATE;
ALTER TABLE sales_invoices ADD COLUMN financial_year VARCHAR(7) NOT NULL DEFAULT '';

-- Backfill invoice_date and financial_year from created_at for existing records
UPDATE sales_invoices
SET invoice_date = created_at::date;

UPDATE sales_invoices
SET financial_year = CASE
    WHEN EXTRACT(MONTH FROM created_at) >= 4 THEN
        EXTRACT(YEAR FROM created_at)::text || '-' || RIGHT(EXTRACT(YEAR FROM created_at + interval '1 year')::text, 2)
    ELSE
        (EXTRACT(YEAR FROM created_at) - 1)::text || '-' || RIGHT(EXTRACT(YEAR FROM created_at)::text, 2)
    END
WHERE financial_year = '';

-- 2. Purchase Invoice Date Tracking
ALTER TABLE purchase_orders ADD COLUMN invoice_date DATE NOT NULL DEFAULT CURRENT_DATE;
UPDATE purchase_orders SET invoice_date = created_at::date;
ALTER TABLE purchase_orders ADD COLUMN financial_year VARCHAR(7) NOT NULL DEFAULT '';
UPDATE purchase_orders
SET financial_year = CASE
    WHEN EXTRACT(MONTH FROM created_at) >= 4 THEN
        EXTRACT(YEAR FROM created_at)::text || '-' || RIGHT(EXTRACT(YEAR FROM created_at + interval '1 year')::text, 2)
    ELSE
        (EXTRACT(YEAR FROM created_at) - 1)::text || '-' || RIGHT(EXTRACT(YEAR FROM created_at)::text, 2)
    END
WHERE financial_year = '';

-- 3. Unit Quantity Code (UQC) for HSN Summaries
ALTER TABLE medicines ADD COLUMN uqc VARCHAR(10) NOT NULL DEFAULT 'NOS';

-- 4. Credit/Debit Notes Enhancement (CDNR/CDNUR)
-- Change note_no from BIGSERIAL to VARCHAR(16)
ALTER TABLE sales_credit_notes DROP CONSTRAINT IF EXISTS sales_credit_notes_note_no_key;
ALTER TABLE sales_credit_notes ALTER COLUMN note_no DROP DEFAULT;
ALTER TABLE sales_credit_notes ALTER COLUMN note_no TYPE VARCHAR(16) USING note_no::text;

-- Backfill existing credit note numbers using a CTE
WITH numbered AS (
    SELECT id, ROW_NUMBER() OVER (ORDER BY created_at, id) AS rn
    FROM sales_credit_notes
)
UPDATE sales_credit_notes scn
SET note_no = 'CN/' || LPAD(n.rn::text, 5, '0')
FROM numbered n
WHERE scn.id = n.id;

ALTER TABLE sales_credit_notes ALTER COLUMN note_no SET NOT NULL;

-- Add new columns
ALTER TABLE sales_credit_notes ADD COLUMN note_date DATE NOT NULL DEFAULT CURRENT_DATE;
ALTER TABLE sales_credit_notes ADD COLUMN original_invoice_no VARCHAR(16);
ALTER TABLE sales_credit_notes ADD COLUMN original_invoice_date DATE;
ALTER TABLE sales_credit_notes ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE SET NULL;
ALTER TABLE sales_credit_notes ADD COLUMN financial_year VARCHAR(7) NOT NULL DEFAULT '';
ALTER TABLE sales_credit_notes ADD COLUMN customer_gstin VARCHAR(15);
ALTER TABLE sales_credit_notes ADD COLUMN supply_type TEXT;

-- Backfill note_date from created_at
UPDATE sales_credit_notes SET note_date = created_at::date;

-- Backfill credit note fields from parent invoice
UPDATE sales_credit_notes scn
SET original_invoice_no = si.invoice_no,
    original_invoice_date = si.invoice_date,
    store_id = si.store_id,
    financial_year = si.financial_year,
    customer_gstin = si.customer_gstin,
    supply_type = si.supply_type
FROM sales_invoices si
WHERE scn.invoice_id = si.id
  AND scn.original_invoice_no IS NULL;

-- 5. Performance Indexes
CREATE INDEX idx_si_created_store ON sales_invoices (created_at, store_id) WHERE store_id IS NOT NULL;
CREATE INDEX idx_si_invoice_date_store ON sales_invoices (invoice_date, store_id) WHERE store_id IS NOT NULL;
CREATE INDEX idx_si_financial_year ON sales_invoices (financial_year, store_id);
CREATE INDEX idx_scn_invoice_date ON sales_credit_notes (note_date, store_id) WHERE store_id IS NOT NULL;
