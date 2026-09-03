-- 028: GSTR-2B document types are 'INV' / 'CRN' / 'DBN' (3 characters) but
-- migration 027 created the column as VARCHAR(2). Widen it for both fresh
-- installs (via the corrected 027) and databases that already ran 027.
ALTER TABLE gstr2b_imports
    ALTER COLUMN doc_type TYPE VARCHAR(3);