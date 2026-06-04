package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/events"
)

type NotificationHandler struct {
	repo   *repository.NotificationRepo
	broker *events.Broker
}

func NewNotificationHandler(repo *repository.NotificationRepo, broker *events.Broker) *NotificationHandler {
	return &NotificationHandler{repo: repo, broker: broker}
}

func (h *NotificationHandler) List(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no user_id in context"})
		return
	}
	limit := 30
	if v := strings.TrimSpace(c.Query("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	rows, unread, err := h.repo.ListForUser(c.Request.Context(), userID, models.NotificationFilter{
		UnreadOnly: c.Query("unread") == "true",
		Limit:      limit,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด notification ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "unread": unread})
}

func (h *NotificationHandler) Count(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no user_id in context"})
		return
	}
	n, err := h.repo.UnreadCount(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดจำนวน notification ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"unread": n})
}

func (h *NotificationHandler) MarkRead(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no user_id in context"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "notification id ไม่ถูกต้อง"})
		return
	}
	_, err := h.repo.MarkRead(c.Request.Context(), userID, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "อ่าน notification ไม่สำเร็จ"})
		return
	}
	unread := h.publishUnread(c, userID)
	c.JSON(http.StatusOK, gin.H{"success": true, "unread": unread})
}

func (h *NotificationHandler) MarkAllRead(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no user_id in context"})
		return
	}
	changed, err := h.repo.MarkAllRead(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "อ่าน notification ทั้งหมดไม่สำเร็จ"})
		return
	}
	unread := h.publishUnread(c, userID)
	c.JSON(http.StatusOK, gin.H{"success": true, "changed": changed, "unread": unread})
}

func (h *NotificationHandler) publishUnread(c *gin.Context, userID string) int {
	unread, _ := h.repo.UnreadCount(c.Request.Context(), userID)
	if h.broker != nil {
		h.broker.Publish(events.Event{
			Type:         events.TypeNotificationUnreadChanged,
			TargetUserID: userID,
			Payload:      map[string]any{"total": unread},
		})
	}
	return unread
}
