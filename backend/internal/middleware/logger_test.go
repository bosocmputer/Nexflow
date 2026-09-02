package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestLoggerPropagatesValidCorrelationID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Logger(zap.NewNop()))
	router.GET("/", func(c *gin.Context) { c.String(http.StatusOK, c.GetString(TraceIDKey)) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(CorrelationIDHeader, "ui-12345678")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Body.String() != "ui-12345678" || recorder.Header().Get(CorrelationIDHeader) != "ui-12345678" {
		t.Fatalf("body=%q header=%q", recorder.Body.String(), recorder.Header().Get(CorrelationIDHeader))
	}
}

func TestLoggerRejectsUnsafeCorrelationID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Logger(zap.NewNop()))
	router.GET("/", func(c *gin.Context) { c.String(http.StatusOK, c.GetString(TraceIDKey)) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(CorrelationIDHeader, "buyer@example.com")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Body.String() == "buyer@example.com" || len(recorder.Body.String()) != 12 {
		t.Fatalf("unsafe correlation ID was accepted: %q", recorder.Body.String())
	}
}
