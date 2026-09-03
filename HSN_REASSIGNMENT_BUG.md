# HSN Reassignment Bug — Root Cause Report

## Status
Confirmed reproduced and fixed.

---

## 1. Reproduction Steps
1. Open the Medicines page, select an existing medicine that already has an HSN
   (e.g. `m brand`, which had HSN `1234`).
2. Open its Tax configuration editor, change the HSN to another existing HSN in
   the same store (e.g. `123456`) and set the desired GST%, then Save.
3. Observe the store-scoped database row for `medicine_tax_config` is correctly
   updated: the old config is closed (`effective_to` set) and a new **active**
   row (`effective_to IS NULL`) for the new HSN is inserted.
4. Re-read the medicine: `GET /api/medicines/:id` / `GET /api/medicines/:id/tax-config`
   frequently return the **OLD HSN**.
5. The Medicine page, Purchase page, and Billing page therefore display the old HSN.

## 2. Expected Behavior
After changing the medicine from HSN `3004` to HSN `3005` and saving:
- Server / database: medicine is associated with HSN `3005` (active config).
- `GET /api/medicines/:id/tax-config` returns HSN `3005`.
- Local IndexedDB: medicine reflects HSN `3005`.
- Medicine page, Purchase page, Billing page: all show HSN `3005`.
- Reload retains HSN `3005`.

## 3. Actual Behavior
The database is updated correctly (new active row with the new HSN is written and
the old row is closed), **but the current-config read paths return the old
closed row**, so the UI keeps showing HSN `3004`.

## 4. API Request Payload
`PUT /api/medicines/:id/tax-config`
```json
{
  "hsn_code_id": "<uuid of 3005, the newly selected HSN>",
  "tax_rate_id": "<uuid of the active tax rate for 3005>",
  "price_includes_tax": true
}
```
The frontend shares the same save path as creating a fresh config — it sends the
correct new `hsn_code_id`. It also first sends `PUT /api/hsn/:hsnId/tax-rate` to
ensure the new HSN has a rate.

## 5. API Response (put)
The `PUT` response returns the raw config row (id, medicine_id, hsn_code_id,
tax_rate_id, effective_from, effective_to, created_at) — the joined `hsn_code`
and `tax_rate` fields are only populated on the GET path.

The subsequent `GET /api/medicines/:id/tax-config` (and `GET /api/medicines/:id`
`tax_config`) can return the **old** HSN (see Root Cause).

## 6. Database State Before Update
Medicine `m brand`:
- Active config: HSN `1234`, `effective_from = 2026-08-29`, `effective_to = NULL`.
- `tax_rates`: one row for HSN `1234` (active).

## 7. Database State After Update
Medicine `m brand`:
- Old config: HSN `1234`, `effective_from = 2026-08-29`, `effective_to = 2026-08-30` (closed).
- New config: HSN `123456`, `effective_from = 2026-08-29`, `effective_to = NULL` (active).

**Critical:** both rows have the SAME `effective_from` (same-day reassignment).

## 8. IndexedDB State Before Update
`medicine_tax_cache` (key `medicine_id`):
- `m brand` → `{ hsn_code: "1234", tax_rate: { gst_rate: ... } }`

## 9. IndexedDB State After Update
Two writers:
- Shared `TaxEditor.tsx` (used by Purchases/POS) enriches the config with the new
  `hsn_code` + `tax_rate` before `upsertCachedMedicineTax` — this is correct.
- `Medicines.tsx` writes the **raw** `PUT` response (missing `hsn_code`/`tax_rate`)
  into the cache, but does not fetch the full detail again. The dominant problem is
  the **server read path** returning the old HSN (below), because `syncLocalCache()`
  (`db.ts:98-101`) repopulates `medicine_tax_cache` from `/api/sync/tax` on login,
  manual sync, and after purchases, and the sync snapshot is correct — but any page
  that reads the current config from the server (Medicine detail) gets the old HSN.

## 10. Root Cause (confirmed by reproduction)
The **read path** that resolves the *current* medicine tax config uses
`ORDER BY effective_from DESC LIMIT 1` with **no tie-breaker that prefers the
active (`effective_to IS NULL`) row**, at:

- `internal/repository/tax_repo.go:44` — `GetMedicineTaxConfig` (drives
  `GET /api/medicines/:id/tax-config` and `GET /api/medicines/:id` `tax_config`).
- `internal/repository/purchase_repo.go:591` — `taxConfigFromTx` (drives invoice
  tax resolution on checkout/purchase).

When a medicine is reassigned to a different HSN **on the same day**, the
`UpsertMedicineTaxConfig` close step records the old row as
`effective_to = CURRENT_DATE + 1`. Because of the date-only granularity:
- Old (closed) row: `effective_from = CURRENT_DATE`, `effective_to = CURRENT_DATE + 1`.
- New (active) row: `effective_from = CURRENT_DATE`, `effective_to = NULL`.

Both rows satisfy the as-of filter
`effective_from <= asOf AND (effective_to IS NULL OR effective_to >= asOf)` for
`asOf = today`, and both tie on `effective_from`. `ORDER BY effective_from DESC
LIMIT 1` then returns whichever row the planner happens to return first — the
**old closed row** — so the API returns the old HSN even though the DB is correct.

### Evidence (live reproduction)
Query used verbatim by `GetMedicineTaxConfig` for medicine `0e4c2610-...` (m brand):

```sql
SELECT mtc.id, h.code
FROM medicine_tax_config mtc
JOIN hsn_codes h ON h.id = mtc.hsn_code_id
WHERE mtc.medicine_id = '0e4c2610-678c-4401-8fd2-34e1c8449525'
  AND mtc.effective_from <= CURRENT_DATE
  AND (mtc.effective_to IS NULL OR mtc.effective_to >= CURRENT_DATE)
ORDER BY mtc.effective_from DESC
LIMIT 1;
```

Result returned HSN **1234** (old), while the medicine's active config row is
HSN **123456** (`effective_to IS NULL`). Repeated runs returned `1234` every time
because the closed row (same `effective_from`) is physically returned first on the
tie.

### Why Operation A (same HSN, new tax rate) still works
Operation A changes `tax_rates` only — it closes the old rate with
`effective_to = CURRENT_DATE + 1` and opens a new rate with the same HSN. The old
rate and new rate have the same `hsn_code_id`, so whichever `ORDER BY effective_from
DESC LIMIT 1` picks yields the same HSN; and `GetActiveTaxRate` (tax_repo.go:82-...)
selects `effective_to IS NULL` so it always returns the new rate. Operation A is
therefore unaffected — consistent with the report's note that fixing Operation A
does not fix Operation B.

---

## Fix
Add a deterministic tie-breaker so the **active** (`effective_to IS NULL`) row
always wins, then the newest `effective_from`:

```sql
ORDER BY (mtc.effective_to IS NULL) DESC, mtc.effective_from DESC, mtc.id
LIMIT 1
```

Applied to:
- `internal/repository/tax_repo.go:44` (`GetMedicineTaxConfig`)
- `internal/repository/purchase_repo.go:591` (`taxConfigFromTx`)

The `/api/sync/tax` snapshot (`ListStoreTaxSnapshot`) already selects active rows
via `effective_to IS NULL` and needs no change; this fix makes the current-config
read consistent with it.
</content>
