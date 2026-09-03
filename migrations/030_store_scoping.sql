-- 030: Store isolation — every business entity belongs to exactly one store.
-- Existing rows are adopted by the first (oldest) store so already-seeded demo
-- data keeps belonging to its current owner. A fresh database has no rows to
-- adopt, so the NOT NULL columns apply from the start.

ALTER TABLE medicines
    ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;

ALTER TABLE batches
    ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;

ALTER TABLE customers
    ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;

ALTER TABLE suppliers
    ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;

ALTER TABLE reconciliation_journals
    ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;

UPDATE medicines        SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL;
UPDATE batches          SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL;
UPDATE customers        SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL;
UPDATE suppliers        SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL;
UPDATE reconciliation_journals
    SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL;

ALTER TABLE medicines ALTER COLUMN store_id SET NOT NULL;
ALTER TABLE batches ALTER COLUMN store_id SET NOT NULL;
ALTER TABLE customers ALTER COLUMN store_id SET NOT NULL;
ALTER TABLE suppliers ALTER COLUMN store_id SET NOT NULL;
ALTER TABLE reconciliation_journals ALTER COLUMN store_id SET NOT NULL;

CREATE INDEX idx_medicines_store ON medicines (store_id);
CREATE INDEX idx_batches_store ON batches (store_id);
CREATE INDEX idx_customers_store ON customers (store_id);
CREATE INDEX idx_suppliers_store ON suppliers (store_id);
CREATE INDEX idx_reconciliation_journals_store ON reconciliation_journals (store_id, created_at DESC);

-- The same customer can exist in two independent stores: the global UNIQUE
-- phone constraint becomes a per-store one.
ALTER TABLE customers DROP CONSTRAINT customers_phone_key;
CREATE UNIQUE INDEX uq_customers_store_phone ON customers (store_id, phone);

-- A batch can never belong to a different store than its medicine.
-- (Verified with a trigger so direct SQL and application bugs abort loudly.)
CREATE OR REPLACE FUNCTION f_verify_batch_store() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.store_id IS DISTINCT FROM (SELECT store_id FROM medicines WHERE id = NEW.medicine_id) THEN
        RAISE EXCEPTION 'batch store % does not match medicine store %',
            NEW.store_id, (SELECT store_id FROM medicines WHERE id = NEW.medicine_id)
            USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_batches_store_match
    BEFORE INSERT OR UPDATE OF store_id, medicine_id ON batches
    FOR EACH ROW EXECUTE FUNCTION f_verify_batch_store();

-- Who created a document (audit trail: the owner on a direct entry, the
-- employee whose approved request produced the row on approval; NULL for
-- rows written before auth existed).
ALTER TABLE sales_invoices
    ADD COLUMN created_by UUID REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE purchase_orders
    ADD COLUMN created_by UUID REFERENCES users(id) ON DELETE SET NULL;

-- verified_by_user_id now points at a real login.
ALTER TABLE reconciliation_journals
    ADD CONSTRAINT fk_reconciliation_verified_by_user
    FOREIGN KEY (verified_by_user_id) REFERENCES users(id) ON DELETE SET NULL;