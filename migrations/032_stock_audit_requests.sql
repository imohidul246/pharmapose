-- 032: Stock audit (reconciliation) approval workflow.
-- Employees record the physically-counted stock on each batch; the owner's
-- approval applies the variance exactly once. The variance is computed against
-- a snapshot of system stock taken at submission time, so a stale audit (stock
-- has moved since then) is rejected with re-validation instead of silently
-- overwriting live stock.

CREATE TABLE stock_audit_requests (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id         UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    requested_by     UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status           purchase_request_status NOT NULL DEFAULT 'PENDING',
    notes            TEXT NOT NULL DEFAULT '',
    journal_id       UUID REFERENCES reconciliation_journals(id) ON DELETE SET NULL,
    reviewed_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at      TIMESTAMPTZ,
    rejection_reason TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_audit_reviewed CHECK (
        (status = 'PENDING'   AND reviewed_by IS NULL AND journal_id IS NULL    AND rejection_reason = '') OR
        (status = 'REJECTED'  AND reviewed_by IS NOT NULL AND journal_id IS NULL AND rejection_reason <> '') OR
        (status = 'CANCELLED' AND reviewed_by IS NULL AND journal_id IS NULL    AND rejection_reason = '') OR
        (status = 'APPROVED'  AND reviewed_by IS NOT NULL AND journal_id IS NOT NULL AND rejection_reason = '')
    )
);

CREATE TABLE stock_audit_request_items (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id        UUID NOT NULL REFERENCES stock_audit_requests(id) ON DELETE CASCADE,
    medicine_id       UUID NOT NULL REFERENCES medicines(id),
    batch_id          UUID NOT NULL REFERENCES batches(id),
    batch_number      VARCHAR(100) NOT NULL,
    system_quantity   INT NOT NULL,
    physical_quantity INT NOT NULL CHECK (physical_quantity >= 0),
    reason            TEXT NOT NULL DEFAULT '',
    UNIQUE (request_id, batch_id)
);

CREATE INDEX idx_stock_audit_requests_store_status ON stock_audit_requests (store_id, status, created_at DESC);
CREATE INDEX idx_stock_audit_items_request ON stock_audit_request_items (request_id);