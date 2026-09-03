-- 024: Fix redundant/duplicate indexes and add performance indexes

-- Drop redundant index (HSN code already has UNIQUE constraint)
DROP INDEX IF EXISTS idx_hsn_codes_code;

-- Drop duplicate index (identical to idx_sales_invoices_created_at from 0004)
DROP INDEX IF EXISTS idx_sales_invoices_created_at_disc;
