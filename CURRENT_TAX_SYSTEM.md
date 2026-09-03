# PharmaPOS — Complete Tax System Documentation

## 1. Overview

PharmaPOS is a pharmacy POS system with Indian GST tax support. Tax is computed
automatically at the point of sale (checkout) and at purchase inward (supplier
invoice) based on each medicine's HSN code and tax rate configuration. The system
supports both **intra-state** (CGST + SGST) and **inter-state** (IGST) supplies,
and both **tax-inclusive** (MRP includes tax) and **tax-exclusive** pricing.

---

## 2. Database Schema — Tax Configuration Tables

### 2.1 `hsn_codes` — HSN Code Master

```sql
CREATE TABLE hsn_codes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code        VARCHAR(20) NOT NULL UNIQUE,   -- e.g. '3004', '2106'
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**Seeded data:**

| Code  | Description | Typical GST |
|-------|------------|-------------|
| 3004  | Medicaments packed for retail sale | 12% |
| 3003  | Medicaments not packed for retail sale | 12% |
| 3002  | Blood, vaccines, toxins, cultures | 12% |
| 3001  | Glands and other organs | 0% |
| 2106  | Food preparations (nutraceuticals, supplements) | 5% |
| 9983  | Other support services (pharmacy service charges) | 18% |

### 2.2 `tax_rates` — Effective-Dated Tax Rates per HSN

```sql
CREATE TABLE tax_rates (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hsn_code_id    UUID NOT NULL REFERENCES hsn_codes(id) ON DELETE CASCADE,
    gst_rate       NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    cgst_rate      NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    sgst_rate      NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    igst_rate      NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    cess_rate      NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    effective_from DATE NOT NULL,
    effective_to   DATE,              -- NULL = currently active
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Only one active (non-expired) tax rate per HSN at a time
CREATE UNIQUE INDEX uq_tax_rates_active_per_hsn ON tax_rates (hsn_code_id)
    WHERE effective_to IS NULL;
```

**Key rules:**
- Each HSN code has at most one **active** tax rate (effective_to IS NULL)
- When rates change, the old rate is "ended" (effective_to = today) and a new
  one is created with effective_from = today
- `gst_rate` is the **total GST percentage** (e.g. 12.00 for 12%)
- `cgst_rate` and `sgst_rate` are each half of gst_rate for intra-state
- `igst_rate` equals gst_rate for inter-state
- `cess_rate` is additional cess (rarely used for medicines)

**Seeded data (HSN 3004 — 12% GST):**

```
gst_rate:  12.00
cgst_rate:  6.00
sgst_rate:  6.00
igst_rate: 12.00
cess_rate:  0.00
```

### 2.3 `medicine_tax_config` — Links Medicine → HSN → Tax Rate

```sql
CREATE TABLE medicine_tax_config (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    medicine_id       UUID NOT NULL REFERENCES medicines(id) ON DELETE CASCADE,
    hsn_code_id       UUID NOT NULL REFERENCES hsn_codes(id) ON DELETE CASCADE,
    tax_rate_id       UUID NOT NULL REFERENCES tax_rates(id) ON DELETE CASCADE,
    price_includes_tax BOOLEAN NOT NULL DEFAULT false,
    effective_from    DATE NOT NULL,
    effective_to      DATE,              -- NULL = currently active
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Only one active (non-expired) tax config per medicine at a time
CREATE UNIQUE INDEX uq_medicine_tax_config_active ON medicine_tax_config (medicine_id)
    WHERE effective_to IS NULL;
```

**Key fields:**
- `price_includes_tax`: **TRUE** means the medicine's MRP/sale price already
  includes GST (tax-inclusive). **FALSE** means tax is added on top (tax-exclusive).
  In Indian pharmacy retail, MRP is **always tax-inclusive** (MRP printed on
  strip includes all taxes).
- When a medicine has **no** entry in this table, it is treated as a "legacy"
  medicine with **zero tax** (no GST computed).

---

## 3. Tax Engine — Core Logic

All tax calculation lives in `internal/tax/` (4 files):

### 3.1 Input Structure (`types.go`)

```go
type TaxInput struct {
    Quantity         decimal.Decimal
    UnitPrice        decimal.Decimal      // MRP / selling price per unit
    DiscountAmount   decimal.Decimal      // absolute ₹ discount on the line
    TaxRate          TaxRate              // GST rates from medicine_tax_config
    PriceIncludesTax bool                 // true = MRP includes tax
    SupplyType       SupplyType           // INTRA_STATE or INTER_STATE
    HSNCode          string
}

type TaxRate struct {
    GSTRate  decimal.Decimal   // total GST % (e.g. 12.00)
    CGSTRate decimal.Decimal   // CGST component % (e.g. 6.00)
    SGSTRate decimal.Decimal   // SGST component % (e.g. 6.00)
    IGSTRate decimal.Decimal   // IGST component % (e.g. 12.00)
    CessRate decimal.Decimal   // Cess % (e.g. 0.00)
}

type SupplyType int
const (
    SupplyTypeIntraState SupplyType = 0   // same state → CGST + SGST
    SupplyTypeInterState SupplyType = 1   // different state → IGST
)
```

### 3.2 Line-Level Tax Calculation (`calculator.go`)

```go
func CalculateLineTax(in TaxInput) TaxLineResult
```

**For TAX-INCLUSIVE pricing** (`price_includes_tax = true`, typical for MRP):

```
gross      = quantity × unit_price
net        = gross - discount
divisor    = 1 + (gst_rate / 100) + (cess_rate / 100)
taxable    = net / divisor
tax        = taxable × gst_rate / 100
cess       = taxable × cess_rate / 100
line_total = net                    (tax already embedded in net)
```

**For TAX-EXCLUSIVE pricing** (`price_includes_tax = false`):

```
gross      = quantity × unit_price
net        = gross - discount
taxable    = net
tax        = taxable × gst_rate / 100
cess       = taxable × cess_rate / 100
line_total = net + tax + cess       (tax added on top)
```

**Tax component split:**
- Intra-state: `CGST = tax / 2`, `SGST = tax - CGST`, `IGST = 0`
- Inter-state: `IGST = tax`, `CGST = 0`, `SGST = 0`

**Output structure:**

```go
type TaxLineResult struct {
    GrossAmount  decimal.Decimal   // quantity × unit_price
    Discount     decimal.Decimal   // ₹ discount applied
    TaxableValue decimal.Decimal   // base amount for tax
    CGSTRate     decimal.Decimal   // CGST %
    CGSTAmount   decimal.Decimal   // ₹ CGST
    SGSTRate     decimal.Decimal   // SGST %
    SGSTAmount   decimal.Decimal   // ₹ SGST
    IGSTRate     decimal.Decimal   // IGST %
    IGSTAmount   decimal.Decimal   // ₹ IGST
    CessRate     decimal.Decimal   // Cess %
    CessAmount   decimal.Decimal   // ₹ Cess
    TaxAmount    decimal.Decimal   // total tax (CGST+SGST or IGST + Cess)
    LineTotal    decimal.Decimal   // what customer pays for this line
    HSNCode      string
}
```

### 3.3 Invoice-Level Aggregation (`calculator.go`)

```go
func CalculateInvoiceTax(lines []TaxLineResult, supplyType SupplyType) TaxInvoiceResult
```

Sums all line results into invoice-level totals:

```
gross_amount   = Σ line.GrossAmount
discount_total = Σ line.Discount
taxable_amount = Σ line.TaxableValue
cgst_total     = Σ line.CGSTAmount
sgst_total     = Σ line.SGSTAmount
igst_total     = Σ line.IGSTAmount
cess_total     = Σ line.CessAmount
tax_total      = Σ line.TaxAmount
grand_total    = taxable + cgst + sgst + igst + cess  (rounded to 2dp)
round_off      = grand_total - sum_of_components       (typically ±0.01)
```

### 3.4 Supply Type Determination (`rules.go`)

```go
func DetermineSupplyType(sellerStateCode, placeOfSupplyStateCode string) SupplyType
```

- If both state codes are the same → `INTRA_STATE` (CGST + SGST)
- If different → `INTER_STATE` (IGST)
- If either code is missing → defaults to `INTRA_STATE`

### 3.5 Rounding (`rounding.go`)

- All monetary values: `Round(2)` (banker's rounding)
- Tax rates: `Round(2)`
- Quantities: `Round(0)` (whole units)

---

## 4. How Tax Flows Through the System

### 4.1 Sale (Checkout) Flow

```
Customer buys medicine → POS checkout → tax computed per line → stored on invoice
```

**Step-by-step:**

1. **Lock batches** — SELECT FOR UPDATE on batch rows to prevent oversell
2. **Determine supply type** — compare store's state code vs customer's state code
3. **For each line item:**
   a. Get `sale_price` from the locked batch
   b. Compute gross = quantity × sale_price
   c. Compute discount (line-level)
   d. **Look up tax config** for the medicine via `GetMedicineTaxConfig(medicine_id, NOW())`
   e. If tax config exists:
      - Build `TaxInput` with: quantity, unit_price, discount, GST rates,
        price_includes_tax, supply_type, HSN code
      - Call `CalculateLineTax()` → get `TaxLineResult`
      - Store tax snapshot on the invoice item (HSN, rates, amounts)
   f. If no tax config:
      - Use `ZeroTaxResult()` → zero tax
4. **Aggregate** all line results via `CalculateInvoiceTax()` → invoice-level totals
5. **Compute chargeable total** = grand_total from tax engine
6. **Credit check** — if credit sale, verify customer's balance + new total ≤ credit_limit
7. **INSERT** sales_invoices with all GST columns (supply_type, gross_amount,
   taxable_amount, cgst_total, sgst_total, igst_total, cess_total, tax_total,
   round_off, grand_total, price_includes_tax)
8. **INSERT** sales_invoice_items with per-line tax snapshots (hsn_code, gross_amount,
   taxable_value, gst_rate, cgst_rate/amount, sgst_rate/amount, igst_rate/amount,
   cess_rate/amount, line_total)

### 4.2 Purchase (Inward) Flow

```
Supplier delivers medicine → Purchase inward → tax computed per line → stored on PO
```

**Step-by-step:**

1. **Determine supply type** — compare store's state code vs supplier's state code
2. **For each line item:**
   a. Compute gross = quantity × purchase_price
   b. Compute discount (line-level)
   c. Compute **blended cost** = (gross - discount) / (quantity + bonus_quantity)
   d. **Look up tax config** for the medicine
   e. If tax config exists:
      - Build `TaxInput` with: quantity, unit_price (purchase_price), discount,
        GST rates, price_includes_tax, supply_type, HSN code
      - Call `CalculateLineTax()` → get `TaxLineResult`
      - Store tax snapshot on the PO item
   f. If no tax config → zero tax
3. **Aggregate** via `CalculateInvoiceTax()`
4. **Begin transaction:**
   a. If new medicine (no medicine_id), INSERT into medicines table
   b. If new medicine has `hsn_code` in input:
      - Find or create HSN code record
      - Find active tax rate for that HSN
      - Create `medicine_tax_config` linking the medicine to HSN + tax rate
   c. UPSERT batch (ON CONFLICT merge stock)
   d. INSERT purchase_order with GST columns
   e. INSERT purchase_order_items with tax snapshots

### 4.3 Tax Lookup Chain

```
medicine_id
    → medicine_tax_config (WHERE medicine_id = ? AND effective_to IS NULL)
        → hsn_codes (JOIN on hsn_code_id)
        → tax_rates (JOIN on tax_rate_id)
            → gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate
```

If any step returns NULL → zero tax (graceful degradation for legacy medicines).

---

## 5. Your Example — Full Walkthrough

**Scenario:**
- Medicine X, HSN 3004 (12% GST, intra-state)
- Buy 10 units at ₹100/unit, get 2 bonus free
- MRP ₹200/unit, price-inclusive (MRP includes tax)
- Sell all 12 units to a customer in the same state

### 5.1 Purchase Inward

```
Input:
  medicine_name:  "Medicine X"
  hsn_code:       "3004"
  batch:          "BATCH-001"
  quantity:       10
  bonus_quantity: 2
  purchase_price: 100.00     (per paid unit)
  sale_price:     200.00     (MRP)
  price_includes_tax: true

Step 1: Determine supply type
  store state: "27" (Maharashtra)
  supplier state: "27" (Maharashtra)
  → INTRA_STATE

Step 2: Tax config lookup
  medicine_tax_config → hsn_codes (3004) → tax_rates (12%)
  gst_rate: 12.00, cgst: 6.00, sgst: 6.00, igst: 12.00, cess: 0.00

Step 3: Line tax calculation (for 10 paid units at ₹100)
  gross = 10 × 100 = 1000.00
  discount = 0
  net = 1000.00

  TAX-INCLUSIVE (price_includes_tax = true):
    divisor = 1 + 12/100 + 0/100 = 1.12
    taxable = 1000.00 / 1.12 = 892.86
    tax = 892.86 × 12/100 = 107.14
    cgst = 107.14 / 2 = 53.57
    sgst = 107.14 - 53.57 = 53.57
    line_total = 1000.00  (tax already in MRP)

Step 4: Batch storage
  effective_price = 1000.00 / 12 = 83.33  (blended cost)
  current_stock = 12

Step 5: Invoice storage
  total_amount = 1000.00
  gross_amount = 1000.00
  taxable_amount = 892.86
  cgst_total = 53.57
  sgst_total = 53.57
  tax_total = 107.14
  grand_total = 1000.00
  supply_type = "INTRA_STATE"
  price_includes_tax = true
```

### 5.2 Sale (Checkout)

```
Input:
  batch_id: "BATCH-001"
  quantity: 12 (all stock)

Step 1: Lock batch, verify stock = 12 ✓

Step 2: Supply type
  store state: "27", customer state: "27"
  → INTRA_STATE

Step 3: Line tax (for 12 units at MRP ₹200)
  sale_price = 200.00 (from batch)
  gross = 12 × 200 = 2400.00
  discount = 0
  net = 2400.00

  TAX-INCLUSIVE:
    divisor = 1.12
    taxable = 2400.00 / 1.12 = 2142.86
    tax = 2142.86 × 12/100 = 257.14
    cgst = 257.14 / 2 = 128.57
    sgst = 128.57
    line_total = 2400.00

Step 4: Invoice
  total_amount = 2400.00
  taxable_amount = 2142.86
  cgst_total = 128.57
  sgst_total = 128.57
  tax_total = 257.14
  grand_total = 2400.00
  round_off = 0.00
```

---

## 6. What the UI Currently Shows

### 6.1 Purchases Page — "New medicine" Form

Fields when adding a new medicine during purchase:
- Brand name, Salt composition, Manufacturer, Packing
- Min reorder level
- **HSN code** (text input, e.g. "3004")
- **MRP includes tax** (checkbox, default: checked)
- Batch number, Expiry date
- Quantity, Free (bonus), Buy ₹ (purchase price), MRP ₹
- Discount (% or ₹)

When staged, the line shows: `Batch BATCH-001 · exp 2027-08-26 · 10+2 free × ₹100.00 · HSN 3004`

### 6.2 Medicines Page — Detail Panel

When viewing a medicine's detail:
- **Tax Configuration** section shows:
  - HSN code, GST rate, CGST/SGST/IGST rates, price-includes-tax status
  - "Edit" button to change the tax config (select HSN, set rates, toggle price-includes-tax)

### 6.3 Sales Invoice Detail Modal

Shows:
- Supply type (Intra-state / Inter-state)
- Per-line: HSN code, GST rate %, discount, amount
- Invoice totals: Gross amount, Discount, Taxable value, CGST, SGST, IGST, Tax total, Grand total

### 6.4 Purchase Invoice Detail Modal

Shows:
- Supplier GSTIN, Supply type
- Per-line: HSN, Quantity, Bonus, Buy ₹, MRP ₹, Tax %, Discount, Amount
- Invoice totals: PO Discount, Taxable value, CGST, SGST, IGST, Tax total, Grand total

### 6.5 POS Receipt (after checkout)

Shows full GST breakdown: Supply type, Gross, Discount, Taxable, CGST/SGST or IGST, Tax total, Grand total.

---

## 7. Key Design Decisions

1. **Tax is computed at transaction time, not stored on the medicine.** The
   medicine_tax_config links to rates, but the actual ₹ amounts are computed
   and frozen at checkout/inward time. Historical invoices are immutable.

2. **Bonus quantity is never taxed.** Only paid quantity × unit_price forms
   the tax base. The bonus units are free additions to stock.

3. **MRP is tax-inclusive in Indian pharmacy.** The `price_includes_tax = true`
   path extracts tax from the MRP: `taxable = MRP / (1 + gst/100)`.

4. **Blended cost for batch purchase_price.** When bonus is present,
   `batch.purchase_price = total_paid / (quantity + bonus)`. This gives the
   true per-unit cost including free units.

5. **Graceful degradation.** If a medicine has no tax_config entry, tax = 0.
   The system doesn't block sales/purchases for unconfigured medicines.

6. **Effective dating.** Tax rates and medicine_tax_config use effective_from/to
   dates so rate changes are tracked historically. Only one active config
   per medicine/HSN at a time.

7. **Supply type is determined dynamically** from store state vs customer/supplier
   state. It is NOT stored on the medicine — it depends on who is buying.

---

## 8. API Endpoints (Tax-Related)

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/hsn` | List all HSN codes |
| POST | `/api/hsn` | Create new HSN code |
| PUT | `/api/hsn/:id/tax-rate` | Upsert active tax rate for an HSN |
| GET | `/api/medicines/:id/detail` | Get medicine detail (includes tax_config) |
| GET | `/api/medicines/:id/tax-config` | Get active tax config for a medicine |
| PUT | `/api/medicines/:id/tax-config` | Assign/update tax config for a medicine |
| POST | `/api/sales/checkout` | Sale with automatic tax computation |
| POST | `/api/purchases` | Purchase inward with automatic tax computation |
| GET | `/api/sales/invoices/:id` | View invoice with full tax breakdown |
| GET | `/api/purchases/invoices/:id` | View purchase invoice with tax breakdown |

---

## 9. Source Code Locations

| File | Purpose |
|------|---------|
| `internal/tax/types.go` | TaxInput, TaxRate, TaxLineResult, TaxInvoiceResult, SupplyType |
| `internal/tax/calculator.go` | CalculateLineTax, CalculateInvoiceTax, ZeroTaxInput/Result |
| `internal/tax/rules.go` | DetermineSupplyType, SplitTaxComponents |
| `internal/tax/rounding.go` | RoundMoney, RoundQuantity, RoundRate |
| `internal/repository/tax_repo.go` | GetMedicineTaxConfig, GetHSNByCode, GetActiveTaxRate, ListHSNCodes, CreateHSNCode, UpsertTaxRate, UpsertMedicineTaxConfig |
| `internal/repository/sale_repo.go` | Checkout — tax lookup + computation per line |
| `internal/repository/purchase_repo.go` | CreateInward — tax lookup + computation, auto-assign HSN for new medicines |
| `internal/repository/invoice_repo.go` | Invoice queries — SELECT all GST columns |
| `internal/handlers/tax.go` | HSN/tax CRUD endpoints |
| `internal/handlers/sales.go` | Checkout + purchase create handlers |
| `internal/handlers/invoices.go` | Invoice detail handlers |
| `migrations/012_create_hsn_codes.sql` | HSN codes table |
| `migrations/013_create_tax_rates.sql` | Tax rates table |
| `migrations/014_create_medicine_tax_config.sql` | Medicine-tax link table |
| `migrations/016_alter_sales_invoices_gst.sql` | GST columns on sales_invoices |
| `migrations/017_alter_sales_invoice_items_gst.sql` | GST columns on sales_invoice_items |
| `migrations/019_alter_purchase_orders_gst.sql` | GST columns on purchase_orders |
| `migrations/020_alter_purchase_order_items_gst.sql` | GST columns on purchase_order_items |
| `migrations/021_seed_hsn_tax_rates.sql` | Default HSN codes + tax rates |
| `web/src/pages/Purchases.tsx` | Purchase form with HSN code input |
| `web/src/pages/Medicines.tsx` | Medicine detail with tax config display/edit |
| `web/src/pages/Invoices.tsx` | Invoice detail modals with tax breakdown |
| `web/src/pages/POS.tsx` | POS receipt with GST summary |
| `web/src/lib/api.ts` | Frontend API methods for HSN/tax CRUD |
| `web/src/types.ts` | HSNCode, TaxRate, MedicineTaxConfig types |

---

## 10. Current Limitations / Gaps

1. **No standalone HSN/tax management page.** HSN codes and tax rates can only
   be managed through the medicine detail panel or purchase form. There is no
   dedicated admin page for managing all HSN codes and their rates.

2. **No bulk tax config assignment.** You must assign tax config to each medicine
   individually. There's no way to assign a default HSN to all medicines of a
   certain type.

3. **Tax config is required before GST appears.** If you create a medicine and
   sell it before assigning a tax config, the invoice will show zero tax. You
   can't retroactively add tax to past invoices.

4. **No GST return filing integration.** The system stores all GST data needed
   for GSTR-1/GSTR-3B filing, but doesn't generate GST return files.

5. **No HSN-wise summary report.** There's no report showing total sales/purchases
   grouped by HSN code with tax breakup (needed for GSTR-1 Table 12).

6. **Purchase price is entered as tax-inclusive or exclusive depending on
   `price_includes_tax`.** The system doesn't auto-detect this from the supplier
   bill. You must configure it correctly per medicine.

7. **No reverse charge mechanism.** For purchases under reverse charge (RCM),
   the tax is paid by the buyer to the government. This is not implemented.

8. **No e-way bill integration.** For consignments above ₹50,000, e-way bills
   are required. Not implemented.

---

## 11. Questions for Gemini Research

When researching Indian GST for pharmacy POS, consider these questions:

1. **HSN code assignment:** What HSN codes are commonly used for different
   medicine categories? Are there state-specific HSN requirements?

2. **GST rates:** What are the current (2024-2026) GST rates for:
   - Ayurvedic medicines
   - Allopathic medicines
   - Surgical items
   - Nutraceuticals / supplements
   - Cosmetics sold in pharmacies

3. **Tax-inclusive vs exclusive:** Is MRP always tax-inclusive for medicines
   in India? Are there cases where purchase price from supplier is tax-exclusive?

4. **Input Tax Credit (ITC):** How does a pharmacy claim ITC on purchases?
   Does the system need to track ITC separately?

5. **GST filing data:** What data does GSTR-1 require? Does the current
   schema capture everything needed?

6. **Composition scheme:** Should small pharmacies use composition scheme
   (1% tax, no ITC)? How does this affect the tax engine?

7. **Cess:** Are there medicines subject to GST compensation cess? When is
   cess applicable?

8. **Inter-state vs intra-state for online orders:** How should supply type
   be determined for home delivery / online orders?

9. **Discount handling:** How should trade discounts, cash discounts, and
   volume discounts be treated for GST purposes?

10. **Returns and credit notes:** How should sales returns affect GST? Is
    the current credit note implementation GST-compliant?
