# HSN / GST Rate / Store-Isolation Test Plan

**Author:** Sub-Agent D (Test Auditor) — RESEARCH ONLY; no code written.
**Scope:** Go backend (`internal/`) + React/Vite frontend (`web/`).
**Date:** 2026-08-30

---

## 1. Existing Test Infrastructure (findings)

### 1.1 Backend — repository tests (`internal/repository/`)

**Test DB bootstrap** (`repository_test.go:30-65`, `TestMain`):
- Connects via `TEST_DATABASE_URL` env var, falling back to
  `postgres://postgres:postgres@localhost:5432/pms_test?sslmode=disable`
  (`repository_test.go:32-35`). Same URL is used verbatim in the handlers
  package (`customers_test.go:36-39`).
- Runs `database.Migrate(ctx, pool)` against the test database, then
  `testutil.SeedStore(ctx, pool)` (`repository_test.go:41-50`).
- A package-level singleton `pool *pgxpool.Pool` and per-repo package globals
  (`medRepo`, `custRepo`, `saleRepo`, `purchRepo`, `reconRepo`, `reportRepo`,
  plus `authRepo`, `prRepo`, `saRepo` declared in auth/approval test files) are
  shared by every test in the package.

**Isolation model — NOT transactions.** There is no per-test transaction or
rollback. Instead a `reset(t *testing.T)` helper (`repository_test.go:70-88`)
TRUNCATEs every business table **CASCADE**, then re-runs `SeedStore` so the
store row always exists. The reset preserves `hsn_codes` and `tax_rates`:
```go
TRUNCATE customer_ledger, reconciliation_items, ...,
         medicine_tax_config, gst_registrations, stores, businesses,
         suppliers, batches, customers, medicines CASCADE
```
Every test begins with `reset(t)` (established convention).

**Seed helpers in `repository_test.go`:**
- `testutil.StoreID` = fixed UUID `00000000-0000-0000-0000-000000000001`
  (`testutil/testutil.go:15`). `SeedStore` inserts it idempotently
  (`ON CONFLICT (id) DO NOTHING`, `testutil.go:24-29`).
- `reset()` re-seeds only this ONE store. There is **no existing helper to
  seed a second store** (confirmed: no "store isolation" / two-store tests
  exist anywhere).
- `seedFixture(t, stock, creditLimit)` (`repository_test.go:96-134`) creates a
  medicine + inward batch + customer on the canonical store and returns IDs.
- `sid(storeID *string)` helper (`repository_test.go:706`) adapts an ID to
  `*string` inputs.

**GST/tax-specific seed** (`gst_test.go`):
- `seedGSTMedicine(t)` (`gst_test.go:15-105`) creates a medicine, backfills
  `hsn_codes`(3004) / `tax_rates`(12/6/6/12) / `medicine_tax_config` if missing,
  then stocks a batch.
- `tax_repo.go` helpers available to tests: `CreateHSNCode`, `GetHSNByCode`,
  `GetActiveTaxRate`, `UpsertTaxRate`, `UpsertMedicineTaxConfig`,
  `GetMedicineTaxConfig(ByMedicine)`, `BackdateTaxConfig`.
- `seedState27Store(t, ctx)` (`gst_test.go:572-589`) inserts a **second store**
  with business + GST registration via a single CTE — the closest existing
  pattern to "seed an additional store" and the model for store-isolation tests.
- `TestInvoiceTaxPersistedAfterRateChange` (`gst_test.go:627-738`) is the
  **existing historical-invoice-preservation test** — it uses a private HSN
  `9999`, checks `sales_invoice_items` snapshot survives a 12%→18% master
  change. This is the direct precedent for required test #10.

**Repo construction & store pinning** (`store_id.go`, `medicine_repo.go:19-23`):
- Repos are constructed with a `storeID` and pin to it via `storeIDRef` (the
  "single-tenant server" model). `store_id.go:30-46` `get()` returns the pinned
  boot store, else lazily adopts the `FirstStoreID` in the DB.
- Because the store is pinned at construction, tests create repos bound to a
  store ID parameter. Two-store scenarios must build **two repo instances**
  (one per store ID) or pass different `StoreID` in inputs.

**Critical schema findings relevant to store isolation:**
- `hsn_codes` is **GLOBAL shared reference data** — it has **no `store_id`
  column** (`migrations/012_create_hsn_codes.sql`). It is seeded once in
  `migrations/021_seed_hsn_tax_rates.sql` and explicitly excluded from
  `reset()`.
- `tax_rates` likewise has **no `store_id`** (`migrations/013`).
- `medicine_tax_config` has **no `store_id`** either — it is scoped only via
  `medicine_id` (`migrations/014`). Because medicines are store-scoped
  (migration `030_store_scoping.sql` adds `store_id` to medicines/batches/
  customers/suppliers/reconciliation_journals), a medicine's *config* is
  effectively store-scoped transitively.
- The HSN list endpoint `GET /api/hsn` calls `TaxRepo.ListHSNCodes`, which
  returns **all** HSN codes globally (`tax_repo.go:154-170`), not filtered by
  store. `POST /api/hsn` and `PUT /api/hsn/:id/tax-rate` operate globally.

> ⚠️ **Gap to flag:** The test requirements assume per-store HSN lists and
> per-store HSN rate configs (A has 3004@5%, B has 3004@different rate).
> The current data model has `hsn_codes`/`tax_rates` as global reference data,
> so "Store B 3004 different rate" and "A's HSN list excludes 3006" are **not
> currently representable** and will need HSN/rate store-scoping (or a
> store-scoped shadow table / per-store rate overrides) before/in tandem with
> those tests. The rest of the plan maps what each test would exercise **given
> that scoping lands**; where a test depends purely on today's model it is
> marked accordingly.

### 1.2 Backend — handler tests (`internal/handlers/`)

**Test router & DB** (`helpers_test` config in `customers_test.go:24-96`):
- `TestMain` connects `testPoolDB` (same `TEST_DATABASE_URL`/`pms_test`),
  migrates, TRUNCATEs all tables, seeds `testutil.SeedStore`, then seeds two
  auth sessions:
  - `ownerRawToken` — STORE_OWNER on `testutil.StoreID`
    (`seedTestSession`, `customers_test.go:100-117`).
  - `employeeRawToken` — EMPLOYEE on `testutil.StoreID`
    (`seedTestEmployeeSession`, `customers_test.go:119-136`).
- Builds the real router: `testRouter = NewRouter(Deps{...})` with every repo,
  `TaxRepo`, and `GSTHandler` wired (`customers_test.go:77-91`). All repos bound
  to `testutil.StoreID` except `SaleRepo`/`PurchaseRepo`/`ReconcileRepo`/
  `ReportRepo` (take store via input) and `TaxRepo` (global).
- **Auth helper factories:**
  - `doJSON(t, method, path, body)` → renders as owner
    (`customers_test.go:158-161`).
  - `doJSONAs(t, method, path, body, rawToken)` → renders with a chosen cookie
    (`customers_test.go:163-178`). Anonymous = pass `""`.
  - `errMessage(t, rec)` extracts `{"error":...}` (`customers_test.go:180-189`).
  - `registerUser(t, phone)` performs a full register/login cycle and returns a
    fresh raw session token (`auth_flow_test.go:26-46`).
  - Direct DB assertions use the shared `testPoolDB` (e.g.
    `auth_flow_test.go:228-300` `TestClientStoreIDIsIgnored`, which already
    proves a rogue client `store_id` is ignored and rows land on the tenant
    store).
- `currentPrincipal(c)` / `RequireAuth` resolve the store from the session
  membership, not from the request body (`internal/auth/principal.go:33-38`,
  `middleware.go:45`). Client `store_id` is deliberately ignored.

**HSN/tax endpoints wired** (`router.go:156-161`, `tax.go`):
- `GET /api/hsn` → `listHSNCodes`
- `POST /api/hsn` → `createHSNCode`
- `PUT /api/hsn/:id/tax-rate` → `upsertTaxRate`
- `GET/PUT /api/medicines/:id/tax-config` → `getMedicineTaxConfig` /
  `upsertMedicineTaxConfig`
- Create-medicine-from-purchase path: `purchase_repo.go:270-312` creates the
  medicine row, and if `it.HSNCode != ""` resolves via `hsnIDForTx` /
  `activeTaxRateIDForTx` and `insertMedicineTaxConfigForTx` on the same
  transaction (no orphaned config on rollback).

**Existing handler coverage relevant here:**
- `TestClientStoreIDIsIgnored` (`auth_flow_test.go:224-301`) — store isolation
  of medicines/purchases/batches/requests; precedent for tests #16 style.
- `TestCreateCustomerDuplicatePhoneConflict` (`customers_test.go:191-209`) —
  duplicate → 409 + friendly message pattern; precedent for test #14.

### 1.3 Frontend — Vitest infrastructure (`web/`)

**Scripts** (`web/package.json:10`): `"test": "vitest run"`.
**Setup** (`web/src/test-setup.ts`):
- `import 'fake-indexeddb/auto'` → IndexedDB is real but in-memory via
  `fake-indexeddb` (`devDependencies`: `fake-indexeddb ^6.2.5`).
- `@testing-library/jest-dom/vitest` matchers; `scrollIntoView` stubbed.
- jsdom environment; React 19 + `@testing-library/react` + `user-event`.

**IndexedDB layer** (`web/src/lib/db.ts`):
- DB name `pms-cache` v1 with two stores: `medicines_cache` (keyPath `id`) and
  `customers_cache` (keyPath `id`, index `by-name`).
- `syncLocalCache()` fetches `/api/sync/inventory` + `/api/sync/customers`,
  clears and rewrites both caches. **Note: no store partition inside the DB** —
  cache reflects whatever the last logged-in store synced; switching stores
  rewrites the same keys.
- `loadCachedMedicines()`, `loadCachedCustomers()`, `upsertCachedCustomer()`.
- There is **no HSN object store** yet and **no per-store keying** in the
  cache.

**Frontend test patterns observed:**
- `POS.test.tsx`: seeds `pms-cache` directly via `idb.openDB` + `put`
  (`POS.test.tsx:10-23`), renders `<POS cacheVersion={1} />`, asserts FEFO
  batch picker. No `fetch` mock needed because POS reads the cache.
- `Customers.test.tsx`: `vi.stubGlobal('fetch', vi.fn(...))` per test; helper
  `json(data, status)`; renders `<Customers onMutated={...}/>`; asserts
  filtering/search; also verifies `fetch` call URLs
  (`Customers.test.tsx:18-38`, `96-100`). `vi.unstubAllGlobals()` in `afterEach`.
- `Invoices.test.tsx`: `installFetchMock(opts)` returns the mock so call counts
  can be asserted; partial-mock fallthrough `throw new Error('unexpected fetch')`
  (`Invoices.test.tsx:70-112`); asserts pagination, dialog render, error+retry.
- `__tests__/GSTReportsPage.test.tsx`: fixture factories (`mkLine`, `gstr3bFixture`)
  — pattern for building tax fixtures.

**Frontend HSN/tax surface:**
- `api.ts:346-374` — `listHSNCodes`, `createHSNCode`, `upsertTaxRate`,
  `getMedicineTaxConfig`, `upsertMedicineTaxConfig`.
- `Medicines.tsx:458-620` — `TaxConfigSection`: on "Edit"/"Assign HSN & tax"
  loads HSN list (`api.listHSNCodes`), then on save upserts tax rate and
  assigns config. Currently requires a pre-selected HSN; cannot create a brand
  new HSN inline (rejects with "Please select an HSN code.",
  `Medicines.tsx:491-497`). Displays HSN/GST/CGST/SGST/IGST chips.
- `Purchases.tsx:272` — new-medicine purchase lines carry an `hsn_code` input
  → server creates medicine + config on inward.
- `types.ts` — `HSNCode`, `TaxRate`, `MedicineTaxConfig`, and the GST snapshot
  fields on `InvoiceItem` / `CheckoutResponse.invoice` (hsn_code, gst_rate,
  cgst/sgst/igst rates+amounts, gross/taxable/line_totals).

**Existing tax/HSN tests:**
- Backend: `internal/repository/gst_test.go` (checkout intra/inter-state,
  fallback, legacy, historical preservation), `internal/tax/calculator_test.go`,
  `internal/tax/gstin_test.go`, `internal/gst/*_test.go` (GSTR-1/2B/3B).
- Frontend: `web/src/pages/__tests__/GSTReportsPage.test.tsx` (report rendering
  only — no HSN CRUD / tax-config tests).
- **No store-isolation tests exist anywhere** (grep for "store A/B"/"isolation"
  → none).

---

## 2. Cross-cutting harness notes & conventions

- **Backend repo tests** live in `internal/repository/`, `package repository_test`,
  always start with `reset(t)`, use package globals for repos, and may insert a
  second store via direct SQL (pattern: `seedState27Store`).
- **Backend API tests** live in `internal/handlers/`, `package handlers`, use
  `doJSON`/`doJSONAs` against `testRouter`, authenticate with `ownerRawToken`
  (or `registerUser`/`employeeRawToken`), and assert DB state via `testPoolDB`.
  Handler TestMain is shared; heavy per-test DB changes should TRUNCATE the
  affected tables (pattern: `cleanupCustomers`, `customers_test.go:150-156`).
- **Frontend React tests** live beside pages as `*.test.tsx`, render with
  `@testing-library/react`, mock `window.fetch` via `vi.stubGlobal`, and drive
  IndexedDB state with `fake-indexeddb` (seed via `idb.openDB` + `put`, or call
  `syncLocalCache` against a stubbed fetch).
- **Asserting "no second API call"** → capture the mocked `fetch` (`vi.fn`)
  and assert call count / URL set (`Customers.test.tsx:96-100`,
  `Invoices.test.tsx:70-112`).
- **Historical-invoice checks** in repo tests read `sales_invoice_items`
  directly via `pool.QueryRow` (pattern: `gst_test.go:696-737`).
- **Regression-critical** items are marked **[REG]**.

---

## 3. Required Tests — mapping, harness, assertions

### 1. Store isolation — Store A HSN 3004 @5%, Store B 3004 different; update A leaves B.
- **Where:** `internal/handlers/tax_test.go` (API, primary) + repo-level in
  `internal/repository/store_isolation_test.go`.
- **Harness:** handler tests bound to `testutil.StoreID` (A). Seed a second
  store B (SQL `seedState27Store`-style or a new `testutil` helper) with a
  Store B-bound owner session (`seedTestSession` with B's ID). Per-store rate
  override requires the HSN-scoping model (see §1.3 gap).
- **Assertions:**
  - As A: put rate `3004 → 5/2.5/2.5/5`; 200.
  - As B: read 3004 → still the original/different value; `api.upsertTaxRate`
    as A again changes only A's effective rate.
  - Poll `testPoolDB` for `tax_rates` rows keyed by `hsn_code_id`+store to
    prove B's row untouched.
- **Flag:** **[REG impl-blocker]** — requires store-scoped rate model.

### 2. HSN list is store-scoped — A has 3004,3005; B has 3004,3006; A's list excludes 3006.
- **Where:** `internal/handlers/tax_test.go` (API) + frontend `web/src/pages/Medicines.test.tsx` (React, selector).
- **Harness:** handler `doJSONAs(GET /api/hsn, rawTokenA)` vs `rawTokenB`.
  Backend `ListHSNCodes` must accept/pin a store; frontend asserts the
  selector `<option>` set.
- **Assertions:**
  - A's list contains 3004 & 3005, not 3006; B's contains 3004 & 3006, not 3005.
  - React: render `TaxConfigSection` with stubbed `listHSNCodes` returning A's
    set → options exclude 3006.
- **Flag:** **[REG impl-blocker]** — today `ListHSNCodes` returns all HSNs globally.

### 3. Create HSN from purchase — server record with correct store_id, returned, IndexedDB updated, appears in selector.
- **Where:** Backend repo `internal/repository/purchase_repo_test.go` +
  handler `internal/handlers/medicines_test.go`; frontend React in
  `web/src/pages/Purchases.test.tsx` / `web/src/pages/Medicines.test.tsx`.
- **Harness:** `purchRepo.CreateInward` with `Items[].HSNCode="9991"` on a new
  medicine (pattern `TestPurchaseInwardCreatesNewMedicineInline`,
  `repository_test.go:363`). Frontend: capture `fetch` POST `/api/hsn` /
  `/api/medicines/:id/tax-config` and assert `syncLocalCache` writes to
  `medicines_cache`.
- **Assertions:**
  - `hsn_codes` row exists with `code=9991`, description, correct store scoping.
  - Medicine got a `medicine_tax_config` link to the created HSN
    (`GetMedicineTaxConfigByMedicine` non-nil).
  - Frontend: after create, `loadCachedMedicines()` surfaces the medicine and
    the HSN appears in the TaxConfigSection `<select>` options.
- **Flag:** **[REG]** store_id correctness (client must not spoof; see #16).

### 4. Update HSN — GST rate change; server, IndexedDB, UI all reflect.
- **Where:** Backend `internal/handlers/tax_test.go`; frontend React `web/src/pages/Medicines.test.tsx`.
- **Harness:** handler PUT `/api/hsn/:id/tax-rate` then GET medicine tax-config;
  React render with stubbed GET returning the updated `MedicineTaxConfig`.
- **Assertions:**
  - PUT returns 200, new `gst_rate`; GET `tax-config` returns the new rate.
  - `tax_rates` shows old row `effective_to` set, new row `effective_from` today.
  - React: `TaxConfigSection` chips show `GST: 18%` after refresh.
  - IndexedDB medicine (if config is cached) updated via sync.

### 5. Existing medicine — HSN/GST/CGST/SGST/IGST loaded with NO per-selection API call.
- **Where:** Backend handler `internal/handlers/medicines_test.go`; frontend React `web/src/pages/POS.test.tsx` + `web/src/pages/Medicines.test.tsx`.
- **Harness:** pre-seed `medicines_cache` with a medicine carrying a full
  `tax_config` (`MedicineWithBatches` + tax snapshot), render POS, count `fetch`
  calls.
- **Assertions:**
  - POS selects the medicine and shows HSN/GST/CGST/SGST/IGST from the cached
    record with **zero** `fetch` to `/tax-config` (assert `fetch` mock never
    called with `/tax-config`).
  - Medicine detail GET returns `tax_config` populated in one call
    (`getMedicineDetail` already embeds `detail.TaxConfig`,
    `medicine_repo.go:373-379`).
- **Flag:** **[REG]** — guards against accidental N+1 tax lookups.

### 6. New medicine select existing HSN — associated with store-specific config.
- **Where:** Backend handler `internal/handlers/medicines_test.go` + repo;
  frontend React `web/src/pages/Medicines.test.tsx`.
- **Harness:** given an existing HSN, PUT `/api/medicines/:id/tax-config` with
  that HSN's ID and an existing `tax_rate_id`; verify config is store-pinned.
- **Assertions:**
  - 200 config returned; row in `medicine_tax_config` points at the HSN; only
    one active (`effective_to IS NULL`) config per medicine (unique index
    `uq_medicine_tax_config_active`).
  - React: select existing HSN from dropdown, save → submits correct
    `hsn_code_id`/`tax_rate_id`, no duplicate `createHSN` POST.

### 7. Create HSN during new-medicine flow — created on server, synced, becomes selectable, medicine uses it; avoid second API call.
- **Where:** Backend handler `internal/handlers/medicines_test.go`; frontend React `web/src/pages/Medicines.test.tsx`.
- **Harness:** The desired UX creates an HSN inline (currently the UI rejects
  blank HSN, `Medicines.tsx:491-497` — **feature gap** if inline create is the
  goal). Otherwise emulate the purchase flow: `purchase_repo.New medicine with
  hsn_code` already creates HSN + config in one TX (`purchase_repo.go:278-312`).
- **Assertions:**
  - Exactly one POST to `/api/hsn` (or one `CreateInward`) that creates HSN +
    config atomically; assert `fetch` call count == 1 for HSN creation
    (+1 config link if separate endpoints).
  - Server row present; medicine's active config uses the new HSN; selector in
    React later shows it.
- **Flag:** **[REG]** — "avoid second API call" is a hard assertion caught by
  counting mocked `fetch` calls.

### 8. Medicine tax config editing — server updated, local cache updated, page reflects.
- **Where:** Backend handler `internal/handlers/medicines_test.go`; frontend React `web/src/pages/Medicines.test.tsx`.
- **Harness:** `upsertMedicineTaxConfig` PUT; React: change rate → save → assert
  PUT payload + re-render chip + IndexedDB medicine updated.
- **Assertions:**
  - PUT 200; subsequent GET `tax-config` returns new `gst_rate`.
  - React `TaxConfigSection` `save()` calls `upsertTaxRate` + `upsertMedicineTaxConfig`
    (assert both `fetch` URLs/METHODs).
  - Page shows updated HSN/GST/CGST/SGST chips without full reload.

### 9. Billing tax editing — select medicine, edit tax, save, continue billing with latest config.
- **Where:** Frontend React `web/src/pages/POS.test.tsx` (+ may need POS to
  support mid-billing tax edit — **feature gap** if not present; falls back to
  "edit config in Medicines then re-bill").
- **Harness:** seed cache, render POS, select an item, invoke edit, change rate,
  save, checkout; stub `/api/sales/checkout` and assert the request body reflects
  the latest config / server recomputed snapshot.
- **Assertions:**
  - Checkout request carries the item using the updated config.
  - On success, receipt shows the new `gst_rate`/sums.
  - No stale cached rate used after edit.
- **Flag:** **[REG]** — end-user correctness in the live billing screen.

### 10. Historical invoice preservation — invoice GST 5%; change config; old invoice stays 5%.
- **Where:** Backend repo `internal/repository/gst_test.go` (extend / add near
  `TestInvoiceTaxPersistedAfterRateChange`, `gst_test.go:627`).
- **Harness:** create medicine+config@5% with private HSN, checkout, re-read
  `sales_invoice_items` snapshot, then `UpsertTaxRate`→ new rate, re-read again
  (exact pattern already proven for 12→18 at `gst_test.go:696-737`).
- **Assertions:**
  - `sii.gst_rate, cgst_amount, sgst_amount` unchanged after master change.
  - `sales_invoices.*_total` totals unchanged.
- **Flag:** **[REG — CRITICAL]** — this is the single most important regression
  guard; precedent test already exists and must keep passing.

### 11. IndexedDB store isolation — sync A, switch to B, A records not in B selector.
- **Where:** Frontend React `web/src/pages/Medicines.test.tsx` / `web/src/pages/POS.test.tsx` (or a focused `web/src/lib/db.test.ts`).
- **Harness:** stub `/api/sync/inventory` to return A's med set, call
  `syncLocalCache()`; then switch "store" and re-stub + re-sync with B's set;
  render POS, search.
- **Assertions:**
  - After B sync, A-only medicine (e.g. HSN 3006 medicine) is **not** in
    selector; B medicine is.
  - Requires the HSN cache to be partitioned by store if HSNs are cached
    (**feature gap** — current `pms-cache` has no store key; switching stores
    rewrites the same keys, so this only works if sync reloads per store).
- **Flag:** **[REG]** store isolation in the offline layer.

### 12. Failed update doesn't corrupt cache — server unchanged, IndexedDB unchanged, UI retains old.
- **Where:** Frontend React `web/src/pages/Medicines.test.tsx` (primary) +
  handler `internal/handlers/medicines_test.go` (server unchanged).
- **Harness:** stub `upsertTaxRate`/`upsertMedicineTaxConfig` to return
  500/error; render, attempt edit, assert old config still shown and
  `medicines_cache` holds the pre-edit `tax_config`.
- **Assertions:**
  - UI keeps old rate; error surfaced (`err instanceof Error → setError`,
    `Medicines.tsx:522-527`).
  - No PUT persisted: `testPoolDB` `tax_rates`/`medicine_tax_config` unchanged.
  - IndexedDB value identical to pre-edit.

### 13. Failed creation → no phantom HSN.
- **Where:** Backend handler `internal/handlers/tax_test.go` / `internal/repository`; frontend React `web/src/pages/Medicines.test.tsx`.
- **Harness:** make the HSN-create request fail mid-transaction; assert neither
  `hsn_codes` nor `medicine_tax_config` rows are left (the inward path already
  guarantees this via `pgx.BeginFunc` + `insertMedicineTaxConfigForTx`,
  `purchase_repo.go:278-312`).
- **Assertions:**
  - `SELECT COUNT(*) FROM hsn_codes` for the code == 0 (or rolled back).
  - No config linked to the failed medicine.
  - React: no new HSN appears in the selector after a failed save.

### 14. Duplicate HSN — conflict/validation error.
- **Where:** Backend handler `internal/handlers/tax_test.go`.
- **Harness:** `POST /api/hsn` twice with same `code`. `hsn_codes.code` is
  `UNIQUE` (`012_create_hsn_codes.sql:4`) and `CreateHSNCode` (`tax_repo.go:229`)
  relies on that; currently violates → expect `mapRepoError` to translate.
- **Assertions:**
  - Second POST returns 409 (or 400 with friendly message) — mirror
    `TestCreateCustomerDuplicatePhoneConflict` (`customers_test.go:191-209`);
    assert `errMessage` is not "internal server error".
  - One row in `hsn_codes`.
- **Flag:** **[REG]** — today the repo returns a raw constraint error; handler
  must map it to 409 (may need `mapRepoError` handling added).

### 15. Same HSN across stores both succeed.
- **Where:** Backend handler `internal/handlers/tax_test.go` (+ repo).
- **Harness:** seed store B + owner session; as A and as B each create the same
  HSN code (or each configure their own store-scoped rate for it). Because
  `hsn_codes.code` is globally UNIQUE, this only "both succeed" if the code
  already exists and the scoping is at the rate/config level (see §1.3 gap).
- **Assertions:**
  - Both requests return 2xx; both stores' configs coexist; no cross-store
    mutation (A's rate ≠ B's rate after both writes).

### 16. Unauthorized store access — auth failure, no DB change.
- **Where:** Backend handler `internal/handlers/tax_test.go`.
- **Harness:** anonymous (`doJSONAs(..., "")`) and employee (`employeeRawToken`)
  hitting HSN create/update; plus rogue `store_id` in body.
- **Assertions:**
  - Anonymous GET/POST `/api/hsn` → 401 (route is behind `RequireAuth` +
    CSRF; matches `auth_flow_test.go:183-188`).
  - Employee (non-owner) attempts: owner-gated mutation → 403 (pattern
    `TestRoleGatingOwnerVsEmployee` `auth_flow_test.go:130-161`; note the `/hsn`
    group has **no** explicit owner gate today — `router.go:156-161` — so this
    test may surface a **permission gap**).
  - Rogue `store_id` in body ignored; rows still land on the session's store
    (pattern `TestClientStoreIDIsIgnored`).
  - Assert zero `hsn_codes`/`tax_rates` rows changed after the denied attempts.

---

## 4. Summary of gaps / prerequisites surfaced by research

1. **HSN & rate store-scoping does not exist** (`hsn_codes`, `tax_rates`, and
   `medicine_tax_config` have no `store_id`; `ListHSNCodes` is global).
   Tests #1, #2, #11, #15 fundamentally depend on per-store HSN/rate scoping
   and must be gated on that feature landing (or an explicit redesign).
2. **No second-store seed helper** for repo tests — add one (or reuse the
   `seedState27Store` CTE pattern) before #1/#15.
3. **No frontend HSN cache / per-store cache key** — test #11 needs cache
   partitioning.
4. **`/api/hsn` group has no owner gate** in `router.go` — test #16 may reveal
   a missing 403 for employees.
5. **Duplicate HSN returns a raw DB error** — test #14 needs `mapRepoError` to
   produce a 409 (`tax_repo.go:229` / `CreateHSNCode`).
6. **Inline HSN creation absent from `TaxConfigSection`** (`Medicines.tsx:491-497`
   rejects blank HSN) — if test #7 requires inline creation, that UI is a gap
   (otherwise reuse the purchase-flow create).

## 5. Confidence flags

- **[REG-CRITICAL]** store isolation (#1,#2,#11,#15,#16) and history
  preservation (#10, already implemented & green in `gst_test.go`).
- **[REG-HIGH]** "no second API call" tests (#5,#7) catch tax-calculation
  regressions and N+1 lookups.
- Backend API tests use the **shared** `testPoolDB` and a single router built in
  TestMain; store-switching scenarios need extra sessions (`seedTestSession`
  with a second store ID) and careful TRUNCATE discipline, since `reset()`-style
  rewrites happen per-package, not per-test, in the handler suite.
