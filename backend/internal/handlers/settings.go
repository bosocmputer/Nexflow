package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/middleware"
	"nexflow/internal/repository"
)

// SettingsHandler manages platform column mapping configuration
type SettingsHandler struct {
	platformRepo *repository.PlatformMappingRepo
	logger       *zap.Logger
}

func NewSettingsHandler(platformRepo *repository.PlatformMappingRepo, logger *zap.Logger) *SettingsHandler {
	return &SettingsHandler{platformRepo: platformRepo, logger: logger}
}

// GET /api/settings/column-mappings/:platform
func (h *SettingsHandler) GetColumnMappings(c *gin.Context) {
	platform := strings.ToLower(c.Param("platform"))
	if platform != "lazada" && platform != "shopee" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "platform must be lazada or shopee"})
		return
	}
	mappings, err := h.platformRepo.Get(platform)
	if err != nil {
		h.logger.Error("get column mappings", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load mappings"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"platform": platform, "mappings": mappings})
}

// PUT /api/settings/column-mappings/:platform
// body: {"mappings": [{"field_name": "order_id", "column_name": "Order ID"}, ...]}
func (h *SettingsHandler) UpdateColumnMappings(c *gin.Context) {
	platform := strings.ToLower(c.Param("platform"))
	if platform != "lazada" && platform != "shopee" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "platform must be lazada or shopee"})
		return
	}

	var req struct {
		Mappings []struct {
			FieldName  string `json:"field_name" binding:"required"`
			ColumnName string `json:"column_name" binding:"required"`
		} `json:"mappings" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	claims := middleware.GetClaims(c)
	var userID *string
	if claims != nil {
		userID = &claims.UserID
	}

	for _, m := range req.Mappings {
		if err := h.platformRepo.Upsert(platform, m.FieldName, m.ColumnName, userID); err != nil {
			h.logger.Error("upsert column mapping", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save mapping"})
			return
		}
	}

	// Return updated mappings
	mappings, _ := h.platformRepo.Get(platform)
	c.JSON(http.StatusOK, gin.H{"platform": platform, "mappings": mappings})
}
