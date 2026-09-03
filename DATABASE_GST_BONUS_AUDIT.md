# Database / Migration Audit — PharmaPOS GST & Bonus Discount

**Auditor:** Database / Migration Auditor
**Date:** 2026-08-26
**Scope:** All 21 migrations (`0001`–`021`), Go models, repository SQL queries

---

## 1. Schema Inventory

| Table | Migration | Key Columns | Notes |
|---|---|---|---|
| `medicines` | 0001 | id (UUID PK), name, salt_composition, manufacturer, min_reorder_level, packing, deleted_at | Soft-delete pattern |
| `batches` | 0002 | id (UUID PK), medicine_id (FK), batch_number, expiry_date, purchase_price, sale_price, current_stock | UNIQUE(medicine_id, batch_number) |
| `customers` | 0003, 015 | id (UUID PK), name, phone (UNIQUE), credit_limit, current_balance, gstin, customer_type, state, state_code | GST fields added in 015 |
| `sales_invoices` | 0004, 0007, 016 | id (UUID PK), invoice_no (BIGSERIAL), customer_id (FK), payment_type, total_amount, discount_total, + 14 nullable GST columns | GST columns added in 016 |
| `sales_invoice_items` | 0004, 0007, 017 | id (UUID PK), invoice_id (FK RESTRICT), medicine_id (FK), batch_id (FK), quantity, unit_sale_price, subtotal, discount columns, + 13 nullable GST snapshot columns | GST snapshots added in 017 |
| `purchase_orders` | 0005, 0009, 019 | id (UUID PK), invoice_no (VARCHAR UNIQUE), supplier_name, total_amount, discount_total, supplier_id (FK), + 13 nullable GST columns | Dual supplier ref |
| `purchase_order_items` | 0005, 0009, 020 | id (UUID PK), purchase_id (FK CASCADE), medicine_id (FK), batch_number, expiry_date, quantity, bonus_quantity, purchase_price, sale_price, discount columns, + 13 nullable GST snapshot columns | No batch_id FK |
| `reconciliation_journals` | 0006 | id (UUID PK), verified_by_user_id, notes | |
| `reconciliation_items` | 0006 | id (UUID PK), journal_id (FK CASCADE), medicine_id (FK), batch_id (FK), system_stock, physical_stock, variance_quantity, cost_impact | |
| `customer_ledger` | 0008 | id (UUID PK), customer_id (FK CASCADE), entry_type (CHECK IN), amount, balance_after, notes | Composite index on (customer_id, created_at) |
| `suppliers` | 010 | id (UUID PK), legal_name, trade_name, gstin, pan, address, state, state_code, phone, email | Partial index on gstin |
| `businesses` | 011 | id (UUID PK), legal_name, trade_name | |
| `gst_registrations` | 011 | id (UUID PK), business_id (FK CASCADE), gstin, state_code, is_active | Partial index on gstin |
| `stores` | 011 | id (UUID PK), gst_registration_id (FK SET NULL), name, address, is_active | |
| `hsn_codes` | 012 | id (UUID PK), code (VARCHAR UNIQUE), description | |
| `tax_rates` | 013 | id (UUID PK), hsn_code_id (FK CASCADE), gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from, effective_to | Partial unique on effective_to IS NULL |
| `medicine_tax_config` | 014 | id (UUID PK), medicine_id (FK CASCADE), hsn_code_id (FK CASCADE), tax_rate_id (FK CASCADE), price_includes_tax, effective_from, effective_to | Partial unique on effective_to IS NULL |
| `sales_credit_notes` | 018 | id (UUID PK), invoice_id (FK RESTRICT), note_no (BIGSERIAL), reason, gross/tax/cgst/sgst/igst/cess totals, grand_total | |
| `sales_credit_note_items` | 018 | id (UUID PK), credit_note_id (FK CASCADE), invoice_item_id (FK SET NULL), medicine_id (FK), batch_id (FK), quantity, GST snapshot columns | |

---

## 2. Findings

### F-01: Dual supplier reference on `purchase_orders` — inconsistency risk
**Files:** `migrations/0005_purchases.sql:4`, `migrations/019_alter_purchase_orders_gst.sql:4`

`purchase_orders` carries **both** `supplier_name VARCHAR(255) NOT NULL` (text, from 0005) and `supplier_id UUID REFERENCES suppliers(id)` (FK, from 019). There is **no constraint** ensuring they agree. If a supplier's `legal_name` is updated in the `suppliers` table, the denormalized `supplier_name` on existing POs becomes stale.

**Severity:** Medium — data drift over time, reporting inconsistencies (report_repo.go:124 groups by `supplier_name`).

**Recommendation:** Either (a) add a deferred trigger that syncs `supplier_name` from `suppliers.legal_name` on UPDATE, or (b) migrate all reads to JOIN through `supplier_id` and deprecate the text column. Option (b) is cleaner but requires a data migration.

---

### F-02: `purchase_orders.grand_total` is nullable — null-dereference risk
**Files:** `migrations/019_alter_purchase_orders_gst.sql:17`, `internal/models/models.go:230`

`grand_total` is `NUMERIC(14,2)` with **no DEFAULT and no NOT NULL**. Pre-GST POs will have `grand_total = NULL`. The Go model correctly maps this as `*float64`, but any frontend or API consumer that does `invoice.grand_total + invoice.discount_total` without nil-checking will crash or produce null propagation.

**Severity:** Medium — silent data corruption if frontend arithmetic doesn't guard against null.

**Recommendation:** Add `DEFAULT 0.00` or COALESCE in all read queries. Alternatively, backfill `grand_total = total_amount` for pre-GST records and add `NOT NULL DEFAULT 0.00`.

---

### F-03: `sales_invoices.grand_total` is nullable — same risk as F-02
**Files:** `migrations/016_alter_sales_invoices_gst.sql:17`, `internal/models/models.go:176`

Identical issue to F-02. Pre-GST invoices have `grand_total = NULL`.

**Severity:** Medium.

---

### F-04: `price_includes_tax` is nullable BOOLEAN — three-state ambiguity
**Files:** `migrations/016_alter_sales_invoices_gst.sql:18`, `migrations/019_alter_purchase_orders_gst.sql:18`, `internal/models/models.go:177,231`

`price_includes_tax BOOLEAN DEFAULT NULL` creates three states: `TRUE`, `FALSE`, `NULL`. For pre-GST records it's `NULL`. Application code in `sale_repo.go:418` sets it to `invoiceResult != nil` (boolean), never NULL for GST invoices. But the schema allows NULL, meaning the three-state is semantically overloaded: NULL = "unknown/pre-GST", FALSE = "price excludes tax", TRUE = "price includes tax".

**Severity:** Low — works in practice but is a design smell. A non-nullable column with a sentinel value (e.g., `'UNKNOWN'` text) or separate `is_gst_applicable` flag would be clearer.

---

### F-05: `sales_invoices` GST totals have inconsistent nullability
**Files:** `migrations/016_alter_sales_invoices_gst.sql:9-18`

The following columns have `DEFAULT 0.00` but are **nullable** (no NOT NULL):
- `cgst_total`, `sgst_total`, `igst_total`, `cess_total`, `tax_total`, `round_off`

While these are NOT nullable:
- (none — `total_amount` is NOT NULL from 0004)

And these have **no default and are nullable**:
- `gross_amount`, `taxable_amount`, `grand_total`

This means for pre-GST invoices: `cgst_total = 0.00` but `grand_total = NULL`. For the application this works (it checks `hasTaxConfig`), but it's confusing for ad-hoc SQL queries — some zero means "no tax" while some NULL means "no tax".

**Severity:** Low — cosmetic inconsistency, but can confuse ad-hoc reporting.

**Recommendation:** Either make ALL GST columns nullable with no defaults (pure NULL = "pre-GST"), or add `NOT NULL DEFAULT 0.00` to all of them and use a separate `is_gst_applicable BOOLEAN` flag.

---

### F-06: `purchase_order_items` missing CHECK on `bonus_quantity >= 0`
**Files:** `migrations/0009_purchase_bonus_discount.sql:3`

`bonus_quantity INT NOT NULL DEFAULT 0` has no CHECK constraint. Application code in `purchase_repo.go:72` validates `it.BonusQuantity < 0`, but a direct SQL insert could set it to -1.

**Severity:** Low — only exploitable via direct SQL, but violates defense-in-depth.

---

### F-07: `purchase_order_items` missing CHECK on `purchase_price >= 0` and `sale_price >= 0`
**Files:** `migrations/0005_purchases.sql:18-19`

`batches` (0002) has `CHECK (purchase_price >= 0)` and `CHECK (sale_price >= 0)`, but `purchase_order_items` does not. The Go code in `purchase_repo.go:74` validates this, but the DB doesn't enforce it.

**Severity:** Low — defense-in-depth gap.

---

### F-08: `sales_invoice_items` missing CHECK on `unit_sale_price >= 0` and `subtotal >= 0`
**Files:** `migrations/0004_sales.sql:26-27`

Neither `unit_sale_price` nor `subtotal` has a non-negative CHECK constraint. Application code doesn't explicitly validate these either (though `quantity > 0` and positive `salePrice` on the batch implicitly prevent it).

**Severity:** Low.

---

### F-09: `tax_rates` missing CHECK constraints on rate values
**Files:** `migrations/013_create_tax_rates.sql:5-9`

No CHECK constraints ensure:
- `gst_rate >= 0 AND gst_rate <= 100`
- `cgst_rate >= 0`, `sgst_rate >= 0`, `igst_rate >= 0`, `cess_rate >= 0`
- `gst_rate = cgst_rate + sgst_rate` (for intra-state)

A typo could insert `gst_rate = 120.00` or `cgst_rate = -5.00`.

**Severity:** Medium — data integrity risk for tax configuration.

---

### F-10: No CHECK on `effective_to > effective_from` in temporal tables
**Files:** `migrations/013_create_tax_rates.sql:11`, `migrations/014_create_medicine_tax_config.sql:9`

Both `tax_rates` and `medicine_tax_config` have `effective_from DATE NOT NULL` and `effective_to DATE` (nullable). There is no CHECK constraint ensuring `effective_to > effective_from` when `effective_to` is not null. A record with `effective_from = '2025-01-01'` and `effective_to = '2024-01-01'` would be silently accepted.

**Severity:** Medium — logical data corruption.

---

### F-11: `customers.customer_type` has no CHECK constraint
**Files:** `migrations/015_alter_customers_gst.sql:5`

`customer_type TEXT NOT NULL DEFAULT 'B2C'` — the application only uses `'B2C'` and presumably `'B2B'`, but the DB accepts any text.

**Severity:** Low — enum-like field without enforcement.

**Recommendation:** Add `CHECK (customer_type IN ('B2C', 'B2B'))` or use a PostgreSQL ENUM type.

---

### F-12: `medicines.min_reorder_level` has no CHECK constraint
**Files:** `migrations/0001_medicines.sql:6`

`min_reorder_level INT NOT NULL DEFAULT 0` — no `CHECK (min_reorder_level >= 0)`. Application code in `purchase_repo.go:78` validates this, but the DB doesn't.

**Severity:** Low.

---

### F-13: Redundant index `idx_hsn_codes_code`
**Files:** `migrations/012_create_hsn_codes.sql:9`

```sql
CREATE INDEX idx_hsn_codes_code ON hsn_codes (code);
```
But `code VARCHAR(20) NOT NULL UNIQUE` (line 4) already creates an implicit unique index. This is a fully redundant index wasting storage and slowing writes.

**Severity:** Low — performance waste.

---

### F-14: Duplicate index `idx_sales_invoices_created_at_disc`
**Files:** `migrations/0007_discounts.sql:9`

```sql
CREATE INDEX idx_sales_invoices_created_at_disc ON sales_invoices (created_at);
```
Migration 0004 already created:
```sql
CREATE INDEX idx_sales_invoices_created_at ON sales_invoices (created_at);
```
These are **identical** indexes on the same column. One should be dropped.

**Severity:** Low — performance waste, ~2x write overhead on this table.

---

### F-15: `purchase_orders.invoice_no` uses ILIKE for search — index not utilized
**Files:** `internal/repository/invoice_repo.go:152`

```sql
AND ($3 = '' OR po.invoice_no ILIKE '%' || $3 || '%')
```
The `UNIQUE` index on `invoice_no` cannot serve a leading-wildcard ILIKE. This forces a sequential scan on `purchase_orders` for every search.

**Severity:** Medium — performance issue as purchase_orders grow.

**Recommendation:** Add a trigram index: `CREATE INDEX idx_purchase_orders_invoice_no_trgm ON purchase_orders USING gin (invoice_no gin_trgm_ops);` (requires `pg_trgm` extension).

---

### F-16: Missing composite index for `report_repo.go` purchase grouping
**Files:** `internal/repository/report_repo.go:129-134`

The purchase summary query groups by `supplier_name` with a `WHERE created_at >= $1 AND created_at < $2` filter. The existing `idx_purchase_orders_created_at` helps with the range scan, but a composite index `(created_at, supplier_name, total_amount)` would be covering and avoid heap lookups.

**Severity:** Low — performance optimization.

---

### F-17: Missing composite index for `report_repo.go` sales daily grouping
**Files:** `internal/repository/report_repo.go:73-80`

The daily sales query groups by `(date, payment_type)` with `WHERE created_at >= $1 AND created_at < $2`. A composite index `(created_at, payment_type, total_amount)` would be covering.

**Severity:** Low — performance optimization.

---

### F-18: `reconciliation_items.system_stock` has no CHECK constraint
**Files:** `migrations/0006_reconciliation.sql:13`

`system_stock INT NOT NULL` — no `CHECK (system_stock >= 0)`. While system stock should always be non-negative (enforced by batch `current_stock >= 0`), the reconciliation item is a snapshot and could theoretically capture a transient negative value.

**Severity:** Low.

---

### F-19: `purchase_order_items` has no FK to `batches`
**Files:** `migrations/0005_purchases.sql:11-20`

`purchase_order_items` stores `batch_number VARCHAR(100)` but not a `batch_id UUID` FK. This is by design (batch is created during purchase), but it means there's no referential integrity between purchase line items and the batch they created. Historical purchase items can't be directly JOINed to their resulting batch.

**Severity:** Low — by design, but noted for completeness. A post-hoc migration could add a `batch_id` column populated from the batch upsert.

---

### F-20: No constraint ensuring `grand_total = taxable_amount + tax_total + round_off`
**Files:** All GST migration files

The application computes these values, but there's no DB-level CHECK or generated column ensuring the arithmetic holds. A corrupted `grand_total` would pass all constraints.

**Severity:** Low — application-computed invariant, but worth noting.

---

### F-21: `seed` migration 021 uses `ON CONFLICT DO NOTHING` on `tax_rates`
**Files:** `migrations/021_seed_hsn_tax_rates.sql:19,24,29,34,39,44`

The `ON CONFLICT DO NOTHING` is on the whole row (no conflict target specified). Since there's no UNIQUE constraint on `(hsn_code_id, effective_from)` or similar, this will only conflict on the PK (which is auto-generated). In practice, this means re-running migration 021 will **insert duplicate rows** for each HSN code's tax rate.

**Severity:** Medium — re-running migrations or running on a partially-seeded database creates duplicate tax rates.

**Recommendation:** Add `ON CONFLICT (hsn_code_id) DO NOTHING` (relies on `uq_tax_rates_active_per_hsn` partial unique) or use `ON CONFLICT ON CONSTRAINT uq_tax_rates_active_per_hsn DO NOTHING`.

---

### F-22: `sales_invoices` missing index for `invoice_no::text LIKE` search
**Files:** `internal/repository/invoice_repo.go:60`

```sql
AND ($3 = '' OR si.invoice_no::text LIKE '%' || $3 || '%')
```
The `BIGSERIAL UNIQUE` on `invoice_no` won't serve leading-wildcard LIKE. Sequential scan for search.

**Severity:** Low — BIGSERIAL means sequential numbers, so search is fast even without index. But as invoice counts grow, this degrades.

---

### F-23: `batches.current_stock` can be decremented below 0 via race condition
**Files:** `internal/repository/sale_repo.go:461-471`

The sale code does:
```sql
UPDATE batches SET current_stock = current_stock - $2
WHERE id = $1 AND current_stock >= $2
```
With a prior `FOR UPDATE` lock, this is safe. But the `FOR UPDATE` is on the batch rows locked at the start of the transaction. If a concurrent reconciliation or adjustment modifies stock between the lock and the update, the `WHERE current_stock >= $2` check catches it. This is correct.

**Severity:** None — properly handled.

---

## 3. Missing Constraints

Add these via a new migration:

```sql
-- F-06: bonus_quantity non-negative
ALTER TABLE purchase_order_items
    ADD CONSTRAINT chk_poi_bonus_quantity CHECK (bonus_quantity >= 0);

-- F-07: purchase_order_items price non-negative
ALTER TABLE purchase_order_items
    ADD CONSTRAINT chk_poi_purchase_price CHECK (purchase_price >= 0),
    ADD CONSTRAINT chk_poi_sale_price CHECK (sale_price >= 0);

-- F-08: sales_invoice_items non-negative values
ALTER TABLE sales_invoice_items
    ADD CONSTRAINT chk_sii_unit_sale_price CHECK (unit_sale_price >= 0),
    ADD CONSTRAINT chk_sii_subtotal CHECK (subtotal >= 0);

-- F-09: tax_rates rate bounds
ALTER TABLE tax_rates
    ADD CONSTRAINT chk_tr_gst_rate CHECK (gst_rate >= 0 AND gst_rate <= 100),
    ADD CONSTRAINT chk_tr_cgst_rate CHECK (cgst_rate >= 0 AND cgst_rate <= 100),
    ADD CONSTRAINT chk_tr_sgst_rate CHECK (sgst_rate >= 0 AND sgst_rate <= 100),
    ADD CONSTRAINT chk_tr_igst_rate CHECK (igst_rate >= 0 AND igst_rate <= 100),
    ADD CONSTRAINT chk_tr_cess_rate CHECK (cess_rate >= 0 AND cess_rate <= 100);

-- F-10: effective_to > effective_from
ALTER TABLE tax_rates
    ADD CONSTRAINT chk_tr_effective CHECK (effective_to IS NULL OR effective_to > effective_from);
ALTER TABLE medicine_tax_config
    ADD CONSTRAINT chk_mtc_effective CHECK (effective_to IS NULL OR effective_to > effective_from);

-- F-11: customer_type enum
ALTER TABLE customers
    ADD CONSTRAINT chk_customer_type CHECK (customer_type IN ('B2C', 'B2B'));

-- F-12: min_reorder_level non-negative
ALTER TABLE medicines
    ADD CONSTRAINT chk_medicine_reorder CHECK (min_reorder_level >= 0);

-- F-18: system_stock non-negative
ALTER TABLE reconciliation_items
    ADD CONSTRAINT chk_ri_system_stock CHECK (system_stock >= 0);
```

---

## 4. Missing Indexes

```sql
-- F-15: Trigram index for purchase invoice search
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX idx_purchase_orders_invoice_no_trgm
    ON purchase_orders USING gin (invoice_no gin_trgm_ops);

-- F-16: Covering index for purchase summary report
CREATE INDEX idx_purchase_orders_date_supplier
    ON purchase_orders (created_at, supplier_name, total_amount);

-- F-17: Covering index for daily sales report
CREATE INDEX idx_sales_invoices_date_payment
    ON sales_invoices (created_at, payment_type, total_amount);
```

Drop redundant indexes:
```sql
-- F-13
DROP INDEX IF EXISTS idx_hsn_codes_code;

-- F-14
DROP INDEX IF EXISTS idx_sales_invoices_created_at_disc;
```

---

## 5. Dangerous Patterns

### D-01: Nullable `grand_total` on invoices — null arithmetic
**Tables:** `sales_invoices`, `purchase_orders`

When `grand_total IS NULL`, any code doing `grand_total + round_off` or `grand_total - discount` will propagate NULL. The Go models use `*float64` which handles this, but JSON serialization will emit `"grand_total": null` which may confuse frontend code.

**Mitigation:** Backfill NULL `grand_total` with `total_amount` for pre-GST records, then set `NOT NULL DEFAULT 0.00`.

### D-02: Nullable `price_includes_tax` — three-state BOOLEAN
**Tables:** `sales_invoices`, `purchase_orders`, `medicine_tax_config`

NULL means "not applicable" (pre-GST), but it's overloaded with the actual boolean meaning. Application code never sets it to NULL for GST invoices, but the schema allows it.

**Mitigation:** Add a generated column or application invariant: `price_includes_tax IS NOT NULL WHEN supply_type IS NOT NULL`.

### D-03: `supplier_name` / `supplier_id` drift
**Table:** `purchase_orders`

No trigger or constraint keeps `supplier_name` in sync with `suppliers.legal_name`. After a supplier name update, historical POs show the old name while new POs show the new name, making reports inconsistent.

**Mitigation:** Add an AFTER UPDATE trigger on `suppliers` that propagates name changes to `purchase_orders`, or deprecate `supplier_name` in favor of JOINs.

### D-04: No unique constraint on `(hsn_code_id, effective_from)` in `tax_rates`
**Table:** `tax_rates`

Without this, you can insert two tax rates with the same HSN code starting on the same date (both with `effective_to IS NULL` would be blocked by `uq_tax_rates_active_per_hsn`, but you could have overlapping ranges like `(2024-01-01, 2024-06-01)` and `(2024-03-01, NULL)`).

**Mitigation:** Add a exclusion constraint or trigger ensuring no overlapping effective ranges per HSN code.

### D-05: No unique constraint on `(medicine_id, effective_from)` in `medicine_tax_config`
**Table:** `medicine_tax_config`

Same overlap risk as D-04. The partial unique `uq_medicine_tax_config_active` only prevents multiple rows with `effective_to IS NULL`, but overlapping date ranges are possible.

**Mitigation:** Same as D-04.

---

## 6. Migration Strategy

### M-01: New migration `022_add_missing_constraints.sql`

Add all CHECK constraints from Section 3. These are all backward-compatible because:
- Existing data satisfies the constraints (application code already enforces them)
- `ALTER TABLE ... ADD CONSTRAINT` with CHECK will scan existing rows and fail only if violated
- No data loss, no column changes

### M-02: New migration `023_fix_indexes.sql`

- Drop `idx_hsn_codes_code` (F-13)
- Drop `idx_sales_invoices_created_at_disc` (F-14)
- Add trigram index for invoice search (F-15)
- Add covering indexes (F-16, F-17)

### M-03: New migration `024_backfill_grand_total.sql`

Backfill NULL `grand_total` values:
```sql
UPDATE sales_invoices SET grand_total = total_amount WHERE grand_total IS NULL;
UPDATE purchase_orders SET grand_total = total_amount WHERE grand_total IS NULL;
```
Then add `NOT NULL DEFAULT 0.00`:
```sql
ALTER TABLE sales_invoices ALTER COLUMN grand_total SET NOT NULL, ALTER COLUMN grand_total SET DEFAULT 0.00;
ALTER TABLE purchase_orders ALTER COLUMN grand_total SET NOT NULL, ALTER COLUMN grand_total SET DEFAULT 0.00;
```

### M-04: New migration `025_seed_hsn_tax_rates_idempotent.sql`

Replace 021's `ON CONFLICT DO NOTHING` with proper upserts that target the correct conflict constraint.

### Migration Safety Notes

- All migrations in 0001–021 are **backward-compatible** — they only ADD columns or CREATE new tables, never DROP or RENAME.
- Migration 0008 (customer_ledger backfill) is the only one that INSERTs data; it uses `ON CONFLICT`-free logic but targets a newly-created table, so it's safe.
- Migration 021 (seed data) is **NOT idempotent** for `tax_rates` — re-running creates duplicates (F-21).
- No migrations drop columns or tables, so no data loss risk.
- Migration numbering is sequential and conflict-free (0001–0009 use 4-digit prefix, 010–021 use 3-digit; both are valid).

---

## Summary of Issue Severity

| Severity | Count | Issues |
|---|---|---|
| **High** | 0 | — |
| **Medium** | 5 | F-01, F-02, F-03, F-09, F-10, F-15, F-21 |
| **Low** | 12 | F-04, F-05, F-06, F-07, F-08, F-11, F-12, F-13, F-14, F-16, F-17, F-18, F-19, F-20 |
| **None** | 1 | F-23 (properly handled) |

**Total findings:** 20 issues (0 high, 7 medium, 13 low)
