-- 036: Composite indexes backing the tenant-scoped read paths hardened for
-- IDOR resistance. Every history/listing query filters by store_id first, so
-- the leading column is always store_id:
--
--   sales_invoices:  ListInvoices / GetInvoiceByNo filter
--                    (store_id, invoice_date) and (store_id, customer_id).
--   purchase_orders: ListInvoices filters (store_id, invoice_date)
--                    (complements the partial idx_po_invoice_date_store).
--   batches:         checkout locks (id, store_id) — the PK covers id;
--                    the store guard filters the single fetched row.
--   customers:       checkout credit checks (id, store_id) — same shape.
--
-- All statements are IF NOT EXISTS so the migration is safe to replay.

CREATE INDEX IF NOT EXISTS idx_sales_invoices_store_date
    ON sales_invoices (store_id, invoice_date DESC);

CREATE INDEX IF NOT EXISTS idx_sales_invoices_store_customer
    ON sales_invoices (store_id, customer_id);

CREATE INDEX IF NOT EXISTS idx_purchase_orders_store_date
    ON purchase_orders (store_id, invoice_date DESC);
