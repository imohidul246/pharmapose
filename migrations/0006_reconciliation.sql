CREATE TABLE reconciliation_journals (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    verified_by_user_id UUID,
    notes              TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE reconciliation_items (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    journal_id        UUID NOT NULL REFERENCES reconciliation_journals(id) ON DELETE CASCADE,
    medicine_id       UUID NOT NULL REFERENCES medicines(id),
    batch_id          UUID NOT NULL REFERENCES batches(id),
    system_stock      INT NOT NULL,
    physical_stock    INT NOT NULL CHECK (physical_stock >= 0),
    variance_quantity INT NOT NULL,
    cost_impact       NUMERIC(14,2) NOT NULL DEFAULT 0.00
);

CREATE INDEX idx_reconciliation_items_journal_id ON reconciliation_items (journal_id);
CREATE INDEX idx_reconciliation_journals_created_at ON reconciliation_journals (created_at);
