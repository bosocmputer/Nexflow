package sml

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	defaultReadinessTimeout = 3 * time.Second
	defaultReadinessTTL     = 60 * time.Second
)

type ReadinessStatus struct {
	Configured bool      `json:"configured"`
	Ready      bool      `json:"ready"`
	Status     string    `json:"status"`
	Tenant     string    `json:"tenant,omitempty"`
	Message    string    `json:"message"`
	HTTPStatus int       `json:"http_status,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
	Cached     bool      `json:"cached"`
}

type ReadinessChecker struct {
	cfg        PartyConfig
	httpClient *http.Client
	ttl        time.Duration
	log        *zap.Logger

	mu       sync.Mutex
	cached   ReadinessStatus
	cachedAt time.Time
}

func NewReadinessChecker(cfg PartyConfig, log *zap.Logger) *ReadinessChecker {
	return &ReadinessChecker{
		cfg:     cfg,
		httpClient: &http.Client{
			Timeout: defaultReadinessTimeout,
		},
		ttl: defaultReadinessTTL,
		log: log,
	}
}

func (c *ReadinessChecker) WithHTTPClient(client *http.Client) *ReadinessChecker {
	if client != nil {
		c.httpClient = client
	}
	return c
}

func (c *ReadinessChecker) WithTTL(ttl time.Duration) *ReadinessChecker {
	if ttl > 0 {
		c.ttl = ttl
	}
	return c
}

func (c *ReadinessChecker) IsConfigured() bool {
	return c != nil &&
		strings.TrimSpace(c.cfg.BaseURL) != "" &&
		strings.TrimSpace(c.cfg.GUID) != "" &&
		strings.TrimSpace(c.cfg.Database) != ""
}

func (c *ReadinessChecker) Check(ctx context.Context, force bool) ReadinessStatus {
	if c == nil {
		return ReadinessStatus{
			Configured: false,
			Ready:      false,
			Status:     "not_configured",
			Message:    "ยังไม่ได้ตั้งค่าการตรวจสอบ SML readiness",
			CheckedAt:  time.Now(),
		}
	}
	now := time.Now()
	if !force {
		c.mu.Lock()
		if !c.cachedAt.IsZero() && now.Sub(c.cachedAt) < c.ttl {
			status := c.cached
			status.Cached = true
			c.mu.Unlock()
			return status
		}
		c.mu.Unlock()
	}

	status := c.checkLive(ctx)
	c.mu.Lock()
	c.cached = status
	c.cachedAt = status.CheckedAt
	c.mu.Unlock()
	return status
}

func (c *ReadinessChecker) checkLive(ctx context.Context) ReadinessStatus {
	now := time.Now()
	tenant := strings.TrimSpace(c.cfg.Database)
	if !c.IsConfigured() {
		return ReadinessStatus{
			Configured: false,
			Ready:      false,
			Status:     "not_configured",
			Tenant:     tenant,
			Message:    "ยังไม่ได้ตั้งค่า SML REST URL, API key หรือฐานข้อมูลร้าน",
			CheckedAt:  now,
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.cfg.BaseURL, "/")+"/health/ready", nil)
	if err != nil {
		return c.failure("invalid_request", tenant, 0, err, now)
	}
	req.Header.Set("X-Tenant", tenant)
	req.Header.Set("X-Api-Key", c.cfg.GUID)
	req.Header.Set("guid", c.cfg.GUID)
	req.Header.Set("provider", c.cfg.Provider)
	req.Header.Set("configFileName", c.cfg.ConfigFile)
	req.Header.Set("databaseName", tenant)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.failure("unreachable", tenant, 0, err, now)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ReadinessStatus{
			Configured: true,
			Ready:      false,
			Status:     "auth_failed",
			Tenant:     tenant,
			Message:    "SML API key หรือ tenant ของร้านนี้ไม่ถูกต้อง กรุณาตรวจหน้าการเชื่อมต่อระบบ",
			HTTPStatus: resp.StatusCode,
			CheckedAt:  now,
		}
	}
	if resp.StatusCode != http.StatusOK {
		return ReadinessStatus{
			Configured: true,
			Ready:      false,
			Status:     "not_ready",
			Tenant:     tenant,
			Message:    HumanizeConnectionError(string(body)),
			HTTPStatus: resp.StatusCode,
			CheckedAt:  now,
		}
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ReadinessStatus{
			Configured: true,
			Ready:      false,
			Status:     "unexpected_response",
			Tenant:     tenant,
			Message:    "SML readiness ตอบข้อมูลไม่ถูกต้อง กรุณาตรวจ service sml-api-bybos",
			HTTPStatus: resp.StatusCode,
			CheckedAt:  now,
		}
	}
	if readinessBodyOK(parsed) {
		return ReadinessStatus{
			Configured: true,
			Ready:      true,
			Status:     "ok",
			Tenant:     tenant,
			Message:    "เชื่อมต่อฐานข้อมูล SML ของร้านนี้ได้",
			HTTPStatus: resp.StatusCode,
			CheckedAt:  now,
		}
	}
	return ReadinessStatus{
		Configured: true,
		Ready:      false,
		Status:     "not_ready",
		Tenant:     tenant,
		Message:    HumanizeConnectionError(fmt.Sprint(parsed["message"], " ", parsed["error"])),
		HTTPStatus: resp.StatusCode,
		CheckedAt:  now,
	}
}

func readinessBodyOK(body map[string]any) bool {
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(body["status"])))
	if status == "ok" || status == "ready" {
		return true
	}
	if success, ok := body["success"].(bool); ok && success {
		return true
	}
	return false
}

func (c *ReadinessChecker) failure(status, tenant string, statusCode int, err error, checkedAt time.Time) ReadinessStatus {
	if c.log != nil {
		c.log.Warn("sml_readiness_check_failed",
			zap.String("status", status),
			zap.String("tenant", tenant),
			zap.Error(err),
		)
	}
	return ReadinessStatus{
		Configured: true,
		Ready:      false,
		Status:     status,
		Tenant:     tenant,
		Message:    HumanizeConnectionError(err.Error()),
		HTTPStatus: statusCode,
		CheckedAt:  checkedAt,
	}
}

func HumanizeConnectionError(raw string) string {
	lower := strings.ToLower(raw)
	switch {
	case strings.TrimSpace(raw) == "":
		return "เชื่อมต่อฐานข้อมูล SML ของร้านนี้ไม่ได้ เครื่อง SML/Postgres อาจยังไม่เปิดหรือเครือข่ายยังไม่พร้อม"
	case strings.Contains(lower, "context deadline exceeded"),
		strings.Contains(lower, "timeout"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "no route to host"),
		strings.Contains(lower, "eof"),
		strings.Contains(lower, "customer_count_failed"),
		strings.Contains(lower, "supplier_count_failed"),
		strings.Contains(lower, "branches_count_failed"),
		strings.Contains(lower, "users_count_failed"),
		strings.Contains(lower, "count customers failed"),
		strings.Contains(lower, "count suppliers failed"),
		strings.Contains(lower, "count branches failed"),
		strings.Contains(lower, "count users failed"),
		strings.Contains(lower, "failed to connect"),
		strings.Contains(lower, "server is down"):
		return "เชื่อมต่อฐานข้อมูล SML ของร้านนี้ไม่ได้ เครื่อง SML/Postgres อาจยังไม่เปิดหรือเครือข่ายยังไม่พร้อม"
	case strings.Contains(lower, "tenant"):
		return "tenant ฐานข้อมูล SML ของร้านนี้ไม่ถูกต้องหรือยังไม่ได้อนุญาตใน sml-api-bybos"
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden"):
		return "SML API key หรือสิทธิ์เข้าถึงฐานข้อมูลร้านนี้ไม่ถูกต้อง"
	default:
		return "เชื่อมต่อ SML ของร้านนี้ไม่สำเร็จ กรุณาตรวจเครื่อง SML/Postgres และลองตรวจอีกครั้ง"
	}
}
