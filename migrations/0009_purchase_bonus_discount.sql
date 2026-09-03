-- Bonus stock (Buy X Get Y Free) support on purchase orders.
ALTER TABLE purchase_order_items
    ADD COLUMN bonus_quantity INT NOT NULL DEFAULT 0;

-- Per-line distributor discount on purchase order items (mirrors sales discount schema).
ALTER TABLE purchase_order_items
    ADD COLUMN discount_type   TEXT         NOT NULL DEFAULT 'NONE',
    ADD COLUMN discount_value  NUMERIC(12,2) NOT NULL DEFAULT 0.00,
    ADD COLUMN discount_amount NUMERIC(14,2) NOT NULL DEFAULT 0.00;

-- PO-level flat discount on the entire purchase order.
ALTER TABLE purchase_orders
    ADD COLUMN discount_total NUMERIC(14,2) NOT NULL DEFAULT 0.00;
