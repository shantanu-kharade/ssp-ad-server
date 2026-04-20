// Package campaign provides campaign management, persistence, and targeting
// evaluation for the SSP ad server.
package campaign

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors for repository operations.
var (
	// ErrNotFound indicates the requested campaign or resource was not found.
	ErrNotFound = errors.New("resource not found")
	// ErrConflict indicates a conflicting resource already exists.
	ErrConflict = errors.New("resource conflict")
	// ErrDBError indicates an unrecoverable database error.
	ErrDBError = errors.New("database error")
)

// Repository defines the interface for campaign persistence operations.
type Repository interface {
	CreateCampaign(ctx context.Context, c *Campaign) error
	GetActiveCampaigns(ctx context.Context) ([]Campaign, error)
	GetCampaignByID(ctx context.Context, id string) (*Campaign, error)
	UpdateCampaign(ctx context.Context, c *Campaign) error
	AddCreative(ctx context.Context, cr *Creative) error
}

type pgRepository struct {
	db *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed Repository.
func NewPGRepository(db *pgxpool.Pool) Repository {
	return &pgRepository{db: db}
}

func (r *pgRepository) CreateCampaign(ctx context.Context, c *Campaign) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return ErrDBError
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO campaigns (id, name, advertiser_id, status, budget_cents, spent_cents, start_date, end_date, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, c.ID, c.Name, c.AdvertiserID, c.Status, c.BudgetCents, c.SpentCents, c.StartDate, c.EndDate, c.CreatedAt)
	if err != nil {
		return ErrDBError
	}

	for _, rule := range c.TargetingRules {
		_, err = tx.Exec(ctx, `
			INSERT INTO targeting_rules (id, campaign_id, rule_type, rule_value, created_at)
			VALUES ($1, $2, $3, $4, $5)
		`, rule.ID, c.ID, rule.RuleType, rule.RuleValue, rule.CreatedAt)
		if err != nil {
			return ErrDBError
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return ErrDBError
	}
	return nil
}

func (r *pgRepository) GetActiveCampaigns(ctx context.Context) ([]Campaign, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, advertiser_id, status, budget_cents, spent_cents, start_date, end_date, created_at
		FROM campaigns
		WHERE status = 'active'
	`)
	if err != nil {
		return nil, ErrDBError
	}
	defer rows.Close()

	var campaigns []Campaign
	for rows.Next() {
		var c Campaign
		if err := rows.Scan(&c.ID, &c.Name, &c.AdvertiserID, &c.Status, &c.BudgetCents, &c.SpentCents, &c.StartDate, &c.EndDate, &c.CreatedAt); err != nil {
			return nil, ErrDBError
		}
		
		c.Creatives, err = r.getCreativesByCampaignID(ctx, c.ID)
		if err != nil {
			return nil, err
		}

		c.TargetingRules, err = r.getTargetingRulesByCampaignID(ctx, c.ID)
		if err != nil {
			return nil, err
		}

		campaigns = append(campaigns, c)
	}
	return campaigns, nil
}

func (r *pgRepository) GetCampaignByID(ctx context.Context, id string) (*Campaign, error) {
	var c Campaign
	err := r.db.QueryRow(ctx, `
		SELECT id, name, advertiser_id, status, budget_cents, spent_cents, start_date, end_date, created_at
		FROM campaigns
		WHERE id = $1
	`, id).Scan(&c.ID, &c.Name, &c.AdvertiserID, &c.Status, &c.BudgetCents, &c.SpentCents, &c.StartDate, &c.EndDate, &c.CreatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, ErrDBError
	}

	c.Creatives, err = r.getCreativesByCampaignID(ctx, c.ID)
	if err != nil {
		return nil, err
	}

	c.TargetingRules, err = r.getTargetingRulesByCampaignID(ctx, c.ID)
	if err != nil {
		return nil, err
	}

	return &c, nil
}

func (r *pgRepository) UpdateCampaign(ctx context.Context, c *Campaign) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE campaigns
		SET status = $1, budget_cents = $2
		WHERE id = $3
	`, c.Status, c.BudgetCents, c.ID)
	if err != nil {
		return ErrDBError
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *pgRepository) AddCreative(ctx context.Context, cr *Creative) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO creatives (id, campaign_id, format, width, height, ad_markup, click_url, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, cr.ID, cr.CampaignID, cr.Format, cr.Width, cr.Height, cr.AdMarkup, cr.ClickURL, cr.Status, cr.CreatedAt)
	if err != nil {
		return ErrDBError
	}
	return nil
}

func (r *pgRepository) getCreativesByCampaignID(ctx context.Context, campaignID string) ([]Creative, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, campaign_id, format, width, height, ad_markup, click_url, status, created_at
		FROM creatives
		WHERE campaign_id = $1
	`, campaignID)
	if err != nil {
		return nil, ErrDBError
	}
	defer rows.Close()

	var creatives []Creative
	for rows.Next() {
		var cr Creative
		if err := rows.Scan(&cr.ID, &cr.CampaignID, &cr.Format, &cr.Width, &cr.Height, &cr.AdMarkup, &cr.ClickURL, &cr.Status, &cr.CreatedAt); err != nil {
			return nil, ErrDBError
		}
		creatives = append(creatives, cr)
	}
	return creatives, nil
}

func (r *pgRepository) getTargetingRulesByCampaignID(ctx context.Context, campaignID string) ([]TargetingRule, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, campaign_id, rule_type, rule_value, created_at
		FROM targeting_rules
		WHERE campaign_id = $1
	`, campaignID)
	if err != nil {
		return nil, ErrDBError
	}
	defer rows.Close()

	var rules []TargetingRule
	for rows.Next() {
		var tr TargetingRule
		if err := rows.Scan(&tr.ID, &tr.CampaignID, &tr.RuleType, &tr.RuleValue, &tr.CreatedAt); err != nil {
			return nil, ErrDBError
		}
		rules = append(rules, tr)
	}
	return rules, nil
}
