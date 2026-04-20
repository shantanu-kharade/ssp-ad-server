package campaign

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/yourusername/ssp-adserver/internal/models"
)

// Service provides campaign business logic, including targeting evaluation.
type Service struct {
	repo Repository
}

// NewService creates a new campaign Service with the given repository.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// EvaluateTargeting returns a list of active campaigns that match the targeting rules
// against the incoming bid request and user segments.
func (s *Service) EvaluateTargeting(ctx context.Context, req models.BidRequest, userSegments []string) ([]Campaign, error) {
	activeCampaigns, err := s.repo.GetActiveCampaigns(ctx)
	if err != nil {
		return nil, err
	}

	var matched []Campaign

	for _, camp := range activeCampaigns {
		if s.matchesTargeting(camp, req, userSegments) {
			matched = append(matched, camp)
		}
	}

	return matched, nil
}

func (s *Service) matchesTargeting(camp Campaign, req models.BidRequest, userSegments []string) bool {
	// If no rules, campaign is untargeted (matches everything)
	if len(camp.TargetingRules) == 0 {
		return true
	}

	// Campaign must match ALL defined rules (AND logic).
	// Alternatively, business logic could be OR. 
	// The plan specifies matching rules, implying if a rule exists, it must be met.
	for _, rule := range camp.TargetingRules {
		switch rule.RuleType {
		case "geo":
			var geo RuleValueGeo
			if err := json.Unmarshal(rule.RuleValue, &geo); err != nil {
				return false
			}
			if !s.matchGeo(geo, req) {
				return false
			}
		case "segment":
			var seg RuleValueSegment
			if err := json.Unmarshal(rule.RuleValue, &seg); err != nil {
				return false
			}
			if !s.matchSegment(seg, userSegments) {
				return false
			}
		case "device":
			var dev RuleValueDevice
			if err := json.Unmarshal(rule.RuleValue, &dev); err != nil {
				return false
			}
			if !s.matchDevice(dev, req) {
				return false
			}
		default:
			// Unknown rule type, safe to fail open or closed depending on requirements.
			// Let's fail closed for safety.
			return false
		}
	}

	return true
}

func (s *Service) matchGeo(geo RuleValueGeo, req models.BidRequest) bool {
	if req.Device != nil && req.Device.IP != "" {
		return strings.HasPrefix(req.Device.IP, geo.IPPrefix)
	}
	return false
}

func (s *Service) matchSegment(seg RuleValueSegment, userSegments []string) bool {
	if len(userSegments) == 0 {
		return false
	}
	// O(N*M) is fine for small lists. Check for any overlap.
	for _, targetSeg := range seg.SegmentIDs {
		for _, userSeg := range userSegments {
			if targetSeg == userSeg {
				return true
			}
		}
	}
	return false
}

func (s *Service) matchDevice(dev RuleValueDevice, req models.BidRequest) bool {
	if req.Device != nil && req.Device.UA != "" {
		return strings.Contains(strings.ToLower(req.Device.UA), strings.ToLower(dev.UASubstring))
	}
	return false
}
