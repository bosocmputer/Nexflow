package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/itemcode"
	"nexflow/internal/services/mapper"
)

type MappingHandler struct {
	mappingRepo *repository.MappingRepo
	mapperSvc   *mapper.Service
	catalogRepo *repository.SMLCatalogRepo
	auditRepo   *repository.AuditLogRepo
	log         *zap.Logger
}

func NewMappingHandler(mappingRepo *repository.MappingRepo, mapperSvc *mapper.Service, catalogRepo *repository.SMLCatalogRepo, auditRepo *repository.AuditLogRepo, log *zap.Logger) *MappingHandler {
	return &MappingHandler{mappingRepo: mappingRepo, mapperSvc: mapperSvc, catalogRepo: catalogRepo, auditRepo: auditRepo, log: log}
}

// GET /api/mappings
func (h *MappingHandler) List(c *gin.Context) {
	mappings, err := h.mappingRepo.ListAll()
	if err != nil {
		h.log.Error("ListAll mappings", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": mappings})
}

// POST /api/mappings
func (h *MappingHandler) Create(c *gin.Context) {
	var req models.CreateMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.ItemCode = strings.TrimSpace(req.ItemCode)
	if !h.validateMappingItemCode(c, req.ItemCode, "mapping_create", "") {
		return
	}

	userID, _ := c.Get("user_id")
	mapping, err := h.mappingRepo.Create(req.RawName, req.ItemCode, req.UnitCode, userID.(string))
	if err != nil {
		h.log.Error("Create mapping", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	h.auditMapping(c, "mapping_created", mapping.ID, gin.H{"raw_name": mapping.RawName, "item_code": mapping.ItemCode})
	c.JSON(http.StatusCreated, mapping)
}

// PUT /api/mappings/:id
func (h *MappingHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		ItemCode  string `json:"item_code" binding:"required"`
		UnitCode  string `json:"unit_code" binding:"required"`
		UpdatedAt string `json:"updated_at" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลแก้ไขไม่ครบ"})
		return
	}
	req.ItemCode = strings.TrimSpace(req.ItemCode)
	if !h.validateMappingItemCode(c, req.ItemCode, "mapping_update", id) {
		return
	}
	version, err := time.Parse(time.RFC3339Nano, req.UpdatedAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลเวอร์ชันไม่ถูกต้อง กรุณารีเฟรชหน้า"})
		return
	}

	userID := c.GetString("user_id")
	mapping, err := h.mappingRepo.UpdateByID(id, req.ItemCode, req.UnitCode, userID, version)
	if errors.Is(err, repository.ErrMappingConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "รายการนี้ถูกแก้ไขโดยผู้ใช้อื่นแล้ว กรุณารีเฟรชหน้า"})
		return
	}
	if err != nil {
		h.log.Error("Update mapping", zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "แก้ไขการจับคู่ไม่สำเร็จ"})
		return
	}
	applied, ready, err := h.mappingRepo.ApplyToOpenNoSKUItems(mapping.RawName, mapping.ItemCode, mapping.UnitCode)
	if err != nil {
		h.log.Error("apply updated mapping", zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกแล้วแต่ปรับเอกสารเปิดไม่สำเร็จ กรุณาลองใหม่"})
		return
	}
	h.auditMapping(c, "mapping_updated", id, gin.H{"raw_name": mapping.RawName, "item_code": mapping.ItemCode, "applied_items": applied, "ready_bills": ready})
	c.JSON(http.StatusOK, gin.H{"mapping": mapping, "applied_items": applied, "ready_bills": ready})
}

// DELETE /api/mappings/:id
func (h *MappingHandler) Delete(c *gin.Context) {
	id := c.Param("id")
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
	deleted, err := h.mappingRepo.DeleteVersion(id, version)
	if err != nil {
		h.log.Error("Delete mapping", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ลบการจับคู่ไม่สำเร็จ"})
		return
	}
	if !deleted {
		c.JSON(http.StatusConflict, gin.H{"error": "รายการนี้ถูกแก้ไขหรือลบแล้ว กรุณารีเฟรชหน้า"})
		return
	}
	h.auditMapping(c, "mapping_deleted", id, nil)
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// GET /api/mappings/stats
func (h *MappingHandler) Stats(c *gin.Context) {
	stats, err := h.mappingRepo.Stats()
	if err != nil {
		h.log.Error("Mapping stats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// POST /api/mappings/feedback — F1 human correction
func (h *MappingHandler) Feedback(c *gin.Context) {
	var req struct {
		BillItemID    string `json:"bill_item_id"`
		RawName       string `json:"raw_name" binding:"required"`
		CorrectedCode string `json:"corrected_item_code" binding:"required"`
		CorrectedUnit string `json:"corrected_unit_code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.CorrectedCode = strings.TrimSpace(req.CorrectedCode)
	if !h.validateMappingItemCode(c, req.CorrectedCode, "mapping_feedback", req.BillItemID) {
		return
	}

	var billItemID *string
	if req.BillItemID != "" {
		billItemID = &req.BillItemID
	}

	if err := h.mapperSvc.LearnFromFeedback(req.RawName, req.CorrectedCode, req.CorrectedUnit, billItemID); err != nil {
		h.log.Error("Feedback: LearnFromFeedback", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "feedback saved"})
}

func (h *MappingHandler) validateMappingItemCode(c *gin.Context, code, context, target string) bool {
	meta := itemcode.Inspect(code)
	if h.catalogRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ตรวจสอบสินค้า SML ไม่สำเร็จ"})
		return false
	}
	cat, err := h.catalogRepo.GetActive(code)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ตรวจสอบสินค้า SML ไม่สำเร็จ"})
		return false
	}
	if cat == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":           "สินค้านี้ไม่มีอยู่หรือปิดใช้งานใน SML กรุณารีเฟรชสินค้าแล้วเลือกใหม่",
			"item_code":       code,
			"clean_item_code": meta.CleanItemCode,
		})
		return false
	}
	if !meta.HasHiddenChars {
		return true
	}
	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		var targetID *string
		if target != "" {
			targetID = &target
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "hidden_item_code_detected",
			TargetID: targetID,
			UserID:   userID,
			Source:   "mappings",
			Level:    "warn",
			TraceID:  c.GetString("trace_id"),
			Detail: map[string]interface{}{
				"context":           context,
				"item_code":         code,
				"clean_item_code":   meta.CleanItemCode,
				"hidden_char_kinds": meta.Kinds,
				"allowed":           true,
				"reason":            "dirty code exists in SML catalog",
			},
		})
	}
	return true
}

func (h *MappingHandler) auditMapping(c *gin.Context, action, target string, detail gin.H) {
	if h.auditRepo == nil {
		return
	}
	userID := c.GetString("user_id")
	var auditUserID *string
	if userID != "" {
		auditUserID = &userID
	}
	var targetID *string
	if target != "" {
		targetID = &target
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action: action, TargetID: targetID, UserID: auditUserID,
		Source: "mappings", Level: "info", TraceID: c.GetString("trace_id"), Detail: detail,
	})
}
