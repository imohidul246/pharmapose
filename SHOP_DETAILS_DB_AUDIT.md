# SHOP DETAILS / DATABASE SCHEMA AUDIT

**Project:** PharmaPOS (Go + PostgreSQL)
**Date:** 2026-08-30
**Scope:** Read-only audit of `migrations/` — no schema modified.
**Migrated table count:** 27 application tables (+ `schema_migrations` runtime tracker).

---

## 1. Migration version sequence & next number

All 34 `.sql` files live in `migrations/` and are embedded via `//go:embed *.sql`
(`migrations/embed.go`). The runner (`internal/database/database.go:47-55`) lists the
embedded `*.sql` files and applies them in **lexicographic filename order**
(`sort.Strings(names)`), recording each applied filename in `schema_migrations
(version TEXT PRIMARY KEY)`. `embed.go` itself is NOT embedded (only `*.sql`), so it
never lands in `schema_migrations`.

Filename patterns are inconsistent but lexicographically safe:
- `0001_medicines.sql` … `0009_purchase_bonus_discount.sql` (0-padded 4 digits)
- `010_create_suppliers.sql` … `033_store_hsn_tax_scoping.sql` (3 digits)

Because `0 < 1` in ASCII, `0009` still sorts before `010`, so the intended order holds.

**Current highest migration:** `033_store_hsn_tax_scoping.sql`
**Next sequential number:** **`034`** (i.e. `034_<name>.sql`).

---

## 2. Complete table inventory (in migration order)

| Migration | Table(s) created / affected |
|-----------|------------------------------|
| 0001 | `medicines` |
| 0002 | `batches` |
| 0003 | `customers` |
| 0004 | `sales_invoices`, `sales_invoice_items`, enum `payment_type` |
| 0005 | `purchase_orders`, `purchase_order_items` |
| 0006 | `reconciliation_journals`, `reconciliation_items` |
| 0007 | alter `sales_invoices`, `sales_invoice_items` (discounts) |
| 0008 | `customer_ledger` |
| 0009 | alter `purchase_order_items`, `purchase_orders` (bonus/discount) |
| 010  | `suppliers` |
| 011  | **`businesses`, `gst_registrations`, `stores`** |
| 012  | `hsn_codes` |
| 013  | `tax_rates` |
| 014  | `medicine_tax_config` |
| 015  | alter `customers` (GST fields) |
| 016  | alter `sales_invoices` (GST totals, store/gst FKs) |
| 017  | alter `sales_invoice_items` (tax snapshot) |
| 018  | `sales_credit_notes`, `sales_credit_note_items` |
| 019  | alter `purchase_orders` (GST, supplier/store/gst FKs) |
| 020  | alter `purchase_order_items` (tax snapshot) |
| 021  | seed `hsn_codes`, `tax_rates` |
| 022  | CHECK constraints |
| 023  | backfill + NOT NULL `grand_total` |
| 024  | drop redundant indexes |
| 025  | `invoice_sequences`; invoice number overhaul; `medicines.uqc` |
| 026  | alter `sales_invoices` (B2B), `sales_invoice_items` (mrp/bonus) |
| 027  | alter `purchase_orders` (ITC); `gstr2b_import_batches`, `gstr2b_imports` |
| 028  | alter `gstr2b_imports.doc_type` VARCHAR(3) |
| 029  | **`users`, `store_memberships`, `sessions`, `audit_logs`**; `stores.max_employees` |
| 030  | store scoping (medicines/batches/customers/suppliers/reconciliation_journals) |
| 031  | `purchase_requests` (enum `purchase_request_status`) |
| 032  | `stock_audit_requests`, `stock_audit_request_items` |
| 033  | store scoping (hsn_codes/tax_rates/medicine_tax_config) |

---

## 3. Target table schema dumps

All PKs are `UUID PRIMARY KEY DEFAULT gen_random_uuid()`.

### users  (migration 029)
| Column | Type | Constraints |
|--------|------|-------------|
| id | UUID | PK |
| name | TEXT | **NOT NULL** |
| phone | VARCHAR(20) | **NOT NULL**, **UNIQUE** |
| password_hash | TEXT | **NOT NULL** |
| is_active | BOOLEAN | NOT NULL DEFAULT true |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT now() |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT now() |

Indexes/constraints: `users_phone_key` (UNIQUE on phone). No FK on users (root entity).

### businesses  (migration 011)
| Column | Type | Constraints |
|--------|------|-------------|
| id | UUID | PK |
| legal_name | TEXT | **NOT NULL** |
| trade_name | TEXT | NOT NULL DEFAULT '' |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT now() |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT now() |

**No `pan` column on `businesses`.** No UNIQUE constraints, no indexes beyond PK.

### gst_registrations  (migration 011)
| Column | Type | Constraints |
|--------|------|-------------|
| id | UUID | PK |
| business_id | UUID | **NOT NULL**, **FK → businesses(id) ON DELETE CASCADE** |
| gstin | VARCHAR(15) | nullable |
| legal_name | TEXT | NOT NULL DEFAULT '' |
| trade_name | TEXT | NOT NULL DEFAULT '' |
| pan | VARCHAR(10) | nullable |
| state_code | VARCHAR(2) | NOT NULL DEFAULT '' |
| state_name | TEXT | NOT NULL DEFAULT '' |
| address | TEXT | NOT NULL DEFAULT '' |
| is_active | BOOLEAN | NOT NULL DEFAULT true |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT now() |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT now() |

Indexes:
- `idx_gst_registrations_business ON (business_id)`
- `idx_gst_registrations_gstin ON (gstin) WHERE gstin IS NOT NULL`
- `idx_gst_registrations_state ON (state_code)`

**This is where GSTIN and PAN (of the registered entity) live.**

### stores  (migration 011 + 029)
| Column | Type | Constraints |
|--------|------|-------------|
| id | UUID | PK |
| gst_registration_id | UUID | **FK → gst_registrations(id) ON DELETE SET NULL** (nullable) |
| name | TEXT | **NOT NULL** |
| address | TEXT | NOT NULL DEFAULT '' |
| is_active | BOOLEAN | NOT NULL DEFAULT true |
| max_employees | INT (added 029) | NOT NULL DEFAULT 2, CHECK `max_employees >= 0` |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT now() |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT now() |

Indexes:
- `idx_stores_gst_registration ON (gst_registration_id)`

**`stores` carries no phone/owner column.** A store's `name`/`address` exist, but no store-level phone, owner name, or DL number.

### store_memberships  (migration 029) — the "memberships"
| Column | Type | Constraints |
|--------|------|-------------|
| id | UUID | PK |
| store_id | UUID | **NOT NULL**, **FK → stores(id) ON DELETE CASCADE** |
| user_id | UUID | **NOT NULL**, **FK → users(id) ON DELETE CASCADE** |
| role | VARCHAR(20) | NOT NULL DEFAULT 'EMPLOYEE'; CHECK in ('STORE_OWNER','EMPLOYEE') |
| is_active | BOOLEAN | NOT NULL DEFAULT true |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT now() |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT now() |

Constraints/indexes:
- `uq_store_memberships_user UNIQUE (store_id, user_id)`
- `uq_active_owner_per_store UNIQUE (store_id) WHERE role='STORE_OWNER' AND is_active=true`
- `idx_memberships_user ON (user_id)`, `idx_memberships_store ON (store_id)`

---

## 4. Relationship graph

```
users ──< store_memberships >── stores ──> gst_registrations ──> businesses
  ^                                ^              ^
  | (sessions, audit_logs,         |              |
  |  purchase_requests,            |              |
  |  stock_audit_requests, etc.)   |  (gst_registration_id FK, SET NULL)
  |                                |
  +-- created_by / verified_by on  +-- store_id FK (ON DELETE CASCADE/SET NULL) on:
        sales_invoices,              medicines, batches, customers, suppliers,
        purchase_orders,              reconciliation_journals, hsn_codes, tax_rates,
        reconciliation_journals       medicine_tax_config, sales_invoices,
                                      purchase_orders, credit notes, gstr2b_*, etc.
```

Key hop-by-hop chain:
- `users` ↔ `stores` via `store_memberships` (many-to-many, a user can belong to many stores; each store ≤1 active owner).
- `stores.gst_registration_id → gst_registrations.id` (SET NULL) — a store optionally maps to one GST registration.
- `gst_registrations.business_id → businesses.id` (CASCADE).
- **`stores` has NO direct FK to `businesses`** — the chain is store → gst_registration → business.
- `sales/purchase` docs additionally carry `store_id` and `gst_registration_id` FKs (016/019).
- Every business entity is store-scoped via `store_id` NOT NULL (030, 033).

---

## 5. Where "shop-details" fields currently live

| Field | Table.Column | Notes |
|-------|--------------|-------|
| Buyer/shop name | `stores.name` | NOT NULL |
| Shop address | `stores.address` | NOT NULL DEFAULT '' |
| Shop owner | `store_memberships` role='STORE_OWNER' → `users.name` | only via FK chain; no direct `stores.owner` |
| Shop (store) phone | **NOWHERE** | `users.phone` exists but is the *user's* phone, not the store's |
| Business legal name | `businesses.legal_name` (and duplicated on `gst_registrations.legal_name`) | |
| Business trade name | `businesses.trade_name` (and `gst_registrations.trade_name`) | |
| PAN | `gst_registrations.pan` (and `suppliers.pan` for vendors) | **`businesses` itself has NO pan column** |
| GSTIN | `gst_registrations.gstin` | |
| State code/name | `gst_registrations.state_code` / `.state_name` | |
| GST registration address | `gst_registrations.address` | |
| DL (drug license) number | **NOWHERE** | no column in any table |
| DL expiry date | **NOWHERE** | no column in any table |

### 5a. GSTIN, PAN, DL number, DL expiry — definitive answer
- **GSTIN** → stored, in `gst_registrations.gstin` (per-state registration), plus snapshot copies on documents (`sales_invoices.customer_gstin`, `purchase_orders.supplier_gstin`, `gstr2b_imports.supplier_gstin`).
- **PAN** → stored, in `gst_registrations.pan` (attached to the GST registration, i.e. the registered entity). Also `suppliers.pan` for vendor PANs. **Not** on `businesses` itself.
- **DL number** → **NOT stored anywhere.**
- **DL expiry date** → **NOT stored anywhere.**

### 5b. Is there any drug-license (DL) table or column?
**No.** Searched all migrations (0001–033) and the entire codebase (`internal/`, `cmd/`, `web/`) for
`drug license`, `licence`, `license_number`, `dl_number`, `dlNumber`, `dl_expiry`, `drugLicense`.
Zero matches. The schema has no DL concept today.

---

## 6. Relevant NOT NULL constraints & data concerns

Relevant NOT NULL columns (would need defaults/backfill before adding new NOT NULL fields):
- `users.name`, `users.phone`, `users.password_hash`
- `businesses.legal_name`
- `gst_registrations.business_id` (+ all its `NOT NULL DEFAULT ''` text fields)
- `stores.name`; `stores.max_employees` NOT NULL DEFAULT 2
- `store_memberships.store_id`, `.user_id`, `.role`, `.is_active`
- All store_scoped master tables enforce `store_id NOT NULL` (medicines, batches, customers, suppliers, reconciliation_journals, hsn_codes, tax_rates, medicine_tax_config).

Data concerns:
- **Users & stores are already in production (auth/seed).** Adding NOT NULL columns to `users`,
  `stores`, `businesses`, or `gst_registrations` requires a sensible DEFAULT or a backfill step
  (pattern used consistently by 030/033: `ADD COLUMN <nullable> → backfill → SET NOT NULL`).
- `gst_registrations` may hold multiple rows per business (one per state/GSTIN). A naive
  `businesses.pan` denormalization could drift from `gst_registrations.pan`.
- `stores.address` is NOT NULL DEFAULT '' — a new `stores.phone` should follow the same
  `NOT NULL DEFAULT ''` convention (see 010 suppliers.phone).
- `uq_active_owner_per_store` guarantees ≤1 active owner per store; owner identity is derived,
  not a stored store column.

---

## 7. Recommendation (new columns & where)

Following existing conventions (NOT NULL DEFAULT '' TEXT scalars; nullable-for-add then backfill;
UUID FKs where relational):

1. **DL number + DL expiry → `stores`** (the physical pharmacy outlet).
   - `stores.drug_license_number TEXT NOT NULL DEFAULT ''`
   - `stores.drug_license_expiry DATE` (nullable; DATE matches `batches.expiry_date` style)
   - Rationale: a DL is granted per physical retail outlet — the `stores` row is the correct,
     natural home. This keeps DL scoped like every other entity (each store has its own license),
     consistent with how GST registration is attached to a store.

2. **PAN → `businesses`** (alongside the existing `gst_registrations.pan`).
   - `businesses.pan VARCHAR(10)` (nullable; same type as `gst_registrations.pan` / `suppliers.pan`)
   - A business-level PAN is the entity's own PAN, distinct from a per-GST-registration PAN. This
     is a denormalization but is the intuitive "business PAN" the shop-details form wants. If the
     desire is strictly "the PAN of the store's GST lookup", the existing `gst_registrations.pan`
     is already sufficient and no new column is needed.

3. **Store phone / owner → `stores`**.
   - `stores.phone TEXT NOT NULL DEFAULT ''` (mirrors `suppliers.phone`).
   - Optionally `stores.owner_name TEXT NOT NULL DEFAULT ''` if a display field is preferred over
     deriving owner via `store_memberships` role='STORE_OWNER' → `users.name`. Prefer deriving the
     owner from the existing membership graph unless a denormalized display value is required.

4. **Do NOT** add `pan` to `users` or `gstin` to `stores` — `gst_registrations` already models the
   GSTIN/state relationship, and `stores` links to it.

### Next migration & requirement

- **A new migration IS required** to add any of the above columns.
- **Number:** `034` → filename `034_<descriptive_name>.sql` (e.g. `034_shop_details.sql`), placed in
  `migrations/`. The runner picks it up automatically by lexicographic order and records it in
  `schema_migrations` as `034_...`.
- Follow the established add-nullable → backfill → SET NOT NULL (if enforcing) pattern of 030/033,
  and use `NOT NULL DEFAULT ''` for the new TEXT scalars to avoid breaking existing rows.

---

*End of audit.*
