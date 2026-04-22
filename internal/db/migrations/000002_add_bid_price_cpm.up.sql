-- Add an explicit CPM bid price column to campaigns.
-- Default of 1.00 ensures existing campaigns participate in auctions
-- at a safe, non-zero price rather than being silently dropped.
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS bid_price_cpm NUMERIC(10, 4) NOT NULL DEFAULT 1.0000;
