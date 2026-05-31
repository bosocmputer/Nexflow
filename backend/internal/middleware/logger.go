package middleware

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TraceIDKey is the gin.Context key for the per-request trace ID.
const TraceIDKey = "trace_id"

// NewTraceID returns a random 12-char hex string for tracing requests and background jobs.
func NewTraceID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func Logger(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := NewTraceID()
		c.Set(TraceIDKey, traceID)

		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		log.Info("request",
			zap.String("trace_id", traceID),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)),
			zap.String("ip", c.ClientIP()),
		)
	}
}
