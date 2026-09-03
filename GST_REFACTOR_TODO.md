# GST Refactor TODO — PharmaPOS

## Phase 0 — Discovery ✅

* [x] Inspect repository structure — 65 source files, Go backend + React SPA
* [x] Inspect all 9 existing migrations (0001-0009)
* [x] Inspect current sales flow — `sale_repo.go:163-339`, atomic checkout with batch locking
* [x] Inspect current purchase flow — `purchase_repo.go:88-237`, batch upsert with inline medicine creation
* [x] Inspect customer/ledger flow — `customer_repo.go:111-169`, atomic credit limit enforcement
* [x] Inspect batch/inventory locking — `FOR UPDATE` with UUID sort order
* [x] Identify every place using `subtotal` — sale_repo.go:225,233; invoice_repo.go:114,130-131; medicine_repo.go:240,289; tests
* [x] Identify every place using `total_amount` — sale_repo.go:226,267-278; purchase_repo.go:123,168-173; report_repo.go; tests
* [x] Identify all invoice-number generation — sales: BIGSERIAL (DB auto), purchases: `PINV-{timestamp}` format
* [x] Identify frontend GST/pricing assumptions — NONE (zero GST code exists)
* [x] Identify tests already present — 21 Go tests, 9 frontend tests, 0 GST tests

## Phase 1 — Domain Design ✅

* [x] Define GST registration model
* [x] Define store → GST registration relationship
* [x] Define supplier model
* [x] Define customer GST fields
* [x] Define HSN model
* [x] Define tax-rate/effective-date model
* [x] Define medicine tax configuration
* [x] Define sales tax snapshot
* [x] Define purchase tax snapshot
* [x] Define invoice immutability rules
* [x] Define rounding policy

## Phase 2 — Database ✅

* [x] Add suppliers table (010)
* [x] Add business/gst_registrations (011)
* [x] Add stores table (012)
* [x] Add hsn_codes table (013)
* [x] Add tax_rates/effective-dated rules (014)
* [x] Add medicine_tax_config (015)
* [x] Alter customers for GST fields (016)
* [x] Alter sales_invoices for GST totals (017)
* [x] Alter sales_invoice_items for tax snapshots (018)
* [x] Add sales_credit_notes tables (019)
* [x] Alter purchase_orders for supplier_id + GST (020)
* [x] Alter purchase_order_items for tax snapshots (021)
* [x] Create seed data for default HSN codes and tax rates (021-seed)

## Phase 3 — Tax Engine ✅

* [x] Add shopspring/decimal dependency
* [x] Implement `internal/tax/types.go` — TaxInput, TaxResult, SupplyType, TaxConfig
* [x] Implement `internal/tax/calculator.go` — CalculateLineTax, CalculateInvoiceTax
* [x] Implement `internal/tax/rounding.go` — Centralized rounding with defined strategy
* [x] Implement `internal/tax/rules.go` — CGST+SGST vs IGST determination
* [x] Add unit tests for tax engine (Tests 1-8 from spec)

## Phase 4 — Sales Refactor ✅

* [x] Extend CheckoutInput with store_id / place_of_supply
* [x] Look up patient's GST config for each item's medicine
* [x] Determine CGST/SGST vs IGST from store vs customer state
* [x] Calculate tax server-side using tax engine
* [x] Store tax snapshots on sales_invoice_items
* [x] Store aggregated tax totals on sales_invoices
* [x] Preserve batch locking behavior
* [x] Preserve stock validation behavior
* [x] Preserve duplicate batch line merging
* [x] Preserve credit limit check (using grand_total)
* [x] Preserve customer ledger (using grand_total for credit)

## Phase 5 — Purchase Refactor ✅

* [x] Extend PurchaseInput with supplier_id, store_id, place_of_supply
* [x] Look up tax config for each item's medicine
* [x] Determine supply type from store vs supplier state
* [x] Calculate tax server-side using tax engine
* [x] Store tax snapshots on purchase_order_items
* [x] Store aggregated tax totals on purchase_orders
* [x] Preserve stock increment behavior
* [x] Preserve bonus quantity behavior
* [x] Preserve batch upsert behavior
* [x] Preserve per-line and PO-level discount behavior

## Phase 6 — API Changes ✅

* [x] Add supplier CRUD endpoints (GET, POST, PUT, DELETE)
* [x] Add SupplierRepo and TaxRepo repositories
* [x] Update checkout endpoint to accept store_id, place_of_supply
* [x] Update purchase endpoint to accept supplier_id, store_id, place_of_supply
* [x] Wire new repos into router and main.go
* [x] Server calculates all tax amounts (never trust frontend)

## Phase 7 — Frontend ✅ (types + API + basic UI)

* [x] Update TypeScript types for GST fields (Customer, InvoiceItem, CheckoutResponse, etc.)
* [x] Add Supplier TypeScript type
* [x] Update API client for supplier endpoints (list, create, update, delete)
* [x] Update checkout response display for GST totals
* [x] Update POS receipt to show supply type and tax total

## Phase 8 — Validation ✅

* [x] Run `go build ./...` — compiles clean
* [x] Run `go vet ./...` — passes clean
* [x] Run `npx tsc --noEmit` — TypeScript compiles clean
* [x] Tax engine unit tests (10/10 PASS)
* [x] GST integration tests written and PASSING (4/4 — intra-state, inter-state, graceful fallback, backward compatibility)
* [x] Existing Go tests — 26/26 PASS
* [ ] Run existing 9 frontend tests — needs backend
* [ ] Run `npm run test` (frontend)

## Remaining Work

### Frontend (deeper UI integration)
* [ ] Add supplier management page
* [ ] Update customer form for GSTIN, B2B/B2C, state
* [ ] Update medicine form for HSN code
* [ ] Update invoice detail modal for tax breakdown
* [ ] Update purchase form for supplier GST info
* [ ] Remove any client-side tax calculation authority

### Testing & Validation
* [ ] Verify no duplicate GST calculation logic
* [ ] Run `npm run test` (frontend)
