-- 033: Store-isolate the HSN / tax master. This completes migration 030 (which
-- scoped medicines/batches/customers/suppliers but NOT the HSN/tax reference data).
-- Historical invoice/purchase rows carry flat snapshot columns (VARCHAR hsn_code +
-- NUMERIC rates/amounts) and have no FK to these tables, so they are untouched.
-- Follows the 030 pattern: add nullable store_id, adopt to the first store, NOT NULL.

ALTER TABLE hsn_codes
    ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;

ALTER TABLE tax_rates
    ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;

ALTER TABLE medicine_tax_config
    ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;

UPDATE hsn_codes            SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL AND EXISTS (SELECT 1 FROM stores);
UPDATE tax_rates            SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL AND EXISTS (SELECT 1 FROM stores);
UPDATE medicine_tax_config  SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL AND EXISTS (SELECT 1 FROM stores);

-- Fresh-install path: when zero stores exist yet (production boot migrates
-- before the first /api/auth/register), the pre-tenancy seed rows from 021
-- cannot belong to any tenant and store-scoped queries could never see them.
-- Drop those orphan seeds so the NOT NULL invariant below holds on fresh
-- databases too; per-store HSN/tax masters are created via the API (or
-- cmd/seed) once the first store exists. Upgraded databases always have a
-- store here, so their adopted rows are untouched.
DELETE FROM medicine_tax_config WHERE store_id IS NULL;
DELETE FROM tax_rates            WHERE store_id IS NULL;
DELETE FROM hsn_codes            WHERE store_id IS NULL;

ALTER TABLE hsn_codes            ALTER COLUMN store_id SET NOT NULL;
ALTER TABLE tax_rates            ALTER COLUMN store_id SET NOT NULL;
ALTER TABLE medicine_tax_config  ALTER COLUMN store_id SET NOT NULL;

-- Per-store uniqueness replaces the three global constraints.
ALTER TABLE hsn_codes DROP CONSTRAINT hsn_codes_code_key;
CREATE UNIQUE INDEX uq_hsn_codes_store_code ON hsn_codes (store_id, code);

DROP INDEX uq_tax_rates_active_per_hsn;
CREATE UNIQUE INDEX uq_tax_rates_active_per_hsn_store
    ON tax_rates (store_id, hsn_code_id) WHERE effective_to IS NULL;

DROP INDEX uq_medicine_tax_config_active;
CREATE UNIQUE INDEX uq_medicine_tax_config_active_store
    ON medicine_tax_config (store_id, medicine_id) WHERE effective_to IS NULL;

-- Per-store lookup indexes.
CREATE INDEX idx_hsn_codes_store           ON hsn_codes (store_id);
CREATE INDEX idx_tax_rates_store           ON tax_rates (store_id);
CREATE INDEX idx_medicine_tax_config_store ON medicine_tax_config (store_id);

-- Cross-entity store integrity (mirrors f_verify_batch_store from 030): a
-- medicine_tax_config must share a store with its medicine, its HSN and its tax rate.
CREATE OR REPLACE FUNCTION f_verify_medicine_tax_store() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.store_id IS DISTINCT FROM (SELECT store_id FROM medicines WHERE id = NEW.medicine_id)
    OR NEW.store_id IS DISTINCT FROM (SELECT store_id FROM hsn_codes WHERE id = NEW.hsn_code_id)
    OR NEW.store_id IS DISTINCT FROM (SELECT store_id FROM tax_rates WHERE id = NEW.tax_rate_id) THEN
        RAISE EXCEPTION 'medicine tax config store % mismatches linked medicine/hsn/tax_rate',
            NEW.store_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_medicine_tax_config_store_match
    BEFORE INSERT OR UPDATE OF store_id, medicine_id, hsn_code_id, tax_rate_id
    ON medicine_tax_config
    FOR EACH ROW EXECUTE FUNCTION f_verify_medicine_tax_store();

-- A tax rate must reference an HSN of the same store.
CREATE OR REPLACE FUNCTION f_verify_tax_rate_store() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.store_id IS DISTINCT FROM (SELECT store_id FROM hsn_codes WHERE id = NEW.hsn_code_id) THEN
        RAISE EXCEPTION 'tax rate store % does not match hsn store', NEW.store_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_tax_rates_store_match
    BEFORE INSERT OR UPDATE OF store_id, hsn_code_id
    ON tax_rates
    FOR EACH ROW EXECUTE FUNCTION f_verify_tax_rate_store();
