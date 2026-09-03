# GST Domain Audit — PharmaPOS

**Date:** 2026-08-26
**Scope:** All tax-related code in `internal/tax/`, `internal/repository/sale_repo.go`, `internal/repository/purchase_repo.go`, `internal/repository/tax_repo.go`, database migrations, and frontend display layer.

---

## 1. Findings

### F-1: `CalculateInvoiceTax` infers supply type from IGST rate instead of using explicit value
**File:** `internal/tax/calculator.go:138-144`

```go
if len(lines) > 0 {
    if lines[0].IGSTRate.IsPositive() {
        result.SupplyType = SupplyTypeInterState
    } else {
        result.SupplyType = SupplyTypeIntraState
    }
}
```

The supply type was already correctly computed by `DetermineSupplyType()` in both `sale_repo.go:238` and `purchase_repo.go:129`, and passed to each `CalculateLineTax()` call. However, `CalculateInvoiceTax` ignores this and re-infers from the first line's IGST rate. This breaks for:
- **Nil-rated inter-state sales** (0% GST, inter-state): IGST rate is 0 → misclassified as IntraState.
- **Zero-rated supplies** with IGST: same issue.
- Any future product with 0% GST sold inter-state.

The function has no `SupplyType` parameter — it cannot receive the already-correct value.

**Current impact:** Low (callers don't use `result.SupplyType` for invoices — they use their own `supplyType.String()` for the INSERT). But it is a latent bug if anyone consumes `TaxInvoiceResult.SupplyType`.

---

### F-2: Tax-inclusive decomposition ignores cess in the divisor
**File:** `internal/tax/calculator.go:44-51`

```go
if in.PriceIncludesTax {
    divisor := one.Add(in.TaxRate.GSTRate.Div(oneHundred))
    taxableValue = net.Div(divisor)
    taxableValue = RoundMoney(taxableValue)
    taxAmount = net.Sub(taxableValue)
    taxAmount = RoundMoney(taxAmount)
}
```

For tax-inclusive pricing with non-zero cess, the correct divisor should be:
```
divisor = 1 + (gstRate + cessRate) / 100
```
Currently only GST rate is in the divisor. The cess is computed separately from taxableValue (line 76) but never deducted from the MRP. For tax-inclusive:
- `lineTotal = net` (the MRP, which supposedly includes everything)
- But `taxableValue + taxAmount = net` (only GST decomposed)
- `cessAmount` is reported but not subtracted from net
- Result: cess is double-counted if added on top of net, or simply "free money" if lineTotal = net

**Current impact:** Low (cess is 0% for all seeded HSN codes in `021_seed_hsn_tax_rates.sql`). Would become a bug if a cess-bearing product uses tax-inclusive MRP.

---

### F-3: `PriceIncludesTax` on invoice is set incorrectly
**File:** `internal/repository/sale_repo.go:418` and `internal/repository/purchase_repo.go:257`

```go
// sale_repo.go:418
inv.PriceIncludesTax = boolPtr(invoiceResult != nil)

// purchase_repo.go:257
chargeableTotal, invoiceResult != nil,
```

`PriceIncludesTax` is set to `true` whenever `invoiceResult` is non-nil — i.e., whenever *any* tax calculation was performed. This is incorrect. It should reflect the actual medicine-level `PriceIncludesTax` flag. For a tax-exclusive sale with a valid tax config, `invoiceResult` is non-nil and `PriceIncludesTax` is wrongly set to `true`.

**Impact:** Medium. This field is stored on the invoice and displayed on the frontend (`Invoices.tsx`, `POS.tsx`). It gives incorrect information about whether the invoice prices are tax-inclusive or tax-exclusive.

---

### F-4: Invoice-level `total_amount` vs `grand_total` semantic mismatch for tax-inclusive
**File:** `internal/repository/sale_repo.go:383-390`

```go
// sale_repo.go line 256-259 (pre-tax total computation)
gross := float64(it.Quantity) * lb.salePrice
net := round2(gross - discAmount)
total = round2(total + net)

// sale_repo.go line 390 (INSERT)
total, discountTotal, ..., 0.0, chargeableTotal, invoiceResult != nil,
```

For tax-inclusive items, `lb.salePrice` is the MRP (tax-inclusive), so `total` is the sum of MRP-based net amounts. But `chargeableTotal` (from `invoiceResult.GrandTotal`) is `taxableAmount + CGST + SGST + IGST + cess`, which for tax-inclusive equals `sum of (taxableValue + taxAmount)` across lines = `sum of net` (approximately). These are independently rounded and could differ by 1 paise at invoice level.

The `total_amount` column stores the pre-tax/price-inclusive net, and `grand_total` stores the computed total. For tax-inclusive they should be identical but rounding creates drift.

**Impact:** Low (1 paise drift possible).

---

### F-5: Dual precision system — float64 in repos vs decimal in tax engine
**Files:** `internal/repository/sale_repo.go:55-57,241-260` and `internal/repository/purchase_repo.go:131-136`

The repos use `float64` arithmetic with `round2()` (using `math.Round`) for the `total` and `discountTotal` fields. The tax engine uses `shopspring/decimal` with `RoundMoney()` (using `decimal.Round(2)`).

```go
// sale_repo.go:55-57
func round2(v float64) float64 {
    return math.Round(v*100) / 100
}
```

```go
// rounding.go:7-9
func RoundMoney(d decimal.Decimal) decimal.Decimal {
    return d.Round(2)
}
```

For example, `math.Round(100.005 * 100) / 100` = `100.0` (float64), while `decimal.NewFromFloat(100.005).Round(2)` = `100.01` (decimal uses banker's rounding). These can diverge for amounts ending in .005.

**Impact:** Low to Medium. The `total_amount` (float64) and `taxable_amount` (decimal→float64) could differ by 1 paise due to different rounding modes.

---

### F-6: `CalculateInvoiceTax` called twice in sale checkout
**File:** `internal/repository/sale_repo.go:338` and `sale_repo.go:369`

```go
// Line 338 — for credit limit check
invResult := tax.CalculateInvoiceTax(taxLines)
chargeableTotal, _ = invResult.GrandTotal.Float64()

// Line 369 — for invoice creation
r := tax.CalculateInvoiceTax(taxLines)
invoiceResult = &r
```

The exact same calculation is performed twice on the same `taxLines` slice. Not a bug, but wasteful and introduces a code path that could diverge if either call site is modified independently.

**Impact:** None (performance only).

---

### F-7: `round_off` field is always 0.0 — never computed
**Files:** `internal/repository/sale_repo.go:390` and `internal/repository/purchase_repo.go:257`

```go
0.0, chargeableTotal, invoiceResult != nil,
```

The `round_off` column is hardcoded to `0.0` in both INSERT statements. The `TaxInvoiceResult.RoundOff` field exists but is never populated by `CalculateInvoiceTax`. Per GST invoicing rules, `round_off` should record the difference between `grand_total` and `(taxable_amount + tax_total + cess_total)`.

**Impact:** Medium. The round-off field is always zero, which is misleading for accounting reconciliation.

---

### F-8: `DetermineSupplyType` silently defaults to IntraState when state codes are missing
**File:** `internal/tax/rules.go:8-10`

```go
if sellerStateCode == "" || placeOfSupplyStateCode == "" {
    return SupplyTypeIntraState
}
```

If a store has no GST registration (and thus no state code), or if the customer/supplier has no state code, the supply type defaults to IntraState. For an inter-state transaction with missing state codes, this would incorrectly split tax as CGST+SGST instead of IGST.

**Current impact:** Medium. A store without a linked GST registration always produces intra-state invoices.

---

### F-9: Tax config lookup outside the database transaction
**Files:** `internal/repository/sale_repo.go:274` and `internal/repository/purchase_repo.go:154`

```go
taxConfig, _ := r.taxRepo.GetMedicineTaxConfig(ctx, lb.medicineID, time.Now())
```

The tax config is fetched using the connection pool (not within the transaction). If the tax config is updated concurrently, the snapshot stored on the invoice could be based on stale data. The error is also silently discarded (`_`).

**Impact:** Low. Tax config changes are admin operations and unlikely to race with checkouts. The silent error discard is more concerning — if the query fails, the item silently falls through to the zero-tax path.

---

### F-10: No database constraint ensuring CGST + SGST = IGST consistency
**File:** `migrations/013_create_tax_rates.sql`

```sql
CREATE TABLE tax_rates (
    gst_rate  NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    cgst_rate NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    sgst_rate NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    igst_rate NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    ...
);
```

No `CHECK` constraint ensures that `cgst_rate + sgst_rate = gst_rate` or `igst_rate = gst_rate`. A misconfigured tax rate row (e.g., `gst_rate=12, cgst_rate=6, sgst_rate=6, igst_rate=18`) would be accepted by the database.

The tax engine partially mitigates this by computing amounts from `GSTRate` alone (via `SplitTaxComponents`), but the stored CGST/SGST/IGST rates on line items would display the database values, not the computed values. Wait — actually the rates in `TaxLineResult` come from `SplitTaxComponents`, which derives from `GSTRate`. So the stored rates ARE consistent. But the `TaxRate.CGSTRate` etc. stored in the `TaxRate` struct (from the database) are never used in calculations — they're overridden by the split. This is correct by design but fragile.

**Impact:** Low (mitigated by the engine deriving rates from GSTRate).

---

### F-11: `CalculateLineTax` rate output overrides stored rates silently
**File:** `internal/tax/calculator.go:60,94-101`

```go
cgstRate, sgstRate, igstRate := SplitTaxComponents(in.TaxRate.GSTRate, in.SupplyType)
// ...
CGSTRate:   RoundRate(cgstRate),   // always GSTRate/2 for intra
SGSTRate:   RoundRate(sgstRate),   // always GSTRate/2 for intra
IGSTRate:   RoundRate(igstRate),   // always GSTRate for inter
```

The output CGSTRate/SGSTRate/IGSTRate in `TaxLineResult` are always derived from `GSTRate` via `SplitTaxComponents`, regardless of what `TaxRate.CGSTRate` etc. were in the input. This means:
1. The stored `cgst_rate` in `TaxRate` struct is ignored.
2. If `TaxRate.GSTRate=12` but `TaxRate.CGSTRate=5`, the output would be `CGSTRate=6` (derived from 12/2), not 5.

This is actually **correct behavior** — the engine derives component rates from the total rate, ensuring consistency. But it means the per-component rates in `tax_rates` table serve only as documentation.

---

### F-12: Cess computation ignores tax-inclusive decomposition
**File:** `internal/tax/calculator.go:76-77`

```go
cessAmount := taxableValue.Mul(in.TaxRate.CessRate).Div(oneHundred)
cessAmount = RoundMoney(cessAmount)
```

For tax-inclusive pricing, `taxableValue` was extracted using only GST rate in the divisor. The cess should also be included in the divisor:
```
divisor = 1 + (GSTRate + CessRate) / 100
```

Currently, for tax-inclusive with non-zero cess:
- `taxableValue` is too high (cess not extracted)
- `cessAmount` is computed on an inflated taxable value
- `lineTotal = net` doesn't change, so the amounts don't reconcile

**Impact:** Low (all seeded HSN codes have 0% cess).

---

## 2. Bugs

| # | Severity | Finding | File:Line | Description |
|---|----------|---------|-----------|-------------|
| B-1 | **Medium** | F-3 | `sale_repo.go:418`, `purchase_repo.go:257` | `PriceIncludesTax` set to `invoiceResult != nil` instead of the actual medicine config value. All tax-exclusive invoices with valid tax config are stored with `price_includes_tax = true`. |
| B-2 | **Medium** | F-7 | `sale_repo.go:390`, `purchase_repo.go:257` | `round_off` hardcoded to `0.0`. The rounding difference between sum-of-lines and grand total is never recorded. |
| B-3 | **Low** | F-1 | `calculator.go:138-144` | `CalculateInvoiceTax` infers supply type from IGST rate. Nil-rated inter-state sales are misclassified. Latent bug (callers don't use this field currently). |
| B-4 | **Low** | F-2, F-12 | `calculator.go:44-51,76-77` | Tax-inclusive decomposition ignores cess in the divisor. Incorrect for non-zero cess + tax-inclusive pricing. |
| B-5 | **Low** | F-8 | `rules.go:8-10` | Missing state codes silently default to IntraState. Could produce wrong CGST/SGST split for inter-state sales with missing data. |
| B-6 | **Low** | F-5 | `sale_repo.go:55-57`, `rounding.go:7-9` | Dual rounding system (float64 `math.Round` vs decimal `Round(2)`) can produce 1-paise drift. |

---

## 3. Risks

### R-1: Silent error swallowing on tax config lookup
**File:** `sale_repo.go:274`, `purchase_repo.go:154`

```go
taxConfig, _ := r.taxRepo.GetMedicineTaxConfig(ctx, lb.medicineID, time.Now())
```

The error is discarded. If the database query fails (connection issue, query error), the item silently gets zero tax. This could produce invoices with missing GST, which is a compliance violation. The user would see a lower price and the business would under-report GST.

### R-2: Invoice tax calculation happens outside transaction for purchase
**File:** `purchase_repo.go:188-196`

```go
// Outside the transaction (before pgx.BeginFunc)
var invoiceResult *tax.TaxInvoiceResult
if len(taxLines) > 0 {
    r := tax.CalculateInvoiceTax(taxLines)
    invoiceResult = &r
}
chargeableTotal := total
if invoiceResult != nil {
    chargeableTotal, _ = invoiceResult.GrandTotal.Float64()
}
```

The tax calculation and chargeable total are computed outside the transaction. If tax config changes between the tax lookup and the INSERT inside the transaction, the stored amounts could be inconsistent with the current tax config.

### R-3: `CalculateInvoiceTax` `GrandTotal` for tax-inclusive invoices
**File:** `calculator.go:130-135`

For tax-inclusive invoices, `GrandTotal = TaxableAmount + CGSTTotal + SGSTTotal + IGSTTotal + CessTotal`. For each line, `taxableValue + taxAmount = net` (by construction). So `GrandTotal = sum of nets + CessTotal`. But `lineTotal = net` for tax-inclusive (no cess included). The grand total would be higher than the sum of line totals by the cess amount. For 0% cess this is fine, but for non-zero cess, the grand total doesn't match the sum of what the customer actually pays per line.

### R-4: `hasTaxConfig` flag controls invoice-level GST fields but not per-line
**File:** `sale_repo.go:244,336-371,416`

```go
hasTaxConfig := false
// ...
if taxConfig != nil && taxConfig.TaxRate != nil {
    hasTaxConfig = true
    // ...
}
// ...
if len(taxLines) > 0 {
    invResult := tax.CalculateInvoiceTax(taxLines) // uses ALL taxLines, including zero-tax lines
    chargeableTotal, _ = invResult.GrandTotal.Float64()
}
// ...
if hasTaxConfig {
    inv.SupplyType = strPtr(supplyType.String())
    inv.PriceIncludesTax = boolPtr(invoiceResult != nil)
```

`hasTaxConfig` is true if *any* item has a tax config. The credit check uses `chargeableTotal` which includes ALL lines (including zero-tax lines). The invoice-level GST fields are populated only if `hasTaxConfig`. This is correct for mixed invoices (some items with tax, some without), but the `chargeableTotal` from `CalculateInvoiceTax` would include zero-tax line totals in `TaxableAmount`, which is correct.

### R-5: No test coverage for rounding edge cases
**File:** `internal/tax/calculator_test.go`

Existing tests use amounts that divide evenly (100, 200, 105, etc.). There are no tests for:
- Tax amounts that produce odd paise (e.g., 33.3333 → CGST = 16.67, SGST = 16.66)
- Multi-line invoices where line-level rounding creates invoice-level drift
- Tax-inclusive pricing with rounding edge cases (e.g., 100.01 MRP at 18%)
- Cess calculations

---

## 4. Proposed Model

### P-1: Add `SupplyType` parameter to `CalculateInvoiceTax`

```go
func CalculateInvoiceTax(lines []TaxLineResult, supplyType SupplyType) TaxInvoiceResult {
    result := TaxInvoiceResult{
        Lines:      lines,
        SupplyType: supplyType,  // Use explicitly passed value
    }
    // ... aggregation unchanged ...
}
```

**Files to change:** `internal/tax/calculator.go:113`, `internal/repository/sale_repo.go:338,369`, `internal/repository/purchase_repo.go:190`

### P-2: Fix `PriceIncludesTax` to reflect actual config

In `sale_repo.go`, collect the `PriceIncludesTax` from the first tax-configured item and use it for the invoice:

```go
var priceIncludesTax bool
// Inside the item loop:
if taxConfig != nil {
    priceIncludesTax = taxConfig.PriceIncludesTax
}
// In INSERT:
inv.PriceIncludesTax = &priceIncludesTax
```

**Files to change:** `internal/repository/sale_repo.go:244,289,390,418`, `internal/repository/purchase_repo.go:155,167,257`

### P-3: Compute `round_off` properly

```go
// After computing grand total in CalculateInvoiceTax:
roundOff := grandTotal.Sub(taxableAmount.Add(taxTotal).Add(cessTotal))
result.RoundOff = RoundMoney(roundOff)
```

Or better: compute `round_off` at the caller level since it's an invoice-level concern:

```go
roundOff := chargeableTotal - (taxableAmount + taxTotal + cessTotal)
roundOff = round2(roundOff)
```

**Files to change:** `internal/repository/sale_repo.go:390`, `internal/repository/purchase_repo.go:257`

### P-4: Add database constraint for tax rate consistency

```sql
ALTER TABLE tax_rates ADD CONSTRAINT chk_tax_rate_consistency
    CHECK (cgst_rate + sgst_rate = gst_rate AND igst_rate = gst_rate);
```

**Files to change:** `migrations/013_create_tax_rates.sql` (new migration)

### P-5: Pass error from tax config lookup

```go
taxConfig, err := r.taxRepo.GetMedicineTaxConfig(ctx, lb.medicineID, time.Now())
if err != nil {
    return fmt.Errorf("lookup tax config for medicine %s: %w", lb.medicineID, err)
}
```

**Files to change:** `internal/repository/sale_repo.go:274`, `internal/repository/purchase_repo.go:154`

### P-6: Unify rounding to decimal throughout

Remove `round2()` from `sale_repo.go` and `purchase_repo.go`. Use `RoundMoney(decimal.NewFromFloat(...))` consistently:

```go
// Replace:
net := round2(gross - discAmount)
total = round2(total + net)

// With:
net := tax.RoundMoney(decimal.NewFromFloat(gross).Sub(decimal.NewFromFloat(discAmount)))
totalDecimal = totalDecimal.Add(net)
```

**Files to change:** `internal/repository/sale_repo.go:55-57,241-260`, `internal/repository/purchase_repo.go:131-136`

### P-7: Fix cess decomposition for tax-inclusive

```go
if in.PriceIncludesTax {
    divisor := one.
        Add(in.TaxRate.GSTRate.Div(oneHundred)).
        Add(in.TaxRate.CessRate.Div(oneHundred))
    taxableValue = net.Div(divisor)
    taxableValue = RoundMoney(taxableValue)
    taxAmount = taxableValue.Mul(in.TaxRate.GSTRate).Div(oneHundred)
    taxAmount = RoundMoney(taxAmount)
    cessAmount = taxableValue.Mul(in.TaxRate.CessRate).Div(oneHundred)
    cessAmount = RoundMoney(cessAmount)
    // lineTotal = net (unchanged)
}
```

**Files to change:** `internal/tax/calculator.go:44-57,76-87`

### P-8: Compute `CalculateInvoiceTax` once in sale checkout

```go
var invoiceResult *tax.TaxInvoiceResult
if len(taxLines) > 0 {
    r := tax.CalculateInvoiceTax(taxLines, supplyType)
    invoiceResult = &r
    chargeableTotal, _ = invoiceResult.GrandTotal.Float64()
}
// Use invoiceResult for both credit check and INSERT
```

**Files to change:** `internal/repository/sale_repo.go:336-371`

---

## 5. Test Cases

### T-1: Nil-rated inter-state sale
```go
func TestNilRatedInterStateSupplyType() {
    // 0% GST, inter-state (seller=27, buyer=07)
    // CalculateInvoiceTax should return SupplyTypeInterState
    // Not SupplyTypeIntraState (current bug behavior)
}
```
**Validates:** P-1, B-3

### T-2: Tax-inclusive pricing with non-zero cess
```go
func TestTaxInclusiveWithCess() {
    // MRP = 118, GST = 12%, Cess = 10%
    // divisor should be 1 + (12+10)/100 = 1.22
    // taxable = 118 / 1.22 = 96.72
    // GST = 96.72 * 12% = 11.61
    // Cess = 96.72 * 10% = 9.67
    // lineTotal = 118 (MRP)
}
```
**Validates:** P-7, B-4

### T-3: `PriceIncludesTax` on invoice
```go
func TestInvoicePriceIncludesTaxField() {
    // Create a tax-exclusive medicine with tax config
    // Checkout → invoice.price_includes_tax should be false
    // Currently it's true (bug)
}
```
**Validates:** P-2, B-1

### T-4: `round_off` field on invoice
```go
func TestRoundOffField() {
    // 3 lines with amounts that produce rounding drift
    // e.g., 33.33 + 33.33 + 33.34 = 100.00
    // Verify round_off captures the 0.01 difference
}
```
**Validates:** P-3, B-2

### T-5: CGST+SGST split rounding with odd paise
```go
func TestCGSTRoundingOddPaise() {
    // Taxable = 100, GST = 5%, taxAmount = 5.00
    // CGST = 2.50, SGST = 2.50 → sum = 5.00 ✓

    // Taxable = 100, GST = 7%, taxAmount = 7.00
    // CGST = 3.50, SGST = 3.50 → sum = 7.00 ✓

    // Taxable = 333.33, GST = 18%, taxAmount = 59.9994 → rounded 60.00
    // CGST = 30.00, SGST = 30.00 → sum = 60.00 ✓

    // Taxable = 333.34, GST = 18%, taxAmount = 60.0012 → rounded 60.00
    // CGST = 30.00, SGST = 30.00 → sum = 60.00 ✓

    // Taxable = 333.37, GST = 18%, taxAmount = 60.0066 → rounded 60.01
    // CGST = 30.005 → rounded 30.01, SGST = 60.01 - 30.01 = 30.00
    // CGST + SGST = 60.01 ✓
}
```
**Validates:** Rounding correctness of the CGST/SGST split approach.

### T-6: Multi-line invoice rounding drift
```go
func TestMultiLineInvoiceRounding() {
    // 3 lines: taxable 333.33, 333.33, 333.34 at 18% GST
    // Line-level CGST: 30.00, 30.00, 30.01 = 90.01
    // Line-level SGST: 30.00, 30.00, 30.00 = 90.00
    // Line-level tax: 60.00, 60.00, 60.01 = 180.01
    // Taxable total: 1000.00
    // Grand total should = 1000.00 + 90.01 + 90.00 = 1180.01
    // Verify no double-rounding at invoice level
}
```
**Validates:** P-8, R-3

### T-7: Missing state codes default
```go
func TestMissingStateCodeDefaultsToIntraState() {
    // sellerStateCode = "", placeOfSupplyStateCode = ""
    // → IntraState (current behavior, documented as intentional)
    // sellerStateCode = "27", placeOfSupplyStateCode = ""
    // → IntraState (may be wrong if buyer is in another state)
}
```
**Validates:** F-8, B-5

### T-8: Tax config lookup error propagation
```go
func TestTaxConfigLookupError() {
    // Mock a database error on GetMedicineTaxConfig
    // Verify the error is returned, not silently swallowed
}
```
**Validates:** P-5, R-1

### T-9: Consistent rounding between float64 and decimal paths
```go
func TestRoundingConsistency() {
    // Amount: 100.005 (or any value ending in .005)
    // float64 math.Round(100.005*100)/100 = 100.0
    // decimal 100.005.Round(2) = 100.01 (banker's rounding)
    // Verify the unified approach produces consistent results
}
```
**Validates:** P-6, B-6

### T-10: Tax config change between lookup and transaction
```go
func TestTaxConfigConsistencyWithinTransaction() {
    // Start checkout
    // Update tax config for medicine
    // Complete checkout
    // Verify the invoice uses the config from before the update
}
```
**Validates:** R-2, F-9

### T-11: Database constraint on tax rate consistency
```sql
-- Verify constraint prevents invalid tax rate combinations
INSERT INTO tax_rates (hsn_code_id, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from)
VALUES ('some-hsn-id', 12.00, 6.00, 6.00, 18.00, 0.00, '2017-07-01');
-- Should fail with CHECK constraint violation
```
**Validates:** P-4, F-10

---

## Summary

| Category | Count |
|----------|-------|
| Findings | 12 |
| Confirmed Bugs | 6 (2 medium, 4 low) |
| Risks | 5 |
| Proposed Fixes | 8 |
| Test Cases | 11 |

**Overall Assessment:** The tax engine core (`CalculateLineTax`) is mathematically sound for the common case (0% cess, tax-exclusive). The CGST+SGST split ensures exact summation. The main issues are:
1. A misleading `PriceIncludesTax` flag on invoices (B-1)
2. Missing `round_off` computation (B-2)
3. Silent error swallowing on tax config lookup (R-1)
4. Dual precision system (B-6)
5. Dead-code supply type inference in `CalculateInvoiceTax` (B-3)

None of these are critical — no incorrect tax amounts are produced for the standard pharmacy use case (0% cess, tax-exclusive pricing). The issues become critical only for edge cases (cess, tax-inclusive with cess, missing state codes).
