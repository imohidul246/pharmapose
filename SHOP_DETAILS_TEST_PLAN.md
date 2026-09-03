# Shop Details (Registration & Store Settings) Test Plan

**Author:** Test Auditor — RESEARCH ONLY; no code/tests modified.
**Scope:** Go backend (`internal/`) + React/Vite frontend (`web/`).
**Date:** 2026-08-30

---

## 1. Current Test Infrastructure

### 1.1 Test frameworks

- **Go:** standard `testing` package only. **No testify** (confirmed in
  `go.mod:5-10` — only gin/pgx runtime deps; assertion helpers are hand-rolled,
  e.g. `errorsAs`/`assertCreditError` in `repository_test.go:682-704`).
- **Frontend:** **Vitest** (`web/package.json:10` `"test": "vitest run"`) +
  **React Testing Library** (`@testing-library/react`, `user-event`, `jest-dom`)
  + **jsdom** + **fake-indexeddb** (`web/package.json:22-35`).
  Setup registered in `web/src/test-setup.ts:1-4` (`fake-indexeddb/auto`,
  `@testing-library/jest-dom/vitest`, `scrollIntoView` stub).

### 1.2 Repository tests (`internal/repository/`, `package repository_test`)

**Test DB bootstrap — NOT transactions.** A package-level `TestMain`
(`repository_test.go:30-65`) connects `TEST_DATABASE_URL`
(`postgres://postgres:postgres@localhost:5432/pms_test?sslmode=disable`),
runs `database.Migrate`, then `testutil.SeedStore`. No per-test transaction or
rollback exists.

**Isolation via TRUNCATE + reseed.** `reset(t)` (`repository_test.go:70-88`)
TRUNCATEs every business table CASCADE then re-runs `SeedStore`:

```go
func reset(t *testing.T) {                      // repository_test.go:70
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		TRUNCATE customer_ledger, reconciliation_items, reconciliation_journals,
		         sales_credit_notes,
		         sales_invoice_items, sales_invoices,
		         purchase_order_items, purchase_orders,
		         gstr2b_imports, gstr2b_import_batches,
		         medicine_tax_config,
		         gst_registrations, stores, businesses,
		         suppliers,
		         batches, customers, medicines CASCADE`)
	...
	if err := testutil.SeedStore(context.Background(), pool); err != nil { ... }
}
```

`auth_repo_test.go` has its own superset variant `resetAuth(t)`
(`auth_repo_test.go:19-37`) that also TRUNCATEs `audit_logs, sessions,
store_memberships, users, purchase_requests, stock_audit_*, gstr2b_*` and
re-seeds the store.

**Shared repo globals** are constructed once in `TestMain`
(`repository_test.go:52-60`): `medRepo`, `custRepo`, `saleRepo`, `purchRepo`,
`reconRepo`, `reportRepo`, plus `authRepo` (`auth_repo_test.go:15`),
`prRepo`, `saRepo`. Store-pinned repos take `testutil.StoreID`.

**The canonical test store** comes from `internal/testutil/testutil.go`:
- `StoreID` = `00000000-0000-0000-0000-000000000001` (`testutil.go:15`).
- `SeedStore(ctx, pool)` inserts it idempotently
  (`INSERT ... ON CONFLICT (id) DO NOTHING`, `testutil.go:24-29`),
  with `max_employees = 2`.
- No helper seeds a **second** store; the two-store pattern used today is a
  raw SQL CTE (`seedState27Store`, `gst_test.go:570-580`) or inline INSERT
  (`TestClientStoreIDIsIgnored`, `auth_flow_test.go:228-233`).

### 1.3 Handler tests (`internal/handlers/`, `package handlers`)

**TestMain** (`customers_test.go:34-96`): connects `testPoolDB`, migrates,
TRUNCATEs all tables, seeds the store, then creates two authenticated sessions:
- `ownerRawToken` — `STORE_OWNER` on `testutil.StoreID`
  (`seedTestSession`, `customers_test.go:100-117`).
- `employeeRawToken` — `EMPLOYEE` on `testutil.StoreID`
  (`seedTestEmployeeSession`, `customers_test.go:119-136`).
- `newSession` inserts a session row and returns the raw cookie
  (`customers_test.go:138-148`).

**Router is the real one**, built once in TestMain
(`customers_test.go:77-91`):

```go
testRouter = NewRouter(Deps{
	AuthRepo:              authRepo,
	PurchaseRequestRepo:   repository.NewPurchaseRequestRepo(testPoolDB),
	StockAuditRequestRepo: repository.NewStockAuditRequestRepo(testPoolDB),
	CookieOptions:         auth.CookieOptions{Secure: false},
	MedicineRepo:          repository.NewMedicineRepo(testPoolDB, testutil.StoreID),
	CustomerRepo:          repository.NewCustomerRepo(testPoolDB, testutil.StoreID),
	SaleRepo:              repository.NewSaleRepo(testPoolDB),
	PurchaseRepo:          repository.NewPurchaseRepo(testPoolDB),
	ReconcileRepo:         repository.NewReconcileRepo(testPoolDB),
	ReportRepo:            repository.NewReportRepo(testPoolDB),
	SupplierRepo:          repository.NewSupplierRepo(testPoolDB, testutil.StoreID),
	TaxRepo:               repository.NewTaxRepo(testPoolDB),
	GSTHandler:            gst.NewHandler(testPoolDB),
})
```

**Request helpers** (all in `customers_test.go`):
- `doJSON(t, method, path, body)` — acts as the owner (`:158-161`).
- `doJSONAs(t, method, path, body, rawToken)` — acts as any cookie / anonymous
  with `""` (`:163-178`).
- `errMessage(t, rec)` — extracts `{"error": ...}` (`:180-189`).
- `registerUser(t, phone)` — full register/login cycle returning `(rawToken,
  userID)` (`auth_flow_test.go:26-46`).
- `cookieFrom(t, rec)` — pulls the session cookie out of a response
  (`auth_flow_test.go:15-24`).
- Per-test cleanup via targeted TRUNCATE, e.g. `cleanupCustomers`
  (`customers_test.go:150-156`).

### 1.4 Frontend tests (`web/src/`)

`*.test.ts` / `*.test.tsx` files:
- `web/src/lib/db.test.ts` — IndexedDB cache (`fake-indexeddb` + `idb.openDB`).
- `web/src/lib/states.test.ts`.
- `web/src/pages/Customers.test.tsx` — stubs `window.fetch`
  (`vi.stubGlobal('fetch', vi.fn(...))`, `:18-38`), helper `json(data, status)`.
- `web/src/pages/POS.test.tsx`, `POS.customer.test.tsx`, `Invoices.test.tsx`,
  `web/src/components/TaxEditor.reassign.test.tsx`,
  `web/src/pages/__tests__/GSTReportsPage.test.tsx` — fixture-factory patterns.

**Notable:** there is **no** `StoreSettings.test.tsx` today
(`web/src/pages/StoreSettings.tsx` exists, but is untested).

---

## 2. Existing relevant tests

### Registration (`Register` with shop fields)
- Repo: `auth_repo_test.go:43-95` `TestRegisterCreatesTenantAndSession` —
  passes `GSTIN`, `StoreName`, `StoreAddress`, `TradeName`, `BusinessName`;
  asserts normalized phone, `STORE_OWNER` role, store linkage to the GST
  registration, session validation.
- Repo: `auth_repo_test.go:97-143` `TestRegisterValidationAndDuplicates` —
  empty `store_name` rejected, empty `password_hash` rejected, structurally
  invalid GSTIN rejected, duplicate phone rejected.
- Handler: `auth_flow_test.go:26-46` `registerUser` + `:48-91`
  `TestRegisterLoginLogoutMeCycle` (minimal body has no GSTIN — so minimal
  registration already works at the API layer).

### Store settings (getStore / updateStore)
- Repo: `auth_repo_test.go:280-309` `TestStoreSettingsSeatResize` —
  `UpdateStoreSettings` rejects negative `max_employees`, empty name, cap below
  occupied seats; asserts seat cap round-trip.
- Handler: only role-gating is tested — `GET /api/store` returns 200 for owner
  and 403 for employee (`auth_flow_test.go:134`, `:146`). **No handler test
  PUTs `/api/store`.**
- Handlers: `employees.go:79-115` (`getStore`, `updateStore`); routes
  `router.go:77-82` behind `auth.RequireOwner()`.

### Authorization (`RequireOwner`)
- `auth/middleware.go:83-92` — `RequireOwner` 403s any non-`STORE_OWNER`.
- `auth_flow_test.go:130-189` `TestRoleGatingOwnerVsEmployee` — 403 matrix for
  employees across `/api/employees`, `/api/store`, purchases, reconcile,
  approvals; anonymous requests → 401 (`:183-188`).
- `auth_flow_test.go:224-301` `TestClientStoreIDIsIgnored` — a rogue
  client-supplied `store_id` is ignored; rows land on the session store.

### Validation / no-DB-change-on-reject
- Repo registration validation: `auth_repo_test.go:109-134`.
- Store-settings validation: `auth_repo_test.go:284-290`.
- Repo side-effect-free rejection: `repository_test.go:161-191`
  (`TestCheckoutRejectsOversellAtomically` counts `sales_invoices` rows == 0),
  precedent for asserting "no DB change" on a failed request.

### GSTIN validation
- Core algorithm: `internal/tax/gstin.go:29-54` `ValidateGSTIN` (pattern +
  ISO 7064 MOD 37,36 checksum). Unit tests: `internal/tax/gstin_test.go:5-33`.
- Repo: `auth_repo.go:86-99` — Register calls `tax.ValidateGSTIN`, rejects
  `"gstin is not a structurally valid GSTIN"`; tested `auth_repo_test.go:127-134`.
- Customers: `customer_repo.go:48-49` `ValidateCustomer` → `"invalid GSTIN"`;
  tested `customer_search_test.go:152-174` and handler-level
  `customers_test.go:211-225` (400 + `errMessage == "invalid GSTIN"`).
- Suppliers: `supplier_repo.go:27-36` `ValidateSupplier`.
- B2B checkout: `sale_repo.go:113`; tested `gst_test.go:582-616`.
- Valid test fixtures (`gstin_test.go:10`, `customer_search_test.go:13-19`):
  `27AAPBC1234F1ZV`, `29AAPBC1234F1ZR`, `27AAAAA1111A1ZW`;
  checksum-invalid pattern: `27AABCU9603R1ZM`.

### Store isolation
- Handler: `auth_flow_test.go:224-301` `TestClientStoreIDIsIgnored` (client
  `store_id` never trusted; rows land on the session's store).
- Repo: two-store seeding exists only as `seedState27Store`
  (`gst_test.go:570-580`). There is **no A-updates-B-untouched test** yet.

### Historical snapshot preserved
- `gst_test.go:618-737` `TestInvoiceTaxPersistedAfterRateChange` — proves
  `sales_invoice_items` snapshots survive a master-rate change.
- `repository_test.go:540-603` — reconcile leaves `sales_invoices` /
  `sales_invoice_items` rows and quantities intact (reads via `pool.QueryRow`).

---

## 3. Required Tests — mapping, harness, assertions

**Shared contract for the "shop details" feature** (implied by the scenarios):
`stores` gains optional shop fields (owner name/phone, GSTIN, PAN, drug
license no., DL expiry); `RegisterInput` and `UpdateStoreSettings`
(plus `/api/auth/register` and `PUT /api/store`) accept them; they are
NULL-able. Scenario wording below follows the current API shapes:
`POST /api/auth/register` (`auth.go:38-87`), `GET/PUT /api/store`
(`employees.go:79-115`), repo `Register` (`auth_repo.go:52-135`),
`GetStore` (`auth_repo.go:415-421`), `UpdateStoreSettings`
(`auth_repo.go:436-460`).

### Test 1 — Minimal registration (no GSTIN/DL/PAN) succeeds; store created; optionals NULL
- **Level:** integration (handler → repo → DB).
- **Harness:** extend/minic `registerUser` (`auth_flow_test.go:26-46`) with a
  body that **omits** all optional fields; then `GET /api/store` as the new
  owner; also query `testPoolDB` directly.
- **Assertions:** register returns 200 with a `STORE_OWNER` principal and a
  store_id; `GET /api/store` returns the store; `gst_registration_id` is NULL
  and each optional column is NULL.
- **Model after:** `TestRegisterLoginLogoutMeCycle` (`auth_flow_test.go:48-91`),
  `TestRegisterCreatesTenantAndSession` (`auth_repo_test.go:43-95`).

### Test 2 — Registration with optional info persists
- **Level:** integration (handler) + repo.
- **Harness:** include `gstin` (`27AAPBC1234F1ZV`), PAN, drug-license no./
  expiry, owner name/phone in the register body; POST, then re-read via
  `GET /api/store` and a direct `testPoolDB` SELECT.
- **Assertions:** store row carries the exact values back; GSTIN stored on the
  linked `gst_registrations` row (`gst_registration_id` set) and/or on the
  store per the new model; all values match byte-for-byte.
- **Model after:** `TestRegisterCreatesTenantAndSession`
  (`auth_repo_test.go:88-94` asserts `GSTRegistrationID` link).

### Test 3 — Settings update of store name/phone/owner/address
- **Level:** handler (primary) + repo.
- **Harness:** as owner `PUT /api/store` with new name/address/owner/phone
  (today the handler accepts only `name/address/max_employees`,
  `employees.go:96-105` — extend it for the new fields); then `GET /api/store`.
- **Assertions:** PUT returns 200 with updated store; subsequent GET reflects
  every changed field; one `audit_logs` row for `store.settings.update`
  (`employees.go:110-113`).
- **Model after:** `TestStoreSettingsSeatResize` (`auth_repo_test.go:280-309`)
  for the repo; `doJSON`/`doJSONAs` for the handler.

### Test 4 — Add optional fields (GSTIN/PAN/DL/DL expiry), verify persistence
- **Level:** repo (primary) + handler.
- **Harness:** register minimal; then update via the settings repo/handler to
  add GSTIN, PAN, DL number, DL expiry; re-read `GetStore` /
  `GET /api/store`.
- **Assertions:** previously-null fields now non-NULL with exact values; GSTIN
  passes `tax.ValidateGSTIN`; audit row written.
- **Model after:** `TestStoreSettingsSeatResize` (`auth_repo_test.go:280-309`);
  GSTIN reuse pattern from `auth_repo.go:86-99`.

### Test 5 — Clear optional fields
- **Level:** repo + handler.
- **Harness:** seed a store with all optional fields populated (via Test 4
  setup or direct SQL INSERT); then update with the optional fields set to
  `""`/`null`; re-read.
- **Assertions:** optional columns are NULL again; mandatory fields untouched;
  clearing GSTIN must also detach/clear the `gst_registration_id` per model.
- **Model after:** SQL-level read back via `testPoolDB` (pattern
  `auth_flow_test.go:250-257`).

### Test 6 — Mandatory validation (empty store_name/owner/phone/address rejected, no DB change)
- **Level:** repo + handler.
- **Harness:** call `Register` / `PUT /api/store` with each mandatory field
  empty in turn; then `SELECT COUNT(*)` on `stores`/the settings row.
- **Assertions:** every empty-mandatory case errors (400 at the handler,
  `"name, phone and store_name are required"` at repo `auth_repo.go:54-55`,
  `"store name is required"` `auth_repo.go:448-449`); **no** row inserted /
  **no** row changed after the failures.
- **Model after:** `TestRegisterValidationAndDuplicates`
  (`auth_repo_test.go:97-143`); no-DB-change precedent
  `repository_test.go:161-191`.

### Test 7 — Optional fields may be empty
- **Level:** repo + handler.
- **Harness:** register/update with every optional field `""`/`null` together.
- **Assertions:** request succeeds (200/201); optional columns NULL;
  `GET /api/store` shows them null, mandatory fields preserved.
- **Model after:** minimal `registerUser` body (`auth_flow_test.go:28-35`) with
  a null-GSTIN check (`auth_repo.go:86` explicitly skips empty GSTIN).

### Test 8 — Store isolation (Store A update doesn't touch Store B)
- **Level:** repo (primary) + handler.
- **Harness:** seed a second store B (pattern: `seedState27Store`
  `gst_test.go:570-580`, or inline INSERT `auth_flow_test.go:228-233`);
  update store A's settings; read store B's row.
- **Assertions:** `UpdateStoreSettings(A, ...)` returns A's new values and
  `GetStore(B)` / a direct `testPoolDB` SELECT on B shows B's original values
  byte-for-byte; `audit_logs` row scoped to A only.
- **Model after:** `TestClientStoreIDIsIgnored` (`auth_flow_test.go:224-301`).

### Test 9 — Unauthorized update (403)
- **Level:** handler.
- **Harness:** `PUT /api/store` as `employeeRawToken`; also anonymous with
  `""` token.
- **Assertions:** employee → 403 (route is behind `RequireOwner`,
  `router.go:77-82`, `middleware.go:83-92`); anonymous → 401 (behind
  `RequireAuth`); assert **no** store row / audit row changed.
- **Model after:** `TestRoleGatingOwnerVsEmployee` (`auth_flow_test.go:130-189`,
  especially the store GET 403 entry at `:134/:146`).

### Test 10 — Existing data regression
- **Level:** repo + handler.
- **Harness:** seed a store + owner session with known mandatory/optional
  values; apply a full settings update; re-read everything.
- **Assertions:** all fields that were NOT part of the update remain bit-for-bit
  identical (e.g. `is_active`, `created_at`, seat cap, GST registration link
  when untouched); update only mutates the intended fields.
- **Model after:** `TestReconcileCorrectsStockAndLeavesSalesHistoryIntact`
  (`repository_test.go:540-603`).

### Test 11 — Historical invoice snapshot preserved
- **Level:** repo (integration, DB-backed).
- **Harness:** create a checkout with the store's current GST registration
  (`seedState27Store` + `seedGSTMedicine`, `gst_test.go:572-589`, `:15-105`);
  record the invoice's `sales_invoices.*` and `sales_invoice_items.*` snapshot;
  change store settings (name/address and any shop-details fields); re-read the
  invoice rows.
- **Assertions:** `sales_invoices` / `sales_invoice_items` GST snapshot columns
  (gstin, rates, amounts via the store's registration at sale time) are
  unchanged after the settings update.
- **Model after:** `TestInvoiceTaxPersistedAfterRateChange`
  (`gst_test.go:618-737`, reads `sales_invoice_items` via `pool.QueryRow` at
  `:696-737`).

### Test 12 — Reload persistence
- **Level:** integration (handler → repo → DB).
- **Harness:** register or update with a full set of optional values; then
  re-fetch through a second code path — a fresh `doJSONAs(GET /api/store)`
  (new request/round-trip rather than reusing the update response) and a direct
  `testPoolDB` SELECT.
- **Assertions:** both reads return identical values; nothing is lost across
  process/round-trip reload.
- **Model after:** `TestRegisterCreatesTenantAndSession`
  (`auth_repo_test.go:76-94` re-reads via `ValidateSession` + `GetStore`).

---

## 4. File / package placement recommended

| Test # | File to create (or extend) | Package | Level |
|---|---|---|---|
| 1, 2 | `internal/handlers/store_details_test.go` | `handlers` | integration |
| 3, 4, 5, 6, 7 | `internal/repository/store_details_test.go` (+ handler file for #3) | `repository_test` | repo / handler |
| 8 | `internal/repository/store_isolation_test.go` + `internal/handlers/store_details_test.go` | `repository_test` / `handlers` | repo / handler |
| 9 | `internal/handlers/store_details_test.go` | `handlers` | handler |
| 10 | `internal/repository/store_details_test.go` | `repository_test` | repo |
| 11 | `internal/repository/gst_test.go` (extend) or new `store_details_test.go` | `repository_test` | repo |
| 12 | `internal/handlers/store_details_test.go` + `internal/repository/store_details_test.go` | `handlers` / `repository_test` | integration |

Conventions to follow: every repo test begins with `reset(t)`; handler tests
use `doJSON`/`doJSONAs` + `testPoolDB` assertions; new frontend tests (if any)
follow the Vitest + RTL + `vi.stubGlobal('fetch', ...)` pattern with
`vi.unstubAllGlobals()` in `afterEach`.

## 5. Gaps / prerequisites surfaced

1. **The `stores` table has no PAN / drug-license (DL) / DL-expiry / owner
   phone columns today** (`migrations/011_create_business_gst_registrations.sql:29-37`
   + `migrations/029_auth_core.sql:39-41`); `gst_registrations.pan` exists
   (`011:16`). Tests 2/4/5/7 require the new columns (or a new settings
   table/registration extension) before they can pass.
2. **`RegisterInput` has no PAN/DL fields** (`auth_repo.go:30-41`); only GSTIN.
   Handler `register` likewise (`auth.go:39-48`). Tests 1/2/7 depend on the
   extended body.
3. **`UpdateStoreSettings` only accepts `name/address/max_employees`**
   (`auth_repo.go:436-460`; handler `employees.go:96-105`). Tests 3-5 need the
   extended signature + audit detail.
4. **No second-store seed helper** — add a `testutil` helper (or reuse the
   `seedState27Store` CTE / `TestClientStoreIDIsIgnored` inline INSERT) for
   Test 8.
5. **No handler-level `PUT /api/store` test and no frontend
   `StoreSettings.test.tsx`** exist yet — both are greenfield.

## 6. Confidence flags

- **[REG-CRITICAL]** Test 11 (historical invoice snapshot) — the single most
  important regression guard; precedent already green in `gst_test.go:618-737`.
- **[REG-HIGH]** Tests 8 & 9 (isolation + 403) mirror already-proven behavior
  in `auth_flow_test.go:224-301` and `:130-189`.
- **[REG]** Tests 1, 6, 7 — minimal/mandatory/optional contract of the
  register + settings surface; block feature completion.
