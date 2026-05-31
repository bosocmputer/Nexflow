package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type OldDataHandler struct {
	db  *sql.DB
	log *zap.Logger
}

type oldDataTableCount struct {
	ToPurge  int     `json:"to_purge"`
	Rows     int     `json:"rows"`
	SizeMB   float64 `json:"size_mb"`
	OldestAt *string `json:"oldest_at"`
}

func NewOldDataHandler(db *sql.DB, log *zap.Logger) *OldDataHandler {
	return &OldDataHandler{db: db, log: log}
}

// Summary returns counts of rows that would be affected by archive/purge operations.
// GET /api/bills/old-data/summary?archive_days=180&purge_days=730
func (h *OldDataHandler) Summary(c *gin.Context) {
	archiveDays := queryInt(c, "archive_days", 180)
	purgeDays := queryInt(c, "purge_days", 730)

	type billCount struct {
		ToArchive int `json:"to_archive"`
		ToPurge   int `json:"to_purge"`
		Archived  int `json:"archived"`
	}
	var bills billCount
	var auditLogs, aiUsageLogs, chatMessages oldDataTableCount
	var diskUsageMB float64

	// bills: sent/failed/skipped + not yet archived → eligible to archive
	//        any bill older than purge_days → eligible to purge (hard delete)
	//        currently archived count
	_ = h.db.QueryRow(`
		SELECT
		  COUNT(*) FILTER (WHERE status IN ('sent','failed','skipped')
		                   AND archived_at IS NULL
		                   AND created_at < NOW() - ($1 || ' days')::INTERVAL),
		  COUNT(*) FILTER (WHERE created_at < NOW() - ($2 || ' days')::INTERVAL),
		  COUNT(*) FILTER (WHERE archived_at IS NOT NULL)
		FROM bills`,
		strconv.Itoa(archiveDays), strconv.Itoa(purgeDays),
	).Scan(&bills.ToArchive, &bills.ToPurge, &bills.Archived)

	auditLogs = h.tableMetrics("audit_logs", purgeDays)
	aiUsageLogs = h.tableMetrics("ai_usage_logs", purgeDays)
	chatMessages = h.tableMetrics("chat_messages", purgeDays)

	// approximate DB size — may return NULL if user lacks pg_database_size permission
	var dbSizeNull *float64
	_ = h.db.QueryRow(`SELECT pg_database_size(current_database()) / 1024.0 / 1024.0`).Scan(&dbSizeNull)
	if dbSizeNull != nil {
		diskUsageMB = *dbSizeNull
	}

	c.JSON(http.StatusOK, gin.H{
		"archive_days":  archiveDays,
		"purge_days":    purgeDays,
		"bills":         bills,
		"audit_logs":    auditLogs,
		"ai_usage_logs": aiUsageLogs,
		"chat_messages": chatMessages,
		"db_size_mb":    diskUsageMB,
		"policy": gin.H{
			"hot_log_days":      90,
			"auto_archive_days": 180,
			"summary_days":      730,
			"purge_mode":        "batch",
		},
	})
}

// Archive sets archived_at on old sent/failed/skipped bills.
// POST /api/bills/old-data/archive
func (h *OldDataHandler) Archive(c *gin.Context) {
	var body struct {
		ArchiveDays int `json:"archive_days"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.ArchiveDays < 30 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "archive_days ต้องมากกว่า 30"})
		return
	}

	userID := c.GetString("user_id")

	res, err := h.db.Exec(`
		UPDATE bills
		SET archived_at = NOW(),
		    archived_by = NULLIF($1, '')::UUID,
		    archive_reason = 'auto-archive: older than ' || $2 || ' days'
		WHERE status IN ('sent','failed','skipped')
		  AND archived_at IS NULL
		  AND created_at < NOW() - ($2 || ' days')::INTERVAL`,
		userID, strconv.Itoa(body.ArchiveDays),
	)
	if err != nil {
		h.log.Error("archive bills", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "archive ไม่สำเร็จ"})
		return
	}
	n, _ := res.RowsAffected()
	c.JSON(http.StatusOK, gin.H{"ok": true, "archived": n})
}

// Purge hard-deletes old data from bills, audit_logs, and/or chat_messages.
// POST /api/bills/old-data/purge
func (h *OldDataHandler) Purge(c *gin.Context) {
	var body struct {
		PurgeDays  int  `json:"purge_days"`
		PurgeBills bool `json:"purge_bills"`
		PurgeAudit bool `json:"purge_audit"`
		PurgeAI    bool `json:"purge_ai"`
		PurgeChat  bool `json:"purge_chat"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.PurgeDays < 90 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "purge_days ต้องมากกว่า 90"})
		return
	}

	result := gin.H{"ok": true}

	if body.PurgeBills {
		n, err := h.batchDelete("bills", body.PurgeDays, 1000)
		if err != nil {
			h.log.Error("purge bills", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "purge bills ไม่สำเร็จ"})
			return
		}
		result["purged_bills"] = n
	}

	if body.PurgeAudit {
		n, err := h.batchDelete("audit_logs", body.PurgeDays, 1000)
		if err != nil {
			h.log.Error("purge audit_logs", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "purge audit logs ไม่สำเร็จ"})
			return
		}
		result["purged_audit_logs"] = n
	}

	if body.PurgeAI {
		n, err := h.batchDelete("ai_usage_logs", body.PurgeDays, 1000)
		if err != nil {
			h.log.Error("purge ai_usage_logs", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "purge AI usage ไม่สำเร็จ"})
			return
		}
		result["purged_ai_usage_logs"] = n
	}

	if body.PurgeChat {
		n, err := h.batchDelete("chat_messages", body.PurgeDays, 1000)
		if err != nil {
			h.log.Error("purge chat_messages", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "purge chat messages ไม่สำเร็จ"})
			return
		}
		result["purged_chat_messages"] = n
	}

	c.JSON(http.StatusOK, result)
}

func (h *OldDataHandler) tableMetrics(table string, purgeDays int) oldDataTableCount {
	var out oldDataTableCount
	query := fmt.Sprintf(`
		SELECT
		  COUNT(*) FILTER (WHERE created_at < NOW() - ($1 || ' days')::INTERVAL),
		  COUNT(*),
		  COALESCE(pg_total_relation_size('%s'::regclass) / 1024.0 / 1024.0, 0),
		  MIN(created_at)::text
		FROM %s`, table, table)
	_ = h.db.QueryRow(query, strconv.Itoa(purgeDays)).Scan(&out.ToPurge, &out.Rows, &out.SizeMB, &out.OldestAt)
	return out
}

func (h *OldDataHandler) batchDelete(table string, purgeDays, batchSize int) (int64, error) {
	var total int64
	for {
		query := fmt.Sprintf(`
			WITH doomed AS (
			  SELECT id FROM %s
			   WHERE created_at < NOW() - ($1 || ' days')::INTERVAL
			   ORDER BY created_at ASC
			   LIMIT $2
			)
			DELETE FROM %s
			 WHERE id IN (SELECT id FROM doomed)`, table, table)
		res, err := h.db.Exec(query, strconv.Itoa(purgeDays), batchSize)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n == 0 || n < int64(batchSize) {
			return total, nil
		}
	}
}

func queryInt(c *gin.Context, key string, defaultVal int) int {
	s := c.Query(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return defaultVal
	}
	return v
}
