# API / Frontend GST & Bonus Quantity Audit

Generated: 2026-08-26

---

## 1. API Contract Analysis

### 1.1 Checkout API — POST /api/sales/checkout

| Field | Frontend `CheckoutRequest` (types.ts:63-69) | Backend `CheckoutInput` (sale_repo.go:39-47) |
|-------|------|---------|
| customer_id | `string?` | `*string` |
| payment_type | `PaymentType` | `models.PaymentType` |
| items | `CheckoutItemInput[]` | `[]CheckoutItemInput` |
| store_id | `string?` | `*string` |
| place_of_supply | `string?` | `*string` |

**Verdict: ✅ MATCH** — All fields align. Handler at `sales.go:12-24` binds via `ShouldBindJSON`.

### 1.2 Purchase API — POST /api/purchases

| Field | Frontend `CreatePurchaseRequest` (types.ts:267-277) | Backend `PurchaseInput` (purchase_repo.go:45-55) |
|-------|------|---------|
| invoice_no | `string?` | `string` |
| supplier_name | `string` | `string` |
| supplier_id | `string?` | `*string` |
| supplier_gstin | `string?` | `*string` |
| supplier_state | `string?` | `*string` |
| store_id | `string?` | `*string` |
| place_of_supply | `string?` | `*string` |
| discount_total | `number` | `float64` |
| items | `PurchaseLineInput[]` | `[]PurchaseItemInput` |

**Verdict: ✅ MATCH** — All fields align. Handler at `sales.go:27-40` binds via `ShouldBindJSON`.

### 1.3 Sales Invoice List — GET /api/sales/invoices

**Frontend** expects `SalesInvoiceRow` (types.ts:332-347):
```
id, invoice_no, customer_id, customer_name, payment_type,
total_amount, discount_total, item_count, units_sold, created_at,
supply_type, grand_total, tax_total
```

**Backend query** (invoice_repo.go:50-63) selects:
```
si.id, si.invoice_no, si.customer_id, si.payment_type,
si.total_amount, si.discount_total, si.created_at,
c.name, COUNT(sii.id), SUM(sii.quantity)
```

**🔴 CRITICAL: `supply_type`, `grand_total`, and `tax_total` are NOT selected.**
The Go struct `SalesInvoiceRow` embeds `models.SalesInvoice` which has these fields, but the SQL doesn't fetch them. They will always be zero-valued in the API response.

### 1.4 Sales Invoice Detail — GET /api/sales/invoices/:id

**Frontend** expects `SalesInvoiceDetail` (types.ts:354-378):
```
invoice: { all GST fields }, items: [{ all tax snapshot fields }]
```

**Backend invoice query** (invoice_repo.go:93-102) selects ONLY:
```
si.id, si.invoice_no, si.customer_id, si.payment_type,
si.total_amount, si.discount_total, si.created_at, c.name
```

**Backend items query** (invoice_repo.go:112-121) selects ONLY:
```
sii.id, sii.invoice_id, sii.medicine_id, sii.batch_id,
sii.quantity, sii.unit_sale_price, sii.subtotal,
sii.discount_type, sii.discount_value, sii.discount_amount,
m.name, b.batch_number
```

**🔴 CRITICAL: ALL GST fields are missing from both queries.** The invoice-level fields `supply_type`, `gross_amount`, `taxable_amount`, `cgst_total`, `sgst_total`, `igst_total`, `cess_total`, `tax_total`, `round_off`, `grand_total`, `price_includes_tax` will all be zero. The item-level fields `hsn_code`, `gross_amount`, `taxable_value`, `gst_rate`, `cgst_rate`, `cgst_amount`, `sgst_rate`, `sgst_amount`, `igst_rate`, `igst_amount`, `cess_rate`, `cess_amount`, `line_total` will all be zero/null.

### 1.5 Purchase Invoice List — GET /api/purchases/invoices

**Frontend** expects `PurchaseInvoiceRow` (types.ts:380-393):
```
id, invoice_no, supplier_name, total_amount, discount_total,
item_count, units_purchased, created_at,
supply_type, tax_total, grand_total
```

**Backend query** (invoice_repo.go:144-155) selects:
```
po.id, po.invoice_no, po.supplier_name,
po.total_amount, po.discount_total, po.created_at,
COUNT(poi.id), SUM(poi.quantity)
```

**🔴 CRITICAL: `supply_type`, `tax_total`, and `grand_total` are NOT selected.** Same issue as sales list.

### 1.6 Purchase Invoice Detail — GET /api/purchases/invoices/:id

**Frontend** expects `PurchaseInvoiceDetail` (types.ts:395-398):
```
invoice: PurchaseOrderInfo (all GST fields),
items: PurchaseOrderItemInfo[] (all tax snapshot fields)
```

**Backend invoice query** (invoice_repo.go:176-178) selects:
```
po.id, po.invoice_no, po.supplier_name, po.total_amount, po.discount_total, po.created_at
```

**Backend items query** (invoice_repo.go:188-197) selects:
```
poi.id, poi.purchase_id, poi.medicine_id, poi.batch_number,
poi.expiry_date, poi.quantity, poi.bonus_quantity,
poi.purchase_price, poi.sale_price,
poi.discount_type, poi.discount_value, poi.discount_amount, m.name
```

**🔴 CRITICAL: ALL GST fields missing from both invoice and items queries.** Purchase order fields `supplier_id`, `supplier_gstin`, `supplier_state_code`, `store_id`, `supply_type`, `gross_amount`, `taxable_amount`, `cgst_total`, `sgst_total`, `igst_total`, `cess_total`, `tax_total`, `grand_total`, `price_includes_tax` all zero. Item fields `hsn_code`, `gross_amount`, `taxable_value`, `gst_rate`, `cgst_rate`, `cgst_amount`, `sgst_rate`, `sgst_amount`, `igst_rate`, `igst_amount`, `cess_rate`, `cess_amount`, `line_total` all zero/null.

---

## 2. Frontend Tax Calculations

### 2.1 POS.tsx — Pre-Tax Cart Total (line 147)

```ts
const total = cart.reduce((acc, l) => acc + lineGross(l) - lineDiscountAmount(l), 0)
```

This computes `quantity × salePrice − discount` per line, summed. This is a **pre-tax** total. Used for:

| Usage | Location | Concern |
|-------|----------|---------|
| Footer display | POS.tsx:479 | Display only — shows pre-tax amount labeled "Payable total" |
| Credit limit check | POS.tsx:151-157 | **🟡 BUG**: Uses pre-tax `total` for `projectedBalance`. Server uses `chargeableTotal = grand_total` (post-tax) at sale_repo.go:336-340. A sale that appears within credit limit on the frontend could be **rejected by the server** if GST pushes it over. |
| Checkout button text | POS.tsx:551 | Display only — button says "Complete Sale — ₹X" with pre-tax amount |

### 2.2 Purchases.tsx — Pre-Tax Invoice Total (line 203-206)

```ts
const totalGross = lines.reduce((acc, l) => acc + l.quantity * l.purchasePrice, 0)
const total = Math.max(0, totalGross - totalLineDiscount - poDiscountNum)
```

Display only. Server recalculates and applies tax. **Not a correctness bug** but the display will differ from `grand_total`.

### 2.3 Invoices.tsx — Sales Detail Gross (line 542)

```ts
const gross = items.reduce((a, it) => a + it.quantity * it.unit_sale_price, 0)
```

Used as fallback for `inv.gross_amount` at line 619: `₹{money(inv.gross_amount ?? gross)}`. Since the backend doesn't return `gross_amount` (see §1.4), the frontend always falls back to this local computation. The local computation is `quantity × unit_sale_price` which is the **pre-discount, pre-tax gross** — equivalent to what the server computes before discount and tax. This is functionally correct for the "Gross amount" label.

### 2.4 Invoices.tsx — Purchase Detail Line Amount (line 766)

```ts
₹{money(it.quantity * it.purchase_price - it.discount_amount)}
```

**🟡 ISSUE**: This computes line total locally without tax. If tax is configured on the server, the actual `line_total` stored in the database includes tax. The frontend ignores `it.line_total` (which the backend doesn't return anyway due to the missing SELECT). So this display is **pre-tax** while the server's invoice totals (`grand_total`) are **post-tax** — creating an inconsistency in the detail modal between line-level and invoice-level amounts.

---

## 3. Type Mismatches

### 3.1 Go Structs vs Frontend Types — Field-Level Comparison

| Frontend Type | Go Struct | Status |
|---------------|-----------|--------|
| `Batch` (types.ts:1-9) | `models.Batch` (models.go:108-118) | ✅ Frontend omits `created_at`/`updated_at` — acceptable |
| `MedicineWithBatches` (types.ts:11-19) | `models.MedicineWithBatches` (models.go:121-124) | ✅ Match, but **neither has tax config fields** |
| `Customer` (types.ts:21-34) | `models.Customer` (models.go:126-140) | ✅ Match |
| `SalesInvoice` (types.ts:98-121 inline) | `models.SalesInvoice` (models.go:153-178) | ✅ Match |
| `InvoiceItem` / `SalesInvoiceItem` (types.ts:71-96) | `models.SalesInvoiceItem` (models.go:180-206) | ✅ Match |
| `PurchaseOrderInfo` (types.ts:279-301) | `models.PurchaseOrder` (models.go:208-232) | ✅ Match |
| `PurchaseOrderItemInfo` (types.ts:303-330) | `models.PurchaseOrderItem` (models.go:234-264) | ✅ Match |
| `PurchaseLineInput` (types.ts:238-265) | `PurchaseItemInput` (purchase_repo.go:26-43) | ✅ Match |
| `CreatePurchaseRequest` (types.ts:267-277) | `PurchaseInput` (purchase_repo.go:45-55) | ✅ Match |
| `CheckoutRequest` (types.ts:63-69) | `CheckoutInput` (sale_repo.go:39-47) | ✅ Match |

### 3.2 JSON Tag Alignment

All frontend field names use `snake_case` which matches Go `json:"..."` tags. No naming mismatches found.

### 3.3 Structural Mismatch: `CheckoutResult` vs `CheckoutResponse`

Go `CheckoutResult` (sale_repo.go:49-52):
```go
type CheckoutResult struct {
    Invoice models.SalesInvoice       `json:"invoice"`
    Items   []models.SalesInvoiceItem `json:"items"`
}
```

Frontend `CheckoutResponse` (types.ts:98-121): defines `invoice` inline with all fields. **Structurally equivalent** — Go serializes the embedded struct fields into the JSON object.

---

## 4. Display Issues

### 4.1 POS Receipt (POS.tsx:263-351)

The receipt renders correctly with a conditional GST summary block (lines 281-341). When `checkout()` returns, the `CheckoutResult` includes fully populated invoice + items from the transaction, so **the POS receipt WILL show correct GST data**. This is the one bright spot — the checkout handler populates all fields before returning.

- ✅ Shows supply type, gross, discount, taxable, IGST/CGST+SGST, tax total, grand total
- ✅ Graceful fallback for no-GST records (lines 342-349)

### 4.2 Purchase Result Banner (Purchases.tsx:696-714)

```tsx
<span className="font-mono font-semibold">₹{money(result.total_amount)}</span>
```

**🟡 Missing GST breakdown.** The `PurchaseOrderInfo` returned by `createPurchase` includes all GST fields (the handler populates them), but the success banner only shows `total_amount`. The `grand_total` and `tax_total` are available but unused.

### 4.3 Invoice List — Both Sales and Purchases

Both `SalesInvoiceRow` and `PurchaseInvoiceRow` lists only show `total_amount` (lines 222 and 292 of Invoices.tsx). Even if the backend query were fixed to select GST fields, the list view doesn't have columns for `grand_total` or `tax_total`. This is arguably fine for a summary view, but the **total shown is pre-tax** while the detail modal would show post-tax `grand_total`.

### 4.4 Invoice Detail Modals — Empty GST Sections

Both `SalesInvoiceModal` (Invoices.tsx:560-654) and `PurchaseInvoiceModal` (Invoices.tsx:704-825) have complete GST breakdown UI:

**Sales modal shows:**
- Supply type, gross amount, discount, taxable value, IGST/CGST+SGST, tax total, grand total
- Per-item: HSN code, tax rate, discount, amount

**Purchase modal shows:**
- Supplier GSTIN, supply type, PO discount, taxable value, IGST/CGST+SGST, tax total, grand total
- Per-item: HSN code, bonus quantity, tax rate, discount, computed amount

**🔴 None of this data will ever display** because the backend queries don't SELECT the GST columns. The UI will show the section but with all values as `—` or zero, or skip the sections entirely (e.g., `inv.supply_type` will be empty so the Supply meta won't render).

### 4.5 Purchase Invoice Line Amount Discrepancy

Invoices.tsx line 766:
```tsx
₹{money(it.quantity * it.purchase_price - it.discount_amount)}
```

This locally computes `quantity × purchasePrice − discount`. The server stores `line_total` which may include tax. Since the backend doesn't return `line_total` (not in SELECT), and the frontend doesn't use it, this is a **pre-tax line total** displayed alongside an invoice-level `grand_total` that includes tax (if it were returned). **Visual inconsistency** once the backend queries are fixed.

---

## 5. Missing Features

### 5.1 No Store / Place-of-Supply Selection in POS

POS.tsx line 171-172:
```ts
store_id: import.meta.env.VITE_STORE_ID || undefined,
place_of_supply: selectedCustomer?.state_code || undefined,
```

- `store_id` is baked into the build via env var — no UI to select store
- `place_of_supply` defaults to customer's state — no override for walk-in sales
- **Impact**: Walk-in cash sales have no `place_of_supply`, so the server defaults to intra-state (`SupplyTypeIntraState` at sale_repo.go:218). This may be incorrect for customers from other states.

### 5.2 No HSN Code Display in POS Cart

The POS cart (POS.tsx:377-469) shows medicine name, batch, expiry, price, quantity, discount. It does **not** show HSN code or tax rate per line. The `MedicineWithBatches` type has no tax info (see §6).

### 5.3 No Tax-Inclusive Pricing Toggle

The POS assumes `sale_price` is always tax-exclusive. There's no UI to indicate `price_includes_tax` for a medicine. The server handles this via `taxConfig.PriceIncludesTax` (sale_repo.go:289), but the frontend can't preview the difference.

### 5.4 No GST Summary on Purchase Entry

The purchase form (Purchases.tsx) shows line-level gross and discount but no tax preview. The "Purchase total" (line 674-676) is purely pre-tax. After submission, the result banner also doesn't show tax (see §4.2).

### 5.5 Reports Don't Show Tax Breakdown

Reports.tsx:
- **Sales** (line 126): Shows `net_sales` — no GST/composition split
- **P&L** (line 188-204): Shows revenue, cost, profit, margin — no tax component
- **Purchases** (line 156): Shows `total_spend` — no GST split

The `SalesReport`, `PurchaseReport`, and `ProfitLossReport` types have no GST fields. This may be intentional (top-level summary) but there's no drill-down for GST returns.

### 5.6 No Customer GSTIN in POS for E-Invoicing

The POS shows customer name/phone for credit sales (POS.tsx:516-518) but doesn't display or validate the customer's GSTIN. For B2B sales requiring e-invoicing, there's no way to confirm GSTIN at checkout.

---

## 6. IndexedDB Concerns

### 6.1 Medicine Cache Has No Tax Configuration

**Sync endpoint** (`/api/sync/inventory`): Calls `MedicineRepo.InventorySnapshot()` (medicine_repo.go:124-192).

**Query** (medicine_repo.go:125-135):
```sql
SELECT m.id, m.name, ..., b.id, b.batch_number, b.expiry_date, ...
FROM medicines m
LEFT JOIN batches b ON ...
```

**No JOIN** with `medicine_tax_config`, `hsn_codes`, or `tax_rates`.

**Frontend type** `MedicineWithBatches` (types.ts:11-19): No tax fields.

**IndexedDB schema** (db.ts:4-8): Stores `MedicineWithBatches` — no tax data.

**Impact**:
- POS cannot preview tax amounts before checkout
- POS cannot display HSN/tax rate in the cart
- POS cannot validate whether a price includes tax
- The cache is purely for stock and pricing — all tax logic lives server-side

### 6.2 Tax Config Changes Don't Invalidate Cache

If an admin changes a medicine's HSN code or tax rate:
- The IndexedDB cache is **not** invalidated (it only stores medicine + batch data, not tax config)
- The server picks up the new tax config at checkout time (sale_repo.go:274)
- **Correct for computation** — the server always uses the current tax config
- **Incorrect for display** — the POS has no way to show the updated tax rate

### 6.3 Batch Stock Staleness

POS.tsx line 23: `maxStock: batch.current_stock` — captured from cache at add-to-cart time.
POS.tsx line 104: `quantity: 1` — user starts at 1.

If a concurrent sale reduces stock, the POS allows the user to add up to the stale `maxStock`. The **server enforces actual stock** (sale_repo.go:248-255) and rejects insufficient stock. The user fills the cart, hits checkout, and gets an error. This is a known tradeoff for offline-capable POS design.

---

## 7. Report Accuracy

### 7.1 Medicine Purchase Stats — Bonus Quantity Bug

medicine_repo.go lines 253-262:
```sql
SELECT COALESCE(SUM(poi.quantity + poi.bonus_quantity), 0)::int,
       COALESCE(SUM((poi.quantity + poi.bonus_quantity) * poi.purchase_price), 0)::float8,
       COUNT(DISTINCT po.id)::int
FROM purchase_order_items poi
```

**🔴 BUG: `total_spend` includes bonus quantity at purchase price.** `bonus_quantity` items are **free** — they don't contribute to spend. The correct formula should be:
```sql
COALESCE(SUM(poi.quantity * poi.purchase_price), 0)::float8
```

This inflates the `total_spend` reported on the medicine detail page (MedicineDetail.purchase_stats).

### 7.2 P&L Cost Calculation

The `ProfitLossLine.cost` comes from the server (Reports.tsx:193). Without seeing the P&L query, we note that if it uses the inflated `total_spend` from §7.1, the **cost column and margin will be wrong** for medicines received with bonus quantity.

### 7.3 Purchase Report Total

Reports.tsx line 156: `₹{money(purchases?.total_spend ?? 0)}` — uses `PurchaseReport.total_spend`. This is server-computed. If it sums `po.total_amount` (pre-tax, pre-bonus), it's correct for accounting. If it sums something else, it needs verification.

### 7.4 Invoice List Totals

Invoices.tsx line 72-73:
```ts
const totalSales = sales.reduce((a, s) => a + s.total_amount, 0)
const totalPurchases = purchases.reduce((a, p) => a + p.total_amount, 0)
```

These use `total_amount` (pre-tax). Since `grand_total` isn't returned by the list queries (see §1.3, §1.5), this is the only option. Once the queries are fixed, these should be updated to use `grand_total`.

---

## Summary of Critical Findings

| # | Severity | Finding | Files |
|---|----------|---------|-------|
| 1 | 🔴 Critical | Sales invoice detail query doesn't SELECT GST fields — detail modal shows all zeros | invoice_repo.go:93-121 |
| 2 | 🔴 Critical | Purchase invoice detail query doesn't SELECT GST fields — detail modal shows all zeros | invoice_repo.go:176-197 |
| 3 | 🔴 Critical | Sales invoice list query doesn't SELECT `supply_type`, `grand_total`, `tax_total` | invoice_repo.go:50-63 |
| 4 | 🔴 Critical | Purchase invoice list query doesn't SELECT `supply_type`, `grand_total`, `tax_total` | invoice_repo.go:144-155 |
| 5 | 🔴 Critical | Medicine purchase stats `total_spend` includes bonus quantity × purchase_price | medicine_repo.go:255 |
| 6 | 🟡 Medium | POS credit limit check uses pre-tax total; server uses post-tax grand_total | POS.tsx:151-157 vs sale_repo.go:336-340 |
| 7 | 🟡 Medium | Purchase result banner shows `total_amount` instead of `grand_total` | Purchases.tsx:704 |
| 8 | 🟡 Medium | Invoice detail line amount computed locally without tax | Invoices.tsx:766 |
| 9 | 🟡 Medium | Medicine cache (IndexedDB) has no tax configuration data | medicine_repo.go:125-135, db.ts |
| 10 | ⚪ Low | No store/place-of-supply UI in POS for walk-in sales | POS.tsx:171-172 |
| 11 | ⚪ Low | No HSN/tax rate display in POS cart or purchase entry | POS.tsx, Purchases.tsx |
