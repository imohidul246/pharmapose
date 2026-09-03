# PharmaPOS — Pharmacy Management System — Full System Report

> **Generated:** 2026-08-26
> **Codebase:** `pms-marg-inspired`
> **Architecture:** Go (Gin) + PostgreSQL + React (Vite + Tailwind)

---

## Table of Contents

1. [System Overview](#1-system-overview)
2. [Technology Stack](#2-technology-stack)
3. [Database Schema (20 Tables)](#3-database-schema)
4. [API Reference (All Endpoints)](#4-api-reference)
5. [Inward Inventory Flow (Purchase)](#5-inward-inventory-flow)
6. [Outward Inventory Flow (Sale)](#6-outward-inventory-flow)
7. [Purchase Invoice Details](#7-purchase-invoice-details)
8. [Sales Invoice Details](#8-sales-invoice-details)
9. [GST / Tax Engine](#9-gst--tax-engine)
10. [Inventory Reconciliation](#10-inventory-reconciliation)
11. [Customer Credit System](#11-customer-credit-system)
12. [Reports](#12-reports)
13. [Entity Relationship Summary](#13-entity-relationship-summary)

---

## 1. System Overview

PharmaPOS is a **full-stack Pharmacy ERP / Point-of-Sale system** inspired by Marg ERP (popular Indian pharmacy software). It is designed for **Indian medical stores** with full **GST compliance** including CGST, SGST, IGST, and Cess calculations.

### Key Features

- **Point-of-Sale (POS):** Offline-first billing with IndexedDB cache for zero-latency product search
- **Purchase Inward:** Supplier invoice recording with batch-level stock tracking
- **Batch-level Inventory:** Each medicine tracked by batch number, expiry, purchase/sale prices
- **GST Compliance:** Full Indian GST with intra-state (CGST+SGST) and inter-state (IGST) tax, HSN codes, effective-dated tax rates
- **Customer Credit (Khata):** Credit sales with ledger, payment collection, and credit limit enforcement
- **Bonus Quantity:** Purchase items can include free bonus stock
- **Discounts:** Per-line percent or flat-amount discounts on both sales and purchases
- **Stock Reconciliation:** Physical stock audit with variance tracking
- **Reports:** Sales, Purchase, Profit & Loss, Expiry alerts, Low-stock alerts
- **Single Binary Deployment:** Go binary serves both API and compiled SPA

---

## 2. Technology Stack

| Layer | Technology | Version |
|---|---|---|
| Backend | Go + Gin | 1.27 / v1.12.0 |
| Database | PostgreSQL (pgx/v5) | v5.10.0 |
| Frontend | React + TypeScript | 19.1 / ~5.8 |
| Build Tool | Vite | 6.3 |
| CSS | Tailwind CSS | 4.1 |
| Client Cache | IndexedDB (via `idb`) | v8.0.3 |
| Decimal Math | shopspring/decimal | Go lib |
| Testing | Go testing, Vitest | -- |

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PMS_ADDR` | `:8080` | Server listen address |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/pms?sslmode=disable` | PostgreSQL connection |

---

## 3. Database Schema

**20 tables**, 1 enum type, 46 indexes, 19 CHECK constraints.

### 3.1 `medicines` — Medicine Catalog

```sql
CREATE TABLE medicines (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              VARCHAR(255) NOT NULL,
    salt_composition  TEXT NOT NULL DEFAULT '',
    manufacturer      VARCHAR(255) NOT NULL DEFAULT '',
    min_reorder_level INT NOT NULL DEFAULT 0 CHECK (min_reorder_level >= 0),
    packing           VARCHAR(100) NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ  -- soft delete
);
```

| Column | Type | Description |
|---|---|---|
| `id` | UUID | Primary key |
| `name` | VARCHAR(255) | Medicine name |
| `salt_composition` | TEXT | Active salt / composition |
| `manufacturer` | VARCHAR(255) | Manufacturer name |
| `min_reorder_level` | INT | Low-stock alert threshold |
| `packing` | VARCHAR(100) | Pack size (e.g., "10x10", "30 tablets") |
| `deleted_at` | TIMESTAMPTZ | Soft delete timestamp |

### 3.2 `batches` — Physical Batch Inventory

```sql
CREATE TABLE batches (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    medicine_id    UUID NOT NULL REFERENCES medicines(id) ON DELETE CASCADE,
    batch_number   VARCHAR(100) NOT NULL,
    expiry_date    DATE NOT NULL,
    purchase_price NUMERIC(12,2) NOT NULL DEFAULT 0.00 CHECK (purchase_price >= 0),
    sale_price     NUMERIC(12,2) NOT NULL DEFAULT 0.00 CHECK (sale_price >= 0),
    current_stock  INT NOT NULL DEFAULT 0 CHECK (current_stock >= 0),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (medicine_id, batch_number)
);
```

| Column | Type | Description |
|---|---|---|
| `medicine_id` | UUID FK | Parent medicine |
| `batch_number` | VARCHAR(100) | Supplier batch number |
| `expiry_date` | DATE | Expiry date of this batch |
| `purchase_price` | NUMERIC(12,2) | Blended cost per unit (after discount + bonus) |
| `sale_price` | NUMERIC(12,2) | MRP / selling price per unit |
| `current_stock` | INT | Available units (never negative) |

**Key constraint:** `(medicine_id, batch_number)` is UNIQUE — re-inwarding the same batch merges stock.

### 3.3 `customers` — Customer Registry

```sql
CREATE TABLE customers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(255) NOT NULL,
    phone           VARCHAR(20) NOT NULL UNIQUE,
    credit_limit    NUMERIC(12,2) NOT NULL DEFAULT 0.00 CHECK (credit_limit >= 0),
    current_balance NUMERIC(12,2) NOT NULL DEFAULT 0.00,
    gstin           VARCHAR(15),
    customer_type   TEXT NOT NULL DEFAULT 'B2C' CHECK (customer_type IN ('B2C', 'B2B')),
    billing_address TEXT,
    shipping_address TEXT,
    state           TEXT,
    state_code      VARCHAR(2),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

| Column | Type | Description |
|---|---|---|
| `name` | VARCHAR(255) | Customer name |
| `phone` | VARCHAR(20) | Unique phone number |
| `credit_limit` | NUMERIC(12,2) | Maximum allowed credit balance |
| `current_balance` | NUMERIC(12,2) | Outstanding credit amount |
| `gstin` | VARCHAR(15) | GST Identification Number (for B2B) |
| `customer_type` | TEXT | `B2C` (retail) or `B2B` (business) |
| `state_code` | VARCHAR(2) | Indian state code for GST supply type |

### 3.4 `suppliers` — Supplier Registry

```sql
CREATE TABLE suppliers (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    legal_name TEXT NOT NULL,
    trade_name TEXT NOT NULL DEFAULT '',
    gstin      VARCHAR(15),
    pan        VARCHAR(10),
    address    TEXT NOT NULL DEFAULT '',
    state      TEXT NOT NULL DEFAULT '',
    state_code VARCHAR(2) NOT NULL DEFAULT '',
    phone      TEXT NOT NULL DEFAULT '',
    email      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 3.5 `sales_invoices` — Sales Invoice Headers

```sql
CREATE TABLE sales_invoices (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_no          BIGSERIAL UNIQUE,
    customer_id         UUID REFERENCES customers(id) ON DELETE SET NULL,
    payment_type        payment_type NOT NULL DEFAULT 'CASH',
    total_amount        NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    discount_total      NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- GST fields
    store_id            UUID REFERENCES stores(id) ON DELETE SET NULL,
    gst_registration_id UUID REFERENCES gst_registrations(id) ON DELETE SET NULL,
    customer_gstin      VARCHAR(15),
    customer_state_code VARCHAR(2),
    supply_type         TEXT DEFAULT NULL,          -- 'INTRA_STATE' or 'INTER_STATE'
    gross_amount        NUMERIC(14,2),
    taxable_amount      NUMERIC(14,2),
    cgst_total          NUMERIC(14,2) DEFAULT 0.00,
    sgst_total          NUMERIC(14,2) DEFAULT 0.00,
    igst_total          NUMERIC(14,2) DEFAULT 0.00,
    cess_total          NUMERIC(14,2) DEFAULT 0.00,
    tax_total           NUMERIC(14,2) DEFAULT 0.00,
    round_off           NUMERIC(6,2) DEFAULT 0.00,
    grand_total         NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    price_includes_tax  BOOLEAN DEFAULT NULL
);
```

| Column | Type | Description |
|---|---|---|
| `invoice_no` | BIGSERIAL | Auto-incrementing invoice number |
| `customer_id` | UUID FK | Link to customer (NULL for walk-in) |
| `payment_type` | ENUM | `CASH` or `CREDIT` |
| `total_amount` | NUMERIC(14,2) | Pre-tax total |
| `discount_total` | NUMERIC(14,2) | Sum of all line discounts |
| `grand_total` | NUMERIC(14,2) | Final chargeable amount (incl. tax) |
| `supply_type` | TEXT | `INTRA_STATE` (CGST+SGST) or `INTER_STATE` (IGST) |

### 3.6 `sales_invoice_items` — Sales Invoice Line Items

```sql
CREATE TABLE sales_invoice_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id      UUID NOT NULL REFERENCES sales_invoices(id) ON DELETE RESTRICT,
    medicine_id     UUID NOT NULL REFERENCES medicines(id),
    batch_id        UUID NOT NULL REFERENCES batches(id),
    quantity        INT NOT NULL CHECK (quantity > 0),
    unit_sale_price NUMERIC(12,2) NOT NULL,
    subtotal        NUMERIC(14,2) NOT NULL,
    discount_type   TEXT NOT NULL DEFAULT 'NONE',
    discount_value  NUMERIC(12,2) NOT NULL DEFAULT 0.00,
    discount_amount NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    -- GST tax snapshot
    hsn_code        VARCHAR(20),
    gross_amount    NUMERIC(14,2),
    taxable_value   NUMERIC(14,2),
    gst_rate        NUMERIC(5,2) DEFAULT NULL,
    cgst_rate       NUMERIC(5,2) DEFAULT NULL,
    cgst_amount     NUMERIC(14,2) DEFAULT NULL,
    sgst_rate       NUMERIC(5,2) DEFAULT NULL,
    sgst_amount     NUMERIC(14,2) DEFAULT NULL,
    igst_rate       NUMERIC(5,2) DEFAULT NULL,
    igst_amount     NUMERIC(14,2) DEFAULT NULL,
    cess_rate       NUMERIC(5,2) DEFAULT NULL,
    cess_amount     NUMERIC(14,2) DEFAULT NULL,
    line_total      NUMERIC(14,2)
);
```

### 3.7 `purchase_orders` — Purchase Invoice Headers

```sql
CREATE TABLE purchase_orders (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_no          VARCHAR(50) NOT NULL UNIQUE,
    supplier_name       VARCHAR(255) NOT NULL,
    total_amount        NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    discount_total      NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- GST fields
    supplier_id         UUID REFERENCES suppliers(id) ON DELETE SET NULL,
    supplier_gstin      VARCHAR(15),
    supplier_state_code VARCHAR(2),
    store_id            UUID REFERENCES stores(id) ON DELETE SET NULL,
    gst_registration_id UUID REFERENCES gst_registrations(id) ON DELETE SET NULL,
    supply_type         TEXT DEFAULT NULL,
    gross_amount        NUMERIC(14,2),
    taxable_amount      NUMERIC(14,2),
    cgst_total          NUMERIC(14,2) DEFAULT 0.00,
    sgst_total          NUMERIC(14,2) DEFAULT 0.00,
    igst_total          NUMERIC(14,2) DEFAULT 0.00,
    cess_total          NUMERIC(14,2) DEFAULT 0.00,
    tax_total           NUMERIC(14,2) DEFAULT 0.00,
    grand_total         NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    price_includes_tax  BOOLEAN DEFAULT NULL
);
```

### 3.8 `purchase_order_items` — Purchase Invoice Line Items

```sql
CREATE TABLE purchase_order_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    purchase_id     UUID NOT NULL REFERENCES purchase_orders(id) ON DELETE CASCADE,
    medicine_id     UUID NOT NULL REFERENCES medicines(id),
    batch_number    VARCHAR(100) NOT NULL,
    expiry_date     DATE NOT NULL,
    quantity        INT NOT NULL CHECK (quantity > 0),
    bonus_quantity  INT NOT NULL DEFAULT 0 CHECK (bonus_quantity >= 0),
    purchase_price  NUMERIC(12,2) NOT NULL DEFAULT 0.00 CHECK (purchase_price >= 0),
    sale_price      NUMERIC(12,2) NOT NULL DEFAULT 0.00 CHECK (sale_price >= 0),
    discount_type   TEXT NOT NULL DEFAULT 'NONE',
    discount_value  NUMERIC(12,2) NOT NULL DEFAULT 0.00,
    discount_amount NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    -- GST tax snapshot
    hsn_code        VARCHAR(20),
    gross_amount    NUMERIC(14,2),
    taxable_value   NUMERIC(14,2),
    gst_rate        NUMERIC(5,2) DEFAULT NULL,
    cgst_rate       NUMERIC(5,2) DEFAULT NULL,
    cgst_amount     NUMERIC(14,2) DEFAULT NULL,
    sgst_rate       NUMERIC(5,2) DEFAULT NULL,
    sgst_amount     NUMERIC(14,2) DEFAULT NULL,
    igst_rate       NUMERIC(5,2) DEFAULT NULL,
    igst_amount     NUMERIC(14,2) DEFAULT NULL,
    cess_rate       NUMERIC(5,2) DEFAULT NULL,
    cess_amount     NUMERIC(14,2) DEFAULT NULL,
    line_total      NUMERIC(14,2)
);
```

### 3.9 `customer_ledger` — Credit Audit Trail

```sql
CREATE TABLE customer_ledger (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id   UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    entry_type    TEXT NOT NULL CHECK (entry_type IN ('CREDIT_SALE', 'PAYMENT', 'ADJUSTMENT')),
    amount        NUMERIC(14,2) NOT NULL,
    balance_after NUMERIC(14,2) NOT NULL,
    notes         TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 3.10 `reconciliation_journals` + `reconciliation_items` — Stock Audit

```sql
CREATE TABLE reconciliation_journals (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    verified_by_user_id UUID,
    notes              TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE reconciliation_items (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    journal_id        UUID NOT NULL REFERENCES reconciliation_journals(id) ON DELETE CASCADE,
    medicine_id       UUID NOT NULL REFERENCES medicines(id),
    batch_id          UUID NOT NULL REFERENCES batches(id),
    system_stock      INT NOT NULL,
    physical_stock    INT NOT NULL CHECK (physical_stock >= 0),
    variance_quantity INT NOT NULL,
    cost_impact       NUMERIC(14,2) NOT NULL DEFAULT 0.00
);
```

### 3.11 GST / Tax Infrastructure

```sql
-- Businesses
CREATE TABLE businesses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    legal_name TEXT NOT NULL, trade_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- GST Registrations (one per store)
CREATE TABLE gst_registrations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    gstin VARCHAR(15), legal_name TEXT, trade_name TEXT, pan VARCHAR(10),
    state_code VARCHAR(2), state_name TEXT, address TEXT,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Stores
CREATE TABLE stores (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    gst_registration_id UUID REFERENCES gst_registrations(id) ON DELETE SET NULL,
    name TEXT NOT NULL, address TEXT, is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- HSN Codes
CREATE TABLE hsn_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(20) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Tax Rates (effective-dated)
CREATE TABLE tax_rates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hsn_code_id UUID NOT NULL REFERENCES hsn_codes(id) ON DELETE CASCADE,
    gst_rate NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    cgst_rate NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    sgst_rate NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    igst_rate NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    cess_rate NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    effective_from DATE NOT NULL,
    effective_to DATE,  -- NULL = currently active
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Medicine Tax Configuration
CREATE TABLE medicine_tax_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    medicine_id UUID NOT NULL REFERENCES medicines(id) ON DELETE CASCADE,
    hsn_code_id UUID NOT NULL REFERENCES hsn_codes(id) ON DELETE CASCADE,
    tax_rate_id UUID NOT NULL REFERENCES tax_rates(id) ON DELETE CASCADE,
    price_includes_tax BOOLEAN NOT NULL DEFAULT false,
    effective_from DATE NOT NULL,
    effective_to DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 3.12 `sales_credit_notes` — Return Credit Notes (Schema Ready)

```sql
CREATE TABLE sales_credit_notes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id UUID NOT NULL REFERENCES sales_invoices(id) ON DELETE RESTRICT,
    note_no BIGSERIAL UNIQUE,
    reason TEXT, gross_amount NUMERIC(14,2), taxable_amount NUMERIC(14,2),
    cgst_total NUMERIC(14,2), sgst_total NUMERIC(14,2),
    igst_total NUMERIC(14,2), cess_total NUMERIC(14,2),
    tax_total NUMERIC(14,2), grand_total NUMERIC(14,2),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sales_credit_note_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    credit_note_id UUID NOT NULL REFERENCES sales_credit_notes(id) ON DELETE CASCADE,
    invoice_item_id UUID REFERENCES sales_invoice_items(id) ON DELETE SET NULL,
    medicine_id UUID NOT NULL REFERENCES medicines(id),
    batch_id UUID NOT NULL REFERENCES batches(id),
    quantity INT NOT NULL CHECK (quantity > 0),
    hsn_code VARCHAR(20), taxable_value NUMERIC(14,2),
    gst_rate NUMERIC(5,2), cgst_amount NUMERIC(14,2),
    sgst_amount NUMERIC(14,2), igst_amount NUMERIC(14,2),
    cess_amount NUMERIC(14,2), line_total NUMERIC(14,2)
);
```

---

## 4. API Reference

Base URL: `http://localhost:8080/api`

### 4.1 Health Check

| | |
|---|---|
| **Method** | `GET` |
| **Endpoint** | `/api/health` |
| **Response** | `{"status": "ok"}` |

---

### 4.2 Sync Endpoints (IndexedDB Cache Population)

#### `GET /api/sync/inventory`

Returns full medicine catalog with all batches for offline POS cache.

**Response:**
```json
{
  "synced_at": "2026-08-26T10:30:00Z",
  "medicines": [
    {
      "id": "uuid",
      "name": "Paracetamol 500mg",
      "salt_composition": "Paracetamol",
      "manufacturer": "Cipla",
      "min_reorder_level": 50,
      "packing": "10x10",
      "created_at": "...",
      "updated_at": "...",
      "batches": [
        {
          "id": "uuid",
          "medicine_id": "uuid",
          "batch_number": "BATCH001",
          "expiry_date": "2027-06-15",
          "purchase_price": 8.50,
          "sale_price": 12.00,
          "current_stock": 200,
          "created_at": "...",
          "updated_at": "..."
        }
      ]
    }
  ]
}
```

#### `GET /api/sync/customers`

**Response:**
```json
{
  "synced_at": "2026-08-26T10:30:00Z",
  "customers": [
    {
      "id": "uuid",
      "name": "Rajesh Kumar",
      "phone": "9876543210",
      "credit_limit": 5000.00,
      "current_balance": 1200.00,
      "gstin": null,
      "customer_type": "B2C",
      "billing_address": null,
      "shipping_address": null,
      "state": null,
      "state_code": null,
      "created_at": "...",
      "updated_at": "..."
    }
  ]
}
```

---

### 4.3 Medicine CRUD

#### `GET /api/medicines`

**Response:**
```json
{
  "medicines": [
    { "id": "uuid", "name": "Paracetamol 500mg", "salt_composition": "Paracetamol", "manufacturer": "Cipla", "min_reorder_level": 50, "packing": "10x10", "created_at": "...", "updated_at": "..." }
  ]
}
```

#### `POST /api/medicines`

**Request:**
```json
{
  "name": "Amoxicillin 250mg",
  "salt_composition": "Amoxicillin Trihydrate",
  "manufacturer": "Sun Pharma",
  "min_reorder_level": 30,
  "packing": "10x10"
}
```

**Response:** `201 Created` — full Medicine object with `id`.

#### `GET /api/medicines/:id`

**Response:** Single Medicine object.

#### `GET /api/medicines/:id/detail`

**Response:**
```json
{
  "id": "uuid",
  "name": "Paracetamol 500mg",
  "salt_composition": "Paracetamol",
  "manufacturer": "Cipla",
  "min_reorder_level": 50,
  "packing": "10x10",
  "created_at": "...",
  "updated_at": "...",
  "batches": [
    {
      "id": "uuid", "medicine_id": "uuid", "batch_number": "B001",
      "expiry_date": "2027-06-15", "purchase_price": 8.50,
      "sale_price": 12.00, "current_stock": 200,
      "created_at": "...", "updated_at": "...", "expired": false
    }
  ],
  "total_stock": 200,
  "sales_stats": { "units_sold": 150, "total_revenue": 1800.00, "invoices": 12 },
  "purchase_stats": { "units_purchased": 300, "total_spend": 2550.00, "orders": 3 },
  "recent_sales": [
    { "invoice_id": "uuid", "invoice_no": 1001, "quantity": 10, "unit_sale_price": 12.00, "subtotal": 120.00, "created_at": "2026-08-25", "customer_name": "Rajesh" }
  ],
  "recent_purchases": [
    { "purchase_id": "uuid", "invoice_no": "PINV-20260820", "supplier_name": "ABC Pharma", "quantity": 100, "bonus_quantity": 10, "purchase_price": 8.50, "created_at": "2026-08-20" }
  ],
  "tax_config": {
    "id": "uuid", "medicine_id": "uuid", "hsn_code_id": "uuid",
    "tax_rate_id": "uuid", "price_includes_tax": false,
    "effective_from": "2026-01-01", "effective_to": null,
    "hsn_code": "3004", "tax_rate": { "gst_rate": 12, "cgst_rate": 6, "sgst_rate": 6, "igst_rate": 12, "cess_rate": 0 }
  }
}
```

#### `PUT /api/medicines/:id`

**Request:** Full Medicine JSON body. Returns updated Medicine.

#### `DELETE /api/medicines/:id`

**Response:** `{"deleted": true}` (soft delete)

---

### 4.4 Customer CRUD + Ledger

#### `GET /api/customers`

**Response:** `{"customers": [Customer...]}`

#### `POST /api/customers`

**Request:**
```json
{
  "name": "Rajesh Kumar",
  "phone": "9876543210",
  "credit_limit": 5000.00,
  "customer_type": "B2C"
}
```

**Response:** `201 Created` — full Customer object.

#### `GET /api/customers/:id`

**Response:** Single Customer object.

#### `PUT /api/customers/:id`

**Request:** Full Customer JSON body. Returns updated Customer.

#### `GET /api/customers/:id/ledger`

**Response:**
```json
{
  "customer": { "id": "uuid", "name": "Rajesh Kumar", "phone": "9876543210", "credit_limit": 5000, "current_balance": 1200 },
  "entries": [
    { "id": "uuid", "customer_id": "uuid", "entry_type": "CREDIT_SALE", "amount": 1500.00, "balance_after": 1500.00, "notes": "Invoice #1001", "created_at": "..." },
    { "id": "uuid", "customer_id": "uuid", "entry_type": "PAYMENT", "amount": -300.00, "balance_after": 1200.00, "notes": "Cash payment", "created_at": "..." }
  ]
}
```

#### `POST /api/customers/:id/payments`

**Request:**
```json
{
  "amount": 500.00,
  "notes": "Cash payment received"
}
```

**Response:**
```json
{
  "customer": { "id": "uuid", "name": "Rajesh", "current_balance": 700.00 },
  "entry": { "id": "uuid", "entry_type": "PAYMENT", "amount": -500.00, "balance_after": 700.00 }
}
```

---

### 4.5 Supplier CRUD

#### `GET /api/suppliers`

**Response:** Array of Supplier objects.

#### `POST /api/suppliers`

**Request:**
```json
{
  "legal_name": "ABC Pharmaceuticals Pvt Ltd",
  "trade_name": "ABC Pharma",
  "gstin": "27AABCU9603R1ZM",
  "pan": "AABCU9603R",
  "address": "Mumbai, Maharashtra",
  "state": "Maharashtra",
  "state_code": "27",
  "phone": "02212345678",
  "email": "contact@abcpharma.com"
}
```

**Response:** `201 Created` — full Supplier object.

#### `GET /api/suppliers/:id`, `PUT /api/suppliers/:id`, `DELETE /api/suppliers/:id`

Standard CRUD operations.

---

### 4.6 HSN Codes & Tax Rates

#### `GET /api/hsn`

**Response:** `{"hsn_codes": [{ "id": "uuid", "code": "3004", "description": "Medicaments", "created_at": "..." }]}`

#### `POST /api/hsn`

**Request:** `{"code": "3004", "description": "Medicaments"}`

**Response:** `201 Created` — HSN code object.

#### `PUT /api/hsn/:id/tax-rate`

**Request:**
```json
{
  "gst_rate": 12.00,
  "cgst_rate": 6.00,
  "sgst_rate": 6.00,
  "igst_rate": 12.00,
  "cess_rate": 0.00
}
```

**Response:** Updated TaxRate object.

#### `GET /api/medicines/:id/tax-config`

**Response:** MedicineTaxConfig object or `null`.

#### `PUT /api/medicines/:id/tax-config`

**Request:**
```json
{
  "hsn_code_id": "uuid",
  "tax_rate_id": "uuid",
  "price_includes_tax": false
}
```

**Response:** Updated MedicineTaxConfig object.

---

### 4.7 Sales (POS Checkout)

#### `POST /api/sales/checkout`

**Request:**
```json
{
  "customer_id": "uuid-or-null",
  "payment_type": "CASH",
  "store_id": "uuid-or-null",
  "place_of_supply": "27",
  "items": [
    {
      "batch_id": "uuid",
      "quantity": 2,
      "discount": { "type": "percent", "value": 10 }
    },
    {
      "batch_id": "uuid",
      "quantity": 1,
      "discount": null
    }
  ]
}
```

**Validation rules:**
- `payment_type`: Must be `CASH` or `CREDIT`
- `items`: At least 1 item required
- Each item: `quantity > 0`, `batch_id` required
- Discount `type`: `percent` or `amount`; `value >= 0`
- Credit sales require `customer_id`
- Duplicate batch lines with conflicting discounts are rejected

**Response:** `201 Created`
```json
{
  "invoice": {
    "id": "uuid",
    "invoice_no": 1001,
    "customer_id": "uuid",
    "payment_type": "CASH",
    "total_amount": 36.00,
    "discount_total": 4.00,
    "created_at": "2026-08-26T10:30:00Z",
    "supply_type": "INTRA_STATE",
    "gross_amount": 40.00,
    "taxable_amount": 40.00,
    "cgst_total": 2.40,
    "sgst_total": 2.40,
    "igst_total": 0.00,
    "cess_total": 0.00,
    "tax_total": 4.80,
    "round_off": 0.00,
    "grand_total": 40.80,
    "price_includes_tax": false
  },
  "items": [
    {
      "id": "uuid",
      "invoice_id": "uuid",
      "medicine_id": "uuid",
      "batch_id": "uuid",
      "quantity": 2,
      "unit_sale_price": 12.00,
      "subtotal": 21.60,
      "discount_type": "percent",
      "discount_value": 10.00,
      "discount_amount": 2.40,
      "hsn_code": "3004",
      "gross_amount": 24.00,
      "taxable_value": 24.00,
      "gst_rate": 12.00,
      "cgst_rate": 6.00,
      "cgst_amount": 1.44,
      "sgst_rate": 6.00,
      "sgst_amount": 1.44,
      "igst_rate": 0.00,
      "igst_amount": 0.00,
      "cess_rate": 0.00,
      "cess_amount": 0.00,
      "line_total": 24.48
    }
  ]
}
```

---

### 4.8 Purchase Inward

#### `POST /api/purchases`

**Request:**
```json
{
  "invoice_no": "SUP-INV-2026-001",
  "supplier_name": "ABC Pharmaceuticals",
  "supplier_id": "uuid",
  "supplier_gstin": "27AABCU9603R1ZM",
  "supplier_state": "27",
  "store_id": "uuid",
  "place_of_supply": "27",
  "discount_total": 50.00,
  "items": [
    {
      "medicine_id": "uuid-or-empty",
      "medicine_name": "New Drug 500mg",
      "salt_composition": "Drug HCl",
      "manufacturer": "XYZ Labs",
      "packing": "10x10",
      "min_reorder_level": 20,
      "hsn_code": "3004",
      "price_includes_tax": false,
      "batch_number": "BATCH-NEW-001",
      "expiry_date": "2028-12-31",
      "quantity": 100,
      "bonus_quantity": 10,
      "purchase_price": 8.50,
      "sale_price": 12.00,
      "discount_type": "percent",
      "discount_value": 5.00
    }
  ]
}
```

**Validation rules:**
- `supplier_name` required
- At least 1 item required
- Each item: requires `medicine_id` OR `medicine_name` (empty `medicine_id` + `medicine_name` = auto-create medicine)
- `batch_number` required; `quantity > 0`; `bonus_quantity >= 0`
- `expiry_date` required (YYYY-MM-DD)
- `discount_type`: `NONE`, `percent`, or `amount`

**Response:** `201 Created`
```json
{
  "purchase_order": {
    "id": "uuid",
    "invoice_no": "SUP-INV-2026-001",
    "supplier_name": "ABC Pharmaceuticals",
    "total_amount": 765.00,
    "discount_total": 50.00,
    "created_at": "2026-08-26T10:30:00Z",
    "supply_type": "INTRA_STATE",
    "gross_amount": 850.00,
    "taxable_amount": 850.00,
    "cgst_total": 51.00,
    "sgst_total": 51.00,
    "igst_total": 0.00,
    "cess_total": 0.00,
    "tax_total": 102.00,
    "grand_total": 867.00,
    "price_includes_tax": false
  },
  "items": [
    {
      "id": "uuid",
      "purchase_id": "uuid",
      "medicine_id": "uuid",
      "batch_number": "BATCH-NEW-001",
      "expiry_date": "2028-12-31",
      "quantity": 100,
      "bonus_quantity": 10,
      "purchase_price": 8.50,
      "sale_price": 12.00,
      "discount_type": "percent",
      "discount_value": 5.00,
      "discount_amount": 42.50,
      "medicine_name": "New Drug 500mg",
      "hsn_code": "3004",
      "gross_amount": 850.00,
      "taxable_value": 807.50,
      "gst_rate": 12.00,
      "cgst_rate": 6.00,
      "cgst_amount": 48.45,
      "sgst_rate": 6.00,
      "sgst_amount": 48.45,
      "igst_rate": 0.00,
      "igst_amount": 0.00,
      "cess_rate": 0.00,
      "cess_amount": 0.00,
      "line_total": 855.90
    }
  ]
}
```

---

### 4.9 Invoice Listing & Detail

#### `GET /api/sales/invoices?start_date=&end_date=&q=`

**Query params:**
- `start_date` / `end_date`: ISO date strings (defaults to last 30 days)
- `q`: optional search by invoice number

**Response:**
```json
{
  "invoices": [
    {
      "id": "uuid",
      "invoice_no": 1001,
      "customer_id": "uuid",
      "payment_type": "CASH",
      "total_amount": 36.00,
      "discount_total": 4.00,
      "created_at": "2026-08-26T10:30:00Z",
      "customer_name": "Rajesh Kumar",
      "item_count": 2,
      "units_sold": 3,
      "supply_type": "INTRA_STATE",
      "grand_total": 40.80,
      "tax_total": 4.80
    }
  ]
}
```

#### `GET /api/sales/invoices/:id`

**Response:**
```json
{
  "invoice": { /* full SalesInvoice with all GST fields */ },
  "customer_name": "Rajesh Kumar",
  "items": [
    {
      "id": "uuid",
      "invoice_id": "uuid",
      "medicine_id": "uuid",
      "batch_id": "uuid",
      "quantity": 2,
      "unit_sale_price": 12.00,
      "subtotal": 21.60,
      "discount_type": "percent",
      "discount_value": 10.00,
      "discount_amount": 2.40,
      "medicine_name": "Paracetamol 500mg",
      "batch_number": "B001",
      "hsn_code": "3004",
      "gross_amount": 24.00,
      "taxable_value": 24.00,
      "cgst_rate": 6.00, "cgst_amount": 1.44,
      "sgst_rate": 6.00, "sgst_amount": 1.44,
      "igst_rate": 0.00, "igst_amount": 0.00,
      "cess_rate": 0.00, "cess_amount": 0.00,
      "line_total": 24.48
    }
  ]
}
```

#### `GET /api/purchases/invoices?start_date=&end_date=&q=`

**Response:**
```json
{
  "invoices": [
    {
      "id": "uuid",
      "invoice_no": "SUP-INV-2026-001",
      "supplier_name": "ABC Pharmaceuticals",
      "total_amount": 765.00,
      "discount_total": 50.00,
      "created_at": "2026-08-26T10:30:00Z",
      "item_count": 3,
      "units_purchased": 310,
      "supply_type": "INTRA_STATE",
      "tax_total": 102.00,
      "grand_total": 867.00
    }
  ]
}
```

#### `GET /api/purchases/invoices/:id`

**Response:**
```json
{
  "invoice": { /* full PurchaseOrder with all GST fields */ },
  "items": [
    {
      "id": "uuid",
      "purchase_id": "uuid",
      "medicine_id": "uuid",
      "batch_number": "BATCH-001",
      "expiry_date": "2028-12-31",
      "quantity": 100,
      "bonus_quantity": 10,
      "purchase_price": 8.50,
      "sale_price": 12.00,
      "discount_type": "percent",
      "discount_value": 5.00,
      "discount_amount": 42.50,
      "medicine_name": "New Drug 500mg",
      "hsn_code": "3004",
      "gross_amount": 850.00,
      "taxable_value": 807.50,
      "gst_rate": 12.00,
      "cgst_rate": 6.00, "cgst_amount": 48.45,
      "sgst_rate": 6.00, "sgst_amount": 48.45,
      "igst_rate": 0.00, "igst_amount": 0.00,
      "cess_rate": 0.00, "cess_amount": 0.00,
      "line_total": 855.90
    }
  ]
}
```

---

### 4.10 Inventory Reconciliation

#### `POST /api/inventory/reconcile`

**Request:**
```json
{
  "notes": "Monthly stock audit",
  "verified_by_user_id": "user-123",
  "items": [
    {
      "medicine_id": "uuid",
      "batch_id": "uuid",
      "physical_stock": 195
    }
  ]
}
```

**Response:** `201 Created`
```json
{
  "journal": {
    "id": "uuid",
    "verified_by_user_id": "user-123",
    "notes": "Monthly stock audit",
    "created_at": "2026-08-26T10:30:00Z",
    "item_count": 1
  },
  "items": [
    {
      "id": "uuid",
      "journal_id": "uuid",
      "medicine_id": "uuid",
      "batch_id": "uuid",
      "system_stock": 200,
      "physical_stock": 195,
      "variance_quantity": -5,
      "cost_impact": -42.50,
      "batch_number": "B001",
      "medicine_name": "Paracetamol 500mg"
    }
  ]
}
```

#### `GET /api/inventory/reconciliations?limit=10`

Returns past reconciliation journals with their items.

---

### 4.11 Reports

#### `GET /api/reports/sales?start_date=&end_date=`

```json
{
  "start_date": "2026-08-01T00:00:00Z",
  "end_date": "2026-08-27T00:00:00Z",
  "breakdown": [
    { "payment_type": "CASH", "invoices": 45, "total": 12500.00, "units_sold": 320 },
    { "payment_type": "CREDIT", "invoices": 12, "total": 4800.00, "units_sold": 95 }
  ],
  "daily": [
    { "day": "2026-08-26", "payment_type": "CASH", "invoices": 8, "total": 2400.00 }
  ],
  "net_sales": 17300.00,
  "net_units": 415
}
```

#### `GET /api/reports/purchase?start_date=&end_date=`

```json
{
  "start_date": "...",
  "end_date": "...",
  "order_count": 15,
  "item_count": 87,
  "total_spend": 45000.00,
  "suppliers": [
    { "supplier_name": "ABC Pharma", "orders": 8, "items": 52, "total": 28000.00 }
  ]
}
```

#### `GET /api/reports/profit-loss?start_date=&end_date=`

```json
{
  "start_date": "...",
  "end_date": "...",
  "lines": [
    { "medicine_id": "uuid", "medicine_name": "Paracetamol 500mg", "units_sold": 150, "revenue": 1800.00, "cost": 1275.00, "profit": 525.00, "margin_pct": 29.17 }
  ],
  "total_revenue": 17300.00,
  "total_cost": 12500.00,
  "total_profit": 4800.00,
  "margin_pct": 27.75
}
```

#### `GET /api/reports/expiry?within_months=6`

```json
{
  "within_months": 6,
  "batches": [
    { "batch_id": "uuid", "medicine_id": "uuid", "medicine_name": "Amoxicillin", "manufacturer": "Sun Pharma", "batch_number": "A001", "expiry_date": "2026-12-15", "current_stock": 50, "purchase_price": 15.00, "sale_price": 22.00, "stock_value": 750.00, "expired": false }
  ]
}
```

#### `GET /api/reports/low-stock`

```json
{
  "items": [
    { "medicine_id": "uuid", "medicine_name": "Paracetamol 500mg", "manufacturer": "Cipla", "min_reorder_level": 50, "total_stock": 30, "shortfall": 20 }
  ]
}
```

---

### 4.12 Error Responses

All errors returned as JSON:

```json
{ "error": "insufficient stock for batch uuid: requested 10, available 5" }
{ "error": "credit limit exceeded for customer Rajesh (uuid): outstanding 4500.00 + invoice 800.00 exceeds limit 5000.00" }
{ "error": "record not found" }
{ "error": "payment_type must be CASH or CREDIT" }
```

| Error Type | HTTP Status |
|---|---|
| Validation / Bad Request | 400 |
| Record Not Found | 404 |
| Insufficient Stock | 400 |
| Credit Limit Exceeded | 400 |
| Overpayment | 400 |
| Internal Error | 500 |

---

## 5. Inward Inventory Flow (Purchase)

The inward flow is triggered by `POST /api/purchases` and handled by `PurchaseRepo.CreateInward()`.

### Flow Diagram

```
Supplier Invoice Received
        │
        ▼
┌─────────────────────────────────────────────────────────┐
│  1. VALIDATE INPUT                                       │
│     - supplier_name required                             │
│     - At least 1 item                                    │
│     - Each item: batch_number, quantity > 0, expiry_date │
│     - Items without medicine_id auto-create medicine     │
└─────────────────────────────────────────────────────────┘
        │
        ▼
┌─────────────────────────────────────────────────────────┐
│  2. CALCULATE LINE TOTALS (pre-transaction)              │
│     - gross = quantity × purchase_price                  │
│     - discount_amount = line discount (percent/amount)   │
│     - net = gross - discount                             │
│     - Effective batch price = net / (quantity + bonus)   │
│     - Tax calculation via tax engine (if tax config)     │
└─────────────────────────────────────────────────────────┘
        │
        ▼
┌─────────────────────────────────────────────────────────┐
│  3. BEGIN TRANSACTION                                    │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3a. Resolve medicine references                 │   │
│     │     - Existing medicine_id → verify exists      │   │
│     │     - No medicine_id + name → INSERT medicine   │   │
│     │     - If HSN provided → create HSN + tax config │   │
│     └────────────────────────────────────────────────┘   │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3b. INSERT purchase_orders header               │   │
│     │     - invoice_no, supplier_name, total_amount   │   │
│     │     - All GST snapshot fields                   │   │
│     └────────────────────────────────────────────────┘   │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3c. UPSERT batches (per item)                   │   │
│     │     INSERT INTO batches                         │   │
│     │       (medicine_id, batch_number, expiry_date,  │   │
│     │        purchase_price, sale_price, current_stock)│   │
│     │     ON CONFLICT (medicine_id, batch_number)     │   │
│     │     DO UPDATE SET                               │   │
│     │       expiry_date = EXCLUDED.expiry_date,       │   │
│     │       purchase_price = EXCLUDED.purchase_price, │   │
│     │       sale_price = EXCLUDED.sale_price,         │   │
│     │       current_stock = batches.current_stock     │   │
│     │                           + EXCLUDED.current_stock│  │
│     │                                                │   │
│     │     ⚡ KEY: Same batch number MERGES stock       │   │
│     └────────────────────────────────────────────────┘   │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3d. INSERT purchase_order_items (per item)      │   │
│     │     - All fields including bonus_quantity,      │   │
│     │       discount, and GST tax snapshots           │   │
│     └────────────────────────────────────────────────┘   │
│  COMMIT                                                 │
└─────────────────────────────────────────────────────────┘
        │
        ▼
  Inventory Updated
  (batch.current_stock increased)
```

### Key Rules

1. **Batch Upsert:** Batches are uniquely identified by `(medicine_id, batch_number)`. Receiving the same batch number again **merges stock** (adds to `current_stock`) and updates prices/expiry.
2. **Bonus Quantity:** `total_stock = quantity + bonus_quantity`. The effective purchase price stored in the batch is **blended**: `total_cost / total_units_received`.
3. **Auto-create Medicine:** If `medicine_id` is empty but `medicine_name` is provided, a new medicine record is created in the same transaction.
4. **Tax Config Auto-assignment:** When creating a new medicine with an `hsn_code`, the system auto-creates the HSN code if needed and assigns the active tax rate.
5. **Atomicity:** Everything runs in a single PostgreSQL transaction. Any failure rolls back all changes.

### Blended Cost Calculation Example

```
Received: 100 units × ₹8.50 = ₹850
Bonus: 10 units
Discount: 5% = ₹42.50
Net cost: ₹850 - ₹42.50 = ₹807.50
Total units: 100 + 10 = 110
Effective batch price: ₹807.50 / 110 = ₹7.34 per unit
```

---

## 6. Outward Inventory Flow (Sale)

The outward flow is triggered by `POST /api/sales/checkout` and handled by `SaleRepo.Checkout()`.

### Flow Diagram

```
Customer at POS
        │
        ▼
┌─────────────────────────────────────────────────────────┐
│  1. VALIDATE INPUT                                       │
│     - payment_type: CASH or CREDIT                       │
│     - At least 1 item with batch_id + quantity > 0       │
│     - Credit sales require customer_id                   │
│     - Discount type must be percent/amount               │
└─────────────────────────────────────────────────────────┘
        │
        ▼
┌─────────────────────────────────────────────────────────┐
│  2. MERGE DUPLICATE BATCH LINES                          │
│     - Same batch_id + same discount → merge quantities   │
│     - Same batch_id + different discount → reject        │
└─────────────────────────────────────────────────────────┘
        │
        ▼
┌─────────────────────────────────────────────────────────┐
│  3. BEGIN TRANSACTION                                    │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3a. Lock batch rows (FOR UPDATE, sorted by ID) │   │
│     │     - Prevents concurrent overselling           │   │
│     │     - Deterministic order prevents deadlocks    │   │
│     └────────────────────────────────────────────────┘   │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3b. Verify stock for each line                  │   │
│     │     - If quantity > current_stock → abort with  │   │
│     │       InsufficientStockError                    │   │
│     └────────────────────────────────────────────────┘   │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3c. Determine supply type (intra/inter state)   │   │
│     │     - Compare store state vs customer state     │   │
│     └────────────────────────────────────────────────┘   │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3d. Calculate tax per line (tax engine)         │   │
│     │     - Look up medicine_tax_config               │   │
│     │     - Compute CGST/SGST or IGST based on supply│   │
│     └────────────────────────────────────────────────┘   │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3e. Credit check (for CREDIT payments)          │   │
│     │     - Lock customer row (FOR UPDATE)            │   │
│     │     - If balance + grand_total > credit_limit   │   │
│     │       → abort with CreditLimitExceededError     │   │
│     └────────────────────────────────────────────────┘   │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3f. INSERT sales_invoices header                │   │
│     │     - Auto-incrementing invoice_no              │   │
│     │     - All GST summary fields                    │   │
│     └────────────────────────────────────────────────┘   │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3g. INSERT sales_invoice_items (per line)       │   │
│     │     - Per-line tax snapshots frozen at sale time│   │
│     └────────────────────────────────────────────────┘   │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3h. DECREMENT batch stock                       │   │
│     │     UPDATE batches SET current_stock -= quantity │   │
│     │     WHERE id = ? AND current_stock >= quantity  │   │
│     │     (double-check with WHERE guard)             │   │
│     └────────────────────────────────────────────────┘   │
│     ┌────────────────────────────────────────────────┐   │
│     │ 3i. Credit ledger (for CREDIT sales)            │   │
│     │     - UPDATE customers.current_balance += total │   │
│     │     - INSERT customer_ledger entry              │   │
│     └────────────────────────────────────────────────┘   │
│  COMMIT                                                 │
└─────────────────────────────────────────────────────────┘
        │
        ▼
  Invoice Returned
  (batch.current_stock decreased)
```

### Key Rules

1. **No Negative Stock:** Stock is checked twice — once after `FOR UPDATE` lock, and again with `WHERE current_stock >= quantity` guard on the UPDATE.
2. **Deterministic Locking:** Batch IDs are sorted before locking to prevent deadlocks in concurrent checkouts.
3. **Tax Snapshots:** Each line item stores its tax rates and amounts at the time of sale, so historical invoices are immune to future tax rate changes.
4. **Credit Limit Enforcement:** `balance + grand_total <= credit_limit` is checked within the transaction with the customer row locked.
5. **Atomicity:** The entire checkout (invoice + items + stock decrement + credit update) is a single transaction.

---

## 7. Purchase Invoice Details

### Header Fields (`purchase_orders`)

| Field | Type | Description |
|---|---|---|
| `invoice_no` | VARCHAR(50) | Supplier's invoice number (unique) |
| `supplier_name` | VARCHAR(255) | Supplier name |
| `supplier_id` | UUID FK | Link to suppliers table |
| `supplier_gstin` | VARCHAR(15) | Supplier's GSTIN |
| `supplier_state_code` | VARCHAR(2) | Supplier's state for GST |
| `store_id` | UUID FK | Receiving store |
| `total_amount` | NUMERIC(14,2) | Sum of line net amounts (before invoice discount) |
| `discount_total` | NUMERIC(14,2) | Invoice-level discount |
| `supply_type` | TEXT | `INTRA_STATE` or `INTER_STATE` |
| `gross_amount` | NUMERIC(14,2) | Sum of all line gross amounts |
| `taxable_amount` | NUMERIC(14,2) | Sum of all line taxable values |
| `cgst_total` / `sgst_total` | NUMERIC(14,2) | Total CGST / SGST |
| `igst_total` | NUMERIC(14,2) | Total IGST (inter-state) |
| `cess_total` | NUMERIC(14,2) | Total Cess |
| `tax_total` | NUMERIC(14,2) | Sum of all taxes |
| `grand_total` | NUMERIC(14,2) | Final chargeable amount |
| `price_includes_tax` | BOOLEAN | Whether listed prices include GST |

### Line Item Fields (`purchase_order_items`)

| Field | Type | Description |
|---|---|---|
| `medicine_id` | UUID FK | Medicine reference |
| `batch_number` | VARCHAR(100) | Batch number (used for upsert key) |
| `expiry_date` | DATE | Expiry of this batch |
| `quantity` | INT | Ordered quantity |
| `bonus_quantity` | INT | Free bonus units |
| `purchase_price` | NUMERIC(12,2) | Per-unit purchase price (before discount) |
| `sale_price` | NUMERIC(12,2) | Per-unit MRP |
| `discount_type` | TEXT | `NONE`, `percent`, or `amount` |
| `discount_value` | NUMERIC(12,2) | Discount percentage or flat amount |
| `discount_amount` | NUMERIC(14,2) | Computed discount in currency |
| `hsn_code` | VARCHAR(20) | HSN code snapshot |
| `gross_amount` | NUMERIC(14,2) | `quantity × purchase_price` |
| `taxable_value` | NUMERIC(14,2) | `gross_amount - discount_amount` |
| `cgst_rate`/`cgst_amount` | NUMERIC | CGST rate and computed amount |
| `sgst_rate`/`sgst_amount` | NUMERIC | SGST rate and computed amount |
| `igst_rate`/`igst_amount` | NUMERIC | IGST rate and computed amount |
| `cess_rate`/`cess_amount` | NUMERIC | Cess rate and computed amount |
| `line_total` | NUMERIC(14,2) | `taxable_value + all taxes` |

### Calculation Flow

```
Line gross = quantity × purchase_price
Line discount = gross × discount_percent / 100   (or flat amount)
Line taxable = gross - discount
Line taxes = taxable × rate / 100                (per tax component)
Line total = taxable + cgst + sgst + igst + cess

Invoice total = Σ(line taxable) - invoice_discount_total
Invoice grand_total = invoice total + Σ(all invoice taxes) + round_off
```

---

## 8. Sales Invoice Details

### Header Fields (`sales_invoices`)

| Field | Type | Description |
|---|---|---|
| `invoice_no` | BIGSERIAL | Auto-generated sequential number |
| `customer_id` | UUID FK | Customer (nullable for walk-in cash sales) |
| `payment_type` | ENUM | `CASH` or `CREDIT` |
| `total_amount` | NUMERIC(14,2) | Sum of line subtotals (after line discounts, before tax) |
| `discount_total` | NUMERIC(14,2) | Sum of all line-level discounts |
| `store_id` | UUID FK | Selling store |
| `customer_gstin` | VARCHAR(15) | Customer's GSTIN (for B2B) |
| `customer_state_code` | VARCHAR(2) | Customer's state for GST |
| `supply_type` | TEXT | `INTRA_STATE` or `INTER_STATE` |
| `gross_amount` | NUMERIC(14,2) | Sum of all line gross amounts |
| `taxable_amount` | NUMERIC(14,2) | Sum of all line taxable values |
| `cgst_total` / `sgst_total` | NUMERIC(14,2) | Total CGST / SGST |
| `igst_total` | NUMERIC(14,2) | Total IGST |
| `cess_total` | NUMERIC(14,2) | Total Cess |
| `tax_total` | NUMERIC(14,2) | Sum of all taxes |
| `round_off` | NUMERIC(6,2) | Rounding adjustment |
| `grand_total` | NUMERIC(14,2) | Final chargeable amount |
| `price_includes_tax` | BOOLEAN | Whether sale prices include GST |

### Line Item Fields (`sales_invoice_items`)

| Field | Type | Description |
|---|---|---|
| `medicine_id` | UUID FK | Medicine reference |
| `batch_id` | UUID FK | Specific batch sold |
| `quantity` | INT | Units sold |
| `unit_sale_price` | NUMERIC(12,2) | Per-unit selling price (from batch) |
| `subtotal` | NUMERIC(14,2) | `quantity × unit_sale_price - discount_amount` |
| `discount_type` | TEXT | `NONE`, `percent`, or `amount` |
| `discount_value` | NUMERIC(12,2) | Discount percentage or flat amount |
| `discount_amount` | NUMERIC(14,2) | Computed discount |
| Tax snapshot fields | | Same as purchase items (HSN, CGST, SGST, IGST, Cess) |
| `line_total` | NUMERIC(14,2) | Final line amount including tax |

### Calculation Flow

```
Line gross = quantity × batch.sale_price
Line discount = gross × discount_percent / 100   (or flat, capped at gross)
Line subtotal = gross - discount
               ─── This is what goes into total_amount ───

Tax calculation (per line, if tax config exists):
  Taxable value = subtotal (tax-exclusive) OR gross (if price includes tax)
  CGST = taxable × cgst_rate / 100
  SGST = taxable × sgst_rate / 100
  IGST = taxable × igst_rate / 100  (inter-state only)
  Cess = taxable × cess_rate / 100
  Line total = taxable + cgst + sgst + igst + cess + round_off

Invoice summary:
  total_amount = Σ(subtotal)
  discount_total = Σ(discount_amount)
  gross_amount = Σ(gross_amount)
  taxable_amount = Σ(taxable_value)
  cgst_total = Σ(cgst_amount)
  sgst_total = Σ(sgst_amount)
  igst_total = Σ(igst_amount)
  cess_total = Σ(cess_amount)
  tax_total = cgst_total + sgst_total + igst_total + cess_total
  grand_total = taxable_amount + tax_total + round_off
```

---

## 9. GST / Tax Engine

The tax engine lives in `internal/tax/` and provides pure calculation functions.

### Supply Type Determination

```
If seller_state_code == buyer_state_code:
    supply_type = INTRA_STATE  →  Apply CGST + SGST
Else:
    supply_type = INTER_STATE  →  Apply IGST
```

### Tax Calculation Per Line

**Tax-Exclusive Pricing** (`price_includes_tax = false`):
```
taxable_value = quantity × unit_price - discount_amount
CGST = taxable_value × cgst_rate / 100
SGST = taxable_value × sgst_rate / 100
IGST = taxable_value × igst_rate / 100
Cess = taxable_value × cess_rate / 100
line_total = taxable_value + CGST + SGST + IGST + Cess
```

**Tax-Inclusive Pricing** (`price_includes_tax = true`):
```
gross_amount = quantity × unit_price - discount_amount
taxable_value = gross_amount / (1 + gst_rate / 100)
CGST = taxable_value × cgst_rate / 100
SGST = taxable_value × sgst_rate / 100
IGST = taxable_value × igst_rate / 100
Cess = taxable_value × cess_rate / 100
line_total = gross_amount + Cess (tax already embedded)
```

### Effective-Dated Tax Config

- `tax_rates.effective_from` / `effective_to` — time-bounded GST rates
- `medicine_tax_config.effective_from` / `effective_to` — time-bounded medicine-to-tax mapping
- Only one active (non-expired) config per HSN / per medicine at any time (enforced by partial UNIQUE indexes)
- Historical invoices store tax snapshots at line level, so rate changes don't affect past records

### GST Rate Seed Data

Migration `021_seed_hsn_tax_rates.sql` seeds common Indian pharmaceutical HSN codes:

| HSN | Description | GST Rate | CGST | SGST | IGST |
|---|---|---|---|---|---|
| 3004 | Medicaments (packaged) | 12% | 6% | 6% | 12% |
| 3003 | Medicaments (not packaged) | 12% | 6% | 6% | 12% |
| 3002 | Human blood, vaccines | 0% | 0% | 0% | 0% |
| 3006 | Pharmaceutical preparations | 12% | 6% | 6% | 12% |
| 2941 | Antibiotics | 12% | 6% | 6% | 12% |

---

## 10. Inventory Reconciliation

Physical stock audit via `POST /api/inventory/reconcile`.

### Flow

```
1. Input: List of (medicine_id, batch_id, physical_stock)
2. For each item:
   a. Read current system_stock from batches table
   b. variance = physical_stock - system_stock
   c. cost_impact = variance × batch.purchase_price
   d. UPDATE batches SET current_stock = physical_stock
3. Create reconciliation_journal + reconciliation_items
4. Return journal with variance details
```

### Rules

- `physical_stock` must be >= 0
- Variance can be positive (found extra stock) or negative (stock shortage)
- `cost_impact = variance × purchase_price` — tracks financial impact
- Every reconciliation is audited with timestamp and optional verifier ID

---

## 11. Customer Credit System

### Credit Sale Flow

```
1. Customer selected at POS with payment_type = CREDIT
2. Lock customer row (FOR UPDATE)
3. Check: current_balance + invoice_grand_total <= credit_limit
4. If exceeded → CreditLimitExceededError (400)
5. On success:
   a. INSERT sales_invoices (payment_type = CREDIT)
   b. DECREMENT batch stock
   c. UPDATE customers SET current_balance += grand_total
   d. INSERT customer_ledger (entry_type = 'CREDIT_SALE')
```

### Payment Collection Flow

```
POST /api/customers/:id/payments
1. Read customer current_balance
2. If payment_amount > current_balance → ErrOverpayment
3. UPDATE customers SET current_balance -= payment_amount
4. INSERT customer_ledger (entry_type = 'PAYMENT', amount = -payment_amount)
```

### Ledger Entry Types

| Type | Amount Sign | Description |
|---|---|---|
| `CREDIT_SALE` | Positive | Invoice amount added to balance |
| `PAYMENT` | Negative | Payment received, reduces balance |
| `ADJUSTMENT` | +/- | Manual correction |

---

## 12. Reports

| Report | Endpoint | Key Metrics |
|---|---|---|
| **Sales Report** | `GET /api/reports/sales` | Total sales by payment type, daily breakdown, net units |
| **Purchase Report** | `GET /api/reports/purchase` | Total spend by supplier, order count, item count |
| **Profit & Loss** | `GET /api/reports/profit-loss` | Per-medicine revenue vs cost (batch-level), margin % |
| **Expiry Report** | `GET /api/reports/expiry` | Batches expiring within N months, stock value at risk |
| **Low Stock** | `GET /api/reports/low-stock` | Medicines below min_reorder_level, shortfall quantity |

All reports accept `start_date` and `end_date` query params (default: last 30 days).

---

## 13. Entity Relationship Summary

```
businesses ──1:N── gst_registrations ──1:N── stores

medicines ──1:N── batches
medicines ──1:N── medicine_tax_config ──N:1── hsn_codes ──1:N── tax_rates

customers ──1:N── sales_invoices ──1:N── sales_invoice_items ──N:1── batches
customers ──1:N── customer_ledger

sales_invoices ──1:N── sales_credit_notes ──1:N── sales_credit_note_items

suppliers ──1:N── purchase_orders ──1:N── purchase_order_items

purchase_orders ── references stores, gst_registrations
sales_invoices ── references stores, gst_registrations

batches ── referenced by reconciliation_items
reconciliation_journals ──1:N── reconciliation_items
```

### Key Architectural Decisions

1. **No ORM** — Raw SQL via pgx/v5, business logic in repository methods
2. **Embedded Migrations** — SQL files embedded in Go binary, applied on startup
3. **Offline-first POS** — Full inventory cached in IndexedDB for instant search
4. **Single Binary** — Go serves both API and compiled React SPA
5. **Decimal Precision** — `shopspring/decimal` library for tax calculations, `math.Round(v*100)/100` for simple money
6. **Soft Delete** — Medicines use `deleted_at` for soft delete; other entities are hard-deleted
7. **Tax Snapshots** — Every invoice line stores tax rates/amounts at time of transaction
