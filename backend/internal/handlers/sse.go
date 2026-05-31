package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"nexflow/internal/services/events"
	"nexflow/internal/services/media"
)

// SSEHandler exposes /api/admin/events (the live event stream) and
// /api/admin/events/token (issues short-lived tokens for the EventSource
// query string — EventSource doesn't support custom headers so we can't
// just pass the JWT).
//
// Auth flow:
//  1. Browser already has JWT (from login). Calls POST /api/admin/events/token
//     → server returns a short-lived (5 min) HMAC token bound to the admin's
//     user_id.
//  2. Browser opens EventSource('/api/admin/events?u=<userID>&t=<token>').
//     Server validates token; if good, subscribes the client to the broker
//     and streams events forever.
type SSEHandler struct {
	broker *events.Broker
	signer *media.Signer
}

func NewSSEHandler(broker *events.Broker, signer *media.Signer) *SSEHandler {
	return &SSEHandler{broker: broker, signer: signer}
}

// IssueToken returns a 5-minute token the EventSource can use as ?t=<token>.
// The token is bound to the calling admin's user_id (extracted from JWT
// middleware). Client must include u=<userID> alongside the token so we can
// re-verify the binding on /events.
//
// POST /api/admin/events/token  (JWT-authenticated)
func (h *SSEHandler) IssueToken(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no user_id in context"})
		return
	}
	tok := h.signer.Sign(userID, 5*time.Minute)
	c.JSON(http.StatusOK, gin.H{
		"token":       tok,
		"user_id":     userID,
		"ttl_seconds": 300,
	})
}

// Stream is the actual SSE endpoint. NOT under the JWT-required group —
// the HMAC token IS the auth (EventSource can't send Authorization headers).
//
// GET /api/admin/events?u=<userID>&t=<token>
func (h *SSEHandler) Stream(c *gin.Context) {
	userID := c.Query("u")
	token := c.Query("t")
	if userID == "" || token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "u and t required"})
		return
	}
	if err := h.signer.Verify(userID, token); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	// SSE response headers. X-Accel-Buffering disables nginx/proxy buffering
	// so events flush immediately — Cloudflare Tunnel respects this too.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}
	// The global HTTP server has a WriteTimeout for normal API safety, but
	// this endpoint is a long-lived SSE stream. Clear the per-response write
	// deadline so Go doesn't close the stream every 60 seconds.
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})

	ch, unsub := h.broker.Subscribe()
	defer unsub()

	// Send a hello event so the browser knows we're alive — useful for
	// distinguishing "connected" from "still TLS-handshaking" in the UI.
	fmt.Fprint(c.Writer, "event: hello\ndata: {}\n\n")
	flusher.Flush()

	// Heartbeat every 20s — keeps idle proxies (and Cloudflare Tunnel) from
	// closing the connection on idle timeout (~60s for many proxies).
	// Comment lines starting with `:` are ignored by EventSource clients
	// per the spec, so they're free pings.
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			// Client disconnected (browser tab closed, network drop, etc).
			// Cleanup runs via deferred unsub.
			return

		case ev, alive := <-ch:
			if !alive {
				// Broker closed our channel — happens during shutdown.
				return
			}
			payload, err := json.Marshal(ev.Payload)
			if err != nil {
				// Bad payload → skip this event but keep the stream open.
				continue
			}
			// SSE wire format: event: <type>\ndata: <json>\n\n
			fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", ev.Type, payload)
			flusher.Flush()

		case <-heartbeat.C:
			fmt.Fprint(c.Writer, ":heartbeat\n\n")
			flusher.Flush()
		}
	}
}
