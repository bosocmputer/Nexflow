package jobs

import (
	"fmt"
	"syscall"

	lineservice "nexflow/internal/services/line"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

type DiskMonitor struct {
	warnPercent int
	lineSvc     *lineservice.Service
	log         *zap.Logger
}

func NewDiskMonitor(warnPercent int, lineSvc *lineservice.Service, log *zap.Logger) *DiskMonitor {
	return &DiskMonitor{warnPercent: warnPercent, lineSvc: lineSvc, log: log}
}

func (j *DiskMonitor) Register(c *cron.Cron) {
	// Every day at 07:00
	c.AddFunc("0 7 * * *", j.Run)
}

func (j *DiskMonitor) Run() {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		j.log.Error("disk monitor: statfs", zap.Error(err))
		return
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free
	usedPct := int(float64(used) / float64(total) * 100)

	j.log.Info("disk monitor", zap.Int("used_pct", usedPct))

	if usedPct >= j.warnPercent {
		msg := fmt.Sprintf("⚠️ Nexflow: Disk usage is %d%% (threshold: %d%%)\nPlease clean up!", usedPct, j.warnPercent)
		if j.lineSvc != nil {
			_ = j.lineSvc.PushAdmin(msg)
		}
	}
}
