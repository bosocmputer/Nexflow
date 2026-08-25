package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/catalog"
	"nexflow/internal/services/itemcode"
	"nexflow/internal/services/sml"
)

// CatalogHandler serves /api/catalog/* endpoints
type CatalogHandler struct {
	catalogSvc    *catalog.SMLCatalogService
	catalogRepo   *repository.SMLCatalogRepo
	productClient *sml.ProductClient
	auditRepo     *repository.AuditLogRepo
	appSettings   *repository.AppSettingsRepo
	cfg           *config.Config
	logger        *zap.Logger
}

type catalogImageMeta struct {
	Roworder   int    `json:"roworder"`
	ImageOrder int    `json:"image_order"`
	Guid       string `json:"guid"`
	Bytes      int64  `json:"bytes"`
	ImageURL   string `json:"image_url"`
}

type catalogUnitOption struct {
	Code             string  `json:"code"`
	Name1            string  `json:"name_1"`
	Name2            string  `json:"name_2"`
	StandValue       float64 `json:"stand_value,omitempty"`
	DivideValue      float64 `json:"divide_value,omitempty"`
	StandValueExact  string  `json:"stand_value_exact,omitempty"`
	DivideValueExact string  `json:"divide_value_exact,omitempty"`
	IsDefault        bool    `json:"is_default,omitempty"`
	GenerationID     string  `json:"generation_id,omitempty"`
}

const (
	catalogRefreshBatchLimit      = 50
	catalogRefreshBatchCodeMaxLen = 64
)

type catalogRefreshBatchRequest struct {
	Codes []string `json:"codes"`
}

type catalogRefreshBatchResult struct {
	Code            string              `json:"code"`
	Status          string              `json:"status"`
	Item            *models.CatalogItem `json:"item,omitempty"`
	NotFound        bool                `json:"not_found,omitempty"`
	Error           string              `json:"error,omitempty"`
	HasHiddenChars  bool                `json:"has_hidden_chars,omitempty"`
	CleanItemCode   string              `json:"clean_item_code,omitempty"`
	HiddenCharKinds []string            `json:"hidden_char_kinds,omitempty"`
}

type catalogRefreshBatchSummary struct {
	Total     int `json:"total"`
	Success   int `json:"success"`
	NotFound  int `json:"not_found"`
	Failed    int `json:"failed"`
	Duplicate int `json:"duplicate"`
}

type hiddenCatalogCodesResponse struct {
	Data    []models.CatalogItem `json:"data"`
	Total   int                  `json:"total"`
	Limit   int                  `json:"limit"`
	HasMore bool                 `json:"has_more"`
}

type catalogMarketplaceCursor struct {
	Source     string `json:"s"`
	AccountKey string `json:"a"`
	ID         string `json:"i"`
}

func NewCatalogHandler(
	svc *catalog.SMLCatalogService,
	repo *repository.SMLCatalogRepo,
	productClient *sml.ProductClient,
	auditRepo *repository.AuditLogRepo,
	appSettings *repository.AppSettingsRepo,
	cfg *config.Config,
	logger *zap.Logger,
) *CatalogHandler {
	return &CatalogHandler{
		catalogSvc:    svc,
		catalogRepo:   repo,
		productClient: productClient,
		auditRepo:     auditRepo,
		appSettings:   appSettings,
		cfg:           cfg,
		logger:        logger,
	}
}

// GET /api/catalog
func (h *CatalogHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	status := ""
	q := c.Query("q") // free-text search on item_code / item_name
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}

	items, total, err := h.catalogRepo.List(page, perPage, status, q)
	if err != nil {
		h.logger.Error("catalog list", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	attachCatalogImageURLs(items)
	c.JSON(http.StatusOK, gin.H{
		"data":     items,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

// GET /api/catalog/stats
func (h *CatalogHandler) Stats(c *gin.Context) {
	total, _, _, _, err := h.catalogRepo.Stats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	hiddenCount, hiddenErr := h.catalogRepo.CountHiddenItemCodes()
	if hiddenErr != nil {
		h.logger.Warn("catalog hidden code count", zap.Error(hiddenErr))
	}
	syncStatus := h.catalogSvc.SyncStatus()
	c.JSON(http.StatusOK, gin.H{
		"total":             total,
		"embedded":          0,
		"pending":           0,
		"error":             0,
		"hidden_code_count": hiddenCount,
		"index_size":        0,
		"embed_running":     false,
		"ai_enabled":        false,
		"sync_running":      syncStatus.Running,
		"sync_status":       syncStatus,
	})
}

// GET /api/catalog/hidden-codes — lazy-loaded detail list for hidden code
// warnings. The table has no stored hidden metadata, so this intentionally
// scans catalog rows only when the admin asks to inspect the problem list.
func (h *CatalogHandler) HiddenCodes(c *gin.Context) {
	if h.catalogRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "catalog repository not configured"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	items, total, err := h.catalogRepo.ListHiddenItemCodes(limit)
	if err != nil {
		h.logger.Error("catalog hidden code list", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดรายการรหัสซ่อนไม่สำเร็จ"})
		return
	}
	attachCatalogImageURLs(items)
	c.JSON(http.StatusOK, hiddenCatalogCodesResponse{
		Data:    items,
		Total:   total,
		Limit:   limit,
		HasMore: total > len(items),
	})
}

// POST /api/catalog/sync  — sync from SML REST API
func (h *CatalogHandler) SyncFromAPI(c *gin.Context) {
	if h.hasPendingRestart(c) {
		return
	}
	if !h.catalogSvc.BeginSync() {
		c.JSON(http.StatusAccepted, gin.H{"message": "catalog sync already running", "sync_running": true})
		return
	}
	go func() {
		h.logger.Info("catalog: sync from API started")
		count, err := h.catalogSvc.SyncFromAPI()
		if err != nil {
			h.logger.Error("catalog sync", zap.Error(err))
		}
		h.catalogSvc.FinishSync(count, err)
	}()
	c.JSON(http.StatusAccepted, gin.H{"message": "catalog sync started", "sync_running": true})
}

func (h *CatalogHandler) hasPendingRestart(c *gin.Context) bool {
	if h.appSettings == nil || h.cfg == nil {
		return false
	}
	pending, keys, err := h.appSettings.PendingRestart(h.cfg)
	if err != nil {
		h.logger.Warn("catalog: check pending restart", zap.Error(err))
		return false
	}
	if !pending {
		return false
	}
	c.JSON(http.StatusConflict, gin.H{
		"error":                    "มีการเปลี่ยนค่าระบบ SML ที่ยังไม่ได้เริ่มใช้ กรุณาติดต่อผู้ดูแลระบบก่อน Sync สินค้า",
		"pending_restart":          true,
		"pending_restart_settings": keys,
	})
	return true
}

// POST /api/catalog/:code/refresh — refresh a single row from SML 248
// Used by the per-row "รีเฟรช" button on /settings/catalog.
func (h *CatalogHandler) RefreshOne(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "item code required"})
		return
	}
	item, notFound, err := h.catalogSvc.RefreshOne(code)
	if err != nil {
		h.logger.Error("catalog refresh one", zap.String("code", code), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if notFound {
		c.JSON(http.StatusNotFound, gin.H{
			"error":     "ไม่พบสินค้า " + code + " ใน SML — อาจถูกลบจาก SML แล้ว",
			"not_found": true,
		})
		return
	}
	if item != nil {
		item.ImageURL = catalogImageURL(item.ItemCode, item.ImageCount, item.PrimaryImageRoworder)
	}
	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:  "catalog_refresh_one",
			UserID:  userID,
			Source:  "catalog",
			Level:   "info",
			TraceID: c.GetString("trace_id"),
			Detail:  map[string]interface{}{"item_code": code, "item_name": item.ItemName},
		})
	}
	c.JSON(http.StatusOK, gin.H{"item": item, "message": "refreshed from SML"})
}

// POST /api/catalog/refresh-batch — refresh a small set of products from SML.
// This is intentionally not a replacement for full sync: it supports admin
// workflows where a few newly-created SML item codes need to appear in
// Nexflow immediately, without paging the entire SML catalog.
func (h *CatalogHandler) RefreshBatch(c *gin.Context) {
	if h.hasPendingRestart(c) {
		return
	}

	var req catalogRefreshBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "codes are required"})
		return
	}

	codes := make([]string, 0, len(req.Codes))
	seen := map[string]bool{}
	duplicates := []string{}
	for _, raw := range req.Codes {
		code := strings.TrimSpace(raw)
		if code == "" {
			continue
		}
		if len([]rune(code)) > catalogRefreshBatchCodeMaxLen {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":    fmt.Sprintf("รหัสสินค้า %s ยาวเกิน %d ตัวอักษร", code, catalogRefreshBatchCodeMaxLen),
				"code":     code,
				"max_size": catalogRefreshBatchCodeMaxLen,
			})
			return
		}
		if seen[code] {
			duplicates = append(duplicates, code)
			continue
		}
		seen[code] = true
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาระบุรหัสสินค้า SML อย่างน้อย 1 รหัส"})
		return
	}
	if len(codes) > catalogRefreshBatchLimit {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":    fmt.Sprintf("ดึงสินค้าได้สูงสุด %d รหัสต่อครั้ง", catalogRefreshBatchLimit),
			"max_size": catalogRefreshBatchLimit,
		})
		return
	}
	if h.catalogSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "catalog service not configured"})
		return
	}

	results := make([]catalogRefreshBatchResult, 0, len(codes)+len(duplicates))
	summary := catalogRefreshBatchSummary{Total: len(codes) + len(duplicates), Duplicate: len(duplicates)}
	for _, code := range duplicates {
		meta := itemcode.Inspect(code)
		results = append(results, catalogRefreshBatchResult{
			Code:            code,
			Status:          "duplicate",
			HasHiddenChars:  meta.HasHiddenChars,
			CleanItemCode:   meta.CleanItemCode,
			HiddenCharKinds: meta.Kinds,
		})
	}

	successCount := 0
	for _, code := range codes {
		meta := itemcode.Inspect(code)
		result := catalogRefreshBatchResult{
			Code:            code,
			HasHiddenChars:  meta.HasHiddenChars,
			CleanItemCode:   meta.CleanItemCode,
			HiddenCharKinds: meta.Kinds,
		}

		item, notFound, err := h.catalogSvc.RefreshOne(code)
		if err != nil {
			h.logger.Warn("catalog refresh batch item failed", zap.String("code", code), zap.Error(err))
			result.Status = "failed"
			result.Error = catalogRefreshUserError(err)
			summary.Failed++
			results = append(results, result)
			continue
		}
		if notFound {
			result.Status = "not_found"
			result.NotFound = true
			result.Error = "ไม่พบรหัสนี้ใน SML master กรุณาตรวจรหัสหรือเพิ่มสินค้าใน SML ก่อน"
			summary.NotFound++
			results = append(results, result)
			continue
		}
		if item != nil {
			item.ImageURL = catalogImageURL(item.ItemCode, item.ImageCount, item.PrimaryImageRoworder)
			result.Item = item
		}
		result.Status = "success"
		summary.Success++
		successCount++
		results = append(results, result)
	}

	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:  "catalog_refresh_batch",
			UserID:  userID,
			Source:  "catalog",
			Level:   "info",
			TraceID: c.GetString("trace_id"),
			Detail: map[string]interface{}{
				"total":     summary.Total,
				"success":   summary.Success,
				"not_found": summary.NotFound,
				"failed":    summary.Failed,
				"duplicate": summary.Duplicate,
			},
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"summary": summary,
		"results": results,
	})
}

func catalogRefreshUserError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "timeout"),
		strings.Contains(msg, "i/o timeout"):
		return "เชื่อมต่อ SML ใช้เวลานานเกินไป เครื่อง SML/Postgres ของร้านนี้อาจยังไม่พร้อม"
	case strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no such host"),
		strings.Contains(msg, "network is unreachable"):
		return "เชื่อมต่อ SML ไม่สำเร็จ กรุณาตรวจว่าเครื่อง SML/Postgres ของร้านเปิดอยู่"
	case strings.Contains(msg, "sml api"):
		return "SML API ตอบกลับว่าดึงสินค้าไม่สำเร็จ กรุณาตรวจสถานะ SML แล้วลองใหม่"
	default:
		return "ดึงสินค้าจาก SML ไม่สำเร็จ กรุณาลองใหม่อีกครั้ง"
	}
}

// DELETE /api/catalog/:code — delete a single row from Nexflow's catalog.
// SML 248 is NOT touched — this is for pruning local zombies left over after
// an SML-side delete, or for clearing rows the admin doesn't want matched.
func (h *CatalogHandler) DeleteOne(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "item code required"})
		return
	}
	if err := h.catalogRepo.Delete(code); err != nil {
		// repo returns sql.ErrNoRows when the code wasn't there — surface as 404
		// so the UI can distinguish "already gone" from "real failure".
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบสินค้า " + code + " ใน Nexflow"})
			return
		}
		h.logger.Error("catalog delete one", zap.String("code", code), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:  "catalog_delete_one",
			UserID:  userID,
			Source:  "catalog",
			Level:   "info",
			TraceID: c.GetString("trace_id"),
			Detail:  map[string]interface{}{"item_code": code},
		})
	}
	c.JSON(http.StatusOK, gin.H{"deleted": code, "message": "ลบจาก Nexflow แล้ว (SML ไม่ถูกแตะ)"})
}

// POST /api/catalog/import-csv  — upload CSV file
func (h *CatalogHandler) ImportCSV(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read file: " + err.Error()})
		return
	}

	count, err := h.catalogSvc.SyncFromCSV(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"imported": count, "message": fmt.Sprintf("imported %d items from CSV", count)})
}

// EmbedOne is retained temporarily for stale clients after AI removal.
func (h *CatalogHandler) EmbedOne(c *gin.Context) {
	featureGone(c, "AI และ embedding ถูกปิดใช้งานแล้ว")
}

// EmbedAll is retained temporarily for stale clients after AI removal.
func (h *CatalogHandler) EmbedAll(c *gin.Context) {
	featureGone(c, "AI และ embedding ถูกปิดใช้งานแล้ว")
}

// ReloadIndex is retained temporarily for stale clients after AI removal.
func (h *CatalogHandler) ReloadIndex(c *gin.Context) {
	featureGone(c, "AI และ embedding ถูกปิดใช้งานแล้ว")
}

// GET /api/catalog/search?q=...&top=20 -- deterministic database search.
func (h *CatalogHandler) Search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if len([]rune(q)) < 2 || len([]rune(q)) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "คำค้นต้องมี 2-100 ตัวอักษร"})
		return
	}
	top, _ := strconv.Atoi(c.DefaultQuery("top", "20"))
	if top < 1 || top > 20 {
		top = 20
	}
	results, err := h.catalogRepo.SearchActive(q, top)
	if err != nil {
		h.logger.Error("catalog database search", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ค้นหาสินค้าไม่สำเร็จ"})
		return
	}
	attachCatalogMatchImageURLs(results)

	c.JSON(http.StatusOK, gin.H{
		"query":   q,
		"method":  "database",
		"results": results,
	})
}

// GET /api/catalog/:code  — get single product detail
func (h *CatalogHandler) GetOne(c *gin.Context) {
	code := c.Param("code")
	item, err := h.catalogRepo.GetOne(code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	item.ImageURL = catalogImageURL(item.ItemCode, item.ImageCount, item.PrimaryImageRoworder)
	c.JSON(http.StatusOK, item)
}

// GET /api/catalog/:code/marketplace-links — lazy-loaded Product Master rows
// that currently point to this SML item. The catalog page receives only small
// per-platform counts until the user explicitly opens the detail dialog.
func (h *CatalogHandler) MarketplaceLinks(c *gin.Context) {
	code := strings.TrimSpace(c.Param("code"))
	if code == "" || strings.ContainsAny(code, "\x00\r\n") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "รหัสสินค้า SML ไม่ถูกต้อง"})
		return
	}
	var cursor catalogMarketplaceCursor
	if raw := c.Query("cursor"); raw != "" {
		if err := decodeMarketplaceCursor(raw, &cursor); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cursor ไม่ถูกต้อง กรุณาเปิดรายละเอียดใหม่"})
			return
		}
	}
	if h.catalogRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "catalog repository not configured"})
		return
	}
	links, hasMore, err := h.catalogRepo.MarketplaceLinks(c.Request.Context(), repository.CatalogMarketplaceLinkFilter{
		ItemCode: code, Limit: marketplacePageLimit(c.Query("limit"), 25, 100),
		AfterSource: cursor.Source, AfterAccountKey: cursor.AccountKey, AfterID: cursor.ID,
	})
	if err != nil {
		h.logger.Error("catalog marketplace links", zap.String("item_code", code), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดสินค้าที่จับคู่จาก Marketplace ไม่สำเร็จ"})
		return
	}
	next := ""
	if hasMore && len(links) > 0 {
		last := links[len(links)-1]
		next = encodeMarketplaceCursor(catalogMarketplaceCursor{Source: last.Source, AccountKey: last.AccountKey, ID: last.ID})
	}
	c.JSON(http.StatusOK, gin.H{"data": links, "has_more": hasMore, "next_cursor": next})
}

// GET /api/sml/units — authenticated proxy for SML unit master data.
func (h *CatalogHandler) GetUnits(c *gin.Context) {
	if h.cfg == nil || strings.TrimSpace(h.cfg.ShopeeSMLURL) == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SML API is not configured"})
		return
	}

	search := strings.TrimSpace(c.Query("search"))
	limit := queryLimit(c, 100, 500)
	listURL := fmt.Sprintf("%s/api/v1/ic/units?size=%d",
		strings.TrimRight(h.cfg.ShopeeSMLURL, "/"),
		limit,
	)
	if search != "" {
		listURL += "&search=" + url.QueryEscape(search)
	}

	units, statusCode, err := h.fetchSMLUnits(c.Request.Context(), listURL)
	if err != nil {
		h.logger.Warn("catalog units: SML request failed", zap.Int("status", statusCode), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "อ่านหน่วยนับจาก SML ไม่สำเร็จ"})
		return
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.JSON(http.StatusOK, gin.H{"units": units})
}

// GET /api/catalog/:code/units — authenticated proxy for units valid for one SML item.
func (h *CatalogHandler) GetProductUnits(c *gin.Context) {
	code := strings.TrimSpace(c.Param("code"))
	if code == "" || strings.ContainsAny(code, "\x00\r\n") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item code"})
		return
	}
	if h.cfg != nil && h.cfg.MarketplaceUnitCatalogEnabled {
		if h.catalogRepo == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local unit catalog is not configured"})
			return
		}
		localUnits, err := h.catalogRepo.ListActiveUnits(c.Request.Context(), code)
		if err != nil {
			h.logger.Error("catalog product units: active generation lookup failed", zap.String("code", code), zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "อ่านหน่วยนับจาก catalog generation ไม่สำเร็จ"})
			return
		}
		units := make([]catalogUnitOption, 0, len(localUnits))
		for _, unit := range localUnits {
			stand, standErr := strconv.ParseFloat(unit.StandValue, 64)
			divide, divideErr := strconv.ParseFloat(unit.DivideValue, 64)
			if standErr != nil || divideErr != nil || stand <= 0 || divide <= 0 {
				c.JSON(http.StatusConflict, gin.H{"error": "unit conversion in the active catalog generation is invalid"})
				return
			}
			units = append(units, catalogUnitOption{
				Code: unit.UnitCode, Name1: unit.UnitName, StandValue: stand, DivideValue: divide,
				StandValueExact: unit.StandValue, DivideValueExact: unit.DivideValue,
				IsDefault: unit.IsDefault, GenerationID: unit.GenerationID,
			})
		}
		c.Header("Cache-Control", "private, max-age=300")
		c.JSON(http.StatusOK, gin.H{"units": units})
		return
	}
	if h.cfg == nil || strings.TrimSpace(h.cfg.ShopeeSMLURL) == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SML API is not configured"})
		return
	}

	listURL := fmt.Sprintf("%s/api/v1/ic/products/%s/units",
		strings.TrimRight(h.cfg.ShopeeSMLURL, "/"),
		url.PathEscape(code),
	)
	units, statusCode, err := h.fetchSMLUnits(c.Request.Context(), listURL)
	if err != nil {
		if fallback, ok := h.catalogUnitFallback(code); ok {
			c.Header("Cache-Control", "private, max-age=300")
			c.JSON(http.StatusOK, gin.H{"units": fallback})
			return
		}
		if statusCode == http.StatusNotFound {
			c.Header("Cache-Control", "private, max-age=300")
			c.JSON(http.StatusOK, gin.H{"units": []catalogUnitOption{}})
			return
		}
		h.logger.Warn("catalog product units: SML request failed",
			zap.String("code", code), zap.Int("status", statusCode), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "อ่านหน่วยนับสินค้าจาก SML ไม่สำเร็จ"})
		return
	}
	if len(units) == 0 {
		if fallback, ok := h.catalogUnitFallback(code); ok {
			units = fallback
		}
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.JSON(http.StatusOK, gin.H{"units": units})
}

func (h *CatalogHandler) catalogUnitFallback(code string) ([]catalogUnitOption, bool) {
	if h.catalogRepo == nil {
		return nil, false
	}
	item, err := h.catalogRepo.GetOne(code)
	if err != nil {
		if h.logger != nil {
			h.logger.Warn("catalog product units: local fallback lookup failed",
				zap.String("code", code), zap.Error(err))
		}
		return nil, false
	}
	if item == nil {
		return nil, false
	}
	unit := strings.TrimSpace(item.UnitCode)
	if unit == "" {
		return nil, false
	}
	return []catalogUnitOption{{
		Code:        unit,
		Name1:       unit,
		StandValue:  1,
		DivideValue: 1,
		IsDefault:   true,
	}}, true
}

func (h *CatalogHandler) fetchSMLUnits(ctx context.Context, listURL string) ([]catalogUnitOption, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build unit request: %w", err)
	}
	for k, v := range h.smlHeaders() {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, resp.StatusCode, fmt.Errorf("SML units HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	units, err := decodeSMLUnitsPayload(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return units, resp.StatusCode, nil
}

func decodeSMLUnitsPayload(r io.Reader) ([]catalogUnitOption, error) {
	var payload struct {
		Data  json.RawMessage     `json:"data"`
		Units []catalogUnitOption `json:"units"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode unit response: %w", err)
	}

	units := payload.Units
	if len(payload.Data) > 0 && string(payload.Data) != "null" {
		var dataObj struct {
			Units []catalogUnitOption `json:"units"`
		}
		if err := json.Unmarshal(payload.Data, &dataObj); err == nil && dataObj.Units != nil {
			units = dataObj.Units
		} else {
			var dataArray []catalogUnitOption
			if err := json.Unmarshal(payload.Data, &dataArray); err == nil {
				units = dataArray
			}
		}
	}
	if units == nil {
		units = []catalogUnitOption{}
	}

	cleaned := make([]catalogUnitOption, 0, len(units))
	seen := make(map[string]struct{}, len(units))
	for _, u := range units {
		u.Code = strings.TrimSpace(u.Code)
		u.Name1 = strings.TrimSpace(u.Name1)
		u.Name2 = strings.TrimSpace(u.Name2)
		if u.Code == "" {
			continue
		}
		if _, ok := seen[u.Code]; ok {
			continue
		}
		seen[u.Code] = struct{}{}
		if u.Name1 == "" {
			u.Name1 = u.Code
		}
		if u.StandValue < 0 {
			u.StandValue = 0
		}
		if u.DivideValue < 0 {
			u.DivideValue = 0
		}
		cleaned = append(cleaned, u)
	}
	return cleaned, nil
}

// GET /api/catalog/:code/image — authenticated proxy for the primary SML image.
func (h *CatalogHandler) GetImage(c *gin.Context) {
	code := strings.TrimSpace(c.Param("code"))
	if code == "" || strings.ContainsAny(code, "\x00\r\n") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item code"})
		return
	}
	if h.cfg == nil || strings.TrimSpace(h.cfg.ShopeeSMLURL) == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SML API is not configured"})
		return
	}

	item, err := h.catalogRepo.GetOne(code)
	if err != nil {
		h.logger.Warn("catalog image: lookup failed", zap.String("code", code), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "catalog lookup failed"})
		return
	}
	if item == nil || item.ImageCount <= 0 || item.PrimaryImageRoworder == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product image not found"})
		return
	}
	h.streamCatalogImage(c, item.ItemCode, *item.PrimaryImageRoworder)
}

// GET /api/catalog/:code/images — authenticated metadata list for SML product images.
func (h *CatalogHandler) GetImages(c *gin.Context) {
	code := strings.TrimSpace(c.Param("code"))
	if code == "" || strings.ContainsAny(code, "\x00\r\n") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item code"})
		return
	}
	if h.cfg == nil || strings.TrimSpace(h.cfg.ShopeeSMLURL) == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SML API is not configured"})
		return
	}

	item, err := h.catalogRepo.GetOne(code)
	if err != nil {
		h.logger.Warn("catalog images: lookup failed", zap.String("code", code), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "catalog lookup failed"})
		return
	}
	if item == nil || item.ImageCount <= 0 {
		c.JSON(http.StatusOK, gin.H{"images": []catalogImageMeta{}})
		return
	}

	listURL := fmt.Sprintf("%s/api/v1/ic/products/%s/images",
		strings.TrimRight(h.cfg.ShopeeSMLURL, "/"),
		url.PathEscape(item.ItemCode),
	)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, listURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "build image list request failed"})
		return
	}
	for k, v := range h.smlImageHeaders() {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		h.logger.Warn("catalog images: SML request failed", zap.String("code", code), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "read product images failed"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		c.JSON(http.StatusOK, gin.H{"images": []catalogImageMeta{}})
		return
	}
	if resp.StatusCode != http.StatusOK {
		h.logger.Warn("catalog images: SML returned non-OK",
			zap.String("code", code), zap.Int("status", resp.StatusCode))
		c.JSON(http.StatusBadGateway, gin.H{"error": "read product images failed"})
		return
	}

	var payload struct {
		Data *struct {
			Images []catalogImageMeta `json:"images"`
		} `json:"data"`
		Images []catalogImageMeta `json:"images"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "decode product images failed"})
		return
	}
	images := payload.Images
	if payload.Data != nil {
		images = payload.Data.Images
	}
	if images == nil {
		images = []catalogImageMeta{}
	}
	for i := range images {
		if images[i].Roworder <= 0 {
			continue
		}
		images[i].ImageURL = catalogImageRowURL(item.ItemCode, images[i].Roworder)
	}

	c.Header("Cache-Control", "private, max-age=300")
	c.JSON(http.StatusOK, gin.H{"images": images})
}

// GET /api/catalog/:code/images/:roworder — authenticated proxy for a specific SML image.
func (h *CatalogHandler) GetImageByRoworder(c *gin.Context) {
	code := strings.TrimSpace(c.Param("code"))
	roworder, err := strconv.Atoi(c.Param("roworder"))
	if code == "" || strings.ContainsAny(code, "\x00\r\n") || err != nil || roworder < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid image request"})
		return
	}
	if h.cfg == nil || strings.TrimSpace(h.cfg.ShopeeSMLURL) == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SML API is not configured"})
		return
	}

	item, err := h.catalogRepo.GetOne(code)
	if err != nil {
		h.logger.Warn("catalog image: lookup failed", zap.String("code", code), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "catalog lookup failed"})
		return
	}
	if item == nil || item.ImageCount <= 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "product image not found"})
		return
	}
	h.streamCatalogImage(c, item.ItemCode, roworder)
}

func (h *CatalogHandler) streamCatalogImage(c *gin.Context, itemCode string, roworder int) {
	imageURL := fmt.Sprintf("%s/api/v1/ic/products/%s/images/%d",
		strings.TrimRight(h.cfg.ShopeeSMLURL, "/"),
		url.PathEscape(itemCode),
		roworder,
	)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, imageURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "build image proxy request failed"})
		return
	}
	for k, v := range h.smlImageHeaders() {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		h.logger.Warn("catalog image: SML request failed", zap.String("code", itemCode), zap.Int("roworder", roworder), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "read product image failed"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "product image not found"})
		return
	}
	if resp.StatusCode != http.StatusOK {
		h.logger.Warn("catalog image: SML returned non-OK",
			zap.String("code", itemCode), zap.Int("roworder", roworder), zap.Int("status", resp.StatusCode))
		c.JSON(http.StatusBadGateway, gin.H{"error": "read product image failed"})
		return
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	extraHeaders := map[string]string{
		"Cache-Control": "private, max-age=3600",
	}
	if etag := strings.TrimSpace(resp.Header.Get("ETag")); etag != "" {
		extraHeaders["ETag"] = etag
	}
	c.DataFromReader(http.StatusOK, resp.ContentLength, contentType, resp.Body, extraHeaders)
}

func (h *CatalogHandler) smlImageHeaders() map[string]string {
	return h.smlHeaders()
}

func (h *CatalogHandler) smlHeaders() map[string]string {
	if h.cfg == nil {
		return map[string]string{}
	}
	hdrs := map[string]string{
		"guid":           h.cfg.ShopeeSMLGUID,
		"provider":       h.cfg.ShopeeSMLProvider,
		"configFileName": h.cfg.ShopeeSMLConfigFile,
		"databaseName":   h.cfg.ShopeeSMLDatabase,
		"X-Tenant":       h.cfg.ShopeeSMLDatabase,
	}
	return hdrs
}

func attachCatalogImageURLs(items []models.CatalogItem) {
	for i := range items {
		items[i].ImageURL = catalogImageURL(items[i].ItemCode, items[i].ImageCount, items[i].PrimaryImageRoworder)
	}
}

func attachCatalogMatchImageURLs(items []models.CatalogMatch) {
	for i := range items {
		items[i].ImageURL = catalogImageURL(items[i].ItemCode, items[i].ImageCount, items[i].PrimaryImageRoworder)
	}
}

func catalogImageURL(itemCode string, imageCount int, primaryRoworder *int) string {
	itemCode = strings.TrimSpace(itemCode)
	if itemCode == "" || imageCount <= 0 || primaryRoworder == nil {
		return ""
	}
	return "/api/catalog/" + url.PathEscape(itemCode) + "/image"
}

func catalogImageRowURL(itemCode string, roworder int) string {
	itemCode = strings.TrimSpace(itemCode)
	if itemCode == "" || roworder <= 0 {
		return ""
	}
	return "/api/catalog/" + url.PathEscape(itemCode) + "/images/" + strconv.Itoa(roworder)
}

// ─── Create new product ──────────────────────────────────────────────────────

// createProductRequest is the body the frontend sends. It's a compact "quick
// form" — only the minimum required for SML to accept the product.
type createProductRequest struct {
	Code      string `json:"code" binding:"required"`      // SML item code (user-supplied)
	Name      string `json:"name" binding:"required"`      // Product name (Thai or English)
	UnitCode  string `json:"unit_code" binding:"required"` // e.g. "ชิ้น", "ถุง"
	WHCode    string `json:"wh_code,omitempty"`            // optional default warehouse
	ShelfCode string `json:"shelf_code,omitempty"`         // optional default shelf
}

func buildQuickCreateProductRequest(req createProductRequest) sml.CreateProductRequest {
	return sml.CreateProductRequest{
		Code: req.Code, Name: req.Name, TaxType: 0, ItemType: 0, UnitType: 1,
		UnitCost: req.UnitCode, UnitStandard: req.UnitCode, PurchasePoint: 0,
		Units: []sml.ProductUnit{{
			UnitCode: req.UnitCode, UnitName: req.UnitCode, StandValue: 1, DivideValue: 1,
		}},
	}
}

// POST /api/catalog/products — quick-create a product in SML and sync to local catalog.
//
// Flow:
//  1. Pre-check: reject if item_code already exists in local sml_catalog
//     (saves a round-trip to SML for an obvious duplicate).
//  2. Call SML POST /SMLJavaRESTService/v3/api/product. SML may return its
//     own assigned code in response.data.code (overrides the requested one).
//  3. Upsert into the local deterministic catalog.
//  4. Audit the product creation.
//
// Returns the canonical code (from SML response) so the frontend can fill
// the bill_item with it.
func (h *CatalogHandler) CreateProduct(c *gin.Context) {
	var req createProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	req.UnitCode = strings.TrimSpace(req.UnitCode)
	if req.Code == "" || req.Name == "" || req.UnitCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code, name, unit_code are required"})
		return
	}
	if meta := itemcode.Inspect(req.Code); meta.HasHiddenChars {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":           "item_code has hidden characters; create product is blocked to avoid dirty SML master data",
			"code":            req.Code,
			"clean_item_code": meta.CleanItemCode,
		})
		return
	}
	if h.productClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "product client not configured"})
		return
	}

	// 1. Local dup-check — fast fail before SML round-trip
	existing, _ := h.catalogRepo.GetOne(req.Code)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{
			"error":    "product code already exists",
			"code":     req.Code,
			"existing": existing,
		})
		return
	}

	// 2. Marketplace prices remain authoritative, so Quick Create deliberately
	// omits SML price formulas.
	smlReq := buildQuickCreateProductRequest(req)

	// 3. POST to SML
	statusCode, smlResp, err := h.productClient.CreateProduct(smlReq)
	if err != nil || smlResp == nil || !smlResp.Success {
		errMsg := ""
		switch {
		case err != nil:
			errMsg = err.Error()
		case smlResp != nil && smlResp.GetMessage() != "":
			errMsg = fmt.Sprintf("SML rejected (HTTP %d): %s", statusCode, smlResp.GetMessage())
		default:
			errMsg = fmt.Sprintf("SML rejected (HTTP %d)", statusCode)
		}
		h.logger.Warn("create_product: SML failed", zap.String("code", req.Code), zap.String("error", errMsg))
		c.JSON(http.StatusBadGateway, gin.H{"error": errMsg})
		return
	}

	// 4. Use canonical code from SML response (may differ from request)
	finalCode := smlResp.Data.Code
	if finalCode == "" {
		finalCode = req.Code
	}

	// 5. Upsert into the local deterministic catalog.
	whCode := req.WHCode
	shelfCode := req.ShelfCode
	if err := h.catalogRepo.Upsert(models.CatalogItem{
		ItemCode:        finalCode,
		ItemName:        req.Name,
		UnitCode:        req.UnitCode,
		WHCode:          whCode,
		ShelfCode:       shelfCode,
		EmbeddingStatus: "disabled",
	}); err != nil {
		h.logger.Error("create_product: catalog upsert failed",
			zap.String("code", finalCode), zap.Error(err))
		// SML already accepted — return success, just log the local-sync miss
	}

	// 6. Audit log
	if h.auditRepo != nil {
		_ = h.auditRepo.Log(models.AuditEntry{
			Action: "product_created",
			Source: "ui",
			Level:  "info",
			Detail: map[string]interface{}{
				"requested_code": req.Code,
				"final_code":     finalCode,
				"name":           req.Name,
				"unit_code":      req.UnitCode,
			},
		})
	}

	c.JSON(http.StatusCreated, gin.H{
		"code":       finalCode,
		"name":       req.Name,
		"unit_code":  req.UnitCode,
		"wh_code":    whCode,
		"shelf_code": shelfCode,
		"message":    "product created and synced to catalog",
	})
}

// POST /api/bills/:id/items/:item_id/confirm-match
// Body: {"item_code": "...", "unit_code": "...", "wh_code": "...", "shelf_code": "..."}
func (h *CatalogHandler) ConfirmMatch(c *gin.Context) {
	billID := c.Param("id")
	itemID := c.Param("item_id")
	if billID == "" || itemID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bill_id and item_id required"})
		return
	}

	var req struct {
		ItemCode  string `json:"item_code" binding:"required"`
		UnitCode  string `json:"unit_code"`
		WHCode    string `json:"wh_code"`
		ShelfCode string `json:"shelf_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Look up catalog item for defaults
	catalogItem, _ := h.catalogRepo.GetOne(req.ItemCode)
	unitCode := req.UnitCode
	whCode := req.WHCode
	shelfCode := req.ShelfCode
	if catalogItem != nil {
		if unitCode == "" {
			unitCode = catalogItem.UnitCode
		}
		if whCode == "" {
			whCode = catalogItem.WHCode
		}
		if shelfCode == "" {
			shelfCode = catalogItem.ShelfCode
		}
	}
	if meta := itemcode.Inspect(req.ItemCode); meta.HasHiddenChars {
		if catalogItem == nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":           "item_code has hidden characters and does not exist in SML catalog",
				"item_code":       req.ItemCode,
				"clean_item_code": meta.CleanItemCode,
			})
			return
		}
		h.logHiddenItemCodeWarning(c, billID, itemID, req.ItemCode, meta, "confirm_match")
	}

	// Update bill_item
	db := h.catalogRepo.DB()
	_, err := db.Exec(`
		UPDATE bill_items
		SET item_code = $1, unit_code = $2, mapped = TRUE
		WHERE id = $3 AND bill_id = $4
	`, req.ItemCode, unitCode, itemID, billID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Check if all items in this bill are now mapped
	var unmapped int
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM bill_items WHERE bill_id = $1 AND mapped = FALSE
	`, billID).Scan(&unmapped)

	allConfirmed := unmapped == 0

	// Build response
	resp := gin.H{
		"ok":            true,
		"all_confirmed": allConfirmed,
		"item_code":     req.ItemCode,
		"unit_code":     unitCode,
		"wh_code":       whCode,
		"shelf_code":    shelfCode,
	}
	if meta := itemcode.Inspect(req.ItemCode); meta.HasHiddenChars {
		resp["has_hidden_chars"] = true
		resp["clean_item_code"] = meta.CleanItemCode
	}

	// Optionally store wh/shelf back to bill_item (for SML payload)
	if whCode != "" || shelfCode != "" {
		_, _ = db.Exec(`
			UPDATE bill_items
			SET unit_code = $1
			WHERE id = $2
		`, unitCode, itemID)

		// Store wh/shelf in candidates field as confirmed metadata
		meta, _ := json.Marshal(map[string]string{
			"confirmed_wh":    whCode,
			"confirmed_shelf": shelfCode,
		})
		_, _ = db.Exec(`
			UPDATE bill_items SET candidates = $1 WHERE id = $2
		`, meta, itemID)
	}

	c.JSON(http.StatusOK, resp)
}

func (h *CatalogHandler) logHiddenItemCodeWarning(c *gin.Context, billID, itemID, code string, meta itemcode.Analysis, context string) {
	if h.auditRepo == nil {
		return
	}
	var userID *string
	if uid := c.GetString("user_id"); uid != "" {
		userID = &uid
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action:   "hidden_item_code_detected",
		TargetID: &itemID,
		UserID:   userID,
		Source:   "catalog",
		Level:    "warn",
		TraceID:  c.GetString("trace_id"),
		Detail: map[string]interface{}{
			"context":         context,
			"bill_id":         billID,
			"item_code":       code,
			"clean_item_code": meta.CleanItemCode,
			"kinds":           meta.Kinds,
			"allowed":         true,
			"reason":          "dirty code exists in SML catalog",
		},
	})
}
