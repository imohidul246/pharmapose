-- 039: Offline cash-payment subscriptions + platform administration.
--
-- Stores carry a subscription validity window collected offline (cash). When
-- subscription_valid_until is in the past (or status is SUSPENDED) every login
-- and every live session for that store's users is rejected; the platform
-- admin extends validity by recording a cash payment in the ledger.
-- users.is_platform_admin marks the global super-admin, who bypasses all
-- store subscription checks and manages every tenant via /api/platform.

-- 1. Stores: subscription window + status.
ALTER TABLE stores
    ADD COLUMN IF NOT EXISTS subscription_valid_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS subscription_status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE';

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_stores_subscription_status') THEN
        ALTER TABLE stores
            ADD CONSTRAINT chk_stores_subscription_status
            CHECK (subscription_status IN ('ACTIVE', 'SUSPENDED'));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_stores_subscription_valid_until
    ON stores (subscription_valid_until);
CREATE INDEX IF NOT EXISTS idx_stores_subscription_status
    ON stores (subscription_status);

-- Existing tenants keep working: anyone without a validity window yet gets a
-- 30-day grace period from migration time. Tests re-seed stores with NULL
-- validity, which the application treats as valid (grace) so bootstrap and
-- legacy flows never lock out.
UPDATE stores
SET subscription_valid_until = now() + interval '30 days'
WHERE subscription_valid_until IS NULL;

-- 2. Users: platform admin flag.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_platform_admin BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_users_platform_admin
    ON users (is_platform_admin) WHERE is_platform_admin = true;

-- 3. Subscription payments ledger (offline cash receipts).
CREATE TABLE IF NOT EXISTS store_subscription_payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    plan_type VARCHAR(20) NOT NULL,
    amount NUMERIC(10, 2) NOT NULL,
    valid_from TIMESTAMPTZ NOT NULL,
    valid_until TIMESTAMPTZ NOT NULL,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_sub_payments_plan CHECK (plan_type IN ('1_MONTH', '6_MONTHS', '1_YEAR')),
    CONSTRAINT chk_sub_payments_amount CHECK (amount >= 0)
);

CREATE INDEX IF NOT EXISTS idx_sub_payments_store ON store_subscription_payments(store_id);
CREATE INDEX IF NOT EXISTS idx_sub_payments_store_created ON store_subscription_payments(store_id, created_at DESC);
