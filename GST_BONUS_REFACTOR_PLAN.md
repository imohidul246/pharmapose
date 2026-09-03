# GST & Bonus Quantity Refactor Plan

**Date:** 2026-08-26
**Status:** PLAN — no implementation started
**Source:** Synthesis of 5 audit documents (GST_AUDIT, BONUS_QUANTITY_AUDIT, DATABASE_GST_BONUS_AUDIT, GST_BONUS_TEST_PLAN, API_FRONTEND_GST_BONUS_AUDIT)

---

## 0. Agreed Business Semantics

| Concept | Agreed Definition | Rationale |
|---------|-------------------|-----------|
| `batch.purchase_price` | **True blended cost** = `(gross - discount) / (quantity + bonus_quantity)` | Standard pharmaceutical weighted-average costing. All downstream consumers (P&L, stock value, reconciliation) inherit correct values. |
| Tax base | Only `quantity` (paid) is taxed. `bonus_quantity` is never taxed. | GST law: free goods from supplier are not a taxable supply. Current behavior is correct. |
| `total_amount` | Sum of pre-tax line nets (paid qty × unit price − line discount − PO discount). Excludes GST. | Represents the pharmacy's actual payment obligation to supplier. |
| `grand_total` | `taxable_amount + CGST + SGST + IGST + cess + round_off`. For tax-inclusive invoices: equals `total_amount` (tax embedded). | Customer/invoice total. |
| `price_includes_tax` | Mirrors the medicine-level `PriceIncludesTax` flag from `medicine_tax_config`. NOT `invoiceResult != nil`. | Correct metadata for the invoice. |
| `round_off` | `grand_total − (taxable_amount + tax_total + cess_total)`. Computed at invoice level. | GST invoicing rule: must record rounding difference. |
| Rounding | All monetary math uses `shopspring/decimal` (`RoundMoney`). No `float64` `math.Round`. | Eliminates 1-paise drift between dual systems. |
| Historical snapshots | Tax amounts stored on invoice/items at checkout time are immutable. Future tax config changes do not alter past invoices. | GST compliance: invoices cannot be retrospectively modified. |

---

## 1. Inventory Cost Fix (Bonus Quantity)

**Root cause:** `purchase_repo.go:139-141` divides by `quantity` (paid) instead of `quantity + bonus_quantity` (received).

### 1.1 Change `effectivePrice` calculation

**File:** `internal/repository/purchase_repo.go:138-142`

```go
// BEFORE:
effectivePrice := it.PurchasePrice
if it.Quantity > 0 && discAmount > 0 {
    effectivePrice = round2((gross - discAmount) / float64(it.Quantity))
}

// AFTER:
totalReceived := it.Quantity + it.BonusQuantity
effectivePrice := it.PurchasePrice
if totalReceived > 0 {
    if discAmount > 0 {
        effectivePrice = round2((gross - discAmount) / float64(totalReceived))
    } else if it.BonusQuantity > 0 {
        effectivePrice = round2(gross / float64(totalReceived))
    }
}
```

**Downstream effects (all automatically fixed by this single change):**

| Component | File:Line | Before | After |
|-----------|-----------|--------|-------|
| P&L COGS | `report_repo.go:194` | Overstated (uses `batch.purchase_price`) | Correct (blended cost) |
| Expiry stock value | `report_repo.go:253` | Overstated | Correct |
| Reconciliation cost impact | `reconcile_repo.go:126` | Overstated | Correct |
| Batch merge price on re-inward | `purchase_repo.go:274` | Overwrites with paid-only price | Overwrites with blended price (correct) |

### 1.2 Fix `medicine_repo.go:255` — purchase stats total spend

**File:** `internal/repository/medicine_repo.go:253-255`

```sql
-- BEFORE (WRONG — includes bonus at purchase price):
COALESCE(SUM((poi.quantity + poi.bonus_quantity) * poi.purchase_price), 0)::float8

-- AFTER (actual spend = sum of paid quantity × purchase price minus discount):
COALESCE(SUM(poi.quantity * poi.purchase_price - poi.discount_amount), 0)::float8
```

This computes the actual money paid, regardless of how `purchase_price` is stored.

---

## 2. Tax Engine Fixes

### 2.1 Add `SupplyType` parameter to `CalculateInvoiceTax`

**File:** `internal/tax/calculator.go:113`

```go
// BEFORE:
func CalculateInvoiceTax(lines []TaxLineResult) TaxInvoiceResult {

// AFTER:
func CalculateInvoiceTax(lines []TaxLineResult, supplyType SupplyType) TaxInvoiceResult {
    result := TaxInvoiceResult{
        Lines:      lines,
        SupplyType: supplyType,
    }
    // ... aggregation unchanged ...
    // REMOVE lines 137-144 (IGST-rate inference)
```

**Callers to update:**
- `sale_repo.go:338` → `tax.CalculateInvoiceTax(taxLines, supplyType)`
- `sale_repo.go:369` → `tax.CalculateInvoiceTax(taxLines, supplyType)`
- `purchase_repo.go:190` → `tax.CalculateInvoiceTax(taxLines, supplyType)`
- `calculator_test.go:272` → update test calls

### 2.2 Fix cess divisor for tax-inclusive decomposition

**File:** `internal/tax/calculator.go:44-51`

```go
// BEFORE:
if in.PriceIncludesTax {
    divisor := one.Add(in.TaxRate.GSTRate.Div(oneHundred))
    taxableValue = net.Div(divisor)
    taxableValue = RoundMoney(taxableValue)
    taxAmount = net.Sub(taxableValue)
    taxAmount = RoundMoney(taxAmount)
}

// AFTER:
if in.PriceIncludesTax {
    // Include cess in the divisor so MRP = taxable + GST + cess
    divisor := one.
        Add(in.TaxRate.GSTRate.Div(oneHundred)).
        Add(in.TaxRate.CessRate.Div(oneHundred))
    taxableValue = net.Div(divisor)
    taxableValue = RoundMoney(taxableValue)
    taxAmount = taxableValue.Mul(in.TaxRate.GSTRate).Div(oneHundred)
    taxAmount = RoundMoney(taxAmount)
    cessAmount = taxableValue.Mul(in.TaxRate.CessRate).Div(oneHundred)
    cessAmount = RoundMoney(cessAmount)
    // lineTotal = net (unchanged — MRP already includes everything)
}
```

Also update the cess computation block (lines 76-77) to only run in the tax-exclusive branch (since tax-inclusive cess is now computed inline):

```go
// AFTER the if/else:
if !in.PriceIncludesTax {
    cessAmount = taxableValue.Mul(in.TaxRate.CessRate).Div(oneHundred)
    cessAmount = RoundMoney(cessAmount)
}
```

### 2.3 Compute `round_off` in `CalculateInvoiceTax`

**File:** `internal/tax/calculator.go` (after grand total computation)

```go
// After computing GrandTotal:
sumComponents := result.TaxableAmount.
    Add(result.CGSTTotal).
    Add(result.SGSTTotal).
    Add(result.IGSTTotal).
    Add(result.CessTotal)
result.RoundOff = result.GrandTotal.Sub(sumComponents)
result.RoundOff = RoundMoney(result.RoundOff)
```

### 2.4 Compute `CalculateInvoiceTax` once in sale checkout

**File:** `internal/repository/sale_repo.go:335-371`

```go
// BEFORE: calculated at line 338 for credit check, then again at line 369 for INSERT

// AFTER: single calculation, used for both
var invoiceResult *tax.TaxInvoiceResult
chargeableTotal := total
if len(taxLines) > 0 && hasTaxConfig {
    r := tax.CalculateInvoiceTax(taxLines, supplyType)
    invoiceResult = &r
    chargeableTotal, _ = invoiceResult.GrandTotal.Float64()
}
// credit check uses chargeableTotal (unchanged)
// INSERT uses invoiceResult (unchanged)
```

---

## 3. Invoice Metadata Fixes

### 3.1 Fix `PriceIncludesTax` to reflect actual config

**File:** `internal/repository/sale_repo.go`

```go
// BEFORE (line 418):
inv.PriceIncludesTax = boolPtr(invoiceResult != nil)

// AFTER: track the actual config value from the first tax-configured item
var priceIncludesTax bool
// Inside the item loop, when taxConfig is found:
if taxConfig != nil && taxConfig.TaxRate != nil {
    priceIncludesTax = taxConfig.PriceIncludesTax  // capture actual value
    hasTaxConfig = true
    // ...
}
// In INSERT:
inv.PriceIncludesTax = &priceIncludesTax
```

Same fix in `purchase_repo.go` (capture `taxConfig.PriceIncludesTax` from first item).

### 3.2 Compute `round_off` for INSERT

**File:** `internal/repository/sale_repo.go:390`

```go
// BEFORE:
0.0, chargeableTotal, invoiceResult != nil,

// AFTER:
derefFloatPtr(invoiceResult, "RoundOff"), chargeableTotal, priceIncludesTax,
```

Same in `purchase_repo.go:257`.

---

## 4. Propagate Tax Config Lookup Errors

### 4.1 `sale_repo.go:274`

```go
// BEFORE:
taxConfig, _ := r.taxRepo.GetMedicineTaxConfig(ctx, lb.medicineID, time.Now())

// AFTER:
taxConfig, err := r.taxRepo.GetMedicineTaxConfig(ctx, lb.medicineID, time.Now())
if err != nil {
    return fmt.Errorf("lookup tax config for medicine %s: %w", lb.medicineID, err)
}
```

### 4.2 `purchase_repo.go:154`

```go
// BEFORE:
taxConfig, _ := r.taxRepo.GetMedicineTaxConfig(ctx, it.MedicineID, time.Now())

// AFTER:
taxConfig, err := r.taxRepo.GetMedicineTaxConfig(ctx, it.MedicineID, time.Now())
if err != nil {
    return fmt.Errorf("lookup tax config for medicine %s: %w", it.MedicineID, err)
}
```

---

## 5. Unify Rounding to Decimal

### 5.1 Remove `round2()` from repos

**Files:** `sale_repo.go:55-57`, `purchase_repo.go` (if present)

Replace all `round2()` calls with `tax.RoundMoney(decimal.NewFromFloat(...))`:

```go
// sale_repo.go line discount computation:
// BEFORE:
net := round2(gross - discAmount)
total = round2(total + net)

// AFTER:
net := tax.RoundMoney(decimal.NewFromFloat(gross).Sub(decimal.NewFromFloat(discAmount)))
totalDecimal = totalDecimal.Add(net)
```

This requires accumulating `total` and `discountTotal` as `decimal.Decimal` throughout the checkout/purchase flow, then converting to `float64` at the INSERT point.

**Scope:** Only the `total` and `discountTotal` accumulation loops in `sale_repo.go` and `purchase_repo.go`. The `lineDiscount()` helper can remain float64 since its output feeds into the decimal path.

---

## 6. Fix Invoice Detail/List Queries (Critical — GST fields missing)

### 6.1 Sales invoice list query

**File:** `internal/repository/invoice_repo.go:50-63`

Add to SELECT:
```sql
si.supply_type,
si.grand_total::float8,
si.tax_total::float8
```

Add to Scan:
```go
&row.SupplyType, &row.GrandTotal, &row.TaxTotal
```

### 6.2 Sales invoice detail query

**File:** `internal/repository/invoice_repo.go:93-102`

Add to SELECT:
```sql
si.supply_type,
si.gross_amount::float8, si.taxable_amount::float8,
si.cgst_total::float8, si.sgst_total::float8, si.igst_total::float8,
si.cess_total::float8, si.tax_total::float8, si.round_off::float8,
si.grand_total::float8, si.price_includes_tax
```

Add to Scan (populate all `d.Invoice.*` fields).

### 6.3 Sales invoice items query

**File:** `internal/repository/invoice_repo.go:112-121`

Add to SELECT:
```sql
sii.hsn_code,
sii.gross_amount::float8, sii.taxable_value::float8, sii.gst_rate::float8,
sii.cgst_rate::float8, sii.cgst_amount::float8,
sii.sgst_rate::float8, sii.sgst_amount::float8,
sii.igst_rate::float8, sii.igst_amount::float8,
sii.cess_rate::float8, sii.cess_amount::float8, sii.line_total::float8
```

### 6.4 Purchase invoice list query

**File:** `internal/repository/invoice_repo.go:144-155`

Add to SELECT:
```sql
po.supply_type,
po.tax_total::float8,
po.grand_total::float8
```

### 6.5 Purchase invoice detail query

**File:** `internal/repository/invoice_repo.go:176-180`

Add to SELECT:
```sql
po.supplier_id::text, po.supplier_gstin, po.supplier_state_code,
po.store_id::text, po.supply_type,
po.gross_amount::float8, po.taxable_amount::float8,
po.cgst_total::float8, po.sgst_total::float8, po.igst_total::float8,
po.cess_total::float8, po.tax_total::float8, po.grand_total::float8,
po.price_includes_tax
```

### 6.6 Purchase invoice items query

**File:** `internal/repository/invoice_repo.go:188-197`

Add to SELECT:
```sql
poi.hsn_code,
poi.gross_amount::float8, poi.taxable_value::float8, poi.gst_rate::float8,
poi.cgst_rate::float8, poi.cgst_amount::float8,
poi.sgst_rate::float8, poi.sgst_amount::float8,
poi.igst_rate::float8, poi.igst_amount::float8,
poi.cess_rate::float8, poi.cess_amount::float8, poi.line_total::float8
```

---

## 7. Database Migrations

### 7.1 Migration `022_add_constraints.sql`

```sql
-- Bonus quantity non-negative (DATABASE_AUDIT F-06)
ALTER TABLE purchase_order_items
    ADD CONSTRAINT chk_poi_bonus_quantity CHECK (bonus_quantity >= 0);

-- Purchase order item prices non-negative (F-07)
ALTER TABLE purchase_order_items
    ADD CONSTRAINT chk_poi_purchase_price CHECK (purchase_price >= 0),
    ADD CONSTRAINT chk_poi_sale_price CHECK (sale_price >= 0);

-- Sales invoice item non-negative values (F-08)
ALTER TABLE sales_invoice_items
    ADD CONSTRAINT chk_sii_unit_sale_price CHECK (unit_sale_price >= 0),
    ADD CONSTRAINT chk_sii_subtotal CHECK (subtotal >= 0);

-- Tax rate bounds (F-09)
ALTER TABLE tax_rates
    ADD CONSTRAINT chk_tr_gst_rate CHECK (gst_rate >= 0 AND gst_rate <= 100),
    ADD CONSTRAINT chk_tr_cess_rate CHECK (cess_rate >= 0 AND cess_rate <= 100);

-- Effective date ordering (F-10)
ALTER TABLE tax_rates
    ADD CONSTRAINT chk_tr_effective CHECK (effective_to IS NULL OR effective_to > effective_from);
ALTER TABLE medicine_tax_config
    ADD CONSTRAINT chk_mtc_effective CHECK (effective_to IS NULL OR effective_to > effective_from);

-- Customer type enum (F-11)
ALTER TABLE customers
    ADD CONSTRAINT chk_customer_type CHECK (customer_type IN ('B2C', 'B2B'));

-- Medicine reorder level non-negative (F-12)
ALTER TABLE medicines
    ADD CONSTRAINT chk_medicine_reorder CHECK (min_reorder_level >= 0);
```

### 7.2 Migration `023_backfill_grand_total.sql`

```sql
-- Backfill NULL grand_total for pre-GST records (F-02, F-03)
UPDATE sales_invoices SET grand_total = total_amount WHERE grand_total IS NULL;
UPDATE purchase_orders SET grand_total = total_amount WHERE grand_total IS NULL;

ALTER TABLE sales_invoices
    ALTER COLUMN grand_total SET DEFAULT 0.00,
    ALTER COLUMN grand_total SET NOT NULL;
ALTER TABLE purchase_orders
    ALTER COLUMN grand_total SET DEFAULT 0.00,
    ALTER COLUMN grand_total SET NOT NULL;
```

### 7.3 Migration `024_fix_indexes.sql`

```sql
-- Drop redundant index (F-13)
DROP INDEX IF EXISTS idx_hsn_codes_code;

-- Drop duplicate index (F-14)
DROP INDEX IF EXISTS idx_sales_invoices_created_at_disc;

-- Trigram index for purchase invoice search (F-15)
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX idx_purchase_orders_invoice_no_trgm
    ON purchase_orders USING gin (invoice_no gin_trgm_ops);
```

---

## 8. Frontend Fixes

### 8.1 POS credit limit check — use server-returned `grand_total`

**File:** `web/src/pages/POS.tsx:151-157`

The server already returns `grand_total` in the `CheckoutResult`. The POS should compare the projected balance against `grand_total`, not the pre-tax `total`. However, since the server enforces the limit and rejects the sale, the frontend mismatch only causes UX confusion (user thinks it will work, server rejects).

**Fix:** After checkout fails with `CreditLimitExceededError`, show the server's error message which includes the correct `InvoiceTotal` (grand_total). No structural change needed — the error handling already displays the server error.

### 8.2 Purchase result banner — show `grand_total`

**File:** `web/src/pages/Purchases.tsx:696-714`

```tsx
// BEFORE:
<span className="font-mono font-semibold">₹{money(result.total_amount)}</span>

// AFTER:
<span className="font-mono font-semibold">₹{money(result.grand_total ?? result.total_amount)}</span>
```

### 8.3 Invoice list totals — use `grand_total`

**File:** `web/src/pages/Invoices.tsx:72-73`

```tsx
// BEFORE:
const totalSales = sales.reduce((a, s) => a + s.total_amount, 0)
const totalPurchases = purchases.reduce((a, p) => a + p.total_amount, 0)

// AFTER (once backend returns grand_total in list queries):
const totalSales = sales.reduce((a, s) => a + (s.grand_total ?? s.total_amount), 0)
const totalPurchases = purchases.reduce((a, p) => a + (p.grand_total ?? p.total_amount), 0)
```

### 8.4 Invoice detail — use `line_total` from server

**File:** `web/src/pages/Invoices.tsx:766`

```tsx
// BEFORE:
₹{money(it.quantity * it.purchase_price - it.discount_amount)}

// AFTER (once backend returns line_total):
₹{money(it.line_total ?? (it.quantity * it.purchase_price - it.discount_amount))}
```

---

## 9. Implementation Order

| Phase | What | Files Changed | Risk |
|-------|------|---------------|------|
| **P1** | Tax engine: SupplyType param, cess divisor, round_off | `internal/tax/calculator.go`, `calculator_test.go` | Low — pure computation, unit-testable |
| **P2** | Invoice queries: add all GST column SELECTs | `internal/repository/invoice_repo.go` | Low — additive, no logic change |
| **P3** | Batch purchase_price: blended cost | `purchase_repo.go`, `medicine_repo.go` | Medium — changes inventory valuation |
| **P4** | Sale checkout: single tax calc, PriceIncludesTax, round_off, error propagation | `sale_repo.go` | Medium — core checkout path |
| **P5** | Purchase: error propagation, PriceIncludesTax, round_off | `purchase_repo.go` | Medium |
| **P6** | Unify rounding to decimal in repos | `sale_repo.go`, `purchase_repo.go` | Medium — touches all monetary accumulation |
| **P7** | Database migrations | `migrations/022-024` | Low — additive constraints, backfill |
| **P8** | Frontend fixes | `POS.tsx`, `Purchases.tsx`, `Invoices.tsx` | Low — display-only changes |
| **P9** | Tests: new + update existing | `calculator_test.go`, `repository_test.go`, `gst_test.go`, `discount_test.go`, frontend tests | Low — additive |

---

## 10. Test Requirements

### 10.1 New unit tests (tax engine)

| Test | Validates |
|------|-----------|
| `TestTaxInclusiveQtyGreaterThanOne` | Qty=3, MRP=105, 5% tax-inclusive → correct decomposition |
| `TestCessCalculation` | 5% GST + 12% Cess, tax-exclusive → cess = taxable × 12% |
| `TestTaxInclusiveWithCess` | MRP=118, GST=12%, Cess=10% → divisor=1.22, correct amounts |
| `TestMultipleLinesDifferentGSTRates` | Multi-rate invoice aggregation |
| `TestRoundingEdgeCasesFractionalPaise` | Qty=3, price=33.33, 18% → no drift |
| `TestCalculateInvoiceTaxWithSupplyType` | Explicit SupplyType param, nil-rated inter-state |
| `TestRoundOffComputation` | 3 lines with rounding drift → round_off captures difference |

### 10.2 New integration tests

| Test | Validates |
|------|-----------|
| `TestBonusQuantityInventoryCostCorrect` | 10+2 bonus → batch.purchase_price = 83.33 |
| `TestBonusQuantityGSTInteraction` | Tax on paid qty only, stock=12 |
| `TestBonusStockSoldCompletely` | Buy 10+2, sell 12 → stock=0 |
| `TestCreditSaleWithGST` | Credit limit checked against grand_total |
| `TestHistoricalInvoiceImmutabilityAfterTaxConfigChange` | Old invoices unaffected |
| `TestPurchaseStatsTotalSpendExcludesBonus` | TotalSpend = paid qty × price − discount |

### 10.3 Updated existing tests

- `TestPurchaseInwardBonusStock`: verify batch.purchase_price is blended (was paid-only)
- `TestPurchaseInwardPerLineDiscount`: verify blended cost with discount
- `TestReportsEndToEnd`: P&L cost may change with blended pricing
- `TestCalculateInvoiceTax`: add supplyType parameter

### 10.4 Regression checklist

All 45 existing tests must continue passing (with adjusted expected values where batch.purchase_price changes).

---

## 11. Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Existing batch `purchase_price` values are overstated | No data migration for historical batches. New purchases use blended cost. Old batches retain their stored price. P&L for historical periods reflects what was stored at the time. |
| Frontend shows pre-tax total vs server grand_total mismatch | Backend error messages include the correct amounts. Display mismatch is cosmetic only. |
| Tax config lookup error could block all checkouts | Only propagates actual database errors (connection failure, query error). Normal "no config found" returns nil, not error. |
| Decimal rounding differs from float64 for edge cases | Unified to decimal throughout. May cause 1-paise differences in existing test expectations — update expected values. |

---

## 12. Out of Scope (Noted but Deferred)

| Item | Reason |
|------|--------|
| PO-level discount allocation to batches | Current behavior reduces PO total but not individual batch prices. Acceptable for now — the PO total reflects actual payment. |
| Batch price weighted-average on re-inward | Currently overwrites. Weighted average is more complex and can be a separate follow-up. |
| Dual supplier_name/supplier_id sync trigger | Data hygiene issue, not a financial correctness bug. |
| Medicine cache (IndexedDB) tax data | POS tax preview is a feature enhancement, not a bug fix. |
| Store/place-of-supply UI in POS | Walk-in sales defaulting to intra-state is documented behavior. |
| Trigram index for invoice search | Performance optimization, not correctness. |

---

**Total changes:** ~12 files modified, 3 new migrations, ~13 new tests, ~7 updated tests.
