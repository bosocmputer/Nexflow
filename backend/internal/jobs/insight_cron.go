package jobs

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"nexflow/internal/repository"
	"nexflow/internal/services/insight"
	lineservice "nexflow/internal/services/line"
)

type InsightCron struct {
	insightSvc  *insight.Service
	billRepo    *repository.BillRepo
	insightRepo *repository.InsightRepo
	lineSvc     *lineservice.Service
	lineNotify  bool
	log         *zap.Logger
}

func NewInsightCron(
	insightSvc *insight.Service,
	billRepo *repository.BillRepo,
	insightRepo *repository.InsightRepo,
	lineSvc *lineservice.Service,
	lineNotify bool,
	log *zap.Logger,
) *InsightCron {
	return &InsightCron{
		insightSvc:  insightSvc,
		billRepo:    billRepo,
		insightRepo: insightRepo,
		lineSvc:     lineSvc,
		lineNotify:  lineNotify,
		log:         log,
	}
}

func (j *InsightCron) Register(c *cron.Cron, hour int) {
	schedule := fmt.Sprintf("0 %d * * *", hour)
	c.AddFunc(schedule, j.Run)
}

func (j *InsightCron) Run() {
	j.log.Info("insight cron: starting")
	start := time.Now()

	stats, err := j.billRepo.DashboardStats()
	if err != nil {
		j.log.Error("insight cron: get stats", zap.Error(err))
		return
	}

	statsBytes, _ := json.Marshal(stats)
	text, err := j.insightSvc.Generate(string(statsBytes))
	if err != nil {
		j.log.Error("insight cron: generate", zap.Error(err))
		return
	}

	if j.insightRepo != nil {
		if err := j.insightRepo.Save(string(statsBytes), text); err != nil {
			j.log.Error("insight cron: save to DB", zap.Error(err))
		}
	}

	j.log.Info("insight cron: done", zap.Duration("elapsed", time.Since(start)))

	if j.lineNotify && j.lineSvc != nil {
		if err := j.lineSvc.PushAdmin("📊 Daily Insight\n" + text); err != nil {
			j.log.Error("insight cron: LINE notify", zap.Error(err))
		}
	}
}
