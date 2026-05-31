package jobs

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	lineservice "nexflow/internal/services/line"
)

// TunnelDriftMonitor checks once a day whether `PUBLIC_BASE_URL` (the
// Cloudflare Quick Tunnel URL) is still reachable and returning the
// backend's own /health response. When the cloudflared process restarts,
// `<random>.trycloudflare.com` rolls — the old URL in .env then points at
// nothing and LINE's servers can't fetch images we send.
//
// Why ping our own /health via the public URL instead of reading
// /tmp/nexflow-tunnel.log directly?
//   - The log lives on the host, not inside the backend container; mounting
//     it would add a docker-compose change every admin must apply.
//   - Pinging the public URL tests the END-TO-END path (DNS → Cloudflare →
//     tunnel → backend) which is what we actually care about. A successful
//     fetch proves the URL is good for LINE Push image delivery.
//
// Throttle: at most 1 LINE admin alert per 24h even if drift persists, so
// the channel doesn't get spammed with the same warning every cron tick.
type TunnelDriftMonitor struct {
	publicBaseURL string
	lineSvc       *lineservice.Service
	httpClient    *http.Client
	logger        *zap.Logger

	mu          sync.Mutex
	lastAlerted time.Time
}

func NewTunnelDriftMonitor(publicBaseURL string, lineSvc *lineservice.Service, logger *zap.Logger) *TunnelDriftMonitor {
	return &TunnelDriftMonitor{
		publicBaseURL: publicBaseURL,
		lineSvc:       lineSvc,
		// 10s timeout — Cloudflare typically resolves in <1s; anything
		// longer is a sign the tunnel is degraded and worth alerting on.
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
	}
}

// Register the daily check. 9am Bangkok = 2am UTC. Picked off-the-hour to
// stagger from the other crons (insight 8am, backup midnight, token check
// Mon, disk monitor 7am, reply-token cleanup hourly @ :07).
func (m *TunnelDriftMonitor) Register(c *cron.Cron) {
	// No public URL configured (e.g. dev environment) → nothing to check.
	// Don't register at all rather than no-op every tick.
	if strings.TrimSpace(m.publicBaseURL) == "" {
		m.logger.Info("tunnel_drift_monitor disabled — PUBLIC_BASE_URL not set")
		return
	}
	_, err := c.AddFunc("0 2 * * *", m.runOnce)
	if err != nil {
		m.logger.Error("register tunnel_drift_monitor", zap.Error(err))
	}
}

func (m *TunnelDriftMonitor) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := strings.TrimSuffix(m.publicBaseURL, "/") + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		m.logger.Warn("tunnel_drift_check_build_request", zap.Error(err))
		return
	}

	resp, err := m.httpClient.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		m.logger.Info("tunnel_drift_check_ok",
			zap.String("public_url", m.publicBaseURL),
			zap.Int("status", resp.StatusCode))
		return
	}

	// Capture error context BEFORE closing the body so the alert message
	// includes everything the dev needs to diagnose.
	var failureDetail string
	switch {
	case err != nil:
		failureDetail = err.Error()
	case resp != nil:
		failureDetail = fmt.Sprintf("HTTP %d", resp.StatusCode)
	default:
		failureDetail = "unknown failure"
	}
	if resp != nil {
		resp.Body.Close()
	}

	m.logger.Warn("tunnel_drift_check_failed",
		zap.String("public_url", m.publicBaseURL),
		zap.String("error", failureDetail))

	// Throttle: skip the LINE push if we already alerted in the last 24h.
	// The cron runs daily so this is normally a no-op, but keeps the
	// behavior correct if someone calls runOnce manually for testing.
	m.mu.Lock()
	if time.Since(m.lastAlerted) < 24*time.Hour {
		m.mu.Unlock()
		m.logger.Info("tunnel_drift_alert_throttled",
			zap.Duration("since_last", time.Since(m.lastAlerted)))
		return
	}
	m.lastAlerted = time.Now()
	m.mu.Unlock()

	if m.lineSvc == nil {
		return
	}

	msg := fmt.Sprintf(
		"⚠ Cloudflare Tunnel ใช้งานไม่ได้\n\n"+
			"PUBLIC_BASE_URL: %s\n"+
			"Error: %s\n\n"+
			"วิธีแก้:\n"+
			"1. ssh ไป server (192.168.2.109)\n"+
			"2. grep -oE 'https://[a-z0-9-]+\\.trycloudflare\\.com' /tmp/nexflow-tunnel.log\n"+
			"3. อัพเดต PUBLIC_BASE_URL ใน .env เป็น URL ใหม่\n"+
			"4. docker compose up -d backend\n\n"+
			"ผลกระทบ: admin ส่งรูปให้ลูกค้าใน LINE ไม่ได้จนกว่าจะแก้",
		m.publicBaseURL, failureDetail,
	)
	if pErr := m.lineSvc.PushAdmin(msg); pErr != nil {
		m.logger.Warn("tunnel_drift_alert_push_failed", zap.Error(pErr))
	}
}
