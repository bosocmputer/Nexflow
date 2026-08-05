package mapper

import (
	"nexflow/internal/models"
)

// mappingRepo abstracts DB access so the service can be unit-tested without a real DB.
type mappingRepo interface {
	FindByRawName(rawName string) (*models.Mapping, error)
	IncrementUsage(id string) error
	Upsert(rawName, itemCode, unitCode, source string, billID *string) error
}

type Service struct {
	repo mappingRepo
}

func New(repo mappingRepo) *Service {
	return &Service{repo: repo}
}

// Match resolves only an exact, human-verified raw-name mapping. Marketplace
// SKU resolution is handled before this service by the import handlers.
func (s *Service) Match(rawName string) models.MatchResult {
	m, err := s.repo.FindByRawName(rawName)
	if err == nil && m != nil {
		_ = s.repo.IncrementUsage(m.ID)
		return models.MatchResult{Mapping: m, Score: 1.0}
	}
	return models.MatchResult{Unmapped: true}
}

// LearnFromFeedback saves an exact human-verified mapping.
func (s *Service) LearnFromFeedback(rawName, itemCode, unitCode string, billID *string) error {
	return s.repo.Upsert(rawName, itemCode, unitCode, "verified", billID)
}
