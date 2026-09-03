-- 014: Link medicines to their HSN/tax configuration (effective-dated)
CREATE TABLE medicine_tax_config (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    medicine_id       UUID NOT NULL REFERENCES medicines(id) ON DELETE CASCADE,
    hsn_code_id       UUID NOT NULL REFERENCES hsn_codes(id) ON DELETE CASCADE,
    tax_rate_id       UUID NOT NULL REFERENCES tax_rates(id) ON DELETE CASCADE,
    price_includes_tax BOOLEAN NOT NULL DEFAULT false,
    effective_from    DATE NOT NULL,
    effective_to      DATE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_medicine_tax_config_medicine ON medicine_tax_config (medicine_id);
CREATE INDEX idx_medicine_tax_config_effective ON medicine_tax_config (effective_from, effective_to);

-- Only one active (non-expired) tax config per medicine at a time
CREATE UNIQUE INDEX uq_medicine_tax_config_active ON medicine_tax_config (medicine_id)
    WHERE effective_to IS NULL;
