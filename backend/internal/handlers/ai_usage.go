package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

type AIUsageHandler struct {
	repo *repository.AIUsageRepo
	log  *zap.Logger
}

func NewAIUsageHandler(repo *repository.AIUsageRepo, log *zap.Logger) *AIUsageHandler {
	return &AIUsageHandler{repo: repo, log: log}
}

func (h *AIUsageHandler) Summary(c *gin.Context) {
	var f models.AIUsageFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	summary, err := h.repo.Summary(f)
	if err != nil {
		h.log.Error("ai usage summary", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, summary)
}

func (h *AIUsageHandler) Logs(c *gin.Context) {
	var f models.AIUsageFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	logs, total, err := h.repo.List(f)
	if err != nil {
		h.log.Error("ai usage logs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if logs == nil {
		logs = []models.AIUsageLog{}
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      logs,
		"total":     total,
		"page":      f.Page,
		"page_size": f.PageSize,
	})
}
