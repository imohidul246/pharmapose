-- 027: GST Compliance Layer — Input Tax Credit, GSTR-3B basis and GSTR-2B reconciliation.
-- Adds ITC fields to purchases and stores imported GSTR-2B documents (GSTN's
-- view of our supplier invoices) so ITC eligibility and reconciliation are
-- explicit and auditable rather than assumed.

-- 1. Purchase-side GST fields.
--    reverse_charge marks supplies where the recipient (the pharmacy) is liable
--    to pay tax under reverse charge e.g. goods transport / specified services.
--    itc_eligible records whether the tax on this inward supply may be claimed.
--    ITC is NEVER assumed: it must be explicitly recorded when the inward is
--    posted, and the claimed amount is what actually flows into GSTR-3B.
ALTER TABLE purchase_orders
    ADD COLUMN reverse_charge  BOOLEAN      NOT NULL DEFAULT FALSE,
    ADD COLUMN itc_eligible    BOOLEAN      NOT NULL DEFAULT TRUE,
    ADD COLUMN itc_amount      NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    ADD COLUMN place_of_supply VARCHAR(2)   NOT NULL DEFAULT '';

-- Performance + range index for GSTR-3B purchase aggregation.
CREATE INDEX idx_po_invoice_date_store ON purchase_orders (invoice_date, store_id)
    WHERE store_id IS NOT NULL;

-- 2. GSTR-2B imports. One row per document (invoice / credit note / debit note)
--    reported by suppliers in GSTN's GSTR-2B for a tax period. The reconciliation
--    work ties each document back to the matching purchase_orders row.
CREATE TABLE gstr2b_import_batches (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id      UUID REFERENCES stores(id) ON DELETE CASCADE,
    gstin         VARCHAR(15) NOT NULL,              -- supplier GSTIN this batch covers
    period        VARCHAR(7)  NOT NULL,              -- 'YYYY-MM' return period
    file_name     TEXT        NOT NULL DEFAULT '',
    doc_count     INT         NOT NULL DEFAULT 0,
    matched_count INT         NOT NULL DEFAULT 0,
    unmatched_count INT       NOT NULL DEFAULT 0,
    status        VARCHAR(20) NOT NULL DEFAULT 'IMPORTED', -- IMPORTED / RECONCILED
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE gstr2b_imports (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    import_batch_id    UUID REFERENCES gstr2b_import_batches(id) ON DELETE CASCADE,
    store_id           UUID REFERENCES stores(id) ON DELETE CASCADE,
    supplier_gstin     VARCHAR(15) NOT NULL,
    doc_type           VARCHAR(3)  NOT NULL DEFAULT 'INV', -- INV / CRN / DBN
    period             VARCHAR(7)  NOT NULL,
    invoice_no         VARCHAR(40) NOT NULL,
    invoice_date       DATE        NOT NULL,
    taxable_value      NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    igst_amount        NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    cgst_amount        NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    sgst_amount        NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    cess_amount        NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    total_value        NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    match_status       VARCHAR(20)  NOT NULL DEFAULT 'UNMATCHED', -- UNMATCHED / MATCHED / AMOUNT_MISMATCH
    matched_purchase_id UUID REFERENCES purchase_orders(id) ON DELETE SET NULL,
    matched_difference NUMERIC(14,2),
    notes              TEXT,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_gstr2b_imports_batch ON gstr2b_imports (import_batch_id);
CREATE INDEX idx_gstr2b_imports_supplier ON gstr2b_imports (supplier_gstin, invoice_no);
CREATE INDEX idx_gstr2b_imports_status ON gstr2b_imports (match_status);