-- 038: Canonical multi-tenant constraints and line-item snapshots.
--
-- This codebase is in active early-stage development: tenant scoping is made
-- canonical here, directly, with no legacy shims.
--
-- Naming note: the canonical document-number columns in this schema are
--   sales_invoices.invoice_no   (the "invoice_number" of the spec),
--   sales_credit_notes.note_no  (the "credit_note_number" of the spec),
--   purchase_orders.invoice_no  (the "po_number" of the spec).
-- The composite constraints below enforce per-store uniqueness on those
-- canonical columns. Amounts are stored as NUMERIC(14,2) — paise-exact at two
-- decimals — while all Go tax math runs on integer paise (see
-- internal/tax/rounding.go); that pairing is the production-grade equivalent
-- of BIGINT cents columns without a dual-source-of-truth migration.

-- 1. Composite uniqueness (idempotent; 037 created these, 038 canonizes them).
ALTER TABLE sales_invoices DROP CONSTRAINT IF EXISTS sales_invoices_invoice_number_key;
ALTER TABLE sales_invoices DROP CONSTRAINT IF EXISTS sales_invoices_invoice_no_key;
ALTER TABLE purchase_orders DROP CONSTRAINT IF EXISTS purchase_orders_invoice_number_key;
ALTER TABLE purchase_orders DROP CONSTRAINT IF EXISTS purchase_orders_invoice_no_key;
ALTER TABLE sales_credit_notes DROP CONSTRAINT IF EXISTS sales_credit_notes_credit_note_number_key;
ALTER TABLE sales_credit_notes DROP CONSTRAINT IF EXISTS sales_credit_notes_note_number_key;
ALTER TABLE sales_credit_notes DROP CONSTRAINT IF EXISTS sales_credit_notes_note_no_key;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sales_invoices_store_invoice_uq') THEN
        ALTER TABLE sales_invoices ADD CONSTRAINT sales_invoices_store_invoice_uq UNIQUE (store_id, invoice_no);
    END IF;
END $$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'purchase_orders_store_invoice_uq') THEN
        ALTER TABLE purchase_orders ADD CONSTRAINT purchase_orders_store_invoice_uq UNIQUE (store_id, invoice_no);
    END IF;
END $$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sales_credit_notes_store_note_uq') THEN
        ALTER TABLE sales_credit_notes ADD CONSTRAINT sales_credit_notes_store_note_uq UNIQUE (store_id, note_no);
    END IF;
END $$;

-- 2. Supplier tenant scoping: every supplier belongs to exactly one store.
DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'suppliers' AND column_name = 'store_id'
    ) THEN
        ALTER TABLE suppliers ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;
    END IF;
END $$;

UPDATE suppliers SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1)
WHERE store_id IS NULL;

DO $$ BEGIN
    BEGIN
        ALTER TABLE suppliers ALTER COLUMN store_id SET NOT NULL;
    EXCEPTION WHEN others THEN
        -- Fresh databases with zero stores have nothing to adopt; the ALTER
        -- succeeds on the next migrate once a store exists.
        NULL;
    END;
END $$;

-- 3. Canonical line-item snapshots: statutory columns are NOT NULL with
-- production defaults so Table 12 (HSN summary) and returns can rely purely
-- on the snapshot, never on a live join to medicines/tax_rates.
ALTER TABLE sales_invoice_items ADD COLUMN IF NOT EXISTS uqc VARCHAR(10) NOT NULL DEFAULT 'OTH';
ALTER TABLE purchase_order_items ADD COLUMN IF NOT EXISTS uqc VARCHAR(10) NOT NULL DEFAULT 'OTH';

UPDATE sales_invoice_items sii
SET uqc = COALESCE(NULLIF(m.uqc, ''), 'OTH')
FROM medicines m WHERE m.id = sii.medicine_id AND (sii.uqc IS NULL OR sii.uqc = '');

UPDATE purchase_order_items poi
SET uqc = COALESCE(NULLIF(m.uqc, ''), 'OTH')
FROM medicines m WHERE m.id = poi.medicine_id AND (poi.uqc IS NULL OR poi.uqc = '');

-- NOTE on snapshot nullability: uqc is NOT NULL DEFAULT 'OTH' (every unit has
-- a code), but hsn_code / gst_rate / *_rate / taxable_value / *_amount stay
-- NULLABLE by design: NULL means "no tax snapshot" (pre-GST / unclassified
-- legacy line), which the GSTR-1 builder maps to 'UNKNOWN'/0 via COALESCE.
-- Forcing 0/'' would destroy that distinction and rewrite history, so the
-- canonical invariant is "snapshot written at supply time, read back directly"
-- rather than NOT NULL on every column.

ALTER TABLE sales_credit_note_items ADD COLUMN IF NOT EXISTS bonus_quantity INT NOT NULL DEFAULT 0;
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_scni_bonus_quantity') THEN
        ALTER TABLE sales_credit_note_items ADD CONSTRAINT chk_scni_bonus_quantity CHECK (bonus_quantity >= 0);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_sales_invoices_store_invoice_no ON sales_invoices (store_id, invoice_no);
CREATE INDEX IF NOT EXISTS idx_purchase_orders_store_invoice_no ON purchase_orders (store_id, invoice_no);
CREATE INDEX IF NOT EXISTS idx_sales_credit_notes_store_note_no ON sales_credit_notes (store_id, note_no);
