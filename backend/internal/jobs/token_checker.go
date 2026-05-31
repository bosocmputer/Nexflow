package jobs

import (
	"fmt"

	lineservice "nexflow/internal/services/line"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// TokenChecker warns admin when LINE token expires within 7 days
// Note: line-bot-sdk does not expose expiry — push a reminder weekly
type TokenChecker struct {
	lineSvc *lineservice.Service
	log     *zap.Logger
}

func NewTokenChecker(lineSvc *lineservice.Service, log *zap.Logger) *TokenChecker {
	return &TokenChecker{lineSvc: lineSvc, log: log}
}

func (j *TokenChecker) Register(c *cron.Cron) {
	// Every Monday 09:00
	c.AddFunc("0 9 * * 1", j.Run)
}

func (j *TokenChecker) Run() {
	j.log.Info("token checker: running")
	if j.lineSvc == nil {
		return
	}
	msg := fmt.Sprintf("🔑 Nexflow: LINE Channel Access Token reminder\nRotate token every 90 days.\nCheck: https://developers.line.biz/console/")
	if err := j.lineSvc.PushAdmin(msg); err != nil {
		j.log.Error("token checker: push admin failed", zap.Error(err))
	}
}
