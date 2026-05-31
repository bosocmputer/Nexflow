package jobs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// BackupCron runs pg_dump from inside the backend container.
// Requires postgresql-client in the image (see Dockerfile) and a writable
// /app/backups volume mounted to ~/nexflow/backups on the host.
type BackupCron struct {
	dbHost     string // postgres service name (Docker network)
	dbPort     string
	dbUser     string
	dbName     string
	dbPassword string
	backupDir  string
	log        *zap.Logger
}

func NewBackupCron(dbHost, dbPort, dbUser, dbName, dbPassword, backupDir string, log *zap.Logger) *BackupCron {
	return &BackupCron{
		dbHost:     dbHost,
		dbPort:     dbPort,
		dbUser:     dbUser,
		dbName:     dbName,
		dbPassword: dbPassword,
		backupDir:  backupDir,
		log:        log,
	}
}

func (j *BackupCron) Register(c *cron.Cron, hour int) {
	schedule := fmt.Sprintf("0 %d * * *", hour)
	c.AddFunc(schedule, j.Run)
}

func (j *BackupCron) Run() {
	j.log.Info("backup cron: starting", zap.String("dir", j.backupDir))

	if err := os.MkdirAll(j.backupDir, 0o755); err != nil {
		j.log.Error("backup cron: mkdir failed", zap.Error(err))
		return
	}

	date := time.Now().Format("20060102")
	outFile := filepath.Join(j.backupDir, date+".sql.gz")

	// Pipe pg_dump → gzip via shell. PGPASSWORD passed via env, never echoed.
	cmd := exec.Command("sh", "-c",
		fmt.Sprintf(
			"pg_dump -h %s -p %s -U %s -d %s | gzip > %s",
			j.dbHost, j.dbPort, j.dbUser, j.dbName, outFile,
		),
	)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+j.dbPassword)

	out, err := cmd.CombinedOutput()
	if err != nil {
		j.log.Error("backup cron: pg_dump failed",
			zap.Error(err),
			zap.String("output", string(out)),
		)
		_ = os.Remove(outFile) // remove partial file
		return
	}

	// Sanity check: file must be > 100 bytes (smaller almost certainly means failure)
	if info, err := os.Stat(outFile); err != nil || info.Size() < 100 {
		j.log.Error("backup cron: output file is empty or missing",
			zap.String("file", outFile),
		)
		_ = os.Remove(outFile)
		return
	}

	// Prune backups older than 30 days
	prune := exec.Command("sh", "-c",
		fmt.Sprintf("find %s -name '*.sql.gz' -mtime +30 -delete", j.backupDir),
	)
	_ = prune.Run()

	info, _ := os.Stat(outFile)
	j.log.Info("backup cron: done",
		zap.String("file", outFile),
		zap.Int64("size_bytes", info.Size()),
	)
}
