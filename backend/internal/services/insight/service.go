package insight

import (
	"nexflow/internal/services/ai"
)

type Service struct {
	aiClient *ai.Client
}

func New(aiClient *ai.Client) *Service {
	return &Service{aiClient: aiClient}
}

// Generate creates a daily insight from stats JSON
// Called by cron at 08:00 (Phase 7)
func (s *Service) Generate(statsJSON string) (string, error) {
	return s.aiClient.GenerateInsight(statsJSON)
}
