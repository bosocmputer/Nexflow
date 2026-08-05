package handlers

import (
	"database/sql"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
)

type SetupHandler struct {
	db          *sql.DB
	cfg         *config.Config
	appSettings *repository.AppSettingsRepo
	auditRepo   *repository.AuditLogRepo
	smlReady    *sml.ReadinessChecker
	logger      *zap.Logger
}

func NewSetupHandler(db *sql.DB, cfg *config.Config, appSettings *repository.AppSettingsRepo, auditRepo *repository.AuditLogRepo, smlReady *sml.ReadinessChecker, logger *zap.Logger) *SetupHandler {
	return &SetupHandler{db: db, cfg: cfg, appSettings: appSettings, auditRepo: auditRepo, smlReady: smlReady, logger: logger}
}

func (h *SetupHandler) Status(c *gin.Context) {
	settings, err := h.appSettings.All()
	if err != nil {
		h.logger.Error("setup status settings", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	pendingRestart, pendingKeys, err := h.appSettings.PendingRestart(h.cfg)
	if err != nil {
		h.logger.Warn("setup status pending restart", zap.Error(err))
	}

	runtime := repository.RuntimeSettingValues(h.cfg)
	smlMissing := missingSettings(settings, runtime, []string{
		"sml.rest_base_url",
		"sml.database",
	})
	smlReadiness := sml.ReadinessStatus{
		Configured: len(smlMissing) == 0,
		Ready:      len(smlMissing) == 0,
		Status:     "not_configured",
		Tenant:     runtime["sml.database"],
		Message:    "ยังไม่ได้ตั้งค่า SML REST URL, API key หรือฐานข้อมูลร้าน",
	}
	if h.smlReady != nil {
		smlReadiness = h.smlReady.Check(c.Request.Context(), c.Query("refresh_sml") == "1")
	}
	smlReady := len(smlMissing) == 0 && !pendingRestart && smlReadiness.Ready
	smlStatus := statusText(smlReady, pendingRestart)
	if len(smlMissing) == 0 && !pendingRestart && !smlReadiness.Ready {
		smlStatus = smlReadiness.Message
	}

	channelReady, channelMissing := h.channelReady()
	catalogReady, catalogDetail := h.catalogReady()
	docCounts := h.documentCounts()
	importCounts := h.importCounts()
	system := h.systemSummary(settings, pendingRestart, pendingKeys)

	steps := []gin.H{
		{
			"key":         "instance",
			"title":       "ข้อมูลร้านและ SML",
			"description": "กรอก SML REST URL และ Database ของร้านนี้",
			"href":        "/settings/instance",
			"ready":       smlReady,
			"status":      smlStatus,
			"missing":     smlMissing,
			"blocking":    true,
		},
		{
			"key":         "channels",
			"title":       "เส้นทางเอกสาร SML",
			"description": "ตั้งปลายทางเอกสารขาย รูปแบบเลขเอกสาร และ endpoint SML ให้ตรงกับช่องทางที่เปิดใช้",
			"href":        "/settings/channels",
			"ready":       channelReady,
			"status":      statusText(channelReady, false),
			"missing":     channelMissing,
			"blocking":    true,
		},
		{
			"key":         "catalog",
			"title":       "สินค้าใน SML",
			"description": "Sync สินค้าจาก SML และสร้างข้อมูลจับคู่เพื่อช่วยตรวจบิล",
			"href":        "/settings/catalog",
			"ready":       catalogReady,
			"status":      catalogDetail,
			"blocking":    true,
		},
		{
			"key":         "operations",
			"title":       "สถานะการใช้งาน",
			"description": "ตรวจจำนวนเอกสารค้าง, รอบนำเข้า และประวัติการทำงานระหว่างใช้งานจริง",
			"href":        "/setup",
			"ready":       true,
			"status":      "พร้อมตรวจสอบ",
			"blocking":    false,
		},
	}

	readyCount := 0
	blockingReadyCount := 0
	blockingTotal := 0
	for _, s := range steps {
		if s["ready"] == true {
			readyCount++
		}
		if s["blocking"] == true {
			blockingTotal++
			if s["ready"] == true {
				blockingReadyCount++
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"ready":                    blockingReadyCount == blockingTotal,
		"ready_count":              readyCount,
		"total_count":              len(steps),
		"blocking_ready_count":     blockingReadyCount,
		"blocking_total_count":     blockingTotal,
		"pending_restart":          pendingRestart,
		"pending_restart_settings": pendingKeys,
		"steps":                    steps,
		"sml_readiness":            smlReadiness,
		"system":                   system,
		"documents":                docCounts,
		"imports":                  importCounts,
	})
}

type resetTestDataRequest struct {
	Confirm         string `json:"confirm"`
	ResetDocCounter bool   `json:"reset_doc_counter"`
}

func (h *SetupHandler) ResetTestData(c *gin.Context) {
	if strings.EqualFold(strings.TrimSpace(h.cfg.Env), "production") &&
		!strings.EqualFold(strings.TrimSpace(os.Getenv("ALLOW_PRODUCTION_RESET_TEST_DATA")), "true") {
		c.JSON(http.StatusForbidden, gin.H{"error": "reset test data is disabled in production"})
		return
	}

	var req resetTestDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if strings.TrimSpace(req.Confirm) != "RESET" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type RESET to confirm"})
		return
	}

	beforeDocs := h.documentCounts()
	beforeImports := h.importCounts()
	var beforeLogs int
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&beforeLogs)

	tx, err := h.db.Begin()
	if err != nil {
		h.logger.Error("reset test data begin", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE mapping_feedback
		   SET bill_item_id = NULL
		 WHERE bill_item_id IN (SELECT id FROM bill_items)`); err != nil {
		h.logger.Error("reset mapping feedback links", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if _, err := tx.Exec(`DELETE FROM bills`); err != nil {
		h.logger.Error("reset bills", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if _, err := execIfTableExists(tx, "shopee_import_runs", `DELETE FROM shopee_import_runs`); err != nil {
		h.logger.Error("reset shopee import runs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if req.ResetDocCounter {
		if _, err := tx.Exec(`DELETE FROM doc_counters`); err != nil {
			h.logger.Error("reset doc counters", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
	}
	if _, err := tx.Exec(`DELETE FROM audit_logs`); err != nil {
		h.logger.Error("reset audit logs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if err := tx.Commit(); err != nil {
		h.logger.Error("reset test data commit", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if h.auditRepo != nil {
		var uid *string
		if v := c.GetString("user_id"); v != "" {
			uid = &v
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action: "maintenance_data_reset",
			UserID: uid,
			Source: "setup",
			Level:  "warn",
			Detail: map[string]interface{}{
				"before_documents":   beforeDocs,
				"before_imports":     beforeImports,
				"before_logs":        beforeLogs,
				"reset_doc_counter":  req.ResetDocCounter,
				"preserved_catalog":  true,
				"preserved_mappings": true,
				"preserved_feedback": true,
				"preserved_settings": true,
			},
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"cleared": gin.H{
			"documents":    beforeDocs,
			"imports":      beforeImports,
			"audit_logs":   beforeLogs,
			"doc_counters": req.ResetDocCounter,
		},
	})
}

func missingSettings(settings map[string]repository.AppSetting, runtime map[string]string, keys []string) []string {
	missing := []string{}
	for _, key := range keys {
		if strings.TrimSpace(settings[key].Value) == "" && strings.TrimSpace(runtime[key]) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

func statusText(ready, pendingRestart bool) string {
	if pendingRestart {
		return "รอรีสตาร์ทเพื่อใช้ค่าใหม่"
	}
	if ready {
		return "พร้อมใช้งาน"
	}
	return "ยังตั้งค่าไม่ครบ"
}

func (h *SetupHandler) channelReady() (bool, []string) {
	rows, err := h.db.Query(`
		SELECT channel, bill_type, doc_format_code, endpoint, doc_prefix, doc_running_format
		  FROM channel_defaults
		 WHERE channel='shopee' AND bill_type='sale'`)
	if err != nil {
		return false, []string{"ตั้งค่าเส้นทางเอกสาร"}
	}
	defer rows.Close()

	foundSale := false
	missing := []string{}
	for rows.Next() {
		var channel, billType, docFormat, endpoint, prefix, running string
		if err := rows.Scan(&channel, &billType, &docFormat, &endpoint, &prefix, &running); err != nil {
			return false, []string{"อ่านค่าเส้นทางเอกสารไม่ได้"}
		}
		if channel == "shopee" && billType == "sale" {
			foundSale = true
		}
		label := channel
		if channel == "shopee_shipped" {
			label = "ใบสั่งซื้อ"
		} else if strings.Contains(strings.ToLower(endpoint), "saleinvoice") || strings.EqualFold(docFormat, "SI") {
			label = "ขายสินค้าและบริการ"
		} else if channel == "shopee" {
			label = "ใบสั่งขาย"
		}
		if strings.TrimSpace(endpoint) == "" {
			missing = append(missing, label+": ปลายทาง SML")
		}
		if strings.TrimSpace(docFormat) == "" {
			missing = append(missing, label+": รหัสเอกสาร")
		}
		if strings.TrimSpace(prefix) == "" {
			missing = append(missing, label+": doc prefix")
		}
		if strings.TrimSpace(running) == "" {
			missing = append(missing, label+": รูปแบบเลขรัน")
		}
	}
	if !foundSale {
		missing = append(missing, "ใบสั่งขาย")
	}
	return len(missing) == 0, missing
}

func (h *SetupHandler) catalogReady() (bool, string) {
	var total int
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM sml_catalog WHERE is_active=TRUE`).Scan(&total)
	if total == 0 {
		return false, "ยังไม่ได้ Sync สินค้า"
	}
	return true, "พร้อมใช้งาน"
}

func (h *SetupHandler) documentCounts() gin.H {
	var total, pending, needsReview, failed, sent, saleorder, saleinvoice int
	_ = h.db.QueryRow(`
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE status='pending'),
		       COUNT(*) FILTER (WHERE status='needs_review'),
		       COUNT(*) FILTER (WHERE status='failed'),
		       COUNT(*) FILTER (WHERE status='sent'),
		       COUNT(*) FILTER (WHERE source IN ('shopee','lazada','tiktok') AND bill_type='sale' AND COALESCE(document_route, 'saleorder')='saleorder'),
		       COUNT(*) FILTER (WHERE source IN ('shopee','lazada','tiktok') AND bill_type='sale' AND document_route='saleinvoice')
		  FROM bills WHERE bill_type='sale'`,
	).Scan(
		&total, &pending, &needsReview, &failed, &sent,
		&saleorder, &saleinvoice,
	)
	return gin.H{
		"total":        total,
		"pending":      pending,
		"needs_review": needsReview,
		"failed":       failed,
		"sent":         sent,
		"saleorder":    saleorder,
		"saleinvoice":  saleinvoice,
	}
}

func (h *SetupHandler) importCounts() gin.H {
	var shopeeRuns, shopeeRunning, shopeeFailed, auditLogs int
	if tableExists(h.db, "shopee_import_runs") {
		_ = h.db.QueryRow(`
			SELECT COUNT(*),
			       COUNT(*) FILTER (WHERE status='running'),
			       COUNT(*) FILTER (WHERE status='failed')
			  FROM shopee_import_runs`,
		).Scan(&shopeeRuns, &shopeeRunning, &shopeeFailed)
	}
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&auditLogs)
	return gin.H{
		"shopee_runs":    shopeeRuns,
		"shopee_running": shopeeRunning,
		"shopee_failed":  shopeeFailed,
		"audit_logs":     auditLogs,
	}
}

type queryRower interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

func tableExists(q queryRower, name string) bool {
	var ok bool
	_ = q.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, "public."+name).Scan(&ok)
	return ok
}

func execIfTableExists(tx *sql.Tx, tableName, query string) (sql.Result, error) {
	if !tableExists(tx, tableName) {
		return nil, nil
	}
	return tx.Exec(query)
}

func (h *SetupHandler) systemSummary(settings map[string]repository.AppSetting, pendingRestart bool, pendingKeys []string) gin.H {
	instanceName := strings.TrimSpace(settings["instance.name"].Value)
	if instanceName == "" {
		instanceName = "Nexflow"
	}
	instanceSlug := strings.TrimSpace(settings["instance.slug"].Value)
	if instanceSlug == "" {
		instanceSlug = "default"
	}
	var lastCatalogSync, lastImportRun sql.NullString
	_ = h.db.QueryRow(`SELECT MAX(synced_at)::text FROM sml_catalog`).Scan(&lastCatalogSync)
	_ = h.db.QueryRow(`SELECT MAX(created_at)::text FROM shopee_import_runs`).Scan(&lastImportRun)
	return gin.H{
		"instance_name":            instanceName,
		"instance_slug":            instanceSlug,
		"env":                      h.cfg.Env,
		"sml_rest_url":             h.cfg.ShopeeSMLURL,
		"sml_database":             h.cfg.ShopeeSMLDatabase,
		"public_base_url":          h.cfg.PublicBaseURL,
		"ai_enabled":               false,
		"purchase_flow_enabled":    false,
		"pending_restart":          pendingRestart,
		"pending_restart_settings": pendingKeys,
		"last_catalog_sync":        lastCatalogSync.String,
		"last_import_run":          lastImportRun.String,
	}
}
