-- 037: Per-store document uniqueness + line-item UQC snapshot.
--
-- Part A — Drop legacy global unique constraints and replace them with
-- composite per-tenant uniqueness so two independent stores can each issue
-- their own INV-0001 / purchase invoice / credit-note sequences without
-- colliding on a single global key (multi-tenant IDOR / constraint collision).
--
--   sales_invoices:  (store_id, invoice_no)   — invoice_no currently has NO
--                    unique key (025 dropped the global BIGSERIAL key and never
--                    added a replacement); add the composite.
--   purchase_orders: (store_id, invoice_no)   — currently GLOBAL UNIQUE on
--                    invoice_no (purchase_orders_invoice_no_key); drop it first.
--   sales_credit_notes: (store_id, note_no)   — currently NO unique key (025
--                    dropped the global key); add the composite.
--
-- store_id is NULLABLE on these document tables (legacy rows predate store
-- scoping), and PostgreSQL treats NULLs as distinct in UNIQUE constraints, so
-- adding the composite is backward-compatible: legacy NULL-store rows never
-- conflict, while every scoped store gets strict per-store uniqueness.
--
-- All statements are idempotent (IF EXISTS / DO-block on pg_constraint) so the
-- migration is safe to replay.

-- Legacy global keys that may still exist under either naming convention.
ALTER TABLE sales_invoices DROP CONSTRAINT IF EXISTS sales_invoices_invoice_number_key;
ALTER TABLE sales_invoices DROP CONSTRAINT IF EXISTS sales_invoices_invoice_no_key;
ALTER TABLE purchase_orders DROP CONSTRAINT IF EXISTS purchase_orders_invoice_number_key;
ALTER TABLE sales_credit_notes DROP CONSTRAINT IF EXISTS sales_credit_notes_note_number_key;
ALTER TABLE sales_credit_notes DROP CONSTRAINT IF EXISTS sales_credit_notes_note_no_key;

-- purchase_orders still carries its global UNIQUE on invoice_no from 0005.
ALTER TABLE purchase_orders DROP CONSTRAINT IF EXISTS purchase_orders_invoice_no_key;

-- Composite per-store uniqueness (store_id, document number).
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

-- Part B — Snapshot UQC on line items.
--
-- GST Table 12 (HSN summary) must report the Unit Quantity Code that applied
-- at the moment of supply. Joining sales_invoice_items -> medicines at report
-- time is wrong: a later UQC edit on the medicine master would rewrite history.
-- Snapshot the medicine's UQC onto every line item at write time and read it
-- back directly in the GSTR-1 builder.
--
-- 'OTH' is the GSTN fallback UQC for unclassified units; legacy rows are
-- backfilled from their medicine master so historical HSN summaries keep the
-- UQC that was current when the migration runs.

ALTER TABLE sales_invoice_items ADD COLUMN IF NOT EXISTS uqc VARCHAR(10) NOT NULL DEFAULT 'OTH';
ALTER TABLE purchase_order_items ADD COLUMN IF NOT EXISTS uqc VARCHAR(10) NOT NULL DEFAULT 'OTH';

-- Backfill historical lines from the medicine master (best-effort snapshot).
UPDATE sales_invoice_items sii
SET uqc = COALESCE(NULLIF(m.uqc, ''), 'OTH')
FROM medicines m
WHERE m.id = sii.medicine_id
  AND (sii.uqc IS NULL OR sii.uqc = 'OTH');

UPDATE purchase_order_items poi
SET uqc = COALESCE(NULLIF(m.uqc, ''), 'OTH')
FROM medicines m
WHERE m.id = poi.medicine_id
  AND (poi.uqc IS NULL OR poi.uqc = 'OTH');

-- Credit-note return lines must track the bonus (free) quantity that is being
-- returned alongside the billed quantity, so inventory restock can restore the
-- FULL physical quantity (billed + bonus) that originally left the batch.
ALTER TABLE sales_credit_note_items ADD COLUMN IF NOT EXISTS bonus_quantity INT NOT NULL DEFAULT 0;
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_scni_bonus_quantity') THEN
        ALTER TABLE sales_credit_note_items ADD CONSTRAINT chk_scni_bonus_quantity CHECK (bonus_quantity >= 0);
    END IF;
END $$;

-- Helpful indexes for the new tenant-scoped lookups.
CREATE INDEX IF NOT EXISTS idx_sales_invoices_store_invoice_no ON sales_invoices (store_id, invoice_no);
CREATE INDEX IF NOT EXISTS idx_purchase_orders_store_invoice_no ON purchase_orders (store_id, invoice_no);
CREATE INDEX IF NOT EXISTS idx_sales_credit_notes_store_note_no ON sales_credit_notes (store_id, note_no);
CREATE INDEX IF NOT EXISTS idx_sales_invoice_items_uqc ON sales_invoice_items (uqc) WHERE uqc IS NOT NULL;
