-- 021: Seed default HSN codes and tax rates for common pharmacy products
-- This provides initial configuration; users can add/modify via the application

INSERT INTO hsn_codes (code, description) VALUES
    ('3004', 'Medicaments consisting of mixed or unmixed products for therapeutic or prophylactic uses, packed for retail sale'),
    ('3003', 'Medicaments consisting of mixed or unmixed products for therapeutic or prophylactic uses, not packed for retail sale'),
    ('3002', 'Human blood, animal blood, vaccines, toxins, cultures of micro-organisms'),
    ('3001', 'Glands and other organs for organotherapeutic uses, dried or powdered'),
    ('2106', 'Food preparations not elsewhere specified (nutraceuticals, supplements)'),
    ('9983', 'Other support services (pharmacy service charges)')
ON CONFLICT (code) DO NOTHING;

-- Insert default active tax rates for common HSN codes
-- These are current Indian GST rates as of 2024; effective_from is set to a safe past date

INSERT INTO tax_rates (hsn_code_id, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from)
SELECT h.id, 12.00, 6.00, 6.00, 12.00, 0.00, '2017-07-01'::date
FROM hsn_codes h WHERE h.code = '3004'
ON CONFLICT DO NOTHING;

INSERT INTO tax_rates (hsn_code_id, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from)
SELECT h.id, 12.00, 6.00, 6.00, 12.00, 0.00, '2017-07-01'::date
FROM hsn_codes h WHERE h.code = '3003'
ON CONFLICT DO NOTHING;

INSERT INTO tax_rates (hsn_code_id, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from)
SELECT h.id, 12.00, 6.00, 6.00, 12.00, 0.00, '2017-07-01'::date
FROM hsn_codes h WHERE h.code = '3002'
ON CONFLICT DO NOTHING;

INSERT INTO tax_rates (hsn_code_id, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from)
SELECT h.id, 0.00, 0.00, 0.00, 0.00, 0.00, '2017-07-01'::date
FROM hsn_codes h WHERE h.code = '3001'
ON CONFLICT DO NOTHING;

INSERT INTO tax_rates (hsn_code_id, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from)
SELECT h.id, 5.00, 2.50, 2.50, 5.00, 0.00, '2017-07-01'::date
FROM hsn_codes h WHERE h.code = '2106'
ON CONFLICT DO NOTHING;

INSERT INTO tax_rates (hsn_code_id, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from)
SELECT h.id, 18.00, 9.00, 9.00, 18.00, 0.00, '2017-07-01'::date
FROM hsn_codes h WHERE h.code = '9983'
ON CONFLICT DO NOTHING;
