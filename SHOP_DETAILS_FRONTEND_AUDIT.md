# SHOP_DETAILS_FRONTEND_AUDIT

Audit of the React + TypeScript frontend (`web/`) of PharmaPOS with focus on
registration, shop/store details, settings, caching and setup completion.

Audit date: 2026-08-30 · Read-only (no code modified)

---

## 1. Routing / auth flow (`web/src/App.tsx`)

- No react-router. The app is a single page with tab state. `App` (App.tsx:58-64)
  renders `<AuthProvider><Gate/></AuthProvider>`.
- `Gate` (App.tsx:66-85): while `loading`, shows a splash; if no `session` it
  returns `<Login/>`; otherwise `<Workspace/>`. **Session presence is the only
  gate to the app.**
- `Workspace` (App.tsx:87-263) keeps `tab` state (`useState<Tab>('pos')`).
  Tabs defined at App.tsx:33-50. Owner-only tabs are Approvals, Employees,
  Settings (App.tsx:45-50, guarded at render App.tsx:257-259).
  Settings renders `<StoreSettings/>` at App.tsx:259 only for `isOwner`.
- Session bootstrap: `AuthProvider` calls `api.me()` on mount (auth.tsx:23-40),
  which resolves the session from the server-side HttpOnly cookie.
  `login()` (auth.tsx:48-53) calls `api.login()` then `api.me()`;
  `register()` (auth.tsx:55-59) calls `api.register()` which sets the session.
- Sync-on-login: App.tsx:110-114 runs `syncLocalCache()` once per session
  (`firstSync` ref). Manual "Sync now" button at App.tsx:221-227.

## 2. Registration page

There is **no separate registration page** — registration is a toggle on the
login page (`web/src/pages/Login.tsx`). `mode` state switches between
`{mode:'login'}` and `{mode:'register'}` (Login.tsx:12, 187-198).

**What the registration form currently collects** (register fields rendered at
Login.tsx:98-147, payload built at Login.tsx:33-42):

| Field          | UI label     | Location         | Required? |
|----------------|--------------|------------------|-----------|
| owner name     | Owner name   | Login.tsx:100-108| implicit  |
| business name  | Business name| Login.tsx:109-117| implicit  |
| store name     | Store name   | Login.tsx:118-126| implicit  |
| store address  | Store address| Login.tsx:127-136| implicit  |
| GSTIN          | GSTIN (optional) | Login.tsx:137-145 | optional (labelled) |
| phone          | Phone (login)| Login.tsx:149-159| yes (gate)|
| password       | Password     | Login.tsx:160-170| yes (>=8, gate)|

Notes:
- There is **no owner/store contact phone** (the `phone` field is the *login*
  phone stored in `users.phone`, not a shop contact number).
- **No PAN, no drug-license (DL), no DL-expiry** fields are collected anywhere
  on the frontend.
- GSTIN is the only field labelled "(optional)" — every other field is
  effectively mandatory but none are labelled required/optional beyond that.
- `trade_name` is accepted by the API (api.ts:103) but never collected by the
  UI.

**Register API call**: `api.register(...)` → `POST /api/auth/register`
(api.ts:98-112), body `{name, phone, password, business_name, trade_name?,
gstin?, store_name, store_address}`.

## 3. Settings page (`web/src/pages/StoreSettings.tsx`)

Owner-only (App.tsx:259; backend route is owner-restricted, router.go:77-82).

- Load: `Promise.all([api.store(), api.employees()])` (StoreSettings.tsx:15-24).
- Editable form section (StoreSettings.tsx:67-107):
  - **Store name** (input) — StoreSettings.tsx:68-75
  - **Address** (textarea) — StoreSettings.tsx:76-84
  - **Staff seat limit** (`max_employees`) — StoreSettings.tsx:85-93
  - Save → `api.updateStore(name, address, seats)` (StoreSettings.tsx:26-41).
- Read-only "GST registration" card (StoreSettings.tsx:109-120) shows only
  `store.gst_registration_id.slice(0,8)` — the UUID prefix. It does **not**
  show the GSTIN, business/trade name, PAN, address or state.

**So a "Shop Details" section already exists in partial form** (store name +
address + seat limit) but is missing: owner name, business name, shop phone,
GSTIN text, PAN, DL, DL expiry. GSTIN collected at registration is never
displayed anywhere in the UI.

**Store API calls**: `api.store()` → `GET /api/store` (api.ts:207-209);
`api.updateStore(name, address, maxEmployees)` → `PUT /api/store`
(api.ts:211-216), body `{name, address, max_employees}`.

## 4. Cache (IndexedDB) for shop details

IndexedDB lives in `web/src/lib/db.ts`:
- DB `pms-cache`, version 2 (db.ts:25-26).
- Object stores (db.ts:4-23): `medicines_cache`, `customers_cache`,
  `hsn_codes_cache`, `medicine_tax_cache`.
- `syncLocalCache()` (db.ts:68-110) fetches `/api/sync/inventory`,
  `/api/sync/customers`, `/api/sync/tax` and clears+rewrites those four stores.
  It is executed automatically after login (App.tsx:110-114) and on manual sync.

**There is NO cache of shop/store/business details.** Store settings are
fetched fresh (network) every time StoreSettings or Employees mounts
(StoreSettings.tsx:17, Employees.tsx:17). There is also **no localStorage /
sessionStorage usage anywhere** (grep for `localStorage|sessionStorage` in
`web/src` returned no matches; session is a cookie managed server-side).

Consequences:
- Registration does not write anything to IndexedDB (no store bootstrap cache).
- Settings changes are never mirrored into any cache (nothing to update,
  because store data is not cached at all).
- Store `name` shown on the Employees seat banner (Employees.tsx:94) also comes
  from a fresh `api.store()` fetch.

## 5. Setup completion determination

There is no `setup_completed` / `shop_created` flag anywhere in the frontend.
"Setup complete" is effectively **"a valid session exists"**:

- `Gate` renders `Login` when `session == null` (App.tsx:82) and `Workspace`
  otherwise.
- `AuthProvider` restores the session via `api.me()` (auth.tsx:23-40); a 401
  (message `unauthorized`) triggers `pms:unauthorized` → `setSession(null)`
  (api.ts:63-66, auth.tsx:42-46).
- The store is identified by `principal.store_id` (types.ts:654), resolved
  server-side from the membership — never persisted client-side (AccountChip
  shows it at AccountChip.tsx:51). `store_id` is not stored in IndexedDB or
  localStorage.
- Login.tsx copy calls registration "First run — open a new store" and says
  "Existing installs skip this" (Login.tsx:89, 94), but there is no client
  detection of "first run"; the user simply toggles to Register mode by hand.

## 6. TypeScript types for store/settings (`web/src/types.ts`)

- `Store` — types.ts:624-633:
  `{id, gst_registration_id?: string|null, name, address, is_active,
  max_employees, created_at, updated_at}`
- `Principal` — types.ts:650-656 (`store_id` at 654) ·
  `AuthSession` — types.ts:667-670 (`user` + `principal`) ·
  `Role` — types.ts:637 (`STORE_OWNER` | `EMPLOYEE`) ·
  `Membership` — types.ts:674-685.
- **No `Business` or `GSTRegistration` TS type exists** in the frontend
  (backend models have them: models.go:26-47). `GSTR3B.gstin` (types.ts:560)
  is the only GSTIN surface the frontend knows.

## 7. Backend field surface (for completeness)

- Register handler: internal/handlers/auth.go:38-87 accepts
  `name, phone, password, business_name, trade_name, gstin?, store_name,
  store_address`.
- Register repo: internal/repository/auth_repo.go:30-41 (`RegisterInput`),
  inserts `users` (52-76), `businesses` (legal_name, trade_name; 78-83),
  `gst_registrations` (gstin → state_code derived only; 85-100) and `stores`
  (gst_registration_id, name, address; 102-108).
- Store GET/PUT handlers: internal/handlers/employees.go:79-91 (GET),
  94-115 (PUT). `UpdateStoreSettings` (auth_repo.go:436-460) updates only
  `name/address/max_employees`.
- Schema: `stores` (migration 011 + 029) has
  `id, gst_registration_id, name, address, is_active, max_employees,
  created_at, updated_at`. `gst_registrations` already supports
  `gstin, legal_name, trade_name, pan, state_code, state_name, address,
  is_active` (migration 011). **There is NO drug-license (DL) / DL-expiry
  column anywhere in the schema** (`dl_number`, `drug_license`, `license`,
  `dl_expiry` produce no matches across `migrations/` and `internal/`).
- Seed install: cmd/seed/main.go:363-396 creates a business + GST registration
  (with PAN) + store, which is why "existing installs skip this" copy exists.
- Store-scoping: a client-supplied `store_id` is never trusted on protected
  routes — `storeIDFor` prefers `principal.store_id` (store.go:19-45).

## 8. Store switching (multi-store)

**No store switching exists in the frontend.** The principal is bound to one
`store_id` (auth.go `ValidateSession`, auth_repo.go:184-217) and every UI page
operates on it. POS sends `store_id: import.meta.env.VITE_STORE_ID || undefined`
(POS.tsx:232) as an optional checkout field, but the server ignores it when the
principal is authenticated (store.go:19-45). The data model is multi-store
capable (migration 030 adds `store_id` to medicines/batches/customers/suppliers;
`businesses`/`gst_registrations` tables exist), but the SPA is single-store.

---

## Current UI Flow (summary)

1. Load app → `AuthProvider` → `api.me()` restores session from cookie
   (auth.tsx:23-40).
2. No session → `Login` page; user may toggle to Register ("First run") and
   submit owner name, business name, store name, store address, optional GSTIN,
   login phone, password → `POST /api/auth/register` (Login.tsx:33-42).
3. Session granted → `Workspace`; first sync fills 4 IndexedDB stores
   (App.tsx:110-114, db.ts:68-110).
4. Owner opens **Settings** tab → `GET /api/store` + `GET /api/employees`
   (StoreSettings.tsx:17) → edits store name / address / seat limit →
   `PUT /api/store` (StoreSettings.tsx:33). GST registration shown as a
   read-only UUID fragment (StoreSettings.tsx:109-120).
5. Shop details are never cached, never displayed in full, and never refreshed
   after name/address edits anywhere except the Settings form itself.

## Recommended Changes

### A. Registration form (Login.tsx register mode)
- Keep minimum user fields: **owner name, login phone, password** (already at
  Login.tsx:100-108, 149-170).
- Keep/store as **mandatory store fields**: **store name, store address,
  business name** and add an explicit **store/shop phone** field (new).
- Make **GSTIN, PAN, DL number, DL expiry** **clearly labelled optional**
  inputs (greyed "(Optional)" labels), grouping them under a "GST & licence
  (optional)" heading so first-run isn't blocked.
- Backend/API work required since the current register endpoint only accepts
  `gstin` (api.ts:98-112, auth.go:38-87): extend it with
  `pan`, `dl_number`, `dl_expiry`, `store_phone` and add the missing DB columns
  (`pan` exists on `gst_registrations`; **DL fields do not exist anywhere**).
- Stop implying all non-GST fields are mandatory by marking the required set
  with `required` attributes and the optional set with "(optional)" text.

### B. Settings page — full Shop Details section (StoreSettings.tsx)
- Extend the existing section (currently only name/address/seats,
  StoreSettings.tsx:67-107) to show **and edit**:
  store name, store address, shop phone, owner name, business (legal) name,
  GSTIN, PAN, DL number, DL expiry, while keeping the seat-limit editor.
- Replace the read-only GST card (StoreSettings.tsx:109-120) with an editable
  GST/compliance block (GSTIN + PAN + state code), and surface the business
  name/trade name (backend `Business` model, models.go:26-32).
- This needs: new TS types (`GSTRegistration`, `Business` — none exist today),
  an expanded `GET /api/store` response (join store → gst_registration →
  business) and an expanded `PUT /api/store` body, plus new API-client methods
  in api.ts.

### C. Cache update strategy
- Today store details are not cached at all, so the safest minimal change is to
  keep fetching `api.store()` fresh (StoreSettings.tsx:17) and update React
  state directly after a successful save (already done at StoreSettings.tsx:34).
- If shop details should be usable offline (e.g., on the POS receipt header):
  - Add a `store_cache` object store in `db.ts` (PMSDB schema, db.ts:4-23),
    keyed by `store_id`; bump DB version to 3 (db.ts:26).
  - Fetch `GET /api/store` inside `syncLocalCache()` (db.ts:68-110) and write
    it with the other stores; call it on registration completion and on
    `updateStore` success so the cache is never stale.
  - Add `upsertCachedStore()` in the same style as `upsertCachedCustomer`
    (db.ts:162-165) and a `loadCachedStore()`/`getCachedStore()` pair.
  - Then StoreSettings/Employees can hydrate from cache with `syncLocalCache`
    as the refresh path, mirroring how HSN/tax caches already behave
    (db.ts; Medicines.tsx:483 comment on store-scoped cache preference).
