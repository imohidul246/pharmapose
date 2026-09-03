-- 013: Create effective-dated tax rates
CREATE TABLE tax_rates (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hsn_code_id    UUID NOT NULL REFERENCES hsn_codes(id) ON DELETE CASCADE,
    gst_rate       NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    cgst_rate      NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    sgst_rate      NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    igst_rate      NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    cess_rate      NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    effective_from DATE NOT NULL,
    effective_to   DATE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tax_rates_hsn ON tax_rates (hsn_code_id);
CREATE INDEX idx_tax_rates_effective ON tax_rates (effective_from, effective_to);

-- Ensure only one active (non-expired) tax rate per HSN at a time
CREATE UNIQUE INDEX uq_tax_rates_active_per_hsn ON tax_rates (hsn_code_id)
    WHERE effective_to IS NULL;
