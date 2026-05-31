package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"nexflow/internal/middleware"
	"nexflow/internal/models"
	"nexflow/internal/repository"
)

type UserSettingsHandler struct {
	users *repository.UserRepo
	audit *repository.AuditLogRepo
	log   *zap.Logger
}

func NewUserSettingsHandler(users *repository.UserRepo, audit *repository.AuditLogRepo, log *zap.Logger) *UserSettingsHandler {
	return &UserSettingsHandler{users: users, audit: audit, log: log}
}

func (h *UserSettingsHandler) List(c *gin.Context) {
	users, err := h.users.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if users == nil {
		users = []models.User{}
	}
	c.JSON(http.StatusOK, gin.H{"data": users})
}

func (h *UserSettingsHandler) Create(c *gin.Context) {
	var in models.UserUpsertRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	normalizeUserInput(&in)
	if msg := validateUserInput(in, true); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "hash password: " + err.Error()})
		return
	}
	user, err := h.users.Create(in.Email, in.Name, in.Role, string(hash))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.logAudit(c, "user_created", user.ID, gin.H{"email": user.Email, "role": user.Role})
	c.JSON(http.StatusCreated, user)
}

func (h *UserSettingsHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var in models.UserUpsertRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	normalizeUserInput(&in)
	if msg := validateUserInput(in, false); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	current, err := h.users.FindByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if current == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if current.Role == "admin" && in.Role != "admin" {
		remaining, err := h.users.CountAdmins(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if remaining == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ต้องมีผู้ดูแลระบบอย่างน้อย 1 คน"})
			return
		}
	}
	var passwordHash *string
	if strings.TrimSpace(in.Password) != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "hash password: " + err.Error()})
			return
		}
		s := string(hash)
		passwordHash = &s
	}
	user, err := h.users.Update(id, in.Email, in.Name, in.Role, passwordHash)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if user == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	h.logAudit(c, "user_updated", user.ID, gin.H{"email": user.Email, "role": user.Role})
	c.JSON(http.StatusOK, user)
}

func (h *UserSettingsHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	claims := middleware.GetClaims(c)
	if claims != nil && claims.UserID == id {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่สามารถลบผู้ใช้ที่กำลังใช้งานอยู่"})
		return
	}
	current, err := h.users.FindByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if current == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if current.Role == "admin" {
		remaining, err := h.users.CountAdmins(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if remaining == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ต้องมีผู้ดูแลระบบอย่างน้อย 1 คน"})
			return
		}
	}
	if err := h.users.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.logAudit(c, "user_deleted", id, gin.H{"email": current.Email, "role": current.Role})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func normalizeUserInput(in *models.UserUpsertRequest) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	in.Role = strings.ToLower(strings.TrimSpace(in.Role))
}

func validateUserInput(in models.UserUpsertRequest, creating bool) string {
	if in.Email == "" {
		return "กรุณากรอกอีเมล"
	}
	if in.Name == "" {
		return "กรุณากรอกชื่อผู้ใช้"
	}
	switch in.Role {
	case "admin", "staff", "viewer":
	default:
		return "role ต้องเป็น admin, staff หรือ viewer"
	}
	if creating && strings.TrimSpace(in.Password) == "" {
		return "กรุณากรอกรหัสผ่าน"
	}
	if strings.TrimSpace(in.Password) != "" && len([]rune(in.Password)) < 6 {
		return "รหัสผ่านต้องมีอย่างน้อย 6 ตัวอักษร"
	}
	return ""
}

func (h *UserSettingsHandler) logAudit(c *gin.Context, action, targetID string, detail gin.H) {
	if h.audit == nil {
		return
	}
	claims := middleware.GetClaims(c)
	var userID *string
	if claims != nil && claims.UserID != "" {
		userID = &claims.UserID
	}
	if err := h.audit.Log(models.AuditEntry{
		Action:   action,
		TargetID: &targetID,
		UserID:   userID,
		Source:   "settings",
		Level:    "info",
		Detail:   detail,
	}); err != nil && h.log != nil {
		h.log.Warn("user audit log failed", zap.Error(err))
	}
}
