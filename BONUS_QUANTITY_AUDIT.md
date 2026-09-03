# Bonus Quantity / Inventory Accounting Audit

**Auditor:** Bonus Quantity / Inventory Accounting Auditor
**Date:** 2026-08-26
**Scope:** All bonus quantity handling, inventory costing, profit calculation, and related code paths.

---

## 1. Current Semantics

### 1.1 Bonus Quantity Semantics

| Question | Answer | Reference |
|----------|--------|-----------|
| What does `quantity` mean in PurchaseItemInput? | The number of units **paid for** by the pharmacy. | `purchase_repo.go:37` |
| What does `bonus_quantity` mean? | The number of units received **free** from the supplier (Buy X Get Y Free). | `purchase_repo.go:38` |
| What quantity is taxed? | Only the paid quantity (`it.Quantity`). Bonus units are excluded from GST calculation. | `purchase_repo.go:157` |
| What quantity is paid for? | `it.Quantity` only. Gross = `quantity × purchasePrice`. | `purchase_repo.go:133` |
| What quantity enters inventory? | `totalStock = it.Quantity + it.BonusQuantity` — both paid and free units. | `purchase_repo.go:266` |
| What determines batch purchase_price / cost? | `effectivePrice = (gross - lineDiscount) / quantity` — computed on **paid quantity only**. | `purchase_repo.go:141` |

### 1.2 Inventory Cost Calculation

**How is `effectivePrice` computed?** (`purchase_repo.go:139-141`)

```go
effectivePrice := it.PurchasePrice
if it.Quantity > 0 && discAmount > 0 {
    effectivePrice = round2((gross - discAmount) / float64(it.Quantity))
}
```

- `gross = it.Quantity * it.PurchasePrice` (paid quantity × unit price)
- `discAmount` = line-level discount (percent or flat)
- Divisor = `it.Quantity` (paid quantity only — **excludes bonus**)

**When there's no discount**, `effectivePrice = it.PurchasePrice` (the raw unit price).

**When there's a discount**, `effectivePrice = (gross - discount) / quantity` (still excludes bonus).

**Does this reflect the true per-unit cost? NO.** The true cost per unit received should be `(gross - discount) / (quantity + bonus_quantity)`.

**Example:**
- 10 paid at ₹100 + 2 bonus. No discount.
- Gross = ₹1,000
- effectivePrice = ₹1,000 / 10 = **₹100** (stored as batch.purchase_price)
- True cost per unit received = ₹1,000 / 12 = **₹83.33**
- Discrepancy: **₹16.67 per unit overstated**

**Is this intentional?** The batch.purchase_price is designed to represent the **cost of paid units only**, not the blended cost across all received units. This is a deliberate design choice, but it creates accounting inconsistencies downstream (see Section 2).

### 1.3 Accounting Policy Currently Implemented

The system implements a **"Paid Unit Cost"** policy:
- `batch.purchase_price` = cost per paid unit (ignoring bonus)
- Inventory is stocked at paid-unit cost, not true cost
- Profit margin is calculated against paid-unit cost
- Stock value is calculated at paid-unit cost

This is **not standard pharmaceutical inventory accounting**. The industry standard is **weighted average cost** where `cost_per_unit = total_amount_paid / total_units_received`.

---

## 2. Bugs

### BUG-1: `batch.purchase_price` Overstates True Unit Cost (SEVERITY: HIGH)

**Location:** `purchase_repo.go:139-141`, `purchase_repo.go:280`

When bonus_quantity > 0, `effectivePrice` is divided by `quantity` (paid) instead of `quantity + bonus_quantity` (received). This stores an inflated cost in `batches.purchase_price`.

**Impact:** Every downstream consumer of `batches.purchase_price` inherits this overstatement.

### BUG-2: Profit & Loss Understates Profit (SEVERITY: HIGH)

**Location:** `report_repo.go:194`

```sql
SUM(sii.quantity * b.purchase_price) AS cost
```

COGS is calculated as `sold_units × batch.purchase_price`. Since `batch.purchase_price` is overstated (BUG-1), cost is overstated, and **profit is understated**.

**Example:**
- Sell 12 units at ₹150 each. Revenue = ₹1,800.
- `batch.purchase_price` = ₹100 (paid-only basis)
- COGS (current) = 12 × ₹100 = ₹1,200 → Profit = ₹600
- True cost = ₹1,000 / 12 = ₹83.33 → True COGS = ₹1,000 → True Profit = ₹800
- **Profit understated by ₹200 (25%)**

### BUG-3: Expiry Report Overstates Stock Value (SEVERITY: MEDIUM)

**Location:** `report_repo.go:253`

```sql
(b.current_stock * b.purchase_price)
```

Uses `batch.purchase_price` which is overstated when bonus units exist. Stock value on the balance sheet is inflated.

**Example:**
- Batch has 12 units. `batch.purchase_price` = ₹100.
- Stock value (current) = 12 × ₹100 = ₹1,200
- True stock value = 12 × ₹83.33 = ₹1,000
- **Stock value overstated by ₹200 (20%)**

### BUG-4: Reconciliation Cost Impact Overstated (SEVERITY: MEDIUM)

**Location:** `reconcile_repo.go:126`

```go
costImpact := round2(float64(variance) * lb.purchasePrice)
```

Uses `batch.purchase_price` which is overstated. A shortage of 1 unit shows a cost impact of ₹100 when the true cost is ₹83.33.

### BUG-5: Medicine Purchase Stats Overstate Total Spend (SEVERITY: MEDIUM)

**Location:** `medicine_repo.go:255`

```sql
SUM((poi.quantity + poi.bonus_quantity) * poi.purchase_price)
```

This multiplies **all received units** by `purchase_price` (which is the per-paid-unit price). This double-counts the bonus portion.

**Example:**
- Purchase: 10 paid at ₹100 + 2 bonus. No discount.
- Actual spend = ₹1,000
- Formula = (10 + 2) × ₹100 = ₹1,200
- **Spend overstated by ₹200 (20%)**

The `UnitsPurchased` field (`medicine_repo.go:254`) correctly includes bonus units in total units received, but `TotalSpend` should only reflect actual money paid.

### BUG-6: Tax Calculated on Paid-Only Quantity (CORRECT)

**Location:** `purchase_repo.go:157`

```go
Quantity: decimal.NewFromInt(int64(it.Quantity)),
```

Tax is calculated on paid quantity only. This is **correct** for GST — bonus/free goods from suppliers are not separately taxable. This is NOT a bug.

---

## 3. Risks

### RISK-1: Financial Reports Are Inaccurate
The Profit & Loss report understates profit when bonus goods are involved. This could lead to incorrect business decisions, wrong tax filings, or compliance issues.

### RISK-2: Stock Valuation on Balance Sheet Is Inflated
The expiry report and any stock valuation query will show higher asset values than reality. This could misrepresent the pharmacy's financial position.

### RISK-3: Reconciliation Audit Trail Is Misleading
The cost impact column in reconciliation journals uses the inflated batch price, making variances appear more costly than they are.

### RISK-4: Medicine-Level Analytics Are Wrong
The `TotalSpend` field in `MedicinePurchaseStats` overstates how much was spent on a medicine, making ROI and margin calculations unreliable.

### RISK-5: Batch Price Overwritten on Re-Inward (SEPARATE ISSUE)
When the same batch number is re-inwarded, `purchase_price` is overwritten (`purchase_repo.go:274`), not averaged. If the supplier charges a different price on the second inward, the batch cost changes abruptly, affecting all unsold stock from that batch.

### RISK-6: PO-Level Discount Not Allocated to Batches
`discount_total` on `purchase_orders` reduces the PO total but is never allocated to individual batches. `effectivePrice` only accounts for line-level discounts. A ₹500 PO discount across 5 items doesn't change any batch's `purchase_price`.

---

## 4. Proposed Model

### Option A: Store True Blended Cost in Batch (RECOMMENDED)

**Change `effectivePrice` calculation:**

```go
// purchase_repo.go:139-141 — CHANGE
effectivePrice := it.PurchasePrice
totalReceived := it.Quantity + it.BonusQuantity
if totalReceived > 0 && discAmount > 0 {
    effectivePrice = round2((gross - discAmount) / float64(totalReceived))
} else if totalReceived > 0 && it.BonusQuantity > 0 {
    effectivePrice = round2(gross / float64(totalReceived))
}
```

**Effect:** `batch.purchase_price` = true cost per unit received.

**Impact on downstream:**
| Component | Before | After | Status |
|-----------|--------|-------|--------|
| `report_repo.go:194` (P&L COGS) | Overstated cost | Correct | Fixed |
| `report_repo.go:253` (Expiry stock value) | Overstated | Correct | Fixed |
| `reconcile_repo.go:126` (cost impact) | Overstated | Correct | Fixed |
| `medicine_repo.go:255` (total spend) | Overstated | Needs fix (see below) | Partial |

### Option B: Store Paid-Unit Cost, Add Separate True Cost Column

Add `true_cost_per_unit NUMERIC(12,2)` to `batches` table. This preserves the original semantics for backward compatibility while providing accurate cost for reporting.

### Fix for `medicine_repo.go:255`

Regardless of which option is chosen, the purchase stats query should be:

```sql
-- Current (WRONG):
SUM((poi.quantity + poi.bonus_quantity) * poi.purchase_price)

-- Correct (actual spend = sum of line net amounts):
SUM(poi.quantity * poi.purchase_price - poi.discount_amount)
```

Or if using Option A where `poi.purchase_price` is stored as the paid-unit price:
```sql
SUM(CASE 
    WHEN poi.bonus_quantity > 0 THEN 
        (poi.quantity * poi.purchase_price * (1 - COALESCE(poi.discount_amount / NULLIF(poi.quantity * poi.purchase_price, 0), 0)))
    ELSE 
        (poi.quantity * poi.purchase_price - poi.discount_amount)
END)
```

### Option C: Hybrid — Store Both Prices

Add `paid_unit_price` and `true_unit_price` columns to `batches`. The first is what was paid per unit, the second is the blended cost. Use `true_unit_price` for all reporting, `paid_unit_price` for display.

---

## 5. Test Cases to Validate

### TC-1: Bonus without discount
```
Input:  quantity=10, bonus=2, price=50, no discount
Expected batch.purchase_price = 41.67 (500/12)
Expected batch.current_stock = 12
Expected po.total_amount = 500
```

### TC-2: Bonus with line discount
```
Input:  quantity=10, bonus=2, price=100, 10% discount
gross=1000, discount=100, net=900
Expected batch.purchase_price = 75.00 (900/12)
Expected batch.current_stock = 12
Expected po.total_amount = 900
```

### TC-3: No bonus, with discount (baseline)
```
Input:  quantity=20, bonus=0, price=100, 10% discount
Expected batch.purchase_price = 90.00 (1800/20)
Expected batch.current_stock = 20
```
This should remain unchanged from current behavior.

### TC-4: Profit & Loss accuracy
```
Setup:  Buy 10+2 at ₹100 each, sell all 12 at ₹150
Expected revenue = 1800
Expected COGS = 1000 (true cost)
Expected profit = 800
Expected margin = 44.4%
```

### TC-5: Expiry stock value
```
Setup:  Buy 10+2 at ₹100 each, sell 0
Expected stock_value = 1000 (not 1200)
```

### TC-6: Reconciliation cost impact
```
Setup:  Buy 10+2 at ₹100 each, reconcile -1 unit
Expected cost_impact = -83.33 (not -100)
```

### TC-7: Medicine purchase stats total spend
```
Setup:  Buy 10+2 at ₹100 each, 10% line discount
Expected total_spend = 900 (not 1200)
```

### TC-8: Tax on bonus (regression)
```
Input:  quantity=10, bonus=2, price=100, GST 18%
Expected tax base = 10 × 100 = 1000 (bonus NOT taxed)
This should remain unchanged.
```

### TC-9: Frontend total (regression)
```
Input:  quantity=10, bonus=2, price=50
Expected PO total = 500 (paid only)
This should remain unchanged.
```

---

## 6. Summary

| Area | Status | Severity |
|------|--------|----------|
| Bonus stock enters inventory correctly | ✅ Correct | — |
| PO total reflects paid amount | ✅ Correct | — |
| Tax excludes bonus units | ✅ Correct | — |
| Frontend total is correct | ✅ Correct | — |
| Validation (non-negative bonus) | ✅ Correct | — |
| `batch.purchase_price` ignores bonus in divisor | ❌ BUG | HIGH |
| P&L profit understated | ❌ BUG | HIGH |
| Expiry stock value overstated | ❌ BUG | MEDIUM |
| Reconciliation cost impact overstated | ❌ BUG | MEDIUM |
| Medicine purchase stats overstated | ❌ BUG | MEDIUM |
| PO-level discount not allocated to batches | ⚠️ RISK | LOW |
| Batch price overwritten on re-inward | ⚠️ RISK | LOW |

**Recommendation:** Adopt **Option A** (store true blended cost in `batch.purchase_price`) as it fixes all downstream consumers with a single change at the source, and aligns with standard pharmaceutical inventory accounting (weighted average cost).
