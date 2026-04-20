CREATE TABLE IF NOT EXISTS campaigns (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    advertiser_id UUID NOT NULL,
    status TEXT NOT NULL,
    budget_cents BIGINT NOT NULL,
    spent_cents BIGINT NOT NULL DEFAULT 0,
    start_date TIMESTAMPTZ NOT NULL,
    end_date TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_campaigns_status ON campaigns(status);

CREATE TABLE IF NOT EXISTS creatives (
    id UUID PRIMARY KEY,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    format TEXT NOT NULL,
    width INT NOT NULL,
    height INT NOT NULL,
    ad_markup TEXT NOT NULL,
    click_url TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_creatives_campaign_id ON creatives(campaign_id);

CREATE TABLE IF NOT EXISTS targeting_rules (
    id UUID PRIMARY KEY,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    rule_type TEXT NOT NULL,
    rule_value JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_targeting_rules_campaign_id ON targeting_rules(campaign_id);
