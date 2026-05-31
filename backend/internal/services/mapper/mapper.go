package mapper

import (
	"strings"

	"github.com/lithammer/fuzzysearch/fuzzy"

	"nexflow/internal/models"
)

const (
	AutoConfirmScore = 0.85
	NeedsReviewScore = 0.60
)

// mappingRepo abstracts DB access so the service can be unit-tested without a real DB.
type mappingRepo interface {
	FindByRawName(rawName string) (*models.Mapping, error)
	ListAll() ([]models.Mapping, error)
	IncrementUsage(id string) error
	Upsert(rawName, itemCode, unitCode, source string, billID *string) error
}

type Service struct {
	repo mappingRepo
}

func New(repo mappingRepo) *Service {
	return &Service{repo: repo}
}

// Match finds the best mapping for a raw item name (F1)
func (s *Service) Match(rawName string) models.MatchResult {
	// 1. Exact match
	m, err := s.repo.FindByRawName(rawName)
	if err == nil && m != nil {
		_ = s.repo.IncrementUsage(m.ID)
		return models.MatchResult{Mapping: m, Score: 1.0}
	}

	// 2. Fuzzy match against all mappings
	all, err := s.repo.ListAll()
	if err != nil || len(all) == 0 {
		return models.MatchResult{Unmapped: true}
	}

	var bestMapping *models.Mapping
	bestScore := 0.0

	lowerRaw := strings.ToLower(rawName)
	for i := range all {
		m := &all[i]
		lowerStored := strings.ToLower(m.RawName)

		// Jaro-Winkler via fuzzy package
		score := fuzzy.LevenshteinDistance(lowerRaw, lowerStored)
		// Normalize: 1.0 = exact, decrease with distance
		maxLen := len(lowerRaw)
		if len(lowerStored) > maxLen {
			maxLen = len(lowerStored)
		}
		if maxLen == 0 {
			continue
		}
		normalized := 1.0 - float64(score)/float64(maxLen)

		// Boost by usage_count (up to +5% for high-usage items)
		boost := float64(m.UsageCount) * 0.001
		if boost > 0.05 {
			boost = 0.05
		}
		normalized += boost

		if normalized > bestScore {
			bestScore = normalized
			bestMapping = m
		}
	}

	if bestMapping == nil || bestScore < NeedsReviewScore {
		return models.MatchResult{Unmapped: true}
	}

	_ = s.repo.IncrementUsage(bestMapping.ID)

	if bestScore >= AutoConfirmScore {
		return models.MatchResult{Mapping: bestMapping, Score: bestScore}
	}
	return models.MatchResult{Mapping: bestMapping, Score: bestScore, NeedsReview: true}
}

// LearnFromFeedback saves a human correction as ai_learned mapping (F1)
func (s *Service) LearnFromFeedback(rawName, itemCode, unitCode string, billID *string) error {
	return s.repo.Upsert(rawName, itemCode, unitCode, "ai_learned", billID)
}
