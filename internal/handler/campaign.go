package handler

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/campaign"
	apperrors "github.com/yourusername/ssp-adserver/pkg/errors"
)

// CampaignHandler handles CRUD endpoints for campaign management.
type CampaignHandler struct {
	repo campaign.Repository
	log  *zap.Logger
}

// NewCampaignHandler creates a new CampaignHandler with the given repository and logger.
func NewCampaignHandler(repo campaign.Repository, log *zap.Logger) *CampaignHandler {
	return &CampaignHandler{
		repo: repo,
		log:  log,
	}
}

// HandleCreateCampaign processes a POST /admin/campaigns request to create a new campaign.
func (h *CampaignHandler) HandleCreateCampaign(c *fiber.Ctx) error {
	var camp campaign.Campaign
	if err := c.BodyParser(&camp); err != nil {
		apiErr := apperrors.NewBadRequestError("invalid request body", err)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	if camp.ID == "" {
		camp.ID = uuid.NewString()
	}
	if camp.Status == "" {
		camp.Status = "active"
	}
	camp.CreatedAt = time.Now()

	if err := h.repo.CreateCampaign(c.Context(), &camp); err != nil {
		h.log.Error("failed to create campaign", zap.Error(err))
		apiErr := apperrors.NewInternalError("failed to create campaign", err)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	return c.Status(fiber.StatusCreated).JSON(camp)
}

// HandleGetActiveCampaigns processes a GET /admin/campaigns request to list active campaigns.
func (h *CampaignHandler) HandleGetActiveCampaigns(c *fiber.Ctx) error {
	campaigns, err := h.repo.GetActiveCampaigns(c.Context())
	if err != nil {
		h.log.Error("failed to get active campaigns", zap.Error(err))
		apiErr := apperrors.NewInternalError("failed to get active campaigns", err)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	return c.Status(fiber.StatusOK).JSON(campaigns)
}

// HandleGetCampaign processes a GET /admin/campaigns/:id request to retrieve a single campaign.
func (h *CampaignHandler) HandleGetCampaign(c *fiber.Ctx) error {
	id := c.Params("id")
	camp, err := h.repo.GetCampaignByID(c.Context(), id)
	if err != nil {
		if errors.Is(err, campaign.ErrNotFound) {
			apiErr := apperrors.NewNotFoundError("campaign not found", err)
			return c.Status(apiErr.StatusCode).JSON(apiErr)
		}
		h.log.Error("failed to get campaign", zap.Error(err), zap.String("id", id))
		apiErr := apperrors.NewInternalError("failed to get campaign", err)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	return c.Status(fiber.StatusOK).JSON(camp)
}

// HandleUpdateCampaign processes a PATCH /admin/campaigns/:id request to update campaign fields.
func (h *CampaignHandler) HandleUpdateCampaign(c *fiber.Ctx) error {
	id := c.Params("id")

	// Parse partial update
	var updateReq struct {
		Status      *string `json:"status"`
		BudgetCents *int64  `json:"budget_cents"`
	}
	if err := c.BodyParser(&updateReq); err != nil {
		apiErr := apperrors.NewBadRequestError("invalid request body", err)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	// Fetch existing to update fields
	camp, err := h.repo.GetCampaignByID(c.Context(), id)
	if err != nil {
		if errors.Is(err, campaign.ErrNotFound) {
			apiErr := apperrors.NewNotFoundError("campaign not found", err)
			return c.Status(apiErr.StatusCode).JSON(apiErr)
		}
		h.log.Error("failed to get campaign for update", zap.Error(err), zap.String("id", id))
		apiErr := apperrors.NewInternalError("failed to get campaign", err)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	if updateReq.Status != nil {
		camp.Status = *updateReq.Status
	}
	if updateReq.BudgetCents != nil {
		camp.BudgetCents = *updateReq.BudgetCents
	}

	if err := h.repo.UpdateCampaign(c.Context(), camp); err != nil {
		h.log.Error("failed to update campaign", zap.Error(err), zap.String("id", id))
		apiErr := apperrors.NewInternalError("failed to update campaign", err)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	return c.Status(fiber.StatusOK).JSON(camp)
}

// HandleAddCreative processes a POST /admin/campaigns/:id/creatives request to add a creative.
func (h *CampaignHandler) HandleAddCreative(c *fiber.Ctx) error {
	id := c.Params("id")
	var cr campaign.Creative
	if err := c.BodyParser(&cr); err != nil {
		apiErr := apperrors.NewBadRequestError("invalid request body", err)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	cr.CampaignID = id

	if err := h.repo.AddCreative(c.Context(), &cr); err != nil {
		h.log.Error("failed to add creative", zap.Error(err), zap.String("campaign_id", id))
		apiErr := apperrors.NewInternalError("failed to add creative", err)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	return c.Status(fiber.StatusCreated).JSON(cr)
}
