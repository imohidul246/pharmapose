# Shop Details — Backend Domain Model Audit (PharmaPOS)

Read-only investigation. No code or schema was modified. Scope: ownership of
`store_name`, `phone`, `owner_name`, `store_address`, `GSTIN`, `DL No`,
`DL Expiry`, `PAN` across the backend.

---

## 1. Conceptual SQL tables & columns

The schema lives in `migrations/*.sql`. Relevant tables:

| Table | Column | Source |
|-------|--------|--------|
| `users` | `id, name, phone, password_hash, is_active, created_at, updated_at` | `migrations/029_auth_core.sql:7-15` |
| `businesses` | `id, legal_name, trade_name, created_at, updated_at` | `migrations/011_create_business_gst_registrations.sql:2-8` |
| `gst_registrations` | `id, business_id, gstin, legal_name, trade_name, pan, state_code, state_name, address, is_active` | `migrations/011:10-23` |
| `stores` | `id, gst_registration_id, name, address, is_active, max_employees` | `migrations/011:29-37` + `029_auth_core.sql:39-41` |
| `store_memberships` | `id, store_id, user_id, role, is_active` | `migrations/029_auth_core.sql:17-27` |

Notes:
- `users.phone` is `UNIQUE` (`029_auth_core.sql:10`) — phone is a login credential, not a shop detail.
- `gst_registrations.pan` exists as a column (`011:16`) but the `gst_registration`
  record belongs to a *business* (multiple possible per business), not to a store or brand.
- `stores` has **no** `phone`, `owner_name`, `gstin`, `pan`, `dl_no`, or `dl_expiry` columns.

---

## 2. Where the `register` flow creates each row

`POST /api/auth/register` → `handlers/auth.go:38-87` → `repository/auth_repo.go:52 Register`.

Inside one transaction (`auth_repo.go:65-130`):
1. **user** — `users (name, phone, password_hash)` ← `Name`, `Phone`, hash (`auth_repo.go:67-70`).
2. **business** — `businesses (legal_name, trade_name)` ← `BusinessName`, `TradeName` (`auth_repo.go:79-83`).
3. **gst_registration** (only if `GSTIN` non-empty & structurally valid) —
   `gst_registrations (business_id, gstin, legal_name, trade_name, state_code)` ←
   `GSTIN`, `BusinessName`, `TradeName`, first-2-chars of GSTIN (`auth_repo.go:86-100`).
   **PAN/address are NOT set here** (columns default to empty).
4. **store** — `stores (gst_registration_id, name, address)` ← `StoreName`, `StoreAddress`
   (`auth_repo.go:102-108`).
5. **membership** — role `STORE_OWNER` (`auth_repo.go:110-116`).
6. **session** (`auth_repo.go:119-124`).

The request body (`auth.go:39-48`) is flat: `name, phone, password, business_name,
trade_name, gstin, store_name, store_address`. There is **no** `owner_name`, `pan`,
`dl_no`, or `dl_expiry` field.

The `cmd/seed/main.go:363-396` `seedGSTShell` mirrors this and additionally sets
`pan = "AAAAA0000A"` and `address` on the `gst_registration` (`main.go:375-380`).

---

## 3. Store vs User — phone & owner name

- **Store** (`models/models.go:49-58`): only `Name`, `Address`, `GSTRegistrationID`,
  `IsActive`, `MaxEmployees`. **No `phone`, no `owner_name`, no GSTIN/PAN/DL.**
- **User** (`models/models.go:449-456`): `Name`, `Phone` exist — but these are the
  *owner/employee login identity*, not shop/owner business details.

`GetStore` (`auth_repo.go:415-432`) reads `id, gst_registration_id, name, address,
is_active, max_employees` — it does NOT join to business or gst_registration, so the
`GET /api/store` response (`handlers/employees.go:79-91`) exposes only name/address/cap.
There is currently no endpoint that returns business or GST-registration details
to the store-settings UI.

---

## 4. Where GSTIN / PAN / DL are stored today

- **GSTIN**: `gst_registrations.gstin` (written on register, `auth_repo.go:94`; seeded at `main.go:376`). Read back in `tax_repo.go:149-151` and used for filings in `gst/gstr1_handler.go:169-181`.
- **PAN**: column `gst_registrations.pan` exists (`011:16`) and is read in `tax_repo.go:149,152,162`. It is **only ever populated by the seeder** (`main.go:376,379`); the `register` flow does not set it and the API does not expose a PAN input.
- **DL No / DL Expiry**: **no storage anywhere** — no column, struct field, or SQL for drug license number or expiry appears in `migrations/`, `models`, or `repository` (grep for `license|dl_|drug` returns nothing beyond unrelated matches).

---

## 5. Is there drug-license storage?

**No.** Confirmed by full-codebase and migration grep. No `dl_*` column, no struct
field, no validation helper. This is a genuine gap if the shop-details UI must show/manage it.

---

## 6. Authorization model for shop settings

- Roles: `STORE_OWNER`, `EMPLOYEE` (`auth/principal.go:6-14`).
- `Store` routes (`GET` + `PUT /api/store`) are gated with `auth.RequireOwner()`
  (`handlers/router.go:77-82`), which requires `p.Role == RoleStoreOwner`
  (`auth/middleware.go:83-92`). So **`PUT /api/store` is owner-only** — employees are
  forbidden (403).
- `RequirePermission`/`Can` (`principal.go:59-64`, `middleware.go:70-79`) grant the
  optional employee permission set; none of the employee permissions cover store/business settings.
- `UpdateStoreSettings` (`auth_repo.go:436-460`) only touches `name, address,
  max_employees` on `stores`. There is no business/GST-registration update path at all.

---

## 7. store_id scoping / access to a store

- The session resolves to a single `Principal` carrying `UserID, Name, StoreID, Role`
  (`auth_repo.go:193-216 ValidateSession`), bound to the request context by the
  middleware (`auth/middleware.go:59`, via `WithPrincipal`).
- Handlers read it via `currentPrincipal(c)` → `auth.PrincipalFromContext`
  (`handlers/store.go:48-50`, `auth/password.go:108-111`) and use `p.StoreID` for every
  store-scoped query (e.g. `employees.go:81,105`, `requests.go`).
- `storeIDFor` (`handlers/store.go:19-45`) prefers `Principal.StoreID` and only falls
  back to client-supplied `store_id` on unauthenticated/bootstrap flows — client
  store_id is never trusted on protected routes.
- Membership is the link: `Register` creates a `STORE_OWNER` membership; every request
  re-validates `users.is_active` AND `store_memberships.is_active`
  (`auth_repo.go:209-214`). So "access to a store" = authenticated user's active membership.

---

## 8. Validation helpers available

- **Phone**: `normalizePhone` — digits-only folding, NOT a validity check
  (`repository/helpers.go:31-39`; used in `auth_repo.go:53,139,236,292`).
- **GSTIN**: `tax.ValidateGSTIN` — structural regex + ISO 7064 MOD 37,36 checksum
  (`tax/gstin.go:29-34`; used at `auth_repo.go:87`, `supplier_repo.go:32`,
  `customer_repo.go:48`, `sale_repo.go:113`, `gst/validate.go:35,56`).
- **PAN**: **no validator exists**. The PAN format is only *documented* inside the GSTIN
  regex comments (`tax/gstin.go:8-10`). No `ValidatePAN`/regex helper anywhere.
- **Name**: `trimSpaces` + non-empty check (`repository/helpers.go:42`;
  `auth_repo.go:447-450`).
- **Other**: `ValidateUQC` (`tax/uqc.go:17`).

---

## Data Ownership Recommendation

Based on what actually exists in the schema and code (not on any external example):

| Field | Where it should live | Rationale |
|-------|----------------------|-----------|
| `store_name` | **stores.name** | Already stored there (`auth_repo.go:105`, `UpdateStoreSettings:453`). Clear, single home. |
| `store_address` | **stores.address** | Already stored there (`auth_repo.go:106`, `UpdateStoreSettings:454`) and read by filings. Correct home. |
| `phone` (store/owner phone) | **users.phone** for the owner's login, but **add a `store.phone`** for the shop contact | `users.phone` is a unique login credential (`029:10`) and belongs to an individual; a *shop* phone is distinct and currently **not modeled**. Best fit: a new `stores.phone`. Do NOT reuse `users.phone`. |
| `owner_name` | **users.name** (the `STORE_OWNER` member) | The owner's name is already the owning user's `name`. Derive it via the active `STORE_OWNER` membership rather than adding a redundant column. No new storage needed. |
| `GSTIN` | **gst_registrations.gstin** (existing) | Already the canonical column (`011:13`, `auth_repo.go:94`, `tax_repo.go:149`). Stores point at it via `stores.gst_registration_id`. Keep as-is. |
| `DL No` | **NEW: gst_registrations.drug_license_no** (or a store-level `stores.drug_license_no`) | Nothing exists today. Because a DL is often per-outlet and tax/filing-adjacent, a natural fit is alongside the GST registration on the same row; if the product needs per-store display, `stores.drug_license_no` is the simpler owner-only-editable home. Recommend a single new column (see note). |
| `DL Expiry` | **NEW: same table as DL No** (`drug_license_expiry DATE`, nullable) | Co-locate with DL No for atomic editing. No current storage. |
| `PAN` | **gst_registrations.pan** (existing, currently unused by register) | Column exists (`011:16`), is read in `tax_repo.go:149,152,162`, seeded at `main.go:376,379`. Fill it from the register/store-settings form. It belongs to the business/registration, not the store or the user. |

Practical implementation notes for whoever wires the shop-details UI:
- Extend `PUT /api/store` (owner-only) to also accept `phone`, `gstin`, `pan`, and
  `drug_license_no` / `drug_license_expiry`; today it only mutates
  `stores.name/address/max_employees` (`auth_repo.go:436-460`).
- Add a `stores.phone` column (new) for the shop contact number.
- Populate `gst_registrations.pan` on register (currently only the seeder sets it).
- Add `drug_license_no`/`drug_license_expiry` columns (new) — highest-confidence home is
  on the row holding the GST/PAN data (`gst_registrations`) so the business license
  block is edited atomically and stays filing-aligned.
- Reuse `tax.ValidateGSTIN` for GSTIN; add a `ValidatePAN` helper (none exists) and
  validate the DL expiry (`date` format + not-in-past if desired).
- Response shape: the store-settings endpoint should join
  `stores → gst_registrations → businesses` (all wiring already exists via
  `stores.gst_registration_id`/`gst_registrations.business_id`) so the UI can render
  name/address/phone (store), GSTIN/PAN/DL (registration), and owner name (from the
  `STORE_OWNER` membership) in one round trip.
