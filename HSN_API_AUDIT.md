# HSN / Tax API — Backend Audit (Store Isolation)

Auditor: Sub-Agent A (Backend/API Auditor) · Date: 2026-08-30 · Scope: `internal/`, `cmd/`, `migrations/`

**Headline finding:** Every HSN / tax-rate / medicine-tax-config endpoint is **globally scoped**. `hsn_codes`, `tax_rates` and `medicine_tax_config` have **no `store_id` column** (migrations 012–014), `TaxRepo` is constructed with only a `*pgxpool.Pool` (no `storeIDRef`, no per-request store), and the three mutating handlers never consult `principal.StoreID`. A store owner (or even an employee) can read and mutate the shared national HSN/tax master for **all** stores. This is the only domain in the codebase that was skipped during the 030 store-scoping migration.

---

## 1. Existing HSN endpoints

Registered in `internal/handlers/router.go:156-161`:

```go
hsn := protected.Group("/hsn")
{
    hsn.GET("", d.listHSNCodes)
    hsn.POST("", d.createHSNCode)
    hsn.PUT("/:id/tax-rate", d.upsertTaxRate)
}
```

Handler implementations in `internal/handlers/tax.go`:

| Route | Handler | Location |
|---|---|---|
| `GET /api/hsn` | `func (d Deps) listHSNCodes` | `internal/handlers/tax.go:10` |
| `POST /api/hsn` | `func (d Deps) createHSNCode` | `internal/handlers/tax.go:20` |
| `PUT /api/hsn/:id/tax-rate` | `func (d Deps) upsertTaxRate` | `internal/handlers/tax.go:38` |

Gating: the `protected` group is `auth.RequireAuth(...)` + `auth.CSRFProtect(...)` (`router.go:62-63`). **No `RequireOwner`, no `RequirePermission`, no `storeIDFor` call.** Any logged-in user (employee included) reaches them.

## 2. Existing tax-rate / medicine-tax-config endpoints

Also in `router.go`, under the `/medicines` group (`router.go:133-134`):

```go
meds.GET("/:id/tax-config", d.getMedicineTaxConfig)
meds.PUT("/:id/tax-config", d.upsertMedicineTaxConfig)
```

Handlers in `internal/handlers/tax.go`:

| Route | Handler | Location |
|---|---|---|
| `GET /api/medicines/:id/tax-config` | `func (d Deps) getMedicineTaxConfig` | `internal/handlers/tax.go:80` |
| `PUT /api/medicines/:id/tax-config` | `func (d Deps) upsertMedicineTaxConfig` | `internal/handlers/tax.go:60` |

Same gating as above — `RequireAuth` only. There is **no** dedicated route to read a single HSN or a single tax rate by ID; `GET /api/hsn` lists all.

## 3. Store scoping / authorization model

### Identity & gating (`internal/auth/`)
- `Principal` (`internal/auth/principal.go:35-40`) carries `UserID`, `StoreID`, `Role`. The doc comment is explicit: *"Handlers MUST use Principal.StoreID for every store-scoped query and MUST ignore any client supplied store_id."*
- `RequireAuth` (`internal/auth/middleware.go:45-62`) resolves the session cookie via `AuthRepo.ValidateSession` and binds the principal with `WithPrincipal` into the request context.
- `RequirePermission(perm)` (`internal/auth/middleware.go:70-79`) ⇒ `Can(role, perm)`; `RequireOwner` (`internal/auth/middleware.go:83-92`) ⇒ `role == RoleStoreOwner`.
- Roles: `STORE_OWNER`, `EMPLOYEE`. Employees get the fixed permission set (`principal.go:45-55`); there is **no** `tax:manage` / `tax:view` permission — tax endpoints are ungated beyond authentication.

### Where the store comes from (`internal/handlers/store.go`)
- `storeIDFor(c *gin.Context)` (`store.go:19-45`): returns the **authenticated principal's** `StoreID` first; only falls back to client-supplied `?store_id=` (query) or JSON body (POST/PUT) when **no principal** is present (bootstrapping flows). Handlers consuming it: `invoices.go:16,25,36,50,59`, `reports.go:17,30,43,60,68`, `reconcile.go:19,40`, `sales.go:55`, `b2b_pdf.go:21`.
- The GST package mirrors this with `principalStoreID` (`internal/gst/auth.go:12-17`), used in `gstr1_handler.go:117`, `gstr3b_handler.go:25`, `gstr2b_handler.go:24,34,50`.

### Construction-time pinning (`internal/repository/store_id.go`)
- `storeIDRef` (`store_id.go:19-47`) is the **"single-tenant server"** pin: a constructor-bound repo holds one `boot` store (env `STORE_ID` or `FirstStoreID`), and lazily adopts the very first store on a fresh boot. It never re-reads the request principal.
- Pinned repos (constructor takes `storeID string`, internally `newStoreIDRef`): **MedicineRepo** (`medicine_repo.go:16-25`), **CustomerRepo** (`customer_repo.go:19-26`), **SupplierRepo** (`supplier_repo.go:16-23`). Every query in these repos carries `AND store_id = $N` built from the ref, not from the session.
- Non-pinned repos (pool only; store passed **per call** from `storeIDFor(c)`): `TaxRepo` (`tax_repo.go:18`), `SaleRepo`, `PurchaseRepo`, `ReconcileRepo`, `ReportRepo`, `AuthRepo`, `PurchaseRequestRepo`, `StockAuditRequestRepo`, `GSTR2BRepo`.
- Wiring in `cmd/server/main.go`: store resolved at lines 42-51 (`FirstStoreID` / env `STORE_ID`); `taxRepo := repository.NewTaxRepo(pool)` at **line 61** — no store. `NewMedicineRepo(pool, storeID)` line 69, `NewCustomerRepo(pool, storeID)` line 70, `NewSupplierRepo(pool, storeID)` line 60.

**Consequence to flag:** even the "pinned" repos are pinned to the server's store, not cross-checked against `principal.StoreID`. With a single-store-per-server deployment that is fine; the moment a DB holds multiple stores, medicine/customer/supplier handlers don't verify `principal.StoreID == ref.get()`. That is a pre-existing, general store-isolation gap; this audit is about the HSN/tax domain, which is global even in the ideal case.

## 4. Tax repository — full inventory (`internal/repository/tax_repo.go`)

`TaxRepo` holds only `db *pgxpool.Pool` (`tax_repo.go:14-18`). **No storeIDRef, no storeID field.**

| Method | Signature | Location | Store-scoped today? | SQL notes |
|---|---|---|---|---|
| `NewTaxRepo` | `(db *pgxpool.Pool) *TaxRepo` | `:18` | — | |
| `GetMedicineTaxConfig` | `(ctx, medicineID string, asOf time.Time) (*models.MedicineTaxConfig, error)` | `:22-55` | ❌ global | `medicine_tax_config` join `hsn_codes`, `tax_rates` by id only (active config, effective-from). |
| `GetHSNByCode` | `(ctx, code string) (*models.HSNCode, error)` | `:58-67` | ❌ global | `WHERE code = $1` |
| `GetActiveTaxRate` | `(ctx, hsnCodeID string) (*models.TaxRate, error)` | `:70-91` | ❌ global | `WHERE hsn_code_id = $1 AND effective_to IS NULL` |
| `GetDefaultStore` | `(ctx) (*models.Store, error)` | `:94-110` | n/a | first active store (bootstrap). |
| `GetStore` | `(ctx, id string) (*models.Store, error)` | `:113-128` | n/a | |
| `GetGSTRegistration` | `(ctx, id string) (*models.GSTRegistration, error)` | `:131-151` | n/a | |
| `ListHSNCodes` | `(ctx) ([]models.HSNCode, error)` | `:154-170` | ❌ global | `SELECT ... FROM hsn_codes ORDER BY code` |
| `UpsertTaxRate` | `(ctx, hsnCodeID string, gst, cgst, sgst, igst, cess float64) (*models.TaxRate, error)` | `:174-198` | ❌ global | tx: end active rate (`WHERE hsn_code_id=$1 AND effective_to IS NULL`) then insert new active. |
| `UpsertMedicineTaxConfig` | `(ctx, medicineID, hsnCodeID, taxRateID string, priceIncludesTax bool) (*models.MedicineTaxConfig, error)` | `:202-226` | ❌ global | tx: end active config for medicine, insert new. **No check that medicine belongs to caller's store.** |
| `CreateHSNCode` | `(ctx, code, description string) (*models.HSNCode, error)` | `:229-240` | ❌ global | plain `INSERT` |
| `GetMedicineTaxConfigByMedicine` | `(ctx, medicineID string) (*models.MedicineTaxConfig, error)` | `:243-245` | ❌ global | = `GetMedicineTaxConfig(..., time.Now())` |
| `BackdateTaxConfig` | `(ctx, configID, taxRateID string, effectiveFrom time.Time) error` | `:251-261` | ❌ global | used by deterministic seed only. |

**Every tax-domain SQL statement lacks a store predicate.** Nothing in `TaxRepo` ever calls `r.storeID(ctx)` or `storeIDFor(c)`.

## 5. DB schema — the root cause

- `migrations/012_create_hsn_codes.sql`: `hsn_codes (id, code UNIQUE, description, created_at)` — **no `store_id`**.
- `migrations/013_create_tax_rates.sql`: `tax_rates (id, hsn_code_id, gst/cgst/sgst/igst/cess, effective_from, effective_to, created_at)` + `uq_tax_rates_active_per_hsn (hsn_code_id) WHERE effective_to IS NULL` — **no `store_id`**.
- `migrations/014_create_medicine_tax_config.sql`: `medicine_tax_config (id, medicine_id, hsn_code_id, tax_rate_id, price_includes_tax, effective_from, effective_to, created_at)` + `uq_medicine_tax_config_active (medicine_id) WHERE effective_to IS NULL` — **no `store_id`**.
- `migrations/030_store_scoping.sql` adds `store_id` + NOT NULL + backfill + indexes + tenant-aware unique constraints **only** for: `medicines`, `batches`, `customers`, `suppliers`, `reconciliation_journals` (`030_store_scoping.sql:6-43`). HSN/tax tables are **absent** from this migration. Migration `021_seed_hsn_tax_rates.sql` treats HSN/tax as shared national reference data (3004, 3003, 3002, 3001, 2106, 9983).
- Tests codify this intent: `internal/repository/repository_test.go:67-69` — *"hsn_codes and tax_rates are preserved — they are reference data seeded by migration 021 and should not be wiped between tests"* (`reset()` truncates `medicine_tax_config` at line 78 but never `hsn_codes`/`tax_rates`).

So today the design is "shared reference catalog"; **there is no tenant boundary at all** — any change via the API is immediately visible/priced in every store.

## 6. Medicine handler & repo — how medicines are scoped, how tax rides along

- **Medicine store scoping (healthy):** `internal/repository/medicine_repo.go` — pinned `store: *storeIDRef` (`:16-20`); every method filters `AND store_id = $N`: `Create :45`, `GetByID :59`, `List :72`, `Update :99`, `SoftDelete :116`, `FindBatchByNumber :139`, `InventorySnapshot :169`, `GetDetail` batches `:250`, sales `:284`, purchases `:316,348`.
- **Tax config riding along (broken boundary):** `GetDetail` (`medicine_repo.go:231-382`) builds a **fresh global** `taxRepo := NewTaxRepo(r.db)` (`:374`) and calls `GetMedicineTaxConfigByMedicine(ctx, id)` (`:375`) with **no store predicate**. The medicine itself was already store-verified by `GetByID` (`:232`), but the tax-config row is resolved purely by `medicine_id` on a table with no `store_id`.
- The medicine handlers (`internal/handlers/medicines.go`) never touch tax; tax is reached via `tax.go`.

## 7. Existing endpoints that could serve each required use case

| Use case | Existing endpoint | Handler / Repo | Scoped? | Extend vs. new |
|---|---|---|---|---|
| (a) Read store-scoped HSN list | `GET /api/hsn` | `tax.go:10` → `TaxRepo.ListHSNCodes:154` | ❌ global | **Extend**: add `store_id` col + filter, or keep shared catalog and only scope rates. Straightforward. |
| (b) Create HSN | `POST /api/hsn` | `tax.go:20` → `TaxRepo.CreateHSNCode:229` | ❌ global | **Extend** (add store_id on insert + `RequireOwner`). |
| (c) Upsert tax rate for an HSN | `PUT /api/hsn/:id/tax-rate` | `tax.go:38` → `TaxRepo.UpsertTaxRate:174` | ❌ global | **Extend** (scope the end-and-replace tx by `store_id`) or **new** if per-store rates want a new row model. |
| (d) Read/update a medicine's tax config | `GET/PUT /api/medicines/:id/tax-config` | `tax.go:80/60` → `GetMedicineTaxConfigByMedicine:243` / `UpsertMedicineTaxConfig:202` | ❌ global (and does not verify medicine ownership) | **Extend** — must first `MedicineRepo.GetByID` (store-scoped) to prove the medicine is the caller's, then scope the config read/write. |
| (e) Sync endpoint | none exist for tax | — | — | **New**: `GET /api/sync/hsn` (or fold HSN + config into `GET /api/sync/inventory`). |

Current sync endpoints (see §8) return **only** medicines+batches (no `tax_config`, no HSN/rates) and customers — so the SPA's offline cache has no tax data to mirror.

## 8. Existing sync endpoints

Router (`router.go:113-117`):

```go
sync := protected.Group("/sync")
{
    sync.GET("/inventory", auth.RequirePermission(auth.PermStockView), d.getInventorySync)
    sync.GET("/customers", auth.RequirePermission(auth.PermCustomerView), d.getCustomersSync)
}
```

Handlers: `getInventorySync` (`internal/handlers/medicines.go:18-26`) → `MedicineRepo.InventorySnapshot` (store-scoped, returns meds + live batches); `getCustomersSync` (`medicines.go:28-35`) → `CustomerRepo.List`. No tax/HSN sync anywhere. These sync handlers are correctly store-scoped via their pinned repos; a new HSN sync must follow the same pinned-repo shape.

## 9. Authorization gaps (Q9 answer)

1. **No ownership validation on mutators.** `createHSNCode` (`tax.go:20`), `upsertTaxRate` (`tax.go:38`), `upsertMedicineTaxConfig` (`tax.go:60`) do **not** call `storeIDFor(c)`, do **not** check `currentPrincipal(c)`, and are gated by **nothing** beyond `RequireAuth`. No `RequireOwner()`, no permission exists for tax.
2. **One store can modify another store's HSN config today — absolutely.** Because the tables are shared and the repo methods are unscoped, store A's `POST /api/hsn` adds codes to the global catalog and `PUT /api/hsn/:id/tax-rate` re-prices an HSN that store B invoices against. The only soft barrier is that HSN ids are UUIDs, but `GET /api/hsn` lists every id and `GET /api/medicines/:id/tax-config` exposes them, so enumeration is trivial for any authenticated user.
3. **`UpsertMedicineTaxConfig` skips medicine-ownership verification.** It inserts into `medicine_tax_config` for an arbitrary `medicine_id` (route `:id`) without confirming the medicine exists in the caller's store via `MedicineRepo.GetByID`. `GetHSNByCode`-style cross-check of `hsn_code_id`/`tax_rate_id` into the caller's scope is also absent (both are global ids).
4. **`MedicineRepo.GetDetail`** instantiates a global `TaxRepo` (`medicine_repo.go:374`); while the medicine is pre-scoped, the loaded config keeps no tenant link.
5. **Role elevation on create/update paths**: `medicines.PUT` and `medicines.POST` (create) — the non-tax medicine mutations — are also ungated (any employee can create/update/delete stock catalog); the tax-config PUT inherits the same laxness.

## 10. Existing tax/HSN tests

- `internal/repository/gst_test.go`:
  - `TestInvoiceTaxPersistedAfterRateChange` (`:627-...`) is the only direct exercise of the tax repo API: `CreateHSNCode("9999",...)` (`:634`), `GetHSNByCode` (`:638`), `UpsertTaxRate` (`:647`), `UpsertMedicineTaxConfig` (`:662`), then Checkout and verifies persisted rates (uses a throwaway HSN `9999` so the seed catalog stays untouched).
  - Other GST tests seed HSN `3004` + `tax_rates` + `medicine_tax_config` directly via SQL (`:32-64`) and assert invoice HSN line items (`:182-183`, `:300-301`).
- `internal/repository/repository_test.go:67-88` — `reset()` preserves `hsn_codes`/`tax_rates` as shared reference data; would need revisiting if HSN becomes per-store.
- `internal/gst/*` tests (`gstr1_test.go`, `gstr3b_test.go`, `gstr2b_test.go`, `validate_test.go`, `gstr1_regression_test.go`) touch GST returns, not the HSN CRUD API. `internal/tax/calculator_test.go`, `gstin_test.go` are pure-domain.
- **No handler-level test** for `GET/POST /api/hsn`, `PUT /api/hsn/:id/tax-rate`, or `GET/PUT /api/medicines/:id/tax-config`. **No store-isolation test** exists for tax in any form.

---

## Honest assessment — what must change for store isolation

**Decision needed first:** is HSN the national shared catalog (real-world view: HSN codes are GSIR-defined and identical for all pharmacies) with **per-store rates/config**, or fully per-store?

- **Recommended (shared `hsn_codes`, per-store `tax_rates` + `medicine_tax_config`):**
  1. New migration: add `store_id` to `tax_rates` and `medicine_tax_config` (NOT NULL, backfill to store 1); optionally keep `hsn_codes` global (its `code` is already globally UNIQUE). Make the active-rate unique index tenant-aware: `uq_tax_rates_active_per_hsn (hsn_code_id, store_id) WHERE effective_to IS NULL`; same for `uq_medicine_tax_config_active (store_id, medicine_id)`.
  2. Repo layer: either give `TaxRepo` a `storeIDRef` (pinned, matching `MedicineRepo`) — consistent with the single-tenant-server model — and add `store_id` to every SQL statement, or thread a `storeID` param through the scoped methods. Given the existing pattern for medicine/customer/supplier, the pinned `storeIDRef` is the path of least surprise.
  3. `UpsertMedicineTaxConfig` must additionally verify via `MedicineRepo.GetByID` (or a store-aware join) that the `medicine_id` belongs to the scoped store before writing, and that the `hsn_code_id`/`tax_rate_id` are in the same tenant scope.
  4. `MedicineRepo.GetDetail` (`medicine_repo.go:374-375`) should reuse a store-aware tax lookup rather than a fresh global `NewTaxRepo`.
  5. Handlers: gate `POST /api/hsn` and `PUT /api/hsn/:id/tax-rate` with `auth.RequireOwner()`; gate `PUT /api/medicines/:id/tax-config` with `RequireOwner()` (config changes re-price the store's inventory). Add an `auth.PermTaxManage` permission if employees should manage under approval.
  6. Sync: extend `InventorySnapshot` (or add `GET /api/sync/hsn`) to ship `hsn_code`, rates, and `price_includes_tax` so the offline cache mirrors tax config; both must be store-scoped.
  7. Tests: add handler-level tests for the five endpoints and a two-store isolation test (store B cannot read/write store A's HSN/rates/config); update `repository_test.go:67-88` semantics if HSN becomes per-store.

- **If `hsn_codes` must also be per-store**: `code UNIQUE` becomes `(store_id, code)`; `GetHSNByCode`, `ListHSNCodes`, `CreateHSNCode` all take store scope; seed (`021_seed_hsn_tax_rates.sql`) and `cmd/seed/main.go` (`assignTaxConfig`, `:398-421`) must seed per-store.

Currently **nothing** in the HSN/tax surface is tenant-aware, so no migration step can rely on existing predicates — every query, unique index, handler call site and test fixture in §7–§10 needs touching.