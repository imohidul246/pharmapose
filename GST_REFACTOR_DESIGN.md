# GST Refactor Design — PharmaPOS

## Current Architecture

```
Frontend (React 19 + TypeScript)
  ↕ REST JSON /api/
Backend (Go 1.27 + Gin)
  ↕ pgx/v5
PostgreSQL
```

- **No service layer**: business logic in repository methods
- **Embedded SQL migrations** tracked by `schema_migrations` table
- **9 sequential migrations** (0001-0009)
- **All money as float64** in Go, NUMERIC(12,2)/NUMERIC(14,2) in PostgreSQL
- **Rounding**: `round2()` in `sale_repo.go` (shared by all repos)
- **Invoice numbers**: Sales = BIGSERIAL (auto), Purchases = `PINV-{timestamp}`
- **Zero GST code** anywhere in the codebase

---

## Proposed Schema

### New Tables

```sql
-- Business entity supporting multi-store deployment
businesses (
    id UUID PK,
    legal_name TEXT,
    trade_name TEXT,
    created_at, updated_at
)

-- Each store has its own GST registration
gst_registrations (
    id UUID PK,
    business_id UUID FK → businesses,
    gstin VARCHAR(15),           -- null for unregistered businesses
    legal_name TEXT,
    trade_name TEXT,
    pan VARCHAR(10),
    state_code VARCHAR(2),       -- 2-digit state code
    state_name TEXT,
    address TEXT,
    is_active BOOLEAN DEFAULT true,
    created_at, updated_at
)

-- Stores reference their applicable GST registration
stores (
    id UUID PK,
    gst_registration_id UUID FK → gst_registrations,
    name TEXT,
    address TEXT,
    is_active BOOLEAN DEFAULT true,
    created_at, updated_at
)

-- HSN code master
hsn_codes (
    id UUID PK,
    code VARCHAR(20) UNIQUE,
    description TEXT,
    created_at
)

-- Effective-dated tax rates supporting CGST/SGST/IGST/cess
tax_rates (
    id UUID PK,
    hsn_code_id UUID FK → hsn_codes,
    gst_rate NUMERIC(5,2),       -- e.g. 12.00 for 12%
    cgst_rate NUMERIC(5,2),      -- typically gst_rate/2
    sgst_rate NUMERIC(5,2),      -- typically gst_rate/2
    igst_rate NUMERIC(5,2),      -- typically gst_rate (full)
    cess_rate NUMERIC(5,2),      -- 0 for most products
    effective_from DATE NOT NULL,
    effective_to DATE,            -- null = currently active
    created_at
)

-- Links each medicine to its applicable HSN/tax config
-- Effective-dated: multiple rows per medicine for rate changes
medicine_tax_config (
    id UUID PK,
    medicine_id UUID FK → medicines,
    hsn_code_id UUID FK → hsn_codes,
    tax_rate_id UUID FK → tax_rates,
    price_includes_tax BOOLEAN DEFAULT false,
    effective_from DATE NOT NULL,
    effective_to DATE,            -- null = currently active
    created_at
)

-- Supplier entity (replaces free-text supplier_name)
suppliers (
    id UUID PK,
    legal_name TEXT NOT NULL,
    trade_name TEXT,
    gstin VARCHAR(15),
    pan VARCHAR(10),
    address TEXT,
    state TEXT,
    state_code VARCHAR(2),
    phone TEXT,
    email TEXT,
    created_at, updated_at
)
```

### Modified Tables

```sql
-- Customers: add GST fields
ALTER TABLE customers
    ADD COLUMN gstin VARCHAR(15),
    ADD COLUMN customer_type TEXT DEFAULT 'B2C',  -- 'B2C' or 'B2B'
    ADD COLUMN billing_address TEXT,
    ADD COLUMN shipping_address TEXT,
    ADD COLUMN state TEXT,
    ADD COLUMN state_code VARCHAR(2);

-- Sales Invoices: add GST totals + snapshot
ALTER TABLE sales_invoices
    ADD COLUMN store_id UUID FK → stores,
    ADD COLUMN gst_registration_id UUID FK → gst_registrations,
    ADD COLUMN customer_gstin VARCHAR(15),
    ADD COLUMN customer_state_code VARCHAR(2),
    ADD COLUMN supply_type TEXT DEFAULT 'INTRA_STATE',  -- 'INTRA_STATE' or 'INTER_STATE'
    ADD COLUMN gross_amount NUMERIC(14,2),
    ADD COLUMN taxable_amount NUMERIC(14,2),
    ADD COLUMN cgst_total NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN sgst_total NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN igst_total NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN cess_total NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN tax_total NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN round_off NUMERIC(6,2) DEFAULT 0,
    ADD COLUMN grand_total NUMERIC(14,2),
    ADD COLUMN price_includes_tax BOOLEAN DEFAULT false;

-- Sales Invoice Items: add tax snapshot
ALTER TABLE sales_invoice_items
    ADD COLUMN hsn_code VARCHAR(20),
    ADD COLUMN gross_amount NUMERIC(14,2),
    ADD COLUMN taxable_value NUMERIC(14,2),
    ADD COLUMN gst_rate NUMERIC(5,2) DEFAULT 0,
    ADD COLUMN cgst_rate NUMERIC(5,2) DEFAULT 0,
    ADD COLUMN cgst_amount NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN sgst_rate NUMERIC(5,2) DEFAULT 0,
    ADD COLUMN sgst_amount NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN igst_rate NUMERIC(5,2) DEFAULT 0,
    ADD COLUMN igst_amount NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN cess_rate NUMERIC(5,2) DEFAULT 0,
    ADD COLUMN cess_amount NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN line_total NUMERIC(14,2);

-- Purchase Orders: add supplier FK + GST
ALTER TABLE purchase_orders
    ADD COLUMN supplier_id UUID FK → suppliers,
    ADD COLUMN supplier_gstin VARCHAR(15),
    ADD COLUMN supplier_state_code VARCHAR(2),
    ADD COLUMN store_id UUID FK → stores,
    ADD COLUMN gst_registration_id UUID FK → gst_registrations,
    ADD COLUMN supply_type TEXT DEFAULT 'INTRA_STATE',
    ADD COLUMN gross_amount NUMERIC(14,2),
    ADD COLUMN taxable_amount NUMERIC(14,2),
    ADD COLUMN cgst_total NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN sgst_total NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN igst_total NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN cess_total NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN tax_total NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN grand_total NUMERIC(14,2),
    ADD COLUMN price_includes_tax BOOLEAN DEFAULT false;

-- Purchase Order Items: add tax snapshot
ALTER TABLE purchase_order_items
    ADD COLUMN hsn_code VARCHAR(20),
    ADD COLUMN gross_amount NUMERIC(14,2),
    ADD COLUMN taxable_value NUMERIC(14,2),
    ADD COLUMN gst_rate NUMERIC(5,2) DEFAULT 0,
    ADD COLUMN cgst_rate NUMERIC(5,2) DEFAULT 0,
    ADD COLUMN cgst_amount NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN sgst_rate NUMERIC(5,2) DEFAULT 0,
    ADD COLUMN sgst_amount NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN igst_rate NUMERIC(5,2) DEFAULT 0,
    ADD COLUMN igst_amount NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN cess_rate NUMERIC(5,2) DEFAULT 0,
    ADD COLUMN cess_amount NUMERIC(14,2) DEFAULT 0,
    ADD COLUMN line_total NUMERIC(14,2);

-- Credit Notes (future returns support)
sales_credit_notes (
    id UUID PK,
    invoice_id UUID FK → sales_invoices,
    note_no BIGSERIAL UNIQUE,
    reason TEXT,
    gross_amount NUMERIC(14,2),
    taxable_amount NUMERIC(14,2),
    cgst_total NUMERIC(14,2) DEFAULT 0,
    sgst_total NUMERIC(14,2) DEFAULT 0,
    igst_total NUMERIC(14,2) DEFAULT 0,
    cess_total NUMERIC(14,2) DEFAULT 0,
    tax_total NUMERIC(14,2) DEFAULT 0,
    grand_total NUMERIC(14,2),
    created_at TIMESTAMPTZ DEFAULT now()
)

sales_credit_note_items (
    id UUID PK,
    credit_note_id UUID FK → sales_credit_notes,
    invoice_item_id UUID FK → sales_invoice_items,
    medicine_id UUID FK → medicines,
    batch_id UUID FK → batches,
    quantity INT,
    hsn_code VARCHAR(20),
    taxable_value NUMERIC(14,2),
    gst_rate NUMERIC(5,2),
    cgst_amount NUMERIC(14,2),
    sgst_amount NUMERIC(14,2),
    igst_amount NUMERIC(14,2),
    cess_amount NUMERIC(14,2),
    line_total NUMERIC(14,2)
)
```

---

## Tax Calculation Flow

```
Checkout Request
  → Validate items
  → Lock batches (existing behavior)
  → For each line:
      1. Look up medicine → medicine_tax_config → hsn_code + tax_rate
      2. Get effective tax_rate for today's date
      3. Determine supply_type from store.state_code vs customer.state_code
      4. Call tax.CalculateLine() with TaxInput{
             Quantity, UnitPrice (MRP), DiscountAmount,
             TaxRate, CGST/SGST/IGST/Cess rates,
             PriceIncludesTax, SupplyType
         }
      5. TaxResult contains: TaxableValue, CGST/SGST/IGST amounts, LineTotal
      6. Snapshot all tax values onto SalesInvoiceItem
  → Sum line totals for grand_total
  → Sum tax components for invoice-level tax totals
  → Round final grand_total with defined strategy
  → Store invoice with complete tax snapshot
  → Existing batch locking/stock decrement/credit logic unchanged
```

---

## Invoice Lifecycle

1. **Creation**: All tax information is calculated server-side and snapshotted onto invoice + items
2. **Finalization**: Invoice is committed atomically (existing behavior preserved)
3. **Immutability**: After creation, NO field on a finalized invoice may be mutated
4. **Corrections**: Use credit notes, debit notes, or cancellation flows
5. **Retrieval**: Historical invoices are served from their snapshotted data, independent of current tax config

---

## Data Migration Strategy

**Decision: Existing records remain untouched with nullable GST fields.**

- All new GST columns on existing tables are `NULL`-able with appropriate defaults
- Pre-GST invoices (before this migration) have NULL for all GST columns
- This means:
  - `grand_total` will be NULL on old invoices → use `total_amount` as fallback
  - `taxable_amount` will be NULL → not computable for old invoices
  - Tax snapshot columns will be NULL → old invoices show without tax breakdown
  - Customer GST fields will be NULL → walk-in customers are B2C by default
  - Supplier `supplier_id` will be NULL → `supplier_name` text is retained
- **No historical GST values are fabricated**
- The frontend must gracefully handle NULL GST fields (show total_amount when grand_total is null)

---

## Rounding Policy

- **Precision**: 2 decimal places (standard INR)
- **Mode**: Banker's rounding (round half to even) via `shopspring/decimal` `Round(2)`
- **When**: After each line calculation and after final invoice total
- **Line-level**: Each item's `line_total` is rounded to 2dp
- **Invoice-level**: `grand_total` is rounded to 2dp; `round_off` captures the adjustment
- **Single authority**: `internal/tax/rounding.go` is the only rounding implementation

---

## Invariants Preserved

1. ✅ Inventory-changing operations remain atomic (`pgx.BeginFunc`)
2. ✅ Batch rows locked with `FOR UPDATE` (UUID sort order)
3. ✅ Stock never becomes negative (`WHERE current_stock >= $2`)
4. ✅ Duplicate batch lines merged with conflict detection
5. ✅ FEFO behavior unchanged (batch picker sorts by expiry)
6. ✅ Credit limit enforcement uses `grand_total` (post-tax)
7. ✅ Customer ledger records `grand_total` for credit sales
8. ✅ Existing purchase flows continue working for non-GST records
9. ✅ No ORM introduced
10. ✅ No float64 for new money calculations (decimal used in tax engine)
11. ✅ Server is source of truth for all tax calculations
12. ✅ Historical invoice tax information is never overwritten
