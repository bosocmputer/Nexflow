package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/services/sml"
)

// SMLWarehouseHandler proxies SML warehouse/shelf master data through an
// in-memory cache for Bill Detail send dialogs.
type SMLWarehouseHandler struct {
	cache  *sml.WarehouseCache
	logger *zap.Logger
}

func NewSMLWarehouseHandler(cache *sml.WarehouseCache, logger *zap.Logger) *SMLWarehouseHandler {
	return &SMLWarehouseHandler{cache: cache, logger: logger}
}

// GET /api/sml/warehouses?search=&limit=20
func (h *SMLWarehouseHandler) SearchWarehouses(c *gin.Context) {
	if h.cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "warehouse cache not configured"})
		return
	}
	q := c.Query("search")
	limit := queryLimit(c, 20, 100)
	warehouses := h.cache.SearchWarehouses(q, limit)
	whCount, shelfCount := h.cache.Counts()
	c.JSON(http.StatusOK, gin.H{
		"data":       warehouses,
		"warehouses": whCount,
		"shelves":    shelfCount,
		"last_sync":  h.cache.LastSync(),
	})
}

// GET /api/sml/warehouses/:code/shelves?search=&limit=50
func (h *SMLWarehouseHandler) SearchShelves(c *gin.Context) {
	if h.cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "warehouse cache not configured"})
		return
	}
	code := c.Param("code")
	w := h.cache.GetByCode(code)
	if w == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "warehouse not found"})
		return
	}
	q := c.Query("search")
	limit := queryLimit(c, 50, 200)
	shelves := h.cache.SearchShelves(code, q, limit)
	c.JSON(http.StatusOK, gin.H{
		"data":      shelves,
		"total":     len(w.Shelves),
		"last_sync": h.cache.LastSync(),
	})
}

// POST /api/sml/refresh-warehouses
func (h *SMLWarehouseHandler) Refresh(c *gin.Context) {
	if h.cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "warehouse cache not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.cache.RefreshNow(ctx); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	whCount, shelfCount := h.cache.Counts()
	c.JSON(http.StatusOK, gin.H{
		"warehouses": whCount,
		"shelves":    shelfCount,
		"last_sync":  h.cache.LastSync(),
	})
}

// GET /api/sml/warehouses/last-sync
func (h *SMLWarehouseHandler) LastSync(c *gin.Context) {
	if h.cache == nil {
		c.JSON(http.StatusOK, gin.H{
			"warehouses": 0,
			"shelves":    0,
			"last_sync":  nil,
		})
		return
	}
	whCount, shelfCount := h.cache.Counts()
	c.JSON(http.StatusOK, gin.H{
		"warehouses": whCount,
		"shelves":    shelfCount,
		"last_sync":  h.cache.LastSync(),
	})
}

func queryLimit(c *gin.Context, fallback, max int) int {
	limit := fallback
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= max {
			limit = n
		}
	}
	return limit
}
