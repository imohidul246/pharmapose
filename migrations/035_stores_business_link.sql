-- 035: Link stores to their business directly.
-- Previously the store→business chain ran store → gst_registration → business,
-- which breaks for stores registered with no GSTIN/PAN (no gst_registration row).
-- A store now carries its own business_id so compliance/GSTIN/PAN can be
-- attached to an existing minimal store on demand. Backfilled from the
-- existing gst_registration link; non-destructive.

ALTER TABLE stores
    ADD COLUMN business_id UUID REFERENCES businesses(id) ON DELETE SET NULL;

UPDATE stores s
SET business_id = gr.business_id
FROM gst_registrations gr
WHERE gr.id = s.gst_registration_id
  AND s.business_id IS NULL;

CREATE INDEX idx_stores_business ON stores (business_id);
