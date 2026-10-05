package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"nexflow/internal/middleware"
	"nexflow/internal/models"
	"nexflow/internal/repository"
)

type AuthHandler struct {
	userRepo       *repository.UserRepo
	navigationRepo salesOrderRouteReader
	jwtExpHours    int
	log            *zap.Logger
}

type salesOrderRouteReader interface {
	HasConfiguredSalesOrderRoute(context.Context) (bool, error)
}

func NewAuthHandler(userRepo *repository.UserRepo, jwtExpHours int, log *zap.Logger) *AuthHandler {
	return &AuthHandler{userRepo: userRepo, jwtExpHours: jwtExpHours, log: log}
}

// WithNavigationCapabilities lets the authenticated user response describe
// which tenant-level document lanes are configured. Authentication remains
// available if this optional capability lookup is not wired during tests.
func (h *AuthHandler) WithNavigationCapabilities(repo salesOrderRouteReader) *AuthHandler {
	if h != nil {
		h.navigationRepo = repo
	}
	return h
}

// POST /api/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.userRepo.FindByEmail(req.Email)
	if err != nil {
		h.log.Error("FindByEmail", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	h.attachNavigationCapabilities(c.Request.Context(), user)

	token, err := middleware.GenerateToken(user.ID, user.Email, user.Role, h.jwtExpHours)
	if err != nil {
		h.log.Error("GenerateToken", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	user.PasswordHash = ""
	c.JSON(http.StatusOK, models.LoginResponse{Token: token, User: *user})
}

// GET /api/auth/me
func (h *AuthHandler) Me(c *gin.Context) {
	userID, _ := c.Get("user_id")
	user, err := h.userRepo.FindByID(userID.(string))
	if err != nil {
		h.log.Error("FindByID", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}
	h.attachNavigationCapabilities(c.Request.Context(), user)
	user.PasswordHash = ""
	c.JSON(http.StatusOK, user)
}

func (h *AuthHandler) attachNavigationCapabilities(ctx context.Context, user *models.User) {
	if user == nil {
		return
	}
	user.NavigationCapabilities = models.NavigationCapabilities{}
	if h.navigationRepo == nil {
		return
	}
	configured, err := h.navigationRepo.HasConfiguredSalesOrderRoute(ctx)
	if err != nil {
		// Navigation should fail closed rather than advertise a queue that the
		// tenant has not configured. Log only the database failure, never user
		// or route details.
		h.log.Warn("resolve navigation capabilities", zap.Error(err))
		return
	}
	user.NavigationCapabilities.SalesOrdersConfigured = configured
}
