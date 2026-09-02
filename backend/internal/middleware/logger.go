package middleware

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TraceIDKey is the gin.Context key for the per-request trace ID.
const TraceIDKey = "trace_id"

const CorrelationIDHeader = "X-Correlation-ID"

var correlationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,63}$`)

// NewTraceID returns a random 12-char hex string for tracing requests and background jobs.
func NewTraceID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func Logger(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := strings.TrimSpace(c.GetHeader(CorrelationIDHeader))
		if !correlationIDPattern.MatchString(traceID) {
			traceID = NewTraceID()
		}
		c.Set(TraceIDKey, traceID)
		c.Header(CorrelationIDHeader, traceID)

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
