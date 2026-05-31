package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"nexflow/internal/middleware"
	"nexflow/internal/models"
	"nexflow/internal/repository"
)

type AuthHandler struct {
	userRepo    *repository.UserRepo
	jwtExpHours int
	log         *zap.Logger
}

func NewAuthHandler(userRepo *repository.UserRepo, jwtExpHours int, log *zap.Logger) *AuthHandler {
	return &AuthHandler{userRepo: userRepo, jwtExpHours: jwtExpHours, log: log}
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
	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}
	user.PasswordHash = ""
	c.JSON(http.StatusOK, user)
}
