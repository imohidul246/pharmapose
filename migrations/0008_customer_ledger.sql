CREATE TABLE customer_ledger (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id   UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    entry_type    TEXT NOT NULL CHECK (entry_type IN ('CREDIT_SALE', 'PAYMENT', 'ADJUSTMENT')),
    amount        NUMERIC(14,2) NOT NULL,
    balance_after NUMERIC(14,2) NOT NULL,
    notes         TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_customer_ledger_customer ON customer_ledger (customer_id, created_at);

-- Backfill: every historical credit sale becomes a ledger entry so existing
-- balances are fully explainable. Running balance computed per customer in
-- chronological order.
INSERT INTO customer_ledger (customer_id, entry_type, amount, balance_after, notes, created_at)
SELECT customer_id,
       'CREDIT_SALE',
       amount,
       running_balance,
       'Invoice #' || invoice_no,
       created_at
FROM (
    SELECT si.customer_id,
           si.total_amount::float8 AS amount,
           SUM(si.total_amount) OVER (
               PARTITION BY si.customer_id
               ORDER BY si.created_at, si.id
               ROWS UNBOUNDED PRECEDING
           )::float8 AS running_balance,
           si.invoice_no,
           si.created_at
    FROM sales_invoices si
    WHERE si.payment_type = 'CREDIT' AND si.customer_id IS NOT NULL
) historic;
