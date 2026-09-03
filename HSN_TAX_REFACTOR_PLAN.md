# HSN / GST Store-Isolated Tax Configuration — Coordinator Refactor Plan

**Coordinator:** Lead Agent (reviewed all four sub-agent audits + verified the code first-hand).
**Date:** 2026-08-30
**Inputs:** `HSN_API_AUDIT.md`, `HSN_DB_AUDIT.md`, `HSN_FRONTEND_AUDIT.md`, `HSN_TEST_PLAN.md`.

---

## 0. Executive summary

The HSN / tax domain is the **one place store isolation was never implemented**.
`hsn_codes`, `tax_rates` and `medicine_tax_config` are **globally shared**, while
every other business entity (medicines, batches, customers, suppliers,
invoices, purchase orders) is already store-scoped via migration `030`.

The task requires full store-level isolation (per-store HSN sets, per-store
rates, per-store uniqueness). The agreed design: **add `store_id` to all three
tables**, mirroring migration `030`'s adopt-then-NOT-NULL pattern, and drive all
scoping from the authenticated principal's store (pinned single-tenant repos in
this codebase).

All four audits agree this is the correct and only consistent approach. The
real-world "HSN is a national catalog" concern is overridden by **Test 2** which
explicitly requires Store A's HSN list `{3004,3005}` to exclude Store B's
`3006` — i.e. HSN sets differ per store. So `hsn_codes` itself must be
store-scoped, not just rates.

---

## 1. APIs — reuse vs new

### 1.1 Existing APIs REUSED (no changes to contract)

| Endpoint | Handler | Used for |
|---|---|---|
| `GET /api/hsn` | `tax.go:listHSNCodes` | Store-scoped HSN list (extended to filter by store) |
| `POST /api/hsn` | `tax.go:createHSNCode` | Create HSN (extended to write `store_id`; owner-gated) |
| `PUT /api/hsn/:id/tax-rate` | `tax.go:upsertTaxRate` | Update tax rate (extended store scope; owner-gated) |
| `GET /api/medicines/:id/tax-config` | `tax.go:getMedicineTaxConfig` | Read a medicine's tax config (owner-checked) |
| `PUT /api/medicines/:id/tax-config` | `tax.go:upsertMedicineTaxConfig` | Assign/update a medicine's config (owner-gated) |
| `GET /api/medicines/:id/detail` | `medicines.go:getMedicineDetail` | Medicine detail incl. embedded `tax_config` |
| `GET/POST /api/purchases` | purchase flow | Purchase inward; already creates HSN inline |

### 1.2 New API (only ONE, justified)

| Endpoint | Justification |
|---|---|
| `GET /api/sync/tax` | The frontend offline cache needs a **bulk** store-scoped snapshot of HSN codes + active rates + all medicine tax configs so POS/Purchases/Medicines can read from IndexedDB without N+1 calls. No existing endpoint returns this snapshot (`/api/sync/inventory` returns only medicines+batches; `GET /api/hsn` returns only codes). Mirrors existing `/api/sync/inventory` and `/api/sync/customers`. |

**No duplicate HSN/tax CRUD endpoints are introduced.** All writes reuse the
existing `/api/hsn` + `/api/medicines/:id/tax-config` surface.

---

## 2. Database changes — migration `033_store_hsn_tax_scoping.sql`

New sequential migration (next valid number after `032`; lexicographic sort via
`//go:embed *.sql` + `Migrate()`).

```sql
-- 033: Store-isolate the HSN / tax master (completes migration 030).
-- Follows the same adopt-then-NOT-NULL pattern as 030.

ALTER TABLE hsn_codes            ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;
ALTER TABLE tax_rates            ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;
ALTER TABLE medicine_tax_config  ADD COLUMN store_id UUID REFERENCES stores(id) ON DELETE CASCADE;

UPDATE hsn_codes            SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL;
UPDATE tax_rates            SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL;
UPDATE medicine_tax_config  SET store_id = (SELECT id FROM stores ORDER BY created_at LIMIT 1) WHERE store_id IS NULL;

ALTER TABLE hsn_codes            ALTER COLUMN store_id SET NOT NULL;
ALTER TABLE tax_rates            ALTER COLUMN store_id SET NOT NULL;
ALTER TABLE medicine_tax_config  ALTER COLUMN store_id SET NOT NULL;

-- Per-store uniqueness (replaces the three global constraints).
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

-- Cross-entity store integrity (mirrors f_verify_batch_store from 030).
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
```

**Historical safety:** `sales_invoice_items`, `purchase_order_items`,
`sales_credit_note_items` carry **flat `hsn_code` + NUMERIC rate/amount snapshot
columns** and have **no FK** to these master tables (017/019/020/018;
`sale_repo.go:560-578`, `purchase_repo.go:447-469`). This migration only
rewrites the three master tables' constraints/indexes and backfills `store_id`;
it **never touches** historical invoice/purchase rows. Already regression-tested
immutable (`gst_test.go:698-736`).

---

## 3. Backend — TaxRepo store scoping

Change `NewTaxRepo(db *pgxpool.Pool)` → `NewTaxRepo(db *pgxpool.Pool, storeID string)`
(matches `NewMedicineRepo`, `NewCustomerRepo`, `NewSupplierRepo`), giving it a
`*storeIDRef` pin. **Every** query against `hsn_codes` / `tax_rates` /
`medicine_tax_config` gains `AND store_id = r.storeID(ctx)` (or the store ref
resolved per call):

- `GetMedicineTaxConfig` (:22) — add store predicate on `mtc`.
- `GetHSNByCode` (:58) — add store predicate. **Signature change:** needs store
  scope → `GetHSNByCode(ctx, storeID, code)` or reuse pinned ref. To keep the
  existing callers (checkout tax resolver, seed) working with minimal churn, the
  pinned ref is preferred; but the checkout path resolves tax config from within
  a repo that already has medicine scope. **Decision:** guest/pass store from the
  pinned `storeIDRef` in `TaxRepo`, and keep `GetHSNByCode(ctx, code)` internally
  scoped by the pin. Where the repo is used in the checkout (sales) path, the
  store is the same pinned tenant.
- `GetActiveTaxRate` (:70) — add store predicate (via join or store col).
- `ListHSNCodes` (:154) — add `WHERE store_id = ...`; **returns per-store set**.
- `UpsertTaxRate` (:174) — write `store_id` on insert; scope the end-active
  `UPDATE ... AND store_id = ...`.
- `UpsertMedicineTaxConfig` (:202) — write `store_id`; scope end-active update.
- `CreateHSNCode` (:229) — write `store_id` on insert; translate a
  `isUniqueViolation` (SQLSTATE 23505) into a friendly duplicate error so the
  handler can respond **409** (mirrors `auth_repo.go` usage; required by Test 14).
- New `ListStoreTaxSnapshot(ctx)` — returns `{ hsn_codes[], tax_configs[] }` for
  the pinned store (HSN codes each with active rate; all medicine tax configs
  each with embedded hsn + rate). Used by the new sync endpoint.

`GetMedicineTaxConfigByMedicine` and `GetDefaultStore`/`GetStore`/`GetGSTRegistration`
are unchanged in behavior (getters on stores only).

---

## 4. Backend — handlers (gating + store enforcement + new sync)

### 4.1 Router (`router.go`)
- `sync.GET("/tax", auth.RequirePermission(auth.PermStockView), d.getTaxConfigSync)`.
- Gate the mutators with `auth.RequireOwner()`:
  - `hsn.POST("", auth.RequireOwner(), d.createHSNCode)`
  - `hsn.PUT("/:id/tax-rate", auth.RequireOwner(), d.upsertTaxRate)`
  - `meds.PUT("/:id/tax-config", auth.RequireOwner(), d.upsertMedicineTaxConfig)`
- `GET /api/medicines/:id/tax-config` stays auth-only. `GET /api/hsn` stays auth-only.

### 4.2 `handlers/tax.go`
- `createHSNCode`, `upsertTaxRate`, `upsertMedicineTaxConfig`: owner-gated; repo
  calls now store-pinned. Duplicate code → 409 (friendly message) instead of 500.
- `getMedicineTaxConfig` / `upsertMedicineTaxConfig`: **verify medicine ownership**
  by calling `d.MedicineRepo.GetByID(ctx, id)` first → 404 if the medicine is not
  in the caller's store. This prevents cross-store config assignment (Test 16).
- New `getTaxConfigSync` handler → `d.TaxRepo.ListStoreTaxSnapshot(ctx)`, returns
  `{ synced_at, hsn_codes, tax_configs }`.

### 4.3 `handlers/medicines.go`
- `getMedicineDetail` already embeds `tax_config` via `MedicineRepo.GetDetail`
  (which uses a fresh global `TaxRepo` at `medicine_repo.go:374`). Change that
  call site to construct a store-pinned `TaxRepo` scoped to the same store as the
  medicine (`r.storeID(ctx)`), so the embedded config is store-consistent.

### 4.4 `cmd/server/main.go`
- `taxRepo := repository.NewTaxRepo(pool, storeID)`.

---

## 5. Backend — purchase transaction helpers (store scoping)

`internal/repository/purchase_repo.go`:

- `hsnIDForTx(ctx, tx, code)` (:610) — create/find HSN **within the store** being
  written (the `CreateInward` already resolves `storeID`; pass it in) and insert
  with `store_id`. `SELECT ... WHERE code = $1 AND store_id = $2`.
- `activeTaxRateIDForTx` (:626) — scope by `store_id`.
- `insertMedicineTaxConfigForTx` (:640) — write `store_id`.
- `CreateInward` medicine-insert already sets `store_id` (:270-277). These helpers
  must use the same store.

---

## 6. Frontend — IndexedDB + sync

### 6.1 `web/src/lib/db.ts`
- Bump `DB_VERSION` 1 → 2; add stores in the existing `upgrade()` guard pattern:
  - `hsn_codes_cache` — `keyPath: 'id'`, value `HSNCode` (with active rate fields).
  - `medicine_tax_cache` — `keyPath: 'medicine_id'`, value `MedicineTaxConfig`
    (already carries `hsn_code` + `tax_rate` snapshot on the type).
- Extend `syncLocalCache()` (`db.ts:48-75`) with a third `Promise.all` arm:
  `fetch('/api/sync/tax')`, then in a readwrite tx `clear()` + `put` into
  `hsn_codes_cache` and `medicine_tax_cache`. (Clear+put = cache is fully
  **replaced on every sync**, so a store switch that triggers sync cannot leak a
  previous store's HSN data — mirrors existing medicine/customer behavior.)
- New read/write helpers (mirroring existing `loadCached*` / `upsertCached*`):
  - `loadCachedHSNCodes()`
  - `loadCachedMedicineTaxConfig(medicineId)`
  - `upsertCachedHSNCode(h)`
  - `upsertCachedMedicineTaxConfig(cfg)`

### 6.2 `web/src/lib/api.ts`
- Add `api.syncTax()` for symmetry (used by `syncLocalCache` instead of a raw
  `fetch`) — or keep raw `fetch` for parity with the existing two arms. **Decision:**
  use raw `fetch` inside `syncLocalCache` to match the existing pattern exactly;
  no new api.ts method strictly required, but add a typed helper if the return
  types are useful. All HSN/tax **write** methods already exist (`createHSNCode`,
  `upsertTaxRate`, `upsertMedicineTaxConfig`, `getMedicineTaxConfig`,
  `medicineDetail`) — reused unchanged.

---

## 7. Frontend — POS (Billing)

- On medicine selection (`setPickerFor` / `addBatch`, `POS.tsx:143-147,93-134`),
  read `loadCachedMedicineTaxConfig(medicine.id)` (one cache read, no API call).
- Show `HSN: 3004`, `GST: 5%`, `CGST/SGST/IGST` in the batch picker / cart line
  (display only from cache).
- Add an **“Edit Tax”** affordance (e.g. on the cart line or per-medicine) that
  opens the extracted tax editor. On save:
  1. `api.upsertTaxRate(hsnId, rates)` (server confirms),
  2. `api.upsertMedicineTaxConfig(medicineId, {...})`,
  3. `upsertCachedMedicineTaxConfig(cfg)` (cache refreshed).
- Checkout payload logic is **unchanged** — the server recomputes tax from the
  (now updated) config; no frontend tax math is duplicated.

---

## 8. Frontend — Purchases

### Scenario A (existing medicine)
- In `pick()` (`Purchases.tsx:137-151`), read `loadCachedMedicineTaxConfig(m.id)`
  and display HSN + GST/CGST/SGST/IGST.
- Add “Edit Tax” (same shared editor) that updates server + cache.

### Scenario B (new medicine)
- Replace the free-text `hsnCode` input (`Purchases.tsx:463-480`) with a
  `<select>` built from `loadCachedHSNCodes()` showing the **current store's** HSNs.
- Selected HSN’s tax info auto-fills.
- Add a **“+ Create New HSN”** action (dialog with code/description + all tax
  fields). On submit: `api.createHSNCode(code, description)` → then
  `api.upsertTaxRate(...)` → `upsertCachedHSNCode(h)` (cache update) →
  auto-select the returned HSN `id` for the new medicine. The **created HSN id
  from the create response is reused directly**, avoiding a second list/lookup
  API call (Test 3/7).
- New-medicine inward continues to send `hsn_code` (+ `price_includes_tax`);
  the backend `CreateInward` resolves/creates the store-scoped HSN + config.

---

## 9. Frontend — Medicines (Tax Configuration)

Make the existing `TaxConfigSection` (`Medicines.tsx:460-665`) **cache-first**:
- Load `detail.tax_config` from `loadCachedMedicineTaxConfig(id)` first, fall
  back to `api.medicineDetail(id)`.
- Source the HSN `<select>` from `loadCachedHSNCodes()` first, fall back to
  `api.listHSNCodes()`.
- Add inline **“Create New HSN”** (currently the section rejects a blank HSN at
  `Medicines.tsx:491-497`) to satisfy Test 7.
- After save: call `upsertCachedMedicineTaxConfig` + `upsertCachedHSNCode` so
  POS/Purchases pick up the new config without a full sync.
- Keep the existing visual style/components (Medicines modal) — no new visual
  system.

---

## 10. Validation / security

- **Backend authoritative** for all validation; never rely on frontend alone.
- HSN: required, trimmed, valid length (existing rules); unique per store (now
  `(store_id, code)`); duplicate → 409.
- Tax rates: non-negative, <= 100 (already `chk_tr_gst_rate` / `chk_tr_cess_rate`).
- Store: all HSN/tax reads/writes scoped to the authenticated principal’s store
  (pinned `storeIDRef`r). Client-supplied `store_id` is **ignored** on these
  routes (never trusted), consistent with `storeIDFor` / `TestClientStoreIDIsIgnored`.
- Owner-gating for HSN/tax mutations; medicine-ownership check for
  `medicine_tax_config` writes.

---

## 11. Tests

Full mapping in `HSN_TEST_PLAN.md`. Implementation priority:

**Backend (handler/API)** — `internal/handlers/tax_test.go`:
- #1 store isolation (A@5%, B@different; update A, B unchanged).
- #2 HSN list store-scoped (A excludes B’s 3006).
- #3 create HSN correct store_id + returned.
- #14 duplicate HSN → 409.
- #15 same HSN across stores both succeed.
- #16 employee/anonymous denied + rogue store_id ignored + no DB change.
- #8/9 config update reflects server + cache.

**Backend (repo/integration)** — `internal/repository/`:
- #10 historical invoice preservation (extend/extend `gst_test.go` pattern).
- #13 failed create leaves no phantom HSN.
- Two-store repo tests via two `NewTaxRepo(pool, storeA/storeB)` instances.

**Frontend (Vitest)** — `web/src/pages/*.test.tsx` (+ `web/src/lib/db.test.ts`):
- #3/7 synced + selector + auto-select + single create call.
- #4/8 server + IndexedDB + UI reflect update.
- #5/9 no per-selection API call; billing edit then continue.
- #11 IndexedDB store isolation (clear-on-sync).
- #12/13 failed update/create leaves cache untouched.

`reset()` in `internal/repository/repository_test.go` currently preserves
`hsn_codes`/`tax_rates` — after 033 these are store-scoped, so the reset
semantics still hold (they belong to the adopted first store), but any test
SQL that INSERTs `hsn_codes`/`tax_rates`/`medicine_tax_config` must now supply
`store_id`.

---

## 12. Definition of Done (from task §28)

All boxes verified through implementation + tests (see final coordinator report).
