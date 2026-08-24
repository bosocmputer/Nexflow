package handlers

import (
	"encoding/base64"
	"encoding/json"
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
	usableOnly, _ := strconv.ParseBool(c.DefaultQuery("usable_only", "false"))
	aliases, total, err := h.aliasRepo.List(strings.TrimSpace(c.Query("source")), strings.TrimSpace(c.Query("q")), usableOnly, page, perPage)
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
	groupedUI   bool
}

func NewMarketplaceAliasHandler(
	aliasRepo *repository.MarketplaceAliasRepo,
	catalogRepo *repository.SMLCatalogRepo,
	auditRepo *repository.AuditLogRepo,
	groupedUI bool,
	logger *zap.Logger,
) *MarketplaceAliasHandler {
	return &MarketplaceAliasHandler{aliasRepo: aliasRepo, catalogRepo: catalogRepo, auditRepo: auditRepo, groupedUI: groupedUI, logger: logger}
}

type marketplaceGroupCursor struct {
	Source     string `json:"s"`
	AccountKey string `json:"a"`
	ParentKey  string `json:"p"`
}

type marketplaceVariantCursor struct {
	VariantID string `json:"v"`
	ID        string `json:"i"`
}

func encodeMarketplaceCursor(value any) string {
	raw, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeMarketplaceCursor(raw string, target any) error {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	return json.Unmarshal(decoded, target)
}

func marketplacePageLimit(raw string, fallback, maximum int) int {
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit < 1 {
		return fallback
	}
	if limit > maximum {
		return maximum
	}
	return limit
}

func (h *MarketplaceAliasHandler) ProductGroups(c *gin.Context) {
	if !h.groupedUI {
		c.JSON(http.StatusNotFound, gin.H{"error": "grouped_ui_disabled"})
		return
	}
	var cursor marketplaceGroupCursor
	if raw := c.Query("cursor"); raw != "" {
		if err := decodeMarketplaceCursor(raw, &cursor); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cursor ไม่ถูกต้อง กรุณาโหลดหน้าแรกใหม่"})
			return
		}
	}
	groups, hasMore, err := h.aliasRepo.ProductGroups(c.Request.Context(), repository.MarketplaceProductGroupFilter{
		Source: c.Query("source"), Query: c.Query("q"), Status: c.Query("status"),
		Limit:       marketplacePageLimit(c.Query("limit"), 30, 50),
		AfterSource: cursor.Source, AfterAccountKey: cursor.AccountKey, AfterParentKey: cursor.ParentKey,
	})
	if err != nil {
		h.logger.Error("list marketplace product groups", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดกลุ่มสินค้า Marketplace ไม่สำเร็จ"})
		return
	}
	next := ""
	if hasMore && len(groups) > 0 {
		last := groups[len(groups)-1]
		next = encodeMarketplaceCursor(marketplaceGroupCursor{Source: last.Source, AccountKey: last.AccountKey, ParentKey: last.ParentKey})
	}
	c.JSON(http.StatusOK, gin.H{"data": groups, "has_more": hasMore, "next_cursor": next})
}

func (h *MarketplaceAliasHandler) ProductGroupVariants(c *gin.Context) {
	if !h.groupedUI {
		c.JSON(http.StatusNotFound, gin.H{"error": "grouped_ui_disabled"})
		return
	}
	source, accountKey, parentKey := strings.TrimSpace(c.Query("source")), strings.TrimSpace(c.Query("account_key")), strings.TrimSpace(c.Param("parent_key"))
	if !isMarketplaceSource(source) || accountKey == "" || parentKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ขอบเขตกลุ่มสินค้าไม่ครบ"})
		return
	}
	var cursor marketplaceVariantCursor
	if raw := c.Query("cursor"); raw != "" {
		if err := decodeMarketplaceCursor(raw, &cursor); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cursor ไม่ถูกต้อง กรุณาเปิดกลุ่มใหม่"})
			return
		}
	}
	variants, hasMore, err := h.aliasRepo.ProductGroupVariants(c.Request.Context(), repository.MarketplaceProductVariantFilter{
		Source: source, AccountKey: accountKey, ParentKey: parentKey, Query: c.Query("q"), Status: c.Query("status"),
		Limit: marketplacePageLimit(c.Query("limit"), 50, 100), AfterVariantID: cursor.VariantID, AfterID: cursor.ID,
	})
	if err != nil {
		h.logger.Error("list marketplace product variants", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดตัวเลือกสินค้า Marketplace ไม่สำเร็จ"})
		return
	}
	next := ""
	if hasMore && len(variants) > 0 {
		last := variants[len(variants)-1]
		next = encodeMarketplaceCursor(marketplaceVariantCursor{VariantID: last.ExternalVariantID, ID: last.ID})
	}
	c.JSON(http.StatusOK, gin.H{"data": variants, "has_more": hasMore, "next_cursor": next})
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
		Source            string `json:"source" binding:"required"`
		AccountKey        string `json:"account_key"`
		ExternalItemID    string `json:"external_item_id"`
		ExternalVariantID string `json:"external_variant_id"`
		BillType          string `json:"bill_type" binding:"required"`
		SourceSKU         string `json:"source_sku"`
		RawName           string `json:"raw_name"`
		NormalizedKey     string `json:"normalized_key"`
		ItemCode          string `json:"item_code" binding:"required"`
		UnitCode          string `json:"unit_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !isMarketplaceSource(req.Source) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported marketplace source"})
		return
	}
	if req.Source == "shopee" && !strings.HasPrefix(strings.TrimSpace(req.AccountKey), "shop:") {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "รายการ Shopee นี้ไม่ได้ระบุร้าน จึงแก้เฉพาะบิลได้แต่ห้ามสร้าง Product Master"})
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
	if product.ItemType == 3 && !product.SetDocumentValid {
		c.JSON(http.StatusConflict, gin.H{"error": "สินค้าชุดนี้ยังไม่พร้อมสร้างเอกสาร กรุณาแก้ส่วนประกอบใน SML แล้วอัปเดตรายการสินค้า"})
		return
	}
	unitCode := req.UnitCode
	if unitCode == "" {
		unitCode = product.UnitCode
	}
	userID := c.GetString("user_id")
	method := "manual_name"
	if strings.TrimSpace(req.ExternalItemID) != "" {
		method = "manual_identity"
	} else if strings.TrimSpace(req.SourceSKU) != "" {
		method = "manual_sku"
	}
	result, err := h.aliasRepo.SaveAndApply(repository.MarketplaceAliasMutation{
		Identity: models.MarketplaceAliasIdentity{
			Source: req.Source, AccountKey: req.AccountKey, ExternalItemID: req.ExternalItemID,
			ExternalVariantID: req.ExternalVariantID, SourceSKU: req.SourceSKU,
			RawName: req.RawName, NormalizedKey: req.NormalizedKey,
		},
		BillType: req.BillType, ItemCode: req.ItemCode, UnitCode: unitCode,
		MatchMethod: method, ScopeConfirmed: true,
		ConfirmedBy: userID,
	})
	if errors.Is(err, repository.ErrMarketplaceAliasConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "สินค้าต้นทางนี้มีการจับคู่ในขอบเขตร้านแล้ว กรุณารีเฟรชและตรวจรายการเดิม"})
		return
	}
	if err != nil {
		h.logger.Error("confirm marketplace alias", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกการจับคู่ไม่สำเร็จ และยังไม่มีข้อมูลใดถูกเปลี่ยน"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"alias":         result.Alias,
		"applied_items": result.AppliedItems,
		"ready_bills":   result.ReadyBills,
		"impact":        result.Impact,
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
	if product.ItemType == 3 && !product.SetDocumentValid {
		c.JSON(http.StatusConflict, gin.H{"error": "สินค้าชุดนี้ยังไม่พร้อมสร้างเอกสาร กรุณาแก้ส่วนประกอบใน SML แล้วอัปเดตรายการสินค้า"})
		return
	}
	unitCode := strings.TrimSpace(req.UnitCode)
	if unitCode == "" {
		unitCode = product.UnitCode
	}
	userID := c.GetString("user_id")
	current, err := h.aliasRepo.GetByID(c.Param("id"))
	if err != nil || current == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบการจับคู่นี้"})
		return
	}
	result, err := h.aliasRepo.SaveAndApply(repository.MarketplaceAliasMutation{
		ID: current.ID, Identity: models.MarketplaceAliasIdentity{
			Source: current.Source, AccountKey: current.AccountKey, ExternalItemID: current.ExternalItemID,
			ExternalVariantID: current.ExternalVariantID, SourceSKU: current.SourceSKU,
			RawName: current.RawName, NormalizedKey: current.NormalizedKey,
		},
		BillType: req.BillType, ItemCode: product.ItemCode, UnitCode: unitCode,
		MatchMethod: current.MatchMethod, ScopeConfirmed: current.ScopeConfirmed,
		ConfirmedBy: userID, Version: &version,
	})
	if errors.Is(err, repository.ErrMarketplaceAliasConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "รายการนี้ถูกแก้ไขโดยผู้ใช้อื่นแล้ว กรุณารีเฟรชหน้า"})
		return
	}
	if err != nil {
		h.logger.Error("update marketplace alias", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "แก้ไขการจับคู่ไม่สำเร็จ และยังไม่มีข้อมูลใดถูกเปลี่ยน"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"alias": result.Alias, "applied_items": result.AppliedItems, "ready_bills": result.ReadyBills, "impact": result.Impact})
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
	impact, err := h.aliasRepo.DeactivateWithImpact(c.Param("id"), version, c.GetString("user_id"))
	if errors.Is(err, repository.ErrMarketplaceAliasConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "รายการนี้ถูกแก้ไขหรือลบแล้ว กรุณารีเฟรชหน้า"})
		return
	}
	if err != nil {
		h.logger.Error("delete marketplace alias", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ลบการจับคู่ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "impact": impact})
}

func (h *MarketplaceAliasHandler) ImpactPreview(c *gin.Context) {
	var req struct {
		AliasID           string `json:"alias_id"`
		Source            string `json:"source" binding:"required"`
		AccountKey        string `json:"account_key"`
		ExternalItemID    string `json:"external_item_id"`
		ExternalVariantID string `json:"external_variant_id"`
		SourceSKU         string `json:"source_sku"`
		RawName           string `json:"raw_name"`
		NormalizedKey     string `json:"normalized_key"`
		ItemCode          string `json:"item_code" binding:"required"`
		Deactivate        bool   `json:"deactivate"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !isMarketplaceSource(req.Source) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลสำหรับตรวจผลกระทบไม่ครบ"})
		return
	}
	impact, err := h.aliasRepo.PreviewImpact(models.MarketplaceAliasIdentity{
		Source: req.Source, AccountKey: req.AccountKey, ExternalItemID: req.ExternalItemID,
		ExternalVariantID: req.ExternalVariantID, SourceSKU: req.SourceSKU,
		RawName: req.RawName, NormalizedKey: req.NormalizedKey,
	}, req.AliasID, strings.TrimSpace(req.ItemCode))
	if err != nil {
		h.logger.Error("preview marketplace alias impact", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ตรวจผลกระทบไม่สำเร็จ"})
		return
	}
	if req.Deactivate {
		impact.DryRunRequired = impact.StockMappings > 0
	}
	c.JSON(http.StatusOK, impact)
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
