# HSN / Tax Master Store-Scoping Audit

**Auditor:** Sub-Agent B (Database Auditor) — RESEARCH ONLY, no migrations executed.
**Date:** 2026-08-30
**Scope:** `migrations/`, `internal/repository/`, `internal/tax/`, `internal/models/`, `internal/database/`, `cmd/server`, `cmd/seed`.

---

## 1. Schemas (quoted from migrations)

### 1.1 `hsn_codes` — `migrations/012_create_hsn_codes.sql`

```sql
CREATE TABLE hsn_codes (                                   -- 012:2
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code        VARCHAR(20) NOT NULL UNIQUE,               -- 012:4  → constraint hsn_codes_code_key
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_hsn_codes_code ON hsn_codes (code);       -- 012:9 (dropped later in 024:4 as redundant with UNIQUE)
```

- **No `store_id`.** No `updated_at`, no `updated_at/deleted_at`.
- The `code UNIQUE` is an **inline column constraint**; PostgreSQL auto-names it `hsn_codes_code_key`.

### 1.2 `tax_rates` — `migrations/013_create_tax_rates.sql` + `022_add_constraints.sql`

```sql
CREATE TABLE tax_rates (                                   -- 013:2
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hsn_code_id    UUID NOT NULL REFERENCES hsn_codes(id) ON DELETE CASCADE,   -- 013:4
    gst_rate       NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    cgst_rate      NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    sgst_rate      NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    igst_rate      NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    cess_rate      NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    effective_from DATE NOT NULL,
    effective_to   DATE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tax_rates_hsn ON tax_rates (hsn_code_id);            -- 013:15
CREATE INDEX idx_tax_rates_effective ON tax_rates (effective_from, effective_to);  -- 013:16

CREATE UNIQUE INDEX uq_tax_rates_active_per_hsn ON tax_rates (hsn_code_id)
    WHERE effective_to IS NULL;                            -- 013:19-20
```

CHECK constraints (`022_add_constraints.sql:18-24`):

```sql
ALTER TABLE tax_rates
    ADD CONSTRAINT chk_tr_gst_rate CHECK (gst_rate >= 0 AND gst_rate <= 100),     -- 022:19
    ADD CONSTRAINT chk_tr_cess_rate CHECK (cess_rate >= 0 AND cess_rate <= 100);  -- 022:20

ALTER TABLE tax_rates
    ADD CONSTRAINT chk_tr_effective CHECK (effective_to IS NULL OR effective_to > effective_from);  -- 022:24
```

- **No `store_id`.**

### 1.3 `medicine_tax_config` — `migrations/014_create_medicine_tax_config.sql` + `022`

```sql
CREATE TABLE medicine_tax_config (                         -- 014:2
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    medicine_id       UUID NOT NULL REFERENCES medicines(id) ON DELETE CASCADE,    -- 014:4
    hsn_code_id       UUID NOT NULL REFERENCES hsn_codes(id) ON DELETE CASCADE,    -- 014:5
    tax_rate_id       UUID NOT NULL REFERENCES tax_rates(id) ON DELETE CASCADE,    -- 014:6
    price_includes_tax BOOLEAN NOT NULL DEFAULT false,
    effective_from    DATE NOT NULL,
    effective_to      DATE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_medicine_tax_config_medicine ON medicine_tax_config (medicine_id);      -- 014:13
CREATE INDEX idx_medicine_tax_config_effective ON medicine_tax_config (effective_from, effective_to);  -- 014:14

CREATE UNIQUE INDEX uq_medicine_tax_config_active ON medicine_tax_config (medicine_id)
    WHERE effective_to IS NULL;                           -- 014:17-18
```

CHECK constraint (`022_add_constraints.sql:25-26`):

```sql
ALTER TABLE medicine_tax_config
    ADD CONSTRAINT chk_mtc_effective CHECK (effective_to IS NULL OR effective_to > effective_from);
```

- **No `store_id`.**

### 1.4 `medicines` — `0001_medicines.sql` + `022` + `025` + `030`

```sql
CREATE TABLE medicines (                                  -- 0001:1
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              VARCHAR(255) NOT NULL,
    salt_composition  TEXT NOT NULL DEFAULT '',
    manufacturer      VARCHAR(255) NOT NULL DEFAULT '',
    min_reorder_level INT NOT NULL DEFAULT 0,
    packing           VARCHAR(100) NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ
);
```

Later additions:
- `uqc VARCHAR(10) NOT NULL DEFAULT 'NOS'` — `025_gst_returns.sql:64`
- `store_id UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE` — `030_store_scoping.sql:6-7, 28`
- `chk_medicine_reorder CHECK (min_reorder_level >= 0)` — `022:34-35`

### 1.5 `stores` — `migrations/011_create_business_gst_registrations.sql` + `029_auth_core.sql`

```sql
CREATE TABLE stores (                                     -- 011:29
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    gst_registration_id  UUID REFERENCES gst_registrations(id) ON DELETE SET NULL,   -- 011:31
    name                 TEXT NOT NULL,
    address              TEXT NOT NULL DEFAULT '',
    is_active            BOOLEAN NOT NULL DEFAULT true,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_stores_gst_registration ON stores (gst_registration_id);   -- 011:39
```

Later addition (`029_auth_core.sql:39-41`):

```sql
ALTER TABLE stores
    ADD COLUMN max_employees INT NOT NULL DEFAULT 2,
    ADD CONSTRAINT chk_stores_max_employees CHECK (max_employees >= 0);
```

`stores` is the parent of every scoped entity. The app uses "first store by `created_at`" as the single-tenant default (`FirstStoreID`, `internal/repository/helpers.go:16-27`; migrate adopt in 030:21-26).

---

## 2. Do the three tables currently have `store_id`?

**Confirmed: NO.** `hsn_codes`, `tax_rates`, and `medicine_tax_config` have **no `store_id` column**. Grep across all migrations shows the only tables these three reference are:
- `tax_rates.hsn_code_id → hsn_codes(id)` (013:4)
- `medicine_tax_config.medicine_id → medicines(id)` (014:4)
- `medicine_tax_config.hsn_code_id → hsn_codes(id)` (014:5)
- `medicine_tax_config.tax_rate_id → tax_rates(id)` (014:6)

They are currently **global/shared reference data**, while everything else (medicines, batches, customers, suppliers, invoices, POs, GSTR-2B, audit logs, memberships) is already store-scoped.

---

## 3. `stores` table — see §1.5 above.

---

## 4. How `medicines` got `store_id` (migration 030) + the project's store-uniqueness style

`migrations/030_store_scoping.sql` establishes the canonical **adopt-then-NOT-NULL** pattern:

```sql
ALTER TABLE medicines
    ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;      -- 030:6-7

UPDATE medicines        SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL;  -- 030:21

ALTER TABLE medicines ALTER COLUMN store_id SET NOT NULL;                  -- 030:28

CREATE INDEX idx_medicines_store ON medicines (store_id);                  -- 030:34
```

The **store-scoped unique** style is `uq_customers_store_phone` (030:40-43), which first **drops the global constraint** then creates a per-store unique index:

```sql
ALTER TABLE customers DROP CONSTRAINT customers_phone_key;
CREATE UNIQUE INDEX uq_customers_store_phone ON customers (store_id, phone);
```

Other applications of the same style:
- `constraint uq_store_memberships_user UNIQUE (store_id, user_id)` — 029:25
- `CREATE UNIQUE INDEX uq_active_owner_per_store ON store_memberships (store_id) WHERE role = 'STORE_OWNER' AND is_active = true;` — 029:33-35 (partial unique)
- Cross-entity store integrity is enforced with a **trigger**, `f_verify_batch_store` / `trg_batches_store_match` (030:47-60), which raises on any store mismatch between `batches` and `medicines`.

---

## 5. Effective-dating mechanism + historical tax snapshots (IMMUTABLE)

### 5.1 Effective dating
- `tax_rates` and `medicine_tax_config` both carry `effective_from DATE NOT NULL` / `effective_to DATE` with `chk_tr_effective` / `chk_mtc_effective` ordering checks (022:24, 022:26).
- "Active" = `effective_to IS NULL`, enforced by the two **partial unique indexes**:
  - `uq_tax_rates_active_per_hsn ON tax_rates (hsn_code_id) WHERE effective_to IS NULL` — 013:19-20
  - `uq_medicine_tax_config_active ON medicine_tax_config (medicine_id) WHERE effective_to IS NULL` — 014:17-18
- Upserts close the active row then insert a new one:
  - `UpsertTaxRate` — `internal/repository/tax_repo.go:174-198` (`UPDATE tax_rates SET effective_to = CURRENT_DATE WHERE hsn_code_id = $1 AND effective_to IS NULL` then `INSERT`)
  - `UpsertMedicineTaxConfig` — `internal/repository/tax_repo.go:202-226` (same pattern on `medicine_id`)
  - `GetActiveTaxRate` — `tax_repo.go:70-91` (`WHERE hsn_code_id = $1 AND effective_to IS NULL ORDER BY effective_from DESC LIMIT 1`)
  - `GetMedicineTaxConfig` — `tax_repo.go:22-55` (`effective_from <= asOf AND (effective_to IS NULL OR effective_to >= asOf)`)
- `BackdateTaxConfig` (`tax_repo.go:251-261`) anchors `effective_from` for the deterministic seed.

### 5.2 Historical snapshots — CONFIRMED IMMUTABLE
Invoice/purchase line items do **NOT** reference `hsn_codes`/`tax_rates` via FK. They store flat **VARCHAR + NUMERIC snapshot columns** written at posting time:

- `sales_invoice_items` — `migrations/017_alter_sales_invoice_items_gst.sql:3-16`:
  `hsn_code VARCHAR(20)`, `gross_amount`, `taxable_value`, `gst_rate`, `cgst_rate`, `cgst_amount`, `sgst_rate`, `sgst_amount`, `igst_rate`, `igst_amount`, `cess_rate`, `cess_amount`, `line_total` (all NUMERIC).
- `purchase_order_items` — `migrations/020_alter_purchase_order_items_gst.sql:2-15`: identical snapshot columns.
- `sales_credit_note_items` — `migrations/018_create_sales_credit_notes.sql:20-35`: `hsn_code`, `taxable_value`, `gst_rate`, `cgst_amount`, `sgst_amount`, `igst_amount`, `cess_amount`, `line_total`.
- Header totals on `sales_invoices` (016:4-18) and `purchase_orders` (019:3-18) mirror the same immutability.
- Written by the repos at posting time:
  - `sale_repo.go:560-578` — `INSERT INTO sales_invoice_items (... hsn_code, gross_amount, taxable_value, gst_rate, cgst_rate, ... line_total)`.
  - `purchase_repo.go:447-469` — `INSERT INTO purchase_order_items (... hsn_code, gross_amount, taxable_value, gst_rate, cgst_rate, ... line_total)`.
- **Regression-tested as immutable:** `internal/repository/gst_test.go:698-736` — after changing the tax master for an HSN from 12% to 18%, it re-reads the historical line and asserts: *"Historical invoice tax was mutated: got rate=... want 12/90/90"* and the comment *"Historical invoice tax must be unchanged: snapshotted at invoice time, never recalculated against the current tax master."*

**Conclusion:** migration 033 can rebuild the master tables' uniqueness without touching any historical invoice/purchase row. It never needs to and must not modify `sales_invoice_items`, `purchase_order_items`, or `sales_credit_note_items`.

---

## 6. Migration numbering / embedding / runtime application — next number is `033`

- **Embedding:** `migrations/embed.go` uses `//go:embed *.sql` into `var FS embed.FS`.
- **Runtime application:** `internal/database/database.go:38-86` `Migrate()`:
  1. creates `schema_migrations(version TEXT PK, applied_at)` (database.go:39-45),
  2. lists `migrations.FS.ReadDir(".")`, **`sort.Strings(names)`** → so filenames must sort lexicographically (zero-padded `012`…`032`),
  3. skips already-recorded versions, runs each SQL in its **own transaction** (`pgx.BeginFunc`), then records the version (database.go:73-79).
- **Callers:** `cmd/server/main.go:38` (`database.Migrate` before serving) and `cmd/seed/main.go:87`.
- Highest existing migration is `032_stock_audit_requests.sql` (`migrations/032_*`). **The next sequential number is `033`.** A `033_*.sql` file sorts after `032_*` and before nothing else; it is safe.

---

## 7. Existing unique constraints (full inventory)

| Location | Constraint / Index | Definition |
|---|---|---|
| `012:4` | `hsn_codes_code_key` (inline `UNIQUE`, auto-named) | `code VARCHAR(20) NOT NULL UNIQUE` — **global, table-wide** |
| `013:19-20` | `uq_tax_rates_active_per_hsn` (partial unique index) | `ON tax_rates (hsn_code_id) WHERE effective_to IS NULL` — **global, one active rate per HSN regardless of store** |
| `014:17-18` | `uq_medicine_tax_config_active` (partial unique index) | `ON medicine_tax_config (medicine_id) WHERE effective_to IS NULL` — **global, one active config per medicine regardless of store** |
| `030:42-43` | `uq_customers_store_phone` (store-scoped unique index) | `ON customers (store_id, phone)` — **the project's per-store uniqueness style** |
| `029:25` | `uq_store_memberships_user` | `ON store_memberships (store_id, user_id)` |
| `029:33-35` | `uq_active_owner_per_store` (partial unique) | `ON store_memberships (store_id) WHERE role='STORE_OWNER' AND is_active=true` |
| `025:14` | `uq_invoice_sequences` | `ON invoice_sequences (store_id, financial_year, prefix)` |

`idx_hsn_codes_code` (012:9) was already dropped as redundant (`024_fix_indexes.sql:4`).

---

## 8. `tax_rates` GST column storage

All split rates are `NUMERIC(5,2) NOT NULL DEFAULT 0.00`:
- `gst_rate` = total GST %
- `cgst_rate` + `sgst_rate` = intra-state split (typically half each)
- `igst_rate` = inter-state tax (equal to `gst_rate`; seeded `12.00, 6.00, 6.00, 12.00` in 021:17)
- `cess_rate` = additional cess %

Bounded by `chk_tr_gst_rate` (0..100) and `chk_tr_cess_rate` (0..100) (022:19-20). Effective window via `effective_from`/`effective_to` + `chk_tr_effective`. The Go model mirrors this: `models.TaxRate` (models.go:67-78), `tax.TaxRate` (internal/tax/types.go:36-42). Seeded defaults for 3004/3003/3002/3001/2106/9983 in 021_seed (12%, 12%, 12%, 0%, 5%, 18%).

---

## 9. Recommendations

### 9.1 Add `store_id` to all three tables — YES
`hsn_codes`, `tax_rates`, and `medicine_tax_config` sit on the *path* `store → medicine → medicine_tax_config → (hsn_codes, tax_rates)`. Today a store's medicine can point at another store's (or the shared) HSN/tax master, and the HSN/tax/edit of store A leaks into store B's catalog/rates. All other entities are already store-scoped. **Add `store_id` to all three**, following the 030 adopt-then-NOT-NULL pattern exactly:

```sql
ALTER TABLE hsn_codes ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;
ALTER TABLE tax_rates ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;
ALTER TABLE medicine_tax_config ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;

UPDATE hsn_codes            SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL;
UPDATE tax_rates            SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL;
UPDATE medicine_tax_config  SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL;

ALTER TABLE hsn_codes           ALTER COLUMN store_id SET NOT NULL;
ALTER TABLE tax_rates           ALTER COLUMN store_id SET NOT NULL;
ALTER TABLE medicine_tax_config ALTER COLUMN store_id SET NOT NULL;
```

(If a *fresh* DB runs 021 before 033, the seeded rows exist and are adopted to the first store, so NOT NULL is safe. The `ON DELETE CASCADE` matches 030's style.)

### 9.2 Proposed migration: `033_store_hsn_tax_scoping.sql`

Order of operations (all inside the single-transaction migration):

1. **Add `store_id` + adopt + `SET NOT NULL`** as in §9.1.
2. **Drop the global `code` UNIQUE on `hsn_codes`, replace with per-store unique index:**
   ```sql
   ALTER TABLE hsn_codes DROP CONSTRAINT hsn_codes_code_key;
   CREATE UNIQUE INDEX uq_hsn_codes_store_code ON hsn_codes (store_id, code);
   ```
   (Auto-generated constraint name `hsn_codes_code_key` follows the exact precedent used for `customers_phone_key` in 030:42 and `sales_invoices_invoice_no_key` in 025:17.)
3. **Drop the global partial unique indexes, recreate store-scoped:**
   ```sql
   DROP INDEX uq_tax_rates_active_per_hsn;
   CREATE UNIQUE INDEX uq_tax_rates_active_per_hsn_store
       ON tax_rates (store_id, hsn_code_id) WHERE effective_to IS NULL;

   DROP INDEX uq_medicine_tax_config_active;
   CREATE UNIQUE INDEX uq_medicine_tax_config_active_store
       ON medicine_tax_config (store_id, medicine_id) WHERE effective_to IS NULL;
   ```
4. **Per-store lookup indexes** (030 pattern):
   ```sql
   CREATE INDEX idx_hsn_codes_store           ON hsn_codes (store_id);
   CREATE INDEX idx_tax_rates_store           ON tax_rates (store_id);
   CREATE INDEX idx_medicine_tax_config_store ON medicine_tax_config (store_id);
   ```
5. **Cross-entity store consistency trigger** (mirrors `f_verify_batch_store`, 030:47-60) so a store's `medicine_tax_config.store_id` must equal the `store_id` of its `medicines`, its `hsn_codes`, and its `tax_rates`:
   ```sql
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
       BEFORE INSERT OR UPDATE OF store_id, medicine_id, hsn_code_id, tax_rate_id ON medicine_tax_config
       FOR EACH ROW EXECUTE FUNCTION f_verify_medicine_tax_store();
   ```

### 9.3 Historical snapshots must NOT be touched — CONFIRMED
The invoice/purchase master tables carry **no FK** to `hsn_codes`/`tax_rates`; they store `hsn_code VARCHAR(20)` + `NUMERIC` rates/amounts frozen at posting time (migrations 017/019/020/018; `sale_repo.go:560-578`, `purchase_repo.go:447-469`; immutable-by-test in `gst_test.go:698-736`). Migration 033 only modifies the three master tables' constraints/indexes and backfills their new `store_id`; it neither touches nor recalculates any `sales_invoice_items` / `purchase_order_items` / `sales_credit_note_items` rows. GST returns (GSTR-1/3B/2B) read the snapshot columns + header totals, so per-store masters do not alter historical liability.

### 9.4 FK columns needing store consistency
- `tax_rates.hsn_code_id → hsn_codes(id)` — a tax rate must reference an HSN of the same store.
- `medicine_tax_config.hsn_code_id → hsn_codes(id)` — same-store HSN.
- `medicine_tax_config.tax_rate_id → tax_rates(id)` — same-store rate (which itself must be same-store *and* same-HSN).
- `medicine_tax_config.medicine_id → medicines(id)` — same-store medicine.

The scalar FKs cannot express "same store as the parent"; only the trigger in §9.2 can. This matches the existing design decision for `batches.medicine_id` (030:45-60).

### 9.5 Downstream code impact (NOTE — outside migration, for the implementer)
- `TaxRepo` is currently constructed without a store (`repository.NewTaxRepo(pool)` in `cmd/server/main.go:61`) and all its queries are unscoped (`tax_repo.go:22-260`). It will need a `*storeIDRef`-style pin (see `store_id.go`) with `store_id` filters on every `hsn_codes`/`tax_rates`/`medicine_tax_config` query, and `handlers/tax.go` will need the store passed through.
- Seed comments state `hsn_codes`/`tax_rates` are "master data seeded by migrations… preserved" (`cmd/seed/main.go:342, 68`); after 033 those rows belong to the first store. Fine for the single-store seed, but a multi-tenant bootstrap should seed per-store defaults.
- `gst_test.go:37-68` self-seeds `hsn_codes`/`tax_rates`/`medicine_tax_config` directly and would need a `store_id` in those INSERTs once the column is NOT NULL.

---

## Quick answers

1. **Full schemas:** §1 (with exact SQL + file:line).
2. **store_id on the three tables today:** **NO** (confirmed).
3. **stores schema:** §1.5 (011:29-37 + 029:39-41).
4. **medicines store_id + uniqueness style:** migration 030 (adopt first store → NOT NULL → per-store unique); style = `uq_customers_store_phone` (030:42-43), `uq_store_memberships_user` (029:25), partial `uq_active_owner_per_store` (029:33-35).
5. **Effective-dating:** `effective_from`/`effective_to` + partial unique `WHERE effective_to IS NULL` (013:19-20, 014:17-18). Historical tax is snapshotted immutably in `sales_invoice_items`/`purchase_order_items`/`sales_credit_note_items` (017/020/018) — **confirmed**.
6. **Numbering/embedding:** `embed.go` + `database.Migrate` (database.go:38-86), lexicographic sort, per-migration tx. **Next number: `033`.**
7. **Existing unique constraints:** §7 table.
8. **tax_rates GST storage:** `NUMERIC(5,2)` `gst_rate`/`cgst_rate`/`sgst_rate`/`igst_rate`/`cess_rate` with CHECK bounds (§8).