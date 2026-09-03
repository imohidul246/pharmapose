# SHOP_DETAILS_REFACTOR_PLAN

Author: Lead Coordinator.
Date: 2026-08-30.
Status: Approved design. Used by backend/frontend/test sub-agents and the
coordinator review. Audit inputs: SHOP_DETAILS_BACKEND_AUDIT.md,
SHOP_DETAILS_DB_AUDIT.md, SHOP_DETAILS_FRONTEND_AUDIT.md,
SHOP_DETAILS_TEST_PLAN.md.

---

## 1. Existing problem

1. Registration (`POST /api/auth/register`, `handlers/auth.go:38-87` +
   `repository/auth_repo.go:52-135`) collects too much and mixes concerns:
   owner name + business name + store name + store address + optional GSTIN.
2. There is no shop/owner **contact phone** anywhere (login `users.phone` is a
   UNIQUE login credential, not a shop detail).
3. **PAN** exists on `gst_registrations.pan` (`011:16`) but is never collected
   by register or edited by any API.
4. **Drug license number / DL expiry do not exist anywhere** (no column, struct
   field, or validation helper).
5. `GET/PUT /api/store` only expose store `name`/`address`/`max_employees`
   (`employees.go:79-115`; `UpdateStoreSettings` `auth_repo.go:436-460`). GSTIN,
   PAN, business/trade name, owner name, shop phone are invisible to the UI.
6. The registration form (Login.tsx register mode) forces compliance-looking
   fields and none are labelled required vs optional. The Settings page
   (StoreSettings.tsx) has no full shop-details block.
7. First-time setup has no per-store "optional info completed later" path; and
   no shop-details cache exists.

## 2. Current data ownership

| Field | Current home |
|-------|--------------|
| store_name | `stores.name` |
| store_address | `stores.address` |
| owner_name | `users.name` of the active `STORE_OWNER` member (derived, `store_memberships`) |
| phone (shop) | NOT STORED |
| GSTIN | `gst_registrations.gstin` (`stores.gst_registration_id →`) |
| PAN | `gst_registrations.pan` (column exists, unused by API) |
| DL number | NOT STORED |
| DL expiry | NOT STORED |

`gst_recording` note: `gst_registrations.gstin` is the canonical GSTIN used by
tax filings (read at `tax_repo.go:149`), and `stores.gst_registration_id`
links a store to exactly one registration. B2B invoice PDF reads current
store/GST registration for the seller header (`b2b_pdf.go:19-60`).

## 3. Proposed data ownership

| Field | Home | Change |
|-------|------|--------|
| store_name | `stores.name` | none |
| store_address | `stores.address` | none |
| owner_name | `users.name` of active owner member | derived; edited via owner user |
| phone (shop) | **`stores.phone`** (new column) | add |
| GSTIN | `gst_registrations.gstin` | reuse existing |
| PAN | `gst_registrations.pan` | reuse existing (wire it up) |
| DL number | **`stores.drug_license_number`** (new column) | add |
| DL expiry | **`stores.drug_license_expiry`** (new column, DATE) | add |

Rationale:
- DL belongs to the physical outlet (`stores`), is store-scoped (satisfies
  store-isolation), and works even when there is no GST registration (minimal
  registration).
- GSTIN/PAN belong to the GST registration entity, not the store; they are
  read by filings. Do NOT duplicate them onto `stores` (would drift).
- `phone` is the store contact, distinct from the login `users.phone`.
- `owner_name` is the owner user's name; no redundant column. Editing it in
  Settings updates the owner user's `name`.

## 4. Registration flow (new)

- Required: owner name (`name`), login phone, password, **store name**,
  **store phone**, **store address**. (`owner_name` == `name`.)
- Optional (clearly labelled): GSTIN, PAN, DL number, DL expiry. (business_name
  remains accepted for back-compat, defaults to store name when empty.)
- Server creates atomically: users, business (+gst_registration when GSTIN/PAN
  given), store (name/address/phone/dl + linked registration), STORE_OWNER
  membership, session.

## 5. Settings flow (new)

- `GET /api/store` returns the full shop-details profile (owner_name, store
  name/address/phone, GSTIN, PAN, DL no, DL expiry, max_employees).
- Owner edits compulsory + optional fields in one form → `PUT /api/store`.
- Server validates, updates stores + (gst_registration create/update) + owner
  name atomically, returns the updated profile.

## 6. API reuse / changes

- **REUSE** `POST /api/auth/register` — extend request body, no new endpoint.
- **REUSE** `GET /api/store` and `PUT /api/store` (owner-only) — extend the
  request/response, no new endpoints. NO `/api/shop`/`/api/shop-details` etc.
- **REUSE** authorization: routes already behind `auth.RequireOwner()`
  (`router.go:77-82`). store_id is derived from the authenticated principal
  (`storeIDFor`), never from the client.
- Handler `updateStore` (`employees.go:94`) re-pointed to the new repo method.

## 7. Database changes (new migration `034_shop_details.sql`)

```sql
ALTER TABLE stores
    ADD COLUMN phone TEXT NOT NULL DEFAULT '',
    ADD COLUMN drug_license_number TEXT NOT NULL DEFAULT '',
    ADD COLUMN drug_license_expiry DATE;
```

- Non-destructive, no backfill required (defaults), preserves existing rows.
- GSTIN/PAN reuses existing `gst_registrations` columns.

## 8. Validation rules

- store_name: required; reject null/empty/whitespace (`trimSpaces`).
- owner_name: required; reject null/empty/whitespace.
- phone: required; `normalizePhone` must be non-empty (digits only, per
  existing convention).
- address: required; reject null/empty/whitespace.
- GSTIN: optional; when non-empty → `tax.ValidateGSTIN` (existing).
- PAN: optional; when non-empty → structural regex `^[A-Z]{5}[0-9]{4}[A-Z]{1}$`
  (new `tax.ValidatePAN`, no over-restriction).
- DL number: optional; no invented format — accept any non-empty string.
- DL expiry: optional; when non-empty parse `YYYY-MM-DD`; accept any valid date
  (documented: no future-date requirement — historical expiry allowed unless a
  future business decision says otherwise).

## 9. Authorization

- `PUT /api/store` is already `RequireOwner()` + `RequireAuth`; employees get
  403, anonymous get 401. Done.
- All store scoping uses `principal.store_id`; client `store_id` is never
  trusted. Done.

## 10. Cache strategy

- No shop-details IndexedDB cache exists today and none is required by the
  task's "reload persistence" test (server round-trip covers it). Keep the
  existing pattern: `GET /api/store` fetched fresh; update React state after a
  successful `PUT`. Do NOT introduce a new cache layer.

## 11. Existing-data migration

- `stores.phone`/`drug_license_number` default `''`, `drug_license_expiry`
  NULL → existing stores keep working; no fake GSTIN/DL/PAN invented.
- Settings page renders optional blanks as empty inputs.

## 12. Testing strategy

Backend (repo + handler tests per SHOP_DETAILS_TEST_PLAN.md):
1 minimal registration, 2 registration with optional info, 3 settings update,
4 add optional, 5 clear optional, 6 mandatory validation (no DB change),
7 optional empty, 8 store isolation, 9 unauthorized 403, 10 existing-data
regression, 11 historical invoice snapshot preserved, 12 reload persistence.

Frontend: extend `Login.tsx` register form, `StoreSettings.tsx` shop-details
section, `api.ts` types/methods. Add `StoreSettings.test.tsx` (optional,
guards UI wiring).

---

## Implementation checklist

Backend:
- migration 034
- models.Store: +Phone, +DrugLicenseNumber, +DrugLicenseExpiry
- repository: ShopDetails read/update; wire PAN/DL/phone/owner into
  Register + UpdateStoreDetails; tax.ValidatePAN
- handlers: register + updateStore body/response
- tests

Frontend:
- types.ts, api.ts register/updateStore, Login.tsx, StoreSettings.tsx
- tests (optional StoreSettings.test.tsx)
