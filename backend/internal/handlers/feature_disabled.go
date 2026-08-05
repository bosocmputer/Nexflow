package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func featureGone(c *gin.Context, message string) {
	c.JSON(http.StatusGone, gin.H{
		"error":   "feature_disabled",
		"message": message,
	})
}

func FeatureGone(message string) gin.HandlerFunc {
	return func(c *gin.Context) {
		featureGone(c, message)
	}
}
