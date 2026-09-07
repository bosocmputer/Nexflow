package handlers

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	lineservice "nexflow/internal/services/line"
)

type lineOAQuotaDTO struct {
	LineOAID          string     `json:"line_oa_id"`
	Name              string     `json:"name"`
	QuotaType         string     `json:"quota_type,omitempty"`
	Limit             *int64     `json:"limit"`
	Used              *int64     `json:"used"`
	Remaining         *int64     `json:"remaining"`
	Status            string     `json:"status"`
	CheckedAt         *time.Time `json:"checked_at"`
	IsStale           bool       `json:"is_stale"`
	ErrorCode         string     `json:"error_code,omitempty"`
	RetryAfterSeconds int        `json:"retry_after_seconds,omitempty"`
}

// Quota returns independently cached, per-OA LINE usage. The overview handler
// does not call this method, so LINE latency can never block the settings page.
func (h *LineNotificationHandler) Quota(c *gin.Context) {
	refreshValue := strings.TrimSpace(c.Query("refresh"))
	if refreshValue != "" && refreshValue != "true" && refreshValue != "false" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ค่า refresh ไม่ถูกต้อง"})
		return
	}
	refresh := refreshValue == "true"
	targetOAID := strings.TrimSpace(c.Query("oa_id"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	accounts, err := h.lineOARepo.ListAllContext(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด LINE OA ไม่สำเร็จ"})
		return
	}
	if targetOAID != "" {
		filtered := accounts[:0]
		for _, account := range accounts {
			if account.ID == targetOAID {
				filtered = append(filtered, account)
			}
		}
		accounts = filtered
		if len(accounts) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบ LINE OA"})
			return
		}
	}

	results := make([]lineOAQuotaDTO, len(accounts))
	var wg sync.WaitGroup
	for index, account := range accounts {
		wg.Add(1)
		go func(index int, account *models.LineOAAccount) {
			defer wg.Done()
			results[index] = h.loadLineOAQuota(ctx, account, refresh)
		}(index, account)
	}
	wg.Wait()
	sort.SliceStable(results, func(left, right int) bool {
		return strings.ToLower(results[left].Name) < strings.ToLower(results[right].Name)
	})
	c.JSON(http.StatusOK, gin.H{
		"data":       results,
		"checked_at": time.Now().UTC(),
	})
}

func (h *LineNotificationHandler) loadLineOAQuota(ctx context.Context, account *models.LineOAAccount, refresh bool) lineOAQuotaDTO {
	startedAt := time.Now()
	dto := lineOAQuotaDTO{LineOAID: account.ID, Name: account.Name, Status: "error"}
	fetch := h.quotaFetch
	if fetch == nil {
		fetch = h.fetchLineQuota
	}
	result, err := h.quotaCacheForRequest().Get(ctx, account.ID, refresh, func(fetchCtx context.Context) (lineservice.MessageQuota, error) {
		return fetch(fetchCtx, account)
	})
	if !result.CheckedAt.IsZero() {
		checkedAt := result.CheckedAt.UTC()
		dto.CheckedAt = &checkedAt
	}
	dto.ErrorCode = result.ErrorCode
	dto.RetryAfterSeconds = result.RetryAfterSeconds
	if err == nil {
		dto.QuotaType = result.Quota.Type
		dto.Limit = result.Quota.Limit
		used := result.Quota.Used
		dto.Used = &used
		dto.Remaining = result.Quota.Remaining
		dto.IsStale = result.IsStale
		if result.IsStale {
			dto.Status = "stale"
		} else {
			dto.Status = "ok"
		}
	} else {
		if dto.ErrorCode == "" {
			dto.ErrorCode = lineservice.QuotaErrorCode(err)
		}
	}
	if h.logger != nil {
		h.logger.Info("line_quota_checked",
			zap.String("oa_id", account.ID),
			zap.String("status", dto.Status),
			zap.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		)
	}
	return dto
}

func (h *LineNotificationHandler) fetchLineQuota(ctx context.Context, account *models.LineOAAccount) (lineservice.MessageQuota, error) {
	service, err := lineservice.New(account.ChannelSecret, account.ChannelAccessToken, "")
	if err != nil {
		return lineservice.MessageQuota{}, &lineservice.QuotaAPIError{Code: "line_client_invalid"}
	}
	return service.GetMessageQuota(ctx)
}

func (h *LineNotificationHandler) quotaCacheForRequest() *lineservice.QuotaCache {
	if h.quotaCache == nil {
		h.quotaCache = lineservice.NewQuotaCache()
	}
	return h.quotaCache
}
