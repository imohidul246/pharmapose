-- 031: Purchase approval workflow.
-- Employees submit purchase REQUESTS; no stock moves until the store owner
-- approves. The request snapshots the entire proposed inward verbatim, and the
-- approval replays it through the same transaction that direct inwards use, so
-- approval and inventory update commit atomically and exactly once.

CREATE TYPE purchase_request_status AS ENUM ('PENDING', 'APPROVED', 'REJECTED', 'CANCELLED');

CREATE TABLE purchase_requests (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id          UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    requested_by      UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status            purchase_request_status NOT NULL DEFAULT 'PENDING',
    purchase_snapshot JSONB NOT NULL,
    purchase_id       UUID REFERENCES purchase_orders(id) ON DELETE SET NULL,
    reviewed_by       UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at       TIMESTAMPTZ,
    rejection_reason  TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A request is either awaiting review, cancelled by its creator, rejected
    -- with a reason, or approved with a real purchase attached — never a mix.
    CONSTRAINT chk_purchase_request_reviewed CHECK (
        (status = 'PENDING'  AND reviewed_by IS NULL    AND purchase_id IS NULL    AND rejection_reason = '') OR
        (status = 'REJECTED' AND reviewed_by IS NOT NULL AND purchase_id IS NULL   AND rejection_reason <> '') OR
        (status = 'CANCELLED' AND reviewed_by IS NULL   AND purchase_id IS NULL    AND rejection_reason = '') OR
        (status = 'APPROVED' AND reviewed_by IS NOT NULL AND purchase_id IS NOT NULL AND rejection_reason = '')
    )
);

CREATE INDEX idx_purchase_requests_store_status ON purchase_requests (store_id, status, created_at DESC);
CREATE INDEX idx_purchase_requests_requester ON purchase_requests (requested_by);