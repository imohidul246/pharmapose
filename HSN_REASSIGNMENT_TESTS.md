# HSN Reassignment (Operation B) — Regression Tests

This document lists the regression tests added to guard against the HSN
reassignment bug. Each test fails against the pre-fix behavior (see
[`HSN_REASSIGNMENT_BUG.md`](./HSN_REASSIGNMENT_BUG.md)) and passes with the fix in
place.

## Root summary

The bug: after reassigning a medicine to a different HSN **on the same day**, the
server read path (`GetMedicineTaxConfig` and `taxConfigFromTx`) returned the OLD
closed config because both the old (closed) and new (active) rows tied on
`effective_from` and the query had no tie-breaker preferring the active row.

Fixed by ordering on `(mtc.effective_to IS NULL) DESC, mtc.effective_from DESC, mtc.id`
in both read queries (`internal/repository/tax_repo.go:44` and
`internal/repository/purchase_repo.go:591`).

---

## Backend regression tests

File: `internal/repository/tax_repo_test.go`

### `TestMedicineTaxConfigReassignmentReturnsNewHSN`
- Creates two dedicated HSNs (`9971`, `9972`) with rates 12% and 18%.
- Creates a medicine and assigns it to HSN 9971, then **reassigns** it to HSN 9972
  the same day (both config rows share `effective_from`, old row closed via
  `effective_to = CURRENT_DATE + 1`).
- Verifies `GetMedicineTaxConfigByMedicine` resolves HSN **9972** (the active/new
  row), that `EffectiveTo` is nil (active row), and that the resolved rate is 18%.
- **Pre-fix failure:** resolved HSN = `"9971"` (the old one) — reproduced the bug.

### `TestMedicineTaxConfigReassignmentResolvesFromDB`
- Same-day reassignment 9973 → 9974; verifies `GetMedicineTaxConfig(..., time.Now())`
  (the as-of read behind `/api/medicines/:id/tax-config`) returns HSN 9974 and the
  active row.

### `TestMedicineTaxConfigReassignmentIsStoreScoped`
- Verifies the reassignment is store-scoped: writes exactly one ACTIVE config in
  store A for the medicine, carries the reassigned `price_includes_tax`, and never
  leaks an active config into a second store.

> Test data isolation: dedicated HSN codes `>= 9970` are used so the shared
> reference data (3004/2106/9983 seeded by migrations) is never mutated.

---

## Frontend regression tests

File: `web/src/components/TaxEditor.reassign.test.tsx`

Renders the real `TaxEditor` against `fake-indexeddb` + a stubbed `fetch`, following
the `db.test.ts` / `GSTReportsPage.test.tsx` mock conventions (no `vi.mock`).

### Test 1 — "persists the reassigned HSN to the API and the local cache (not the old one)"
- Renders `TaxEditor` given an existing config for HSN `3004` (old).
- Selects HSN `3005` (new) in the dropdown and clicks "Save tax config".
- Asserts:
  - The `PUT /api/medicines/:id/tax-config` body carries `hsn_code_id = <hsn-B>`
    (the **new** HSN), not the old one.
  - The real IndexedDB `medicine_tax_cache` entry for the medicine has
    `hsn_code = "3005"` (so Medicine/Purchases/Billing pages render the new HSN).
  - `onSaved` is called with the enriched config whose `hsn_code = "3005"`.

### Test 2 — "does not send the old HSN id when saving a reassignment"
- Guards against the regression where the old `hsn_code_id` is sent on reassignment.

---

## How to run

Backend (must be run with `-p 1` — concurrent test packages against the shared
`pms_test` DB cause unrelated deadlocks):

```bash
cd /home/mohi/dev/pms-marg-inspired
go build ./... && go vet ./...
go test -count=1 -p 1 ./internal/repository/ ./internal/gst/

# just the new regression tests
go test -count=1 -p 1 -run 'TestMedicineTaxConfig' -v ./internal/repository/
```

Frontend:

```bash
cd /home/mohi/dev/pms-marg-inspired/web
npx vitest run
npx vitest run src/components/TaxEditor.reassign.test.tsx   # just the new regression tests
npm run build
```

## Current results
- Backend: `internal/repository` + `internal/gst` pass (fresh `-count=1`), new
  HSN tests all PASS.
- Frontend: `8` test files / `43` tests pass, including the 2 new TaxEditor
  regression tests; `npm run build` succeeds.
- Confirmed the regression tests catch the bug: reverting just the read-path
  `ORDER BY` makes `TestMedicineTaxConfigReassignmentReturnsNewHSN` fail with the
  old HSN, and re-applying the fix makes it pass.
</content>
