package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

type LogHandler struct {
	auditRepo *repository.AuditLogRepo
	log       *zap.Logger
}

func NewLogHandler(auditRepo *repository.AuditLogRepo, log *zap.Logger) *LogHandler {
	return &LogHandler{auditRepo: auditRepo, log: log}
}

// GET /api/logs
func (h *LogHandler) List(c *gin.Context) {
	var f models.AuditLogFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	f.CursorMode = c.Query("cursor") != "" || c.Query("limit") != ""
	if v := c.Query("include_total"); v != "" {
		f.IncludeTotal, _ = strconv.ParseBool(v)
	}

	result, err := h.auditRepo.List(f)
	if err != nil {
		h.log.Error("list audit logs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if result.Logs == nil {
		result.Logs = []models.AuditLog{}
	}

	resp := gin.H{
		"data":        result.Logs,
		"page":        result.Page,
		"page_size":   result.PageSize,
		"limit":       result.PageSize,
		"has_more":    result.HasMore,
		"next_cursor": result.NextCursor,
	}
	if result.Total != nil {
		resp["total"] = *result.Total
	}
	c.JSON(http.StatusOK, resp)
}
