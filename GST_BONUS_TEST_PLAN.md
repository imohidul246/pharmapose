# PharmaPOS — GST & Bonus Quantity Test Plan

> Generated: 2026-08-26
> Status: AUDIT — no test files modified

---

## 1. Current Coverage Summary

### 1.1 Tax Engine Unit Tests (`internal/tax/calculator_test.go`)

| # | Test | Lines | What It Verifies |
|---|------|-------|------------------|
| 1 | `TestTaxInclusive5PercentIntraState` | 17-46 | 5% MRP=105, intra-state, tax-inclusive, qty=1 |
| 2 | `TestTaxInclusive5PercentWithDiscount` | 49-81 | 5% MRP=105 + ₹5 discount, tax-inclusive |
| 3 | `TestIGSTInterState` | 84-110 | 5% tax-exclusive, inter-state, IGST only |
| 4 | `TestIntraStateCGSTSGST` | 113-139 | 5% tax-exclusive, intra-state, CGST+SGST split |
| 5 | `TestNilRatedProduct` | 142-171 | 0% GST, all tax amounts zero |
| 6 | `TestPercentageDiscountTaxExclusive` | 174-201 | Qty=10, 10% discount on tax-exclusive |
| 7 | `TestFlatDiscountTaxExclusive` | 204-224 | Qty=10, flat ₹100 discount on tax-exclusive |
| 8 | `TestDiscountClamping` | 227-250 | ₹500 discount on ₹100 gross → clamped to ₹100 |
| 9 | `TestDetermineSupplyType` | 252-270 | Table: same-state, cross-state, empty strings |
| 10 | `TestCalculateInvoiceTax` | 272-312 | 2-line invoice aggregation, same 5% rate |

**Count: 10 tests**

### 1.2 Repository Integration Tests (`internal/repository/repository_test.go`)

| # | Test | Lines | What It Verifies |
|---|------|-------|------------------|
| 1 | `TestCheckoutDecrementsBatchStock` | 123-145 | Cash sale of 30 from 100 → stock=70, total=450 |
| 2 | `TestCheckoutRejectsOversellAtomically` | 147-176 | 40+20 demand > 50 supply → InsufficientStockError, no invoice left |
| 3 | `TestCheckoutConcurrencyNeverNegativeStock` | 178-225 | 12 workers × 15 units on 100 stock → stock=0, 100 invoices |
| 4 | `TestCreditLimitEnforced` | 227-282 | ₹100 limit: 7 units fails, 6 passes, 1 more fails |
| 5 | `TestCreditSaleRequiresCustomer` | 284-295 | Credit sale without customer → error |
| 6 | `TestPurchaseInwardMergesSameBatchNumber` | 297-340 | 2nd inward on same batch → stock merged, single row |
| 7 | `TestPurchaseInwardCreatesNewMedicineInline` | 342-388 | MedicineName provided, no MedicineID → auto-creates medicine |
| 8 | `TestPurchaseInwardRequiresMedicineReference` | 390-405 | No medicine ref at all → error |
| 9 | `TestPurchaseInwardBonusStock` | 407-442 | Qty=10, BonusQty=2 → stock=12, PO total=500 (paid only) |
| 10 | `TestPurchaseInwardPerLineDiscount` | 444-482 | 20×₹100, 10% discount → total=1800, effective purchase=₹90 |
| 11 | `TestPurchaseInwardPOLevelDiscount` | 484-512 | 10×₹100, PO discount ₹500 → total=500 |
| 12 | `TestReconcileCorrectsStockAndLeavesSalesHistoryIntact` | 514-576 | Physical count 55 vs system 60 → variance=-5, history preserved |
| 13 | `TestReportsEndToEnd` | 578-651 | Sales, purchases, P&L, expiry, low-stock reports |

**Count: 13 tests** (including `TestCreditSaleRequiresCustomer`)

### 1.3 GST Integration Tests (`internal/repository/gst_test.go`)

| # | Test | Lines | What It Verifies |
|---|------|-------|------------------|
| 1 | `TestCheckoutWithGSTIntraState` | 104-200 | 12% GST, same state (27), qty=10 → CGST=90, SGST=90, grand=1680 |
| 2 | `TestCheckoutWithGSTInterState` | 202-269 | 12% GST, different state (07), qty=5 → IGST=90, grand=840 |
| 3 | `TestCheckoutWithoutTaxConfigGracefulFallback` | 271-299 | No tax config → nil tax fields, total_amount works |
| 4 | `TestLegacyInvoiceBackwardCompatibility` | 301-328 | No store, no tax → total=30, no GST fields |

**Count: 4 tests**

### 1.4 Credit Ledger Tests (`internal/repository/credit_ledger_test.go`)

| # | Test | Lines | What It Verifies |
|---|------|-------|------------------|
| 1 | `TestCreditSaleWritesLedgerEntry` | 18-51 | CREDIT_SALE entry, amount=60, balance matches |
| 2 | `TestCashSaleWritesNoLedgerEntry` | 53-68 | Cash sale → zero ledger entries |
| 3 | `TestPaymentFlowFullAndPartial` | 70-120 | 2 credit sales, partial pay, full pay, 4 ledger entries |
| 4 | `TestOverpaymentRejectedAtomically` | 122-151 | Pay more than balance → error, no mutation |
| 5 | `TestInvalidPaymentsRejected` | 153-169 | Zero/negative/unknown customer → errors |

**Count: 5 tests**

### 1.5 Discount Tests (`internal/repository/discount_test.go`)

| # | Test | Lines | What It Verifies |
|---|------|-------|------------------|
| 1 | `TestCheckoutDiscounts` | 13-88 | Percent, flat, over-discount clamped, negative rejected |
| 2 | `TestCreditCheckUsesDiscountedTotal` | 90-124 | Credit limit checked against discounted total |
| 3 | `TestConflictingDuplicateLineDiscountsRejected` | 126-140 | Same batch two lines, one with discount → rejected |

**Count: 3 tests**

### 1.6 Frontend POS Tests (`web/src/pages/POS.test.tsx`)

| # | Test | Lines | What It Verifies |
|---|------|-------|------------------|
| 1 | Synced catalog lists immediately | 39-46 | Medicine visible before typing |
| 2 | Popup opens on Enter | 48-58 | Type name → Enter → batch popup |
| 3 | Popup opens on mouse click | 60-69 | Type name → click medicine → batch popup |
| 4 | FEFO batch added on Enter | 72-85 | Enter → select nearest-expiry batch |
| 5 | Quick-add with number keys | 87-99 | Press "2" to select 2nd batch |

**Count: 5 tests**

### 1.7 Frontend Invoices Tests (`web/src/pages/Invoices.test.tsx`)

| # | Test | Lines | What It Verifies |
|---|------|-------|------------------|
| 1 | Pagination | 114-126 | 12 rows → 2 pages, page 1=8, page 2=4 |
| 2 | Sales invoice detail | 128-141 | View button → dialog with medicine name, discount, gross |
| 3 | Purchase invoice detail | 143-155 | View button → dialog with supplier, expiry |
| 4 | Retryable error | 157-171 | Failed detail → error message → Retry → success |
| 5 | Escape closes dialog | 173-184 | Escape key dismisses dialog |

**Count: 5 tests**

### Grand Total: 45 tests across 7 files

---

## 2. Test Matrix

### 2.1 GST Scenarios (A–G)

| ID | Scenario | Tax Engine | Integration | Frontend |
|----|----------|------------|-------------|----------|
| **A** | Intra-state, tax-inclusive, 5%, qty=1, no discount | `TestTaxInclusive5PercentIntraState` | — | — |
| **B** | Intra-state, tax-inclusive, 5%, qty=1, with discount | `TestTaxInclusive5PercentWithDiscount` | — | — |
| **C** | Inter-state, tax-exclusive, 5%, IGST | `TestIGSTInterState` | — | — |
| **D** | Intra-state, tax-exclusive, 5%, CGST+SGST | `TestIntraStateCGSTSGST` | — | — |
| **E** | Nil-rated (0% GST) | `TestNilRatedProduct` | — | — |
| **F** | Intra-state, 12% GST, tax-exclusive, qty=10 | — | `TestCheckoutWithGSTIntraState` | — |
| **G** | Inter-state, 12% GST, tax-exclusive, qty=5 | — | `TestCheckoutWithGSTInterState` | — |

### 2.2 Bonus Quantity Scenarios (H–O)

| ID | Scenario | Currently Tested? | Test File & Function |
|----|----------|-------------------|---------------------|
| **H** | Purchase with bonus: qty=10, bonus=2 | **YES** | `repository_test.go:407` `TestPurchaseInwardBonusStock` |
| **I** | Bonus qty included in stock | **YES** | `repository_test.go:439` (stock=12) |
| **J** | PO total excludes bonus from cost | **YES** | `repository_test.go:429` (total=500) |
| **K** | Bonus qty + GST interaction on purchase | **NO** | — |
| **L** | Bonus quantity affects inventory cost correctly (weighted avg) | **NO** | — |
| **M** | Bonus stock sold out: purchase 10+2 bonus, sell 12 | **NO** | — |
| **N** | Bonus qty validated as non-negative | **YES** (via validation) | `repository_test.go:390` requires medicine ref, but bonus validation is in `purchase_repo.go:72` |
| **O** | Bonus qty on batch merge (2nd inward, same batch) | **NO** | — |

### 2.3 Concurrency Scenarios

| ID | Scenario | Currently Tested? | Test Location |
|----|----------|-------------------|---------------|
| **C1** | Concurrent checkout same batch (12 workers) | **YES** | `repository_test.go:178` |
| **C2** | Oversell rejected atomically | **YES** | `repository_test.go:147` |
| **C3** | Purchase + sale on same batch simultaneously | **NO** | — |
| **C4** | Concurrent credit sales (balance consistency) | **NO** | — |
| **C5** | Concurrent reconcile + sale | **NO** | — |

### 2.4 Regression Scenarios

| ID | Scenario | Currently Tested? | Test Location |
|----|----------|-------------------|---------------|
| **R1** | Legacy checkout (no GST config) still works | **YES** | `gst_test.go:271` |
| **R2** | Historical invoice immutability after reconcile | **YES** | `repository_test.go:514` |
| **R3** | Credit limit enforced | **YES** | `repository_test.go:227` |
| **R4** | Discounted total used for credit check | **YES** | `discount_test.go:90` |
| **R5** | Batch merge preserves stock correctly | **YES** | `repository_test.go:297` |
| **R6** | Inline medicine creation on purchase | **YES** | `repository_test.go:342` |
| **R7** | Reports reflect all operations | **YES** | `repository_test.go:578` |
| **R8** | Negative discount rejected | **YES** | `discount_test.go:80` |
| **R9** | Over-discount clamped to zero | **YES** | `discount_test.go:68` |
| **R10** | Conflicting line discounts rejected | **YES** | `discount_test.go:126` |
| **R11** | Cash sale does not write ledger | **YES** | `credit_ledger_test.go:53` |
| **R12** | Overpayment rejected atomically | **YES** | `credit_ledger_test.go:122` |
| **R13** | FEFO batch ordering in POS | **YES** | `POS.test.tsx:72` |
| **R14** | Invoice detail dialog with GST fields | **PARTIAL** | `Invoices.test.tsx:128` (shows `Gross amount` but no GST field assertions) |

---

## 3. Missing Tests

### 3.1 Tax Engine Unit Tests — Missing

| Priority | Test Name | Scenario | File Location |
|----------|-----------|----------|---------------|
| **P0** | `TestTaxInclusiveQtyGreaterThanOne` | Qty=3, MRP=105, 5% tax-inclusive → gross=315, taxable=300, tax=15 | `calculator_test.go` |
| **P0** | `TestCessCalculation` | 5% GST + 12% Cess, tax-exclusive → cess = taxable × 12/100 | `calculator_test.go` |
| **P1** | `TestMultipleLinesDifferentGSTRates` | Line 1: 5%, Line 2: 12% → correct aggregation in `CalculateInvoiceTax` | `calculator_test.go` |
| **P1** | `TestTaxInclusiveWithDiscountAndMultipleLines` | 2 lines, tax-inclusive, with discounts → correct aggregation | `calculator_test.go` |
| **P1** | `TestRoundingEdgeCasesFractionalPaise` | Qty=3, price=33.33, 18% GST → verify rounding consistency | `calculator_test.go` |
| **P2** | `TestTaxInclusiveCessCalculation` | Tax-inclusive + Cess → cess extracted from MRP | `calculator_test.go` |
| **P2** | `TestHistoricalTaxSnapshotImmutability` | Verify `TaxLineResult` is a value copy, not a reference | `calculator_test.go` |
| **P2** | `TestZeroQuantityLine` | Qty=0 → gross=0, tax=0, total=0 | `calculator_test.go` |
| **P3** | `TestNegativeDiscountClampedToZero` | DiscountAmount=-5 → clamped to 0 | `calculator_test.go` |
| **P3** | `TestParseSupplyType` | Verify `ParseSupplyType("INTRA_STATE")`, etc. | `calculator_test.go` |
| **P3** | `TestSupplyTypeString` | Verify `SupplyTypeInterState.String()` == "INTER_STATE" | `calculator_test.go` |
| **P3** | `TestZeroTaxInput` | Verify `ZeroTaxInput` helper | `calculator_test.go` |
| **P3** | `TestZeroTaxResult` | Verify `ZeroTaxResult` helper | `calculator_test.go` |

### 3.2 Integration Tests — Missing

| Priority | Test Name | Scenario | File Location |
|----------|-----------|----------|---------------|
| **P0** | `TestBonusQuantityGSTInteraction` | Purchase 10+2 bonus with 12% GST → stock=12, PO total reflects only 10 paid, tax on paid qty only | `gst_test.go` or `repository_test.go` |
| **P0** | `TestBonusStockSoldCompletely` | Purchase 10+2 bonus → sell 12 → stock=0 | `repository_test.go` |
| **P0** | `TestBonusQuantityInventoryCostCorrect` | 10 paid@₹50 + 2 bonus → effective purchase price calculation | `repository_test.go` |
| **P1** | `TestTaxInclusivePurchase` | Purchase with `price_includes_tax=true` → correct batch purchase_price | `gst_test.go` |
| **P1** | `TestHistoricalInvoiceImmutabilityAfterTaxConfigChange` | Create invoice without tax config → add tax config → old invoice unchanged | `gst_test.go` |
| **P1** | `TestPurchaseMultipleLinesDifferentGSTRates` | Purchase with 5% and 12% items → correct per-line tax | `gst_test.go` |
| **P1** | `TestCreditSaleWithGST` | Credit sale with tax config → credit check uses `grand_total` not `total_amount` | `gst_test.go` |
| **P2** | `TestConcurrentPurchaseAndSaleSameBatch` | One worker purchases, another sells → no negative stock | `repository_test.go` |
| **P2** | `TestConcurrentCreditSalesBalanceConsistency` | Multiple concurrent credit sales → balance = sum of invoices | `credit_ledger_test.go` |
| **P2** | `TestConcurrentReconcileAndSale` | Reconcile during active sale → no crash, consistent state | `repository_test.go` |
| **P2** | `TestBonusQtyOnBatchMerge` | 2nd inward on same batch with bonus → stock merged correctly | `repository_test.go` |
| **P3** | `TestMultipleCreditPaymentsSingleCustomer` | 3 credit sales + 2 payments → ledger order correct | `credit_ledger_test.go` |
| **P3** | `TestZeroPaymentRejected` | ₹0 payment → rejected | `credit_ledger_test.go` (partially covered by `TestInvalidPaymentsRejected`) |
| **P3** | `TestReconcileMultipleBatches` | Reconcile 2 batches in one journal | `repository_test.go` |

### 3.3 Frontend Tests — Missing

| Priority | Test Name | Scenario | File Location |
|----------|-----------|----------|---------------|
| **P1** | GST fields in invoice detail dialog | Verify `CGST`, `SGST`, `IGST`, `HSN`, `Cess` displayed | `Invoices.test.tsx` |
| **P1** | Bonus quantity shown in invoice detail | Verify `bonus_quantity` displayed for purchase invoices | `Invoices.test.tsx` |
| **P1** | Tax-inclusive MRP displayed | Verify MRP vs taxable value shown correctly | `POS.test.tsx` |
| **P2** | Search filtering (by salt composition) | Type salt name → filtered results | `POS.test.tsx` |
| **P2** | Empty search → no results | Type non-existent → "no medicines found" | `POS.test.tsx` |
| **P2** | Purchase invoice list shows GST total | Invoice list shows `tax_total` column | `Invoices.test.tsx` |
| **P3** | Keyboard navigation in batch popup | Arrow keys navigate batch list | `POS.test.tsx` |
| **P3** | Pagination edge cases (empty list, single page) | 0 invoices → "no invoices" | `Invoices.test.tsx` |

---

## 4. Regression Checklist

Every item below **must** continue passing. Each is a behavioral contract.

### 4.1 Tax Engine

| # | Contract | Current Test |
|---|----------|--------------|
| T1 | Tax-inclusive: `gross = qty × unit_price`, `taxable = gross / (1+rate/100)` | `TestTaxInclusive5PercentIntraState` |
| T2 | Tax-inclusive with discount: `taxable = (gross - discount) / (1+rate/100)` | `TestTaxInclusive5PercentWithDiscount` |
| T3 | Tax-exclusive: `taxable = gross - discount`, `tax = taxable × rate/100` | `TestPercentageDiscountTaxExclusive` |
| T4 | Intra-state: tax splits equally into CGST + SGST | `TestIntraStateCGSTSGST` |
| T5 | Inter-state: full tax goes to IGST, CGST/SGST = 0 | `TestIGSTInterState` |
| T6 | Nil-rated: all tax = 0 | `TestNilRatedProduct` |
| T7 | Discount > gross → clamped to gross, taxable = 0 | `TestDiscountClamping` |
| T8 | Negative discount → clamped to 0 | (in `discount_test.go`, not `calculator_test.go`) |
| T9 | Same state codes → IntraState, different → InterState, empty → IntraState | `TestDetermineSupplyType` |
| T10 | Invoice aggregation sums line-level amounts | `TestCalculateInvoiceTax` |
| T11 | CessRate × taxableValue → CessAmount | **NOT TESTED** |
| T12 | `CalculateInvoiceTax.CessTotal` aggregates line cess | **NOT TESTED** |
| T13 | GrandTotal = Taxable + CGST + SGST + IGST + Cess (line 130-134) | Partially tested (no cess) |

### 4.2 Purchase / Inward

| # | Contract | Current Test |
|---|----------|--------------|
| P1 | Stock incremented by `quantity + bonus_quantity` | `TestPurchaseInwardBonusStock` |
| P2 | PO `total_amount` = `sum(quantity × purchase_price)` (excludes bonus cost) | `TestPurchaseInwardBonusStock` |
| P3 | Same batch number → stock merged, no duplicate batch row | `TestPurchaseInwardMergesSameBatchNumber` |
| P4 | `MedicineName` provided → medicine auto-created | `TestPurchaseInwardCreatesNewMedicineInline` |
| P5 | Neither MedicineID nor MedicineName → error | `TestPurchaseInwardRequiresMedicineReference` |
| P6 | Per-line discount → effective purchase_price adjusted | `TestPurchaseInwardPerLineDiscount` |
| P7 | PO-level discount → total reduced | `TestPurchaseInwardPOLevelDiscount` |
| P8 | `bonus_quantity` must be ≥ 0 | `purchase_repo.go:72` (validation, no dedicated test) |

### 4.3 Sale / Checkout

| # | Contract | Current Test |
|---|----------|--------------|
| S1 | Cash sale decrements batch stock | `TestCheckoutDecrementsBatchStock` |
| S2 | `InsufficientStockError` with correct `AvailableStock`/`RequestedQty` | `TestCheckoutRejectsOversellAtomically` |
| S3 | Failed checkout leaves no invoice/item rows | `TestCheckoutRejectsOversellAtomically` |
| S4 | Concurrent checkout never yields negative stock | `TestCheckoutConcurrencyNeverNegativeStock` |
| S5 | `CreditLimitExceededError` when over limit | `TestCreditLimitEnforced` |
| S6 | Rejected credit sale does not mutate balance or stock | `TestCreditLimitEnforced` |
| S7 | Credit sale requires customer | `TestCreditSaleRequiresCustomer` |
| S8 | Discounted total used for credit limit check | `TestCreditCheckUsesDiscountedTotal` |
| S9 | Conflicting discount lines on same batch rejected | `TestConflictingDuplicateLineDiscountsRejected` |
| S10 | Over-discount clamped, total ≥ 0 | `TestCheckoutDiscounts` |
| S11 | Negative discount rejected | `TestCheckoutDiscounts` |

### 4.4 GST Integration

| # | Contract | Current Test |
|---|----------|--------------|
| G1 | Intra-state checkout populates CGST/SGST on invoice + items | `TestCheckoutWithGSTIntraState` |
| G2 | Inter-state checkout populates IGST on invoice + items | `TestCheckoutWithGSTInterState` |
| G3 | No tax config → `TaxTotal`/`GrandTotal` = nil | `TestCheckoutWithoutTaxConfigGracefulFallback` |
| G4 | Legacy checkout (no store, no tax) works identically to pre-GST | `TestLegacyInvoiceBackwardCompatibility` |
| G5 | HSN code persisted on invoice item | `TestCheckoutWithGSTIntraState` (item.HSNCode) |
| G6 | `supply_type` persisted on invoice | `TestCheckoutWithGSTIntraState` |

### 4.5 Credit Ledger

| # | Contract | Current Test |
|---|----------|--------------|
| L1 | Credit sale → `CREDIT_SALE` entry with amount and running balance | `TestCreditSaleWritesLedgerEntry` |
| L2 | Cash sale → no ledger entry | `TestCashSaleWritesNoLedgerEntry` |
| L3 | Payment reduces balance, `PAYMENT` entry written | `TestPaymentFlowFullAndPartial` |
| L4 | Overpayment rejected, no mutation | `TestOverpaymentRejectedAtomically` |
| L5 | Zero/negative/unknown customer rejected | `TestInvalidPaymentsRejected` |
| L6 | Ledger entries in reverse-chronological order with correct running balance | `TestPaymentFlowFullAndPartial` |

### 4.6 Reports

| # | Contract | Current Test |
|---|----------|--------------|
| RP1 | Sales report: `NetSales`, `NetUnits` correct | `TestReportsEndToEnd` |
| RP2 | Purchase report: `OrderCount`, `ItemCount`, `TotalSpend` correct | `TestReportsEndToEnd` |
| RP3 | P&L: `Revenue`, `Cost`, `Profit` correct | `TestReportsEndToEnd` |
| RP4 | Expiry report: batches listed with correct stock | `TestReportsEndToEnd` |
| RP5 | Low-stock report: only below-threshold items shown | `TestReportsEndToEnd` |

### 4.7 Frontend

| # | Contract | Current Test |
|---|----------|--------------|
| F1 | Catalog lists medicines from IndexedDB cache | `POS.test.tsx:39` |
| F2 | Batch popup opens on Enter and click | `POS.test.tsx:48`, `POS.test.tsx:60` |
| F3 | FEFO (first-expiry-first-out) batch highlighted | `POS.test.tsx:72` |
| F4 | Number key quick-add selects specific batch | `POS.test.tsx:87` |
| F5 | Invoice list paginates (8 per page) | `Invoices.test.tsx:114` |
| F6 | Sales/purchase detail dialogs open with correct data | `Invoices.test.tsx:128`, `Invoices.test.tsx:143` |
| F7 | Failed detail load shows retryable error | `Invoices.test.tsx:157` |
| F8 | Escape closes dialog | `Invoices.test.tsx:173` |

---

## 5. Priority Order for Writing Missing Tests

### Phase 1: Critical GST + Bonus Integration (P0)

These cover the core new features with the highest regression risk.

| Order | Test | Why First |
|-------|------|-----------|
| 1 | `TestTaxInclusiveQtyGreaterThanOne` (tax unit) | Qty > 1 is the most common real-world scenario; existing tests all use qty=1 |
| 2 | `TestCessCalculation` (tax unit) | Cess is fully implemented (`calculator.go:76`) but has **zero test coverage** |
| 3 | `TestBonusQuantityGSTInteraction` (integration) | Bonus + GST is the main new feature; no test verifies tax correctness when bonus is present |
| 4 | `TestBonusStockSoldCompletely` (integration) | Verify the full lifecycle: purchase 10+2 → sell 12 → stock=0 |
| 5 | `TestBonusQuantityInventoryCostCorrect` (integration) | Cost accounting with bonus must be verified |

### Phase 2: Edge Cases + Coverage Gaps (P1)

| Order | Test | Why |
|-------|------|-----|
| 6 | `TestMultipleLinesDifferentGSTRates` (tax unit) | Real invoices have mixed rates; `CalculateInvoiceTax` needs multi-rate coverage |
| 7 | `TestTaxInclusiveWithDiscountAndMultipleLines` (tax unit) | Combined scenario untested |
| 8 | `TestRoundingEdgeCasesFractionalPaise` (tax unit) | Indian GST requires precise rounding; fractional paise can cause ₹0.01 drift |
| 9 | `TestCreditSaleWithGST` (integration) | Credit limit check must use `grand_total` not `total_amount` when GST is present |
| 10 | `TestTaxInclusivePurchase` (integration) | Purchase-side tax-inclusive not tested |
| 11 | `TestHistoricalInvoiceImmutabilityAfterTaxConfigChange` (integration) | Critical for data integrity |
| 12 | GST fields in frontend invoice detail | Users need to see GST breakdown in the UI |
| 13 | Bonus quantity shown in frontend invoice detail | Users need to see bonus in purchase invoices |

### Phase 3: Concurrency + Robustness (P2)

| Order | Test | Why |
|-------|------|-----|
| 14 | `TestConcurrentPurchaseAndSaleSameBatch` (integration) | Real-world concurrent operations |
| 15 | `TestConcurrentCreditSalesBalanceConsistency` (integration) | Balance must never be negative or inconsistent |
| 16 | `TestBonusQtyOnBatchMerge` (integration) | 2nd inward with bonus on existing batch |
| 17 | `TestPurchaseMultipleLinesDifferentGSTRates` (integration) | Multi-rate purchases |
| 18 | `TestTaxInclusiveCessCalculation` (tax unit) | Combined tax-inclusive + cess |
| 19 | Search/filtering in POS frontend | UX coverage |

### Phase 4: Low Priority / Nice to Have (P3)

| Order | Test | Why |
|-------|------|-----|
| 20 | `TestZeroQuantityLine` (tax unit) | Defensive edge case |
| 21 | `TestParseSupplyType` / `TestSupplyTypeString` (tax unit) | String helpers |
| 22 | `TestZeroTaxInput` / `TestZeroTaxResult` (tax unit) | Helper functions |
| 23 | `TestConcurrentReconcileAndSale` (integration) | Rare concurrent scenario |
| 24 | `TestReconcileMultipleBatches` (integration) | Multi-batch reconcile |
| 25 | Pagination edge cases in Invoices frontend | Empty/single-page scenarios |

---

## Appendix: Test File → Line Number Cross-Reference

| File | Line Range | Test Count |
|------|-----------|------------|
| `internal/tax/calculator_test.go` | 1-312 | 10 |
| `internal/repository/repository_test.go` | 1-675 | 13 |
| `internal/repository/gst_test.go` | 1-332 | 4 |
| `internal/repository/credit_ledger_test.go` | 1-169 | 5 |
| `internal/repository/discount_test.go` | 1-140 | 3 |
| `web/src/pages/POS.test.tsx` | 1-100 | 5 |
| `web/src/pages/Invoices.test.tsx` | 1-185 | 5 |

**Total existing: 45 tests**
**Total identified gaps: 25 tests recommended**
**Projected total after implementation: 70 tests**
