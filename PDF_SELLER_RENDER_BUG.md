# PDF Seller Render Bug — Debugging Note

## 1. Where seller data originates
The B2B invoice PDF handler (`internal/handlers/b2b_pdf.go`, `generateB2BInvoicePDF`)
builds `pdf.InvoiceData.Seller` from the **current store's live configuration**:

1. `TaxRepo.GetStore(storeID)` — store identity: `Name`, `Address`, `Phone`.
2. `TaxRepo.GetGSTRegistration(gstRegID)` — GST answers: `GSTIN`, `PAN`,
   `StateCode`, `StateName`, plus `TradeName`/`Address` used as fallbacks when
   the store row is empty.
3. The store id comes from the authenticated principal via `storeIDFor(c)`.

`pdf.InvoiceData` carries this into `GenerateInvoicePDF(...)`, where
`sellerLines()` renders: Name, GSTIN, PAN, Address, Phone, State.

## 2. What fields are populated
After the fix, when a store exists with a linked GST registration:
`Name`, `Address`, `Phone` (from stores), `GSTIN`, `PAN`, `StateCode`,
`StateName` (from gst_registrations) are all populated and rendered.

## 3. What fields were empty (before the fix)
`Seller.Phone` was **always empty**.

## 4. Where the data is lost
`TaxRepo.GetStore` (internal/repository/tax_repo.go) selected only:

```sql
SELECT id::text, gst_registration_id::text, name, address, is_active,
       created_at, updated_at FROM stores WHERE id = $1
```

The `stores` table gained a `phone` column in migration `034_shop_details.sql`
(`ALTER TABLE stores ADD COLUMN phone ...`), and the `models.Store.Phone` field
exists, but `GetStore` never selected or scanned `phone`. So `store.Phone`
was unconditionally `""`, and `seller.Phone` was therefore always empty — the
`Phone: ...` line in the SELLER / FROM box never rendered.

## 5. Root cause
`TaxRepo.GetStore` did not SELECT/Scan the `phone` column that exists on the
`stores` table. The PDF renderer and `sellerLines()` were correct; the seller
phone simply never reached `InvoiceData.Seller.Phone`.

Two secondary hard-coded fallbacks also violated the spec (a fabricated
"Pharmacy" identity) and were removed:
- `internal/handlers/b2b_pdf.go`: `SellerInfo{Name: "Pharmacy", ...}` default.
- `internal/pdf/invoice.go` `drawFooter`: `storeName = "Pharmacy"` when empty.

Additionally, the top-left of `drawHeaderBand` duplicated the seller
name/address, which is redundant with the SELLER / FROM box (the seller
identity now lives only in that box).

## 6. Fix applied
- `internal/repository/tax_repo.go` `GetStore`: added `phone` to the SELECT and
  Scan so the authoritative store phone populates `Seller.Phone`.
- `internal/handlers/b2b_pdf.go`: removed the hard-coded `"Pharmacy"` default;
  seller fields are now populated only from real store/GST-registration data.
- `internal/pdf/invoice.go` `drawHeaderBand`: removed the redundant seller
  name/address block; the header now shows a centered TAX INVOICE + Invoice
  No / Date / Financial Year / Supply, with full seller identity in the
  SELLER / FROM box.
- `internal/pdf/invoice.go` `drawFooter`: only draws "For <store name>" when a
  real name is configured; never a fabricated placeholder.

## 7. Table header bug — root cause & fix
The medicine table headers were rendered through `drawCellValue(...)`, which
auto-shrinks each header independently down to an unreadable 4.5pt when its
column is narrow (e.g. "Sell Price", "Bonus", "Discount"). Headers therefore
ended up at dramatically different sizes and were hard to read.

Fix in `internal/pdf/invoice.go`:
- Rebalanced `itemColumns` so every header fits at a **single uniform** bold
  size (7.5pt), never auto-shrunk per column. Medicine and Tax get the most
  width; compact headers are used (`SELL PRICE`, `BONUS`, `DISCOUNT`,
  `TAXABLE`, `TOTAL`).
- `drawHeaderRow` now centers each header at that uniform size with a slightly
  taller header band (`rowH = 7.6`), so no header is clipped or unreadably
  small.
- Column widths still sum exactly to `contentWidth` (180mm) — enforced by
  `TestColumnWidthsSumToContent`.
