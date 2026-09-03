-- 034: Shop details — optional store phone, drug license number and expiry.
-- The drug license belongs to the physical outlet (stores) and is store-scoped,
-- so it works even when a store has no GST registration yet. GSTIN/PAN stay on
-- gst_registrations (the canonical, filing-read home). All new columns are
-- non-destructive with safe defaults; existing store rows are preserved.

ALTER TABLE stores
    ADD COLUMN phone TEXT NOT NULL DEFAULT '',
    ADD COLUMN drug_license_number TEXT NOT NULL DEFAULT '',
    ADD COLUMN drug_license_expiry DATE;
