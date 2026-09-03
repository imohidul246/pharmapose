-- 018: Create sales_credit_notes for future returns support
CREATE TABLE sales_credit_notes (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id        UUID NOT NULL REFERENCES sales_invoices(id) ON DELETE RESTRICT,
    note_no           BIGSERIAL UNIQUE,
    reason            TEXT NOT NULL DEFAULT '',
    gross_amount      NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    taxable_amount    NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    cgst_total        NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    sgst_total        NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    igst_total        NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    cess_total        NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    tax_total         NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    grand_total       NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sales_credit_notes_invoice ON sales_credit_notes (invoice_id);

CREATE TABLE sales_credit_note_items (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    credit_note_id    UUID NOT NULL REFERENCES sales_credit_notes(id) ON DELETE CASCADE,
    invoice_item_id   UUID REFERENCES sales_invoice_items(id) ON DELETE SET NULL,
    medicine_id       UUID NOT NULL REFERENCES medicines(id),
    batch_id          UUID NOT NULL REFERENCES batches(id),
    quantity          INT NOT NULL CHECK (quantity > 0),
    hsn_code          VARCHAR(20),
    taxable_value     NUMERIC(14,2) NOT NULL DEFAULT 0.00,
    gst_rate          NUMERIC(5,2) DEFAULT 0.00,
    cgst_amount       NUMERIC(14,2) DEFAULT 0.00,
    sgst_amount       NUMERIC(14,2) DEFAULT 0.00,
    igst_amount       NUMERIC(14,2) DEFAULT 0.00,
    cess_amount       NUMERIC(14,2) DEFAULT 0.00,
    line_total        NUMERIC(14,2) NOT NULL DEFAULT 0.00
);

CREATE INDEX idx_sales_credit_note_items_cn ON sales_credit_note_items (credit_note_id);
CREATE INDEX idx_sales_credit_note_items_medicine ON sales_credit_note_items (medicine_id);
