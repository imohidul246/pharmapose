# HSN / Tax — Frontend & IndexedDB Audit

Sub-Agent C · Frontend/IndexedDB Auditor · RESEARCH ONLY (no code written)
Scope: `/home/mohi/dev/pms-marg-inspired/web/src`

---

## 1. API client — `web/src/lib/api.ts` (447 lines)

Base path style: **`/api/...`** everywhere. One shared `request<T>(path, init)` helper at `api.ts:47-70` wraps `fetch` with JSON headers, `{error}` body unwrapping, and an `UnauthorizedError` on `401` + `message === 'unauthorized'` (dispatches `pms:unauthorized`). The `api` object starts at `api.ts:84`. There is **no auth token in the client** — the backend uses cookie sessions.

### Methods relevant to medicines / HSN / tax / sync / stores / purchases

| Method | Line | Endpoint |
|---|---|---|
| `api.medicineDetail(id)` | `api.ts:326` | `GET /api/medicines/:id/detail` — returns `MedicineDetail` **including `tax_config`** (probably-attached by repo) |
| `api.listHSNCodes()` | `api.ts:346` | `GET /api/hsn` → `{ hsn_codes: HSNCode[] }` |
| `api.createHSNCode(code, description)` | `api.ts:350` | `POST /api/hsn` |
| `api.upsertTaxRate(hsnCodeId, {gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate})` | `api.ts:354` | `PUT /api/hsn/:hsnCodeId/tax-rate` |
| `api.getMedicineTaxConfig(medicineId)` | `api.ts:364` | `GET /api/medicines/:medicineId/tax-config` → `MedicineTaxConfig \| null` |
| `api.upsertMedicineTaxConfig(medicineId, {hsn_code_id, tax_rate_id, price_includes_tax})` | `api.ts:368` | `PUT /api/medicines/:medicineId/tax-config` |
| `api.checkout(req)` | `api.ts:218` | `POST /api/sales/checkout` → `CheckoutResponse` (backend computes tax) |
| `api.createPurchase(req)` | `api.ts:225` | `POST /api/purchases` |
| `api.createPurchaseRequest(req)` / `purchaseRequests()` / approve/reject/cancel | `api.ts:127-152` | `/api/purchase-requests…` |
| `api.suppliers()` / create/update/delete | `api.ts:330-344` | `/api/suppliers…` |
| `api.store()` / `api.updateStore(...)` | `api.ts:207-216` | `GET/PUT /api/store` |
| `api.customers()` / `searchCustomers(opts)` / create/update | `api.ts:245-264` | `/api/customers…` (search uses `?search=&type=&limit=`) |
| GST returns (`gstr1Preview`, `downloadGSTR1JSON/CSV`, `gstr3b`, `gstr2bBatches`, `gstr2bBatch`, `importGSTR2B`, `downloadB2BInvoicePDF`) | `api.ts:376-446` | `/api/gst/…`, `/api/sales/invoices/:id/pdf` — these accept an **optional `store_id` query param** |

### Why it matters for the plan
- The HSN/tax write path already exists end-to-end (`listHSNCodes`, `upsertTaxRate`, `upsertMedicineTaxConfig`).
- **There is no bulk/sync read of HSN/tax configs.** `medicineDetail` returns tax per-medicine only, and `listHSNCodes` returns only codes (no rates). The `MedicineTaxConfig` and `TaxRate` types already model the data (`types.ts:490-514`).

---

## 2. IndexedDB layer — `web/src/lib/db.ts` (103 lines)

### Structure
- DB name **`pms-cache`** (`db.ts:16`), version **1** (`db.ts:17`).
- Schema `PMSDB` (`db.ts:4-14`):
  - `medicines_cache` — keyPath `id`, value `MedicineWithBatches` (`db.ts:5-8`)
  - `customers_cache` — keyPath `id`, value `Customer`, index **`by-name` on `name`** (`db.ts:9-13`)
- Singleton `dbPromise` via `idb`'s `openDB` (`db.ts:19-36`); `upgrade()` creates stores guarded by `objectStoreNames.contains(...)`.

### Existing store-naming convention
**`<entity>_cache`** (plural entity, snake_case): `medicines_cache`, `customers_cache`. There is **no HSN/tax store** today.

### Naming-convention summary
- Store names use the pattern `medicines_cache` / `customers_cache` → for HSN/tax this should be `hsn_codes_cache` and/or `medicine_tax_cache` (see Recommendations §R1).

### Sync / offline caching flow (THE pattern to mirror)
`syncLocalCache()` (`db.ts:48-75`), called from `App.tsx`:
1. `Promise.all([ fetch('/api/sync/inventory'), fetch('/api/sync/customers') ])` (`db.ts:49-52`) — **raw `fetch`, not the `api` object**.
2. Parses `SyncInventoryResponse` (`{synced_at, medicines: MedicineWithBatches[]}`) and `{customers: Customer[]}` (`db.ts:56-57`).
3. Two `readwrite` transactions: `txM.store.clear()` then `put` each medicine into `medicines_cache` (`db.ts:60-63`); same for customers into `customers_cache` (`db.ts:65-68`).
4. Returns `{syncedAt, medicineCount, customerCount}` (`db.ts:70-74`).

Read helpers:
- `loadCachedMedicines()` → `db.getAll('medicines_cache')` (`db.ts:77-80`)
- `loadCachedCustomers()` → `db.getAllFromIndex('customers_cache', 'by-name')` (`db.ts:82-85`)
- `upsertCachedCustomer(c)` — single-record put after inline customer creation in POS (`db.ts:90-93`)

Backend plumbing (for reference): routes are registered at `internal/handlers/router.go:113-117`:
- `GET /api/sync/inventory` → `d.getInventorySync` (`internal/handlers/medicines.go:18-26`) → `MedicineRepo.InventorySnapshot`, returns `{synced_at, medicines}`.
- `GET /api/sync/customers` → `d.getCustomersSync` (`internal/handlers/medicines.go:28-35`) → `CustomerRepo.List`, returns `{synced_at, customers}`.

### Where sync is triggered
- `App.tsx:98-108` `doSync()` → `syncLocalCache()`; first run on login (`App.tsx:110-114`) and manually via the **“Sync now”** button (`App.tsx:221-227`).
- `cacheVersion = sync.result?.syncedAt.getTime() ?? 0` is threaded into POS / Purchases / Reconcile as a prop (`App.tsx:236-252`) to re-read the cache when a sync completes.

---

## 3. Billing page (POS) — `web/src/pages/POS.tsx` (1081 lines)

### Medicine selection (line numbers)
- Catalog loaded from IndexedDB: `Promise.all([loadCachedMedicines(), loadCachedCustomers()])` (`POS.tsx:76-88`).
- Live search: `hits = useMemo(() => searchMedicines(medicines, query), …)` (`POS.tsx:90`); `searchMedicines` is fully local against the cache (`lib/search.ts:23-64`).
- Result list rendering: `SearchRow` items (`POS.tsx:311-319`, component at `POS.tsx:854-901`); Enter / click sets `setPickerFor(hit.medicine)` (`POS.tsx:143-147`).
- Batch chooser modal `BatchPickerModal` (`POS.tsx:838-849` render; `POS.tsx:903-1081`).
- `addBatch(m, batchId)` adds a cart line using FEFO batch (`POS.tsx:93-134`); new `CartLine` object built at `POS.tsx:113-130`.

### HSN / tax display
- **None at selection time.** `MedicineWithBatches` has no HSN/tax fields (`types.ts:11-20`), `CartLine` has no tax fields (`POS.tsx:16-31`), and neither `SearchRow` nor `BatchPickerModal` nor cart lines render HSN/GST.
- The only tax UI is the **post-checkout receipt**: GST summary at `POS.tsx:360-421` reading `receipt.invoice.supply_type / gross_amount / taxable_amount / igst_total / cgst_total / sgst_total / tax_total / grand_total` — all values **returned by the backend**.

### How tax is computed
- **Backend only.** Checkout payload sends `payment_type, customer_id, store_id, place_of_supply, sale_type, buyer_*, items[{batch_id, quantity, sell_price, bonus_quantity, discount}]` — **no tax/HSN values** (`POS.tsx:210-252`). The server (`POST /api/sales/checkout`) resolves each medicine's tax config and computes amounts, returning them in `CheckoutResponse`/`InvoiceItem` (`types.ts:78-106`).
- Frontend arithmetic is only discount math (`POS.tsx:41-46`).

### "Edit Tax" capability
- **None in POS.** No HSN/GST editing anywhere on the billing screen.

---

## 4. Purchase page — `web/src/pages/Purchases.tsx` (885 lines)

### Existing-medicine flow
- `modeToggle === 'existing'`: catalog search over `loadCachedMedicines()` (`Purchases.tsx:117-125`), `hits` (`:131-134`), `pick(m)` fills batch/prices from FEFO batch but **never reads any tax config** (`Purchases.tsx:137-151`).
- Staged line for existing medicine carries **empty `hsnCode` and the shared `priceIncludesTax` draft flag** (`Purchases.tsx:207-208`), and its payload sends only `medicine_id, batch_number, …, purchase_price, sale_price, discount_*` (`Purchases.tsx:254-265`) — **no HSN, no tax**.
- The `MedicineWithBatches` type has no HSN/tax field, so nothing is available to prefill.

### New-medicine flow
- `modeToggle === 'new'`: manual `newMed` fields (`Purchases.tsx:79-85`); HSN + “MRP includes tax” inputs at `Purchases.tsx:463-480` (bound to `draft.hsnCode`, `draft.priceIncludesTax`).
- New-medicine payload optionally sends `hsn_code` and `price_includes_tax` (`Purchases.tsx:267-282`).
- Submit builds `PurchaseLineInput[]` (`Purchases.tsx:253-283`), wraps with supplier + invoice meta (`:285-293`), then `api.createPurchaseRequest(payload)` (employee/submit mode) or `api.createPurchase(payload)` (owner/record mode) (`:294-304`).

### Where a medicine's tax config is read
- **Nowhere in Purchases.** Tax config is only read via `api.medicineDetail(id).tax_config` on the Medicines page. The purchase submit path just passes through.

---

## 5. Medicine page — `web/src/pages/Medicines.tsx` (687 lines)

### Tax Configuration section
- Detail loads via `api.medicineDetail(id)` → `loadDetail` (`Medicines.tsx:34-46`); `MedicineDetail.tax_config` flows in (`types.ts:464-481`; repo embeds it at `internal/repository/medicine_repo.go:~374-377`).
- Rendering cascade: `DetailPanel` (`:271-456`) mounts `TaxConfigSection` at `Medicines.tsx:303`.

### `TaxConfigSection` (`Medicines.tsx:460-665`) — current state
- **It is NOT read-only.** It already has a full read + Edit flow:
  - Read-view render: `:529-561` — shows `HSN`, `GST`, `CGST`, `SGST`, conditional `IGST` (only if `> 0`), `Price includes tax`; **Edit / “Assign HSN & tax” button** at `:534-539`.
  - Empty state text: “No tax configuration assigned. Tax will not be computed at checkout.” (`:557`).
  - Edit form: `:563-664` — HSN `<select>` populated from `api.listHSNCodes()` in `startEdit` (`:474-484`); GST/CGST/SGST/IGST/Cess inputs (`:585-631`); “MRP includes tax” checkbox (`:633-641`).
  - `save()` (`:486-527`): 1) selects required HSN (`:490-497`), 2) `api.upsertTaxRate(hsnId, {gst, cgst = gst/2, sgst = gst/2, igst = gst, cess})` (`:500-511`), 3) `api.upsertMedicineTaxConfig(medicineId, {hsn_code_id, tax_rate_id, price_includes_tax})` (`:514-518`).

### Gaps
- Tax data is **network-only** — `detail.tax_config` from `medicineDetail`, HSN list from `listHSNCodes`. Nothing is cached (contrast with medicines/customers which are fully offline-capable).
- After a save, the local React state stays stale; there's a `saved` flash but no cache update.

---

## 6. Auth / store context — `web/src/lib/auth.tsx` + `web/src/types.ts`

### How the frontend knows the store
- `AuthProvider` holds `session: AuthSession | null` (`auth.tsx:18`), loaded once via `api.me()` (`auth.tsx:25-40`), cleared on `pms:unauthorized`.
- `AuthSession = { user: AuthUser, principal: Principal }` (`types.ts:650-653`).
- `Principal` (`types.ts:633-639`) carries **`store_id: string`** plus `id, name, role, permissions`.
- Consumers get it via `useAuth().session.principal` (`auth.tsx:76-80`). It is surfaced in the UI at `components/AccountChip.tsx:51` (`p.store_id.slice(0,8)`).

### How the active store is applied
- **It is NOT sent on most API calls.** The API is cookie-session based and single-tenant: repos are pinned to one store via `storeIDRef` (`internal/repository/store_id.go:19-47`), and the `/api/sync/*` and `/api/medicines/*` handlers all resolve the store server-side.
- The only frontend store plumbing:
  - `store_id: import.meta.env.VITE_STORE_ID || undefined` on checkout (`POS.tsx:224`).
  - Optional `store_id` query param on GST-return endpoints (`api.ts:380-408`).

---

## 7. Store-scoping of medicine fetches

- `syncLocalCache()` calls `fetch('/api/sync/inventory')` with **no `store_id`** (`db.ts:50`); the handler `getInventorySync` (`internal/handlers/medicines.go:18-26`) → `MedicineRepo.InventorySnapshot` — pinned to the single store. Same for `/api/sync/customers`.
- The **frontend never passes `store_id` for medicine/tax reads**. Any new HSN/tax sync should follow the same shape (no `store_id` param) to stay consistent with the single-tenant model, or reuse the existing `store_id`-as-query-param convention if multi-store support is intended later.

---

## 8. Tax calculation approach (frontend duplicate vs backend)

- **Backend computes tax.** The frontend only:
  - Displays backend-computed tax on the POS receipt (`POS.tsx:360-421`) and on Invoices (`pages/Invoices.tsx:470,516,544-561`).
  - Builds the tax-rate upsert payload in `TaxConfigSection.save` with defaults `cgst = gst/2, sgst = gst/2, igst = gst` (`Medicines.tsx:500-504`) — this is config authoring, not checkout math.
- `gst/cgst/sgst/igst` searches across `POS.tsx` and `Purchases.tsx` confirm: **zero frontier tax arithmetic** in both; all matches are buyer-GSTIN metadata (POS) and supplier GSTIN/HSN passthrough (Purchases). `hsn_code` is only ever a free-text string for new medicines (`Purchases.tsx:268`) or display text.
- The GST engine lives server-side (`internal/tax/calculator.go`, `internal/tax/rules.go`) and tax configs are read from `internal/repository/tax_repo.go` (`GetMedicineTaxConfig`, `GetActiveTaxRate`, `GetHSNByCode`).

**Implication:** any “Edit Tax at POS / Purchases” feature only needs to change the **tax config** (HSN + rate + price-includes), never recompute a bill — checkout re-reads config server-side.

---

## 9. Test setup — `web/package.json`, `web/vite.config.ts`, `web/src/test-setup.ts`

### Runner
- **Vitest**: `"test": "vitest run"` (`web/package.json:10`); deps `vitest ^4.1.11`, `jsdom ^30`, `@testing-library/react ^16`, `@testing-library/user-event ^14`, `@testing-library/jest-dom ^7` (`package.json:22-34`).
- Vite config: `test.environment = 'jsdom'`, `test.setupFiles = './src/test-setup.ts'` (`vite.config.ts:7-10`).

### IndexedDB in tests
- **`fake-indexeddb`** is enabled globally in `test-setup.ts:1` (`import 'fake-indexeddb/auto'`), plus jest-dom matchers (`:2`) and a `scrollIntoView` stub (`:4`).
- Tests **seed the same schema by hand** with `openDB('pms-cache', 1, upgrade(){…})` replicating `medicines_cache` + `customers_cache` (e.g. `POS.test.tsx:10-23`, `POS.customer.test.tsx:78-92`), then `db.put('medicines_cache', …)`.
- `fetch` is stubbed per-test with `vi.stubGlobal('fetch', …)` returning canned JSON (`POS.customer.test.tsx:22-75`, `Customers.test.tsx:19 ff.`); `afterEach(() => { cleanup(); vi.unstubAllGlobals() })`.

### Existing test files
- `web/src/pages/POS.test.tsx`
- `web/src/pages/POS.customer.test.tsx`
- `web/src/pages/Customers.test.tsx`
- `web/src/pages/Invoices.test.tsx`
- `web/src/pages/__tests__/GSTReportsPage.test.tsx`
- `web/src/lib/states.test.ts`

---

## Recommendations

### R1. IndexedDB object store naming (follow existing convention)

Existing stores are `<entity>_cache`. Propose two stores:

- **`hsn_codes_cache`** — `keyPath: 'id'`, value `HSNCode` (optionally enriched with its active `TaxRate`). Mirrors `customers_cache`.
- **`medicine_tax_cache`** — `keyPath: 'id'` where **`id` = `medicine_id`**, value `MedicineTaxConfig` (the type already embeds `hsn_code` + `tax_rate` snapshot, `types.ts:503-514`). Keying by medicine id gives O(1) lookups at POS add-line and Purchases pick, mirroring `medicines_cache` (keyed by medicine id).

Bump `DB_VERSION` 1 → 2 (`db.ts:17`) and create both stores in the existing `upgrade()` guard pattern (`db.ts:24-32`). No index needed for either initially (lookup by id; HSN list reads via `getAll`).

### R2. Sync flow mirroring medicine/customer sync

Add a third arm to the existing pipeline:

1. **Backend**: register `GET /api/sync/tax` in the existing `sync` group (`internal/handlers/router.go:113-117`), e.g. `sync.GET("/tax", auth.RequirePermission(auth.PermStockView), d.getTaxConfigSync)`. Handler returns `{ synced_at, hsn_codes: HSNCode[], tax_configs: MedicineTaxConfig[] }` by adding a repo method that lists all HSN codes + active rates + all medicine tax configs (there is no store_id scope in `tax_repo.go` today — consistent with `/api/sync/inventory`'s single-tenant behavior).
2. **db.ts**: extend `syncLocalCache()` (`db.ts:48-75`) to `Promise.all` a third `fetch('/api/sync/tax')`, then in a readwrite tx `clear()` + `put` all into `hsn_codes_cache` and `medicine_tax_cache`. Optionally extend `SyncResult` (`db.ts:38-42`) with `hsnCodeCount`/`taxConfigCount`.
3. This slots into the existing trigger points automatically: `doSync` at `App.tsx:98-108` (login + Sync-now button) — no App.tsx changes required beyond optionally surfacing counts.
4. Use **raw `fetch`** for the sync fetch (matching the current pattern in `db.ts:49-52`), not the `api` object — OR add a thin `api.syncTaxSnapshot()` for symmetry if preferred. Parity with the existing code argues for raw `fetch` inside `syncLocalCache`.

### R3. Reads should hit IndexedDB first

Add to `db.ts`, mirroring `loadCachedMedicines`/`loadCachedCustomers`:
- `loadCachedHSNCodes(): Promise<HSNCode[]>` (`getAll('hsn_codes_cache')`)
- `loadCachedMedicineTaxConfig(medicineId): Promise<MedicineTaxConfig | undefined>` (`get('medicine_tax_cache', medicineId)`)
- `upsertCachedMedicineTaxConfig(cfg)` / `upsertCachedHSNCode(h)` for post-edit cache refresh (mirror `upsertCachedCustomer`, `db.ts:90-93`).

Read flow everywhere: **IndexedDB first → network fallback on miss → upsert cache on success.** This matches the offline-first principle already used by POS/Purchases/Medicines catalog loads and by `searchMedicines` (`lib/search.ts`).

### R4. Components needing cached HSN read + Edit Tax

- **`web/src/pages/POS.tsx`**
  - Show HSN + GST% in `SearchRow` (`:854-901`) and/or `BatchPickerModal` (`:903-1081`) and on cart lines (from `medicine_tax_cache[medicineId]`; extend `CartLine` at `:16-31` with `hsnCode`/`gstRate` display fields). Lookup at `addBatch`/`setPickerFor` so one cache read per added medicine.
  - Add an **“Edit Tax”** affordance on the cart line or a small per-medicine inspector reusing the tax editor (ideally extracted from `Medicines.tsx`), with `upsertCachedMedicineTaxConfig` + `api.upsertTaxRate`/`api.upsertMedicineTaxConfig` on save.
- **`web/src/pages/Purchases.tsx`**
  - In `pick()` (`:137-151`), read cached tax config for the selected medicine and display HSN + GST (+ optionally prefill `draft.hsnCode` or an existing-med tax field).
  - Add Edit Tax before staging, especially to wire HSN to existing-medicine lines (currently they send no HSN at all, `:254-265`).
- **`web/src/pages/Medicines.tsx`**
  - `TaxConfigSection` (`:460-665`) already implements read + Edit; keep it, but:
    - load `detail.tax_config` cache-first (fall back to `api.medicineDetail`), and source the HSN dropdown from `loadCachedHSNCodes()` before hitting `api.listHSNCodes()` (`:479`);
    - after an edit, `upsertCachedMedicineTaxConfig` + refresh HSN list cache so POS/Purchases pick it up without a sync.

### R5. Exact API client methods needed (reuse vs new)

**Reuse (already in `api.ts`, no changes):**
- `api.listHSNCodes()` — `api.ts:346`
- `api.upsertTaxRate(hsnCodeId, rates)` — `api.ts:354`
- `api.getMedicineTaxConfig(medicineId)` — `api.ts:364` (network fallback for a single medicine)
- `api.upsertMedicineTaxConfig(medicineId, config)` — `api.ts:368`
- `api.medicineDetail(id)` — `api.ts:326` (unchanged; still returns `tax_config`)

**New (only for bulk sync — nothing else needed):**
- One method/endpoint for the HSN+tax snapshot, e.g. `GET /api/sync/tax` as described in R2. Implementation consistent with existing sync = raw `fetch` in `db.ts`; if instead routed through the `api` object, add a single method like `api.syncTax()`. **No other new API client methods are required** — the full HSN/tax write surface already exists.

---

### Appendix — key line-number index

| Concern | Location |
|---|---|
| IndexedDB schema / stores | `web/src/lib/db.ts:4-36` |
| Sync pipeline | `web/src/lib/db.ts:48-75` |
| Cache loaders | `web/src/lib/db.ts:77-93` |
| HSN/tax API methods | `web/src/lib/api.ts:346-374` |
| POS medicine selection | `web/src/pages/POS.tsx:90,143-147,311-319,838-849,903-1081` |
| POS cart line (no tax) | `web/src/pages/POS.tsx:16-31` |
| POS receipt tax display | `web/src/pages/POS.tsx:360-421` |
| POS checkout (no tax sent) | `web/src/pages/POS.tsx:210-252` |
| Purchases staged line / draft | `web/src/pages/Purchases.tsx:16-61` |
| Purchases pick (no tax read) | `web/src/pages/Purchases.tsx:137-151` |
| Purchases new-med HSN inputs | `web/src/pages/Purchases.tsx:463-480` |
| Purchases submit (existing lacks HSN) | `web/src/pages/Purchases.tsx:253-304` |
| Medicines TaxConfigSection | `web/src/pages/Medicines.tsx:460-665` |
| Auth session / principal.store_id | `web/src/lib/auth.tsx:18,25-40`; `web/src/types.ts:633-653` |
| Sync routes (backend) | `internal/handlers/router.go:113-117` |
| Sync handlers (backend) | `internal/handlers/medicines.go:18-35` |
| Tax handlers (backend) | `internal/handlers/tax.go` (GET/PUT tax-config, HSN, tax-rate) |
| Store scoping (single-tenant) | `internal/repository/store_id.go:19-47` |
| Test runner / setup | `web/vite.config.ts:7-10`; `web/src/test-setup.ts:1-4`; `web/package.json:10` |