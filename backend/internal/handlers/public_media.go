package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/repository"
	"nexflow/internal/services/media"
)

// PublicMediaHandler serves chat media bytes via signed, short-lived URLs.
// LINE's servers fetch this endpoint when delivering image messages to
// customers — there is no JWT, only the HMAC token.
type PublicMediaHandler struct {
	mediaRepo *repository.ChatMediaRepo
	signer    *media.Signer
	logger    *zap.Logger
}

func NewPublicMediaHandler(mediaRepo *repository.ChatMediaRepo, signer *media.Signer, logger *zap.Logger) *PublicMediaHandler {
	return &PublicMediaHandler{mediaRepo: mediaRepo, signer: signer, logger: logger}
}

// GET /public/media/:mediaID?t=<token>
//
// No auth — the HMAC token IS the auth. LINE only fetches once per customer
// delivery, but tokens are valid for 1 hour to handle their retry behaviour.
func (h *PublicMediaHandler) Serve(c *gin.Context) {
	mediaID := c.Param("mediaID")
	token := c.Query("t")
	if mediaID == "" || token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return
	}
	if err := h.signer.Verify(mediaID, token); err != nil {
		h.logger.Warn("public media: bad token",
			zap.String("media_id", mediaID), zap.Error(err))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	data, m, err := h.mediaRepo.ReadBytes(mediaID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if m == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ct := m.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	// Cache hint for LINE — public, short-lived. LINE caches images briefly.
	c.Header("Cache-Control", "public, max-age=300")
	c.Header("Content-Disposition", "inline; filename=\""+m.Filename+"\"")
	c.Data(http.StatusOK, ct, data)
}
