package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"nexflow/internal/services/marketplacestock"
)

// MarketplaceStockHandler deliberately keeps GET endpoints read-only. All
// external inventory writes will be owned by the durable worker, never by a
// browser request handler.
type MarketplaceStockHandler struct {
	service *marketplacestock.Service
	enabled bool
}

func NewMarketplaceStockHandler(service *marketplacestock.Service, enabled bool) *MarketplaceStockHandler {
	return &MarketplaceStockHandler{service: service, enabled: enabled}
}

func (h *MarketplaceStockHandler) Overview(c *gin.Context) {
	if !h.checkEnabled(c) {
		return
	}
	result, err := h.service.Overview(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *MarketplaceStockHandler) Candidates(c *gin.Context) {
	if !h.checkEnabled(c) {
		return
	}
	result, err := h.service.Candidates(c.Request.Context(), strings.TrimSpace(c.Query("source")))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *MarketplaceStockHandler) CreatePool(c *gin.Context) {
	if !h.checkEnabled(c) {
		return
	}
	var request marketplacestock.PoolInput
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลกลุ่มสต๊อกไม่ถูกต้อง"})
		return
	}
	result, err := h.service.CreatePool(c.Request.Context(), request, c.GetString("user_id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *MarketplaceStockHandler) UpdatePool(c *gin.Context) {
	if !h.checkEnabled(c) {
		return
	}
	poolID := strings.TrimSpace(c.Param("pool_id"))
	if !uuidPattern.MatchString(poolID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pool_id ไม่ถูกต้อง"})
		return
	}
	var request marketplacestock.PoolUpdate
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลกลุ่มสต๊อกไม่ถูกต้อง"})
		return
	}
	result, err := h.service.UpdatePool(c.Request.Context(), poolID, request, c.GetString("user_id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *MarketplaceStockHandler) UpdateSettings(c *gin.Context) {
	if !h.checkEnabled(c) {
		return
	}
	var request marketplacestock.SettingsUpdate
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลตั้งค่าสต๊อกไม่ถูกต้อง"})
		return
	}
	result, err := h.service.UpdateSettings(c.Request.Context(), request, c.GetString("user_id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *MarketplaceStockHandler) checkEnabled(c *gin.Context) bool {
	if h != nil && h.enabled && h.service != nil {
		return true
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Marketplace Stock Control ยังไม่ได้เปิดสำหรับร้านนี้"})
	return false
}

func (h *MarketplaceStockHandler) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, marketplacestock.ErrInvalidPoolInput), errors.Is(err, marketplacestock.ErrInvalidAllocationMode),
		errors.Is(err, marketplacestock.ErrInvalidBufferPercent), errors.Is(err, marketplacestock.ErrInvalidPoolMember),
		errors.Is(err, marketplacestock.ErrInvalidUnitFactor):
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลตั้งค่าสต๊อกไม่ถูกต้อง"})
	case errors.Is(err, marketplacestock.ErrAllocationExceedsOneHundred):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "สัดส่วนโควตารวมต้องไม่เกิน 100%"})
	case errors.Is(err, marketplacestock.ErrSharedRiskNotAcknowledged):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "กรุณายอมรับความเสี่ยงของการใช้สต๊อกร่วมก่อนบันทึก"})
	case errors.Is(err, marketplacestock.ErrConfirmationRequired):
		c.JSON(http.StatusConflict, gin.H{"error": "กรุณายืนยันการเปลี่ยนแปลงก่อนบันทึก"})
	case errors.Is(err, marketplacestock.ErrConfigVersionConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "ข้อมูลถูกแก้ไขโดยผู้ใช้อื่น กรุณาโหลดใหม่ก่อนบันทึก"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกการควบคุมสต๊อกไม่สำเร็จ"})
	}
}
