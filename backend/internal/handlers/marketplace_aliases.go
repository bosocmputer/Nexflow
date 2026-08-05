package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

func isMarketplaceSource(source string) bool {
	switch source {
	case "shopee", "lazada", "tiktok":
		return true
	}
	return false
}

func (h *MarketplaceAliasHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	aliases, total, err := h.aliasRepo.List(strings.TrimSpace(c.Query("source")), strings.TrimSpace(c.Query("q")), page, perPage)
	if err != nil {
		h.logger.Error("list marketplace aliases", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดรายการจับคู่สินค้าไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": aliases, "total": total, "page": page, "per_page": perPage})
}

type MarketplaceAliasHandler struct {
	aliasRepo   *repository.MarketplaceAliasRepo
	catalogRepo *repository.SMLCatalogRepo
	auditRepo   *repository.AuditLogRepo
	logger      *zap.Logger
}

func NewMarketplaceAliasHandler(
	aliasRepo *repository.MarketplaceAliasRepo,
	catalogRepo *repository.SMLCatalogRepo,
	auditRepo *repository.AuditLogRepo,
	logger *zap.Logger,
) *MarketplaceAliasHandler {
	return &MarketplaceAliasHandler{aliasRepo: aliasRepo, catalogRepo: catalogRepo, auditRepo: auditRepo, logger: logger}
}

func (h *MarketplaceAliasHandler) ReviewGroups(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	result, err := h.aliasRepo.ReviewGroupsPaged(models.MarketplaceAliasReviewFilter{
		BillType: c.Query("bill_type"),
		Source:   c.Query("source"),
		Query:    c.Query("q"),
		Sort:     c.DefaultQuery("sort", "impact"),
		Page:     page,
		PerPage:  perPage,
	})
	if err != nil {
		h.logger.Error("marketplace alias review groups", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load review groups"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":     result.Groups,
		"total":    result.Total,
		"page":     result.Page,
		"per_page": result.PerPage,
	})
}

func (h *MarketplaceAliasHandler) Confirm(c *gin.Context) {
	var req struct {
		Source        string `json:"source" binding:"required"`
		BillType      string `json:"bill_type" binding:"required"`
		SourceSKU     string `json:"source_sku"`
		RawName       string `json:"raw_name"`
		NormalizedKey string `json:"normalized_key"`
		ItemCode      string `json:"item_code" binding:"required"`
		UnitCode      string `json:"unit_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !isMarketplaceSource(req.Source) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported marketplace source"})
		return
	}

	product, err := h.catalogRepo.GetActive(req.ItemCode)
	if err != nil {
		h.logger.Error("validate marketplace alias product", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ตรวจสอบสินค้า SML ไม่สำเร็จ"})
		return
	}
	if product == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "สินค้านี้ไม่มีอยู่หรือปิดใช้งานใน SML กรุณารีเฟรชสินค้าแล้วเลือกใหม่"})
		return
	}
	unitCode := req.UnitCode
	if unitCode == "" {
		unitCode = product.UnitCode
	}
	userID := c.GetString("user_id")
	alias, err := h.aliasRepo.Upsert(req.Source, req.SourceSKU, req.RawName, req.ItemCode, unitCode, userID)
	if err != nil {
		h.logger.Error("confirm marketplace alias", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save alias"})
		return
	}
	normalizedKey := req.NormalizedKey
	if alias != nil {
		normalizedKey = alias.NormalizedKey
	}
	applied, ready, err := h.aliasRepo.ApplyToOpenItems(req.Source, req.BillType, req.SourceSKU, normalizedKey, req.RawName, req.ItemCode, unitCode)
	if err != nil {
		h.logger.Error("apply marketplace alias", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to apply alias"})
		return
	}
	if h.auditRepo != nil && alias != nil {
		var auditUserID *string
		if userID != "" {
			auditUserID = &userID
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action: "marketplace_alias_confirmed",
			UserID: auditUserID,
			Source: req.Source,
			Level:  "info",
			Detail: map[string]interface{}{
				"alias_id":       alias.ID,
				"source_sku":     alias.SourceSKU,
				"normalized_key": alias.NormalizedKey,
				"raw_name":       alias.RawName,
				"item_code":      alias.ItemCode,
				"unit_code":      alias.UnitCode,
				"applied_items":  applied,
				"ready_bills":    ready,
			},
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"alias":         alias,
		"applied_items": applied,
		"ready_bills":   ready,
	})
}

func (h *MarketplaceAliasHandler) Update(c *gin.Context) {
	var req struct {
		ItemCode  string `json:"item_code" binding:"required"`
		UnitCode  string `json:"unit_code"`
		UpdatedAt string `json:"updated_at" binding:"required"`
		BillType  string `json:"bill_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลแก้ไขไม่ครบ"})
		return
	}
	version, err := time.Parse(time.RFC3339Nano, req.UpdatedAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลเวอร์ชันไม่ถูกต้อง กรุณารีเฟรชหน้า"})
		return
	}
	product, err := h.catalogRepo.GetActive(strings.TrimSpace(req.ItemCode))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ตรวจสอบสินค้า SML ไม่สำเร็จ"})
		return
	}
	if product == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "สินค้านี้ไม่มีอยู่หรือปิดใช้งานใน SML กรุณารีเฟรชสินค้าแล้วเลือกใหม่"})
		return
	}
	unitCode := strings.TrimSpace(req.UnitCode)
	if unitCode == "" {
		unitCode = product.UnitCode
	}
	userID := c.GetString("user_id")
	alias, err := h.aliasRepo.Update(c.Param("id"), product.ItemCode, unitCode, userID, version)
	if errors.Is(err, repository.ErrMarketplaceAliasConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "รายการนี้ถูกแก้ไขโดยผู้ใช้อื่นแล้ว กรุณารีเฟรชหน้า"})
		return
	}
	if err != nil {
		h.logger.Error("update marketplace alias", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "แก้ไขการจับคู่ไม่สำเร็จ"})
		return
	}
	billType := strings.TrimSpace(req.BillType)
	if billType == "" {
		billType = "sale"
	}
	applied, ready, err := h.aliasRepo.ApplyToOpenItems(alias.Source, billType, alias.SourceSKU, alias.NormalizedKey, alias.RawName, alias.ItemCode, alias.UnitCode)
	if err != nil {
		h.logger.Error("apply updated marketplace alias", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกแล้วแต่ปรับเอกสารเปิดไม่สำเร็จ กรุณาลองใหม่"})
		return
	}
	h.audit(c, "marketplace_alias_updated", alias, gin.H{"applied_items": applied, "ready_bills": ready})
	c.JSON(http.StatusOK, gin.H{"alias": alias, "applied_items": applied, "ready_bills": ready})
}

func (h *MarketplaceAliasHandler) Delete(c *gin.Context) {
	var req struct {
		UpdatedAt string `json:"updated_at" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลเวอร์ชันไม่ครบ"})
		return
	}
	version, err := time.Parse(time.RFC3339Nano, req.UpdatedAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลเวอร์ชันไม่ถูกต้อง กรุณารีเฟรชหน้า"})
		return
	}
	deleted, err := h.aliasRepo.Deactivate(c.Param("id"), version)
	if err != nil {
		h.logger.Error("delete marketplace alias", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ลบการจับคู่ไม่สำเร็จ"})
		return
	}
	if !deleted {
		c.JSON(http.StatusConflict, gin.H{"error": "รายการนี้ถูกแก้ไขหรือลบแล้ว กรุณารีเฟรชหน้า"})
		return
	}
	if h.auditRepo != nil {
		userID := c.GetString("user_id")
		var auditUserID *string
		if userID != "" {
			auditUserID = &userID
		}
		_ = h.auditRepo.Log(models.AuditEntry{Action: "marketplace_alias_deleted", UserID: auditUserID, Source: "marketplace_alias", Level: "info", Detail: map[string]interface{}{"alias_id": c.Param("id")}})
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MarketplaceAliasHandler) audit(c *gin.Context, action string, alias *models.MarketplaceItemAlias, detail gin.H) {
	if h.auditRepo == nil || alias == nil {
		return
	}
	userID := c.GetString("user_id")
	var auditUserID *string
	if userID != "" {
		auditUserID = &userID
	}
	if detail == nil {
		detail = gin.H{}
	}
	detail["alias_id"] = alias.ID
	detail["source_sku"] = alias.SourceSKU
	detail["item_code"] = alias.ItemCode
	_ = h.auditRepo.Log(models.AuditEntry{Action: action, UserID: auditUserID, Source: alias.Source, Level: "info", Detail: detail})
}
