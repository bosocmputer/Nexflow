package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/services/shopeeapi"
)

const (
	shopeeAPIOAuthTTL           = 15 * time.Minute
	shopeeAPIAccessTokenSkew    = 10 * time.Minute
	shopeeAPIRefreshTokenTTL    = 30 * 24 * time.Hour
	shopeeAPIMaxRange           = 15 * 24 * time.Hour
	shopeeAPIDefaultPageSize    = 50
	shopeeAPIMaxDetailBatchSize = 50
)

var shopeeAPIReadyToBillStatuses = []string{"SHIPPED", "TO_CONFIRM_RECEIVE", "COMPLETED"}
var shopeeAPILocalOnlyStatusFilters = map[string]bool{
	// Shopee live order detail returns TO_CONFIRM_RECEIVE, but get_order_list
	// rejects it as an order_status filter. Fetch without status and filter
	// locally from detail to avoid dropping ready-to-bill orders.
	"TO_CONFIRM_RECEIVE": true,
}

type ShopeeAPIReadinessCheck struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Status string `json:"status"` // ok | warning | blocked
	Detail string `json:"detail,omitempty"`
}

type ShopeeAPIStatus struct {
	Enabled          bool                      `json:"enabled"`
	Configured       bool                      `json:"configured"`
	Environment      string                    `json:"environment"`
	BaseURL          string                    `json:"base_url,omitempty"`
	PartnerID        int64                     `json:"partner_id,omitempty"`
	RedirectURL      string                    `json:"redirect_url,omitempty"`
	Connected        bool                      `json:"connected"`
	ShopID           int64                     `json:"shop_id,omitempty"`
	ShopName         string                    `json:"shop_name,omitempty"`
	AccessExpiresAt  string                    `json:"access_expires_at,omitempty"`
	RefreshExpiresAt string                    `json:"refresh_expires_at,omitempty"`
	LastSyncAt       string                    `json:"last_sync_at,omitempty"`
	LastSyncStatus   string                    `json:"last_sync_status,omitempty"`
	LastSyncError    string                    `json:"last_sync_error,omitempty"`
	TokenState       string                    `json:"token_state,omitempty"`
	CanConnect       bool                      `json:"can_connect"`
	CanFetch         bool                      `json:"can_fetch"`
	BlockingReason   string                    `json:"blocking_reason,omitempty"`
	Checks           []ShopeeAPIReadinessCheck `json:"checks"`
}

type ShopeeAPIConnection struct {
	ID               string
	ShopID           int64
	MerchantID       sql.NullInt64
	ShopName         string
	Label            string
	AccessToken      string
	RefreshToken     string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
	Environment      string
	DisabledAt       sql.NullTime
	LastSyncAt       sql.NullTime
	LastSyncStatus   string
	LastSyncError    string
	LastErrorCode    string
	ConnectedAt      time.Time
	UpdatedAt        time.Time
}

type ShopeeAPIConnectionView struct {
	ID               string `json:"id"`
	ShopID           int64  `json:"shop_id"`
	MerchantID       *int64 `json:"merchant_id,omitempty"`
	ShopName         string `json:"shop_name,omitempty"`
	Label            string `json:"label"`
	Environment      string `json:"environment"`
	AccessExpiresAt  string `json:"access_expires_at"`
	RefreshExpiresAt string `json:"refresh_expires_at"`
	DisabledAt       string `json:"disabled_at,omitempty"`
	LastSyncAt       string `json:"last_sync_at,omitempty"`
	LastSyncStatus   string `json:"last_sync_status,omitempty"`
	LastSyncError    string `json:"last_sync_error,omitempty"`
	LastErrorCode    string `json:"last_error_code,omitempty"`
	TokenState       string `json:"token_state"`
	CanFetch         bool   `json:"can_fetch"`
	ConnectedAt      string `json:"connected_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

type ShopeeAPIConnectionPatchRequest struct {
	Label    *string `json:"label"`
	Disabled *bool   `json:"disabled"`
}

type ShopeeAPIPreviewRequest struct {
	ConnectionID   string `json:"connection_id"`
	TimeFrom       string `json:"time_from"`
	TimeTo         string `json:"time_to"`
	TimeRangeField string `json:"time_range_field"`
	OrderStatus    string `json:"order_status"`
	PageSize       int    `json:"page_size"`
	Cursor         string `json:"cursor"`
}

type shopeeOAuthState struct {
	UserID      string
	Environment string
	RedirectURL string
}

// GetAPIStatus returns Shopee Open API readiness and the active shop connection.
func (h *ShopeeImportHandler) GetAPIStatus(c *gin.Context) {
	status := h.shopeeAPIStatus()
	conns, err := h.listShopeeAPIConnections(c.Request.Context(), false)
	if err != nil {
		h.logger.Warn("shopee_api: status connection lookup failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดสถานะ Shopee API ไม่ได้"})
		return
	}
	if len(conns) > 0 {
		conn := &conns[0]
		status.Connected = true
		status.ShopID = conn.ShopID
		status.ShopName = conn.DisplayLabel()
		status.AccessExpiresAt = conn.AccessExpiresAt.Format(time.RFC3339)
		status.RefreshExpiresAt = conn.RefreshExpiresAt.Format(time.RFC3339)
		status.LastSyncStatus = conn.LastSyncStatus
		status.LastSyncError = conn.LastSyncError
		if conn.LastSyncAt.Valid {
			status.LastSyncAt = conn.LastSyncAt.Time.Format(time.RFC3339)
		}
	}
	status.finalizeReadiness(time.Now())
	c.JSON(http.StatusOK, status)
}

// ListAPIConnections returns all Shopee shops connected for the current
// environment. Tokens are never returned to the client.
func (h *ShopeeImportHandler) ListAPIConnections(c *gin.Context) {
	conns, err := h.listShopeeAPIConnections(c.Request.Context(), true)
	if err != nil {
		h.logger.Warn("shopee_api: list connections failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดรายการร้าน Shopee ไม่ได้"})
		return
	}
	out := make([]ShopeeAPIConnectionView, 0, len(conns))
	now := time.Now()
	for i := range conns {
		out = append(out, shopeeAPIConnectionView(&conns[i], now))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// UpdateAPIConnection lets admins label or disable a shop connection without
// deleting token history. Reconnect OAuth can re-enable the same shop later.
func (h *ShopeeImportHandler) UpdateAPIConnection(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing connection id"})
		return
	}
	var req ShopeeAPIConnectionPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request ไม่ถูกต้อง: " + err.Error()})
		return
	}
	labelSet := req.Label != nil
	label := ""
	if labelSet {
		label = strings.TrimSpace(*req.Label)
		if label == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ชื่อร้านต้องไม่ว่าง"})
			return
		}
		if len(label) > 120 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ชื่อร้านยาวเกินไป"})
			return
		}
	}
	disabledSet := req.Disabled != nil
	disabled := false
	if disabledSet {
		disabled = *req.Disabled
	}
	if !labelSet && !disabledSet {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่มีข้อมูลให้แก้ไข"})
		return
	}

	conn, err := h.patchShopeeAPIConnection(c.Request.Context(), id, label, labelSet, disabled, disabledSet)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบร้าน Shopee ที่ต้องการแก้ไข"})
			return
		}
		h.logger.Warn("shopee_api: patch connection failed", zap.String("connection_id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "แก้ไขร้าน Shopee ไม่สำเร็จ"})
		return
	}

	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		targetID := conn.ID
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "shopee_api_connection_updated",
			TargetID: &targetID,
			UserID:   userID,
			Source:   "shopee_api",
			Level:    "info",
			TraceID:  c.GetString("trace_id"),
			Detail: map[string]interface{}{
				"shop_id":       conn.ShopID,
				"label":         conn.Label,
				"disabled":      conn.DisabledAt.Valid,
				"label_changed": labelSet,
			},
		})
	}

	c.JSON(http.StatusOK, shopeeAPIConnectionView(conn, time.Now()))
}

// CreateAPIAuthURL creates a short-lived state and returns the Shopee authorize URL.
func (h *ShopeeImportHandler) CreateAPIAuthURL(c *gin.Context) {
	status := h.shopeeAPIStatus()
	if !status.Enabled || !status.Configured {
		respondShopeeAPIError(c, http.StatusBadRequest, fmt.Errorf("not configured"), "Shopee Open API ยังไม่ได้ตั้งค่า partner_id/key บน server")
		return
	}
	state, err := randomState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "สร้าง OAuth state ไม่ได้"})
		return
	}
	redirectURL := h.shopeeAPIRedirectURL()
	if redirectURL == "" {
		respondShopeeAPIError(c, http.StatusBadRequest, fmt.Errorf("redirect URL is required"), "PUBLIC_BASE_URL หรือ SHOPEE_OPEN_API_REDIRECT_URL ยังไม่พร้อม")
		return
	}
	userID, userStatus, err := h.resolveShopeeOAuthUserID(c.Request.Context(), c)
	if err != nil {
		h.logger.Warn("shopee_api: resolve oauth user failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ตรวจสอบผู้ใช้ไม่สำเร็จ"})
		return
	}
	if userStatus != "" {
		statusCode := http.StatusUnauthorized
		if userStatus == "forbidden" {
			statusCode = http.StatusForbidden
		}
		c.JSON(statusCode, gin.H{
			"error":      shopeeOAuthUserStatusMessage(userStatus),
			"error_code": userStatus,
		})
		return
	}
	if err := h.expirePendingShopeeOAuthStates(c.Request.Context(), userID, status.Environment, redirectURL); err != nil {
		h.logger.Warn("shopee_api: expire pending oauth states failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เตรียม OAuth state ไม่ได้"})
		return
	}
	stateHash := hashState(state)
	_, err = h.billRepo.DB().ExecContext(
		c.Request.Context(),
		`INSERT INTO shopee_api_oauth_states
		   (state_hash, user_id, environment, redirect_url, expires_at)
		 VALUES ($1, $2, $3, $4, NOW() + $5::interval)`,
		stateHash, userID, status.Environment, redirectURL, fmt.Sprintf("%d seconds", int(shopeeAPIOAuthTTL.Seconds())),
	)
	if err != nil {
		h.logger.Warn("shopee_api: insert oauth state failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึก OAuth state ไม่ได้"})
		return
	}
	authURL, err := h.shopeeAPIClient().AuthURL(redirectURL, state, time.Now())
	if err != nil {
		respondShopeeAPIError(c, http.StatusBadRequest, err, "สร้างลิงก์ Shopee OAuth ไม่สำเร็จ")
		return
	}
	c.JSON(http.StatusOK, gin.H{"auth_url": authURL, "redirect_url": redirectURL})
}

func (h *ShopeeImportHandler) resolveShopeeOAuthUserID(ctx context.Context, c *gin.Context) (string, string, error) {
	tokenUserID := strings.TrimSpace(c.GetString("user_id"))
	if tokenUserID == "" {
		return "", "missing_user", nil
	}

	id, role, err := h.findUserIdentity(ctx, "id::text = $1", tokenUserID)
	if err == nil {
		if role != "admin" {
			return "", "forbidden", nil
		}
		return id, "", nil
	}
	if err != sql.ErrNoRows {
		return "", "", err
	}

	tokenEmail := strings.TrimSpace(c.GetString("user_email"))
	if tokenEmail == "" {
		return "", "session_expired", nil
	}
	id, role, err = h.findUserIdentity(ctx, "lower(email) = lower($1)", tokenEmail)
	if err == sql.ErrNoRows {
		return "", "session_expired", nil
	}
	if err != nil {
		return "", "", err
	}
	if role != "admin" {
		return "", "forbidden", nil
	}
	h.logger.Warn(
		"shopee_api: jwt user_id missing from users table; resolved by email",
		zap.String("token_user_id", tokenUserID),
		zap.String("email", tokenEmail),
		zap.String("resolved_user_id", id),
	)
	return id, "", nil
}

func (h *ShopeeImportHandler) findUserIdentity(ctx context.Context, whereClause, arg string) (string, string, error) {
	var id, role string
	err := h.db.QueryRowContext(ctx,
		`SELECT id::text, role FROM users WHERE `+whereClause,
		arg,
	).Scan(&id, &role)
	if err != nil {
		return "", "", err
	}
	return id, role, nil
}

func shopeeOAuthUserStatusMessage(status string) string {
	switch status {
	case "missing_user":
		return "Session ไม่พบผู้ใช้ กรุณา login ใหม่"
	case "session_expired":
		return "Session ผู้ใช้หมดอายุหรือไม่ตรงกับข้อมูลปัจจุบัน กรุณา logout แล้ว login ใหม่"
	case "forbidden":
		return "ผู้ใช้ปัจจุบันไม่มีสิทธิ์เชื่อมต่อร้าน Shopee"
	default:
		return "ตรวจสอบผู้ใช้ไม่สำเร็จ"
	}
}

// APICallback exchanges Shopee's one-time auth code for access/refresh tokens.
func (h *ShopeeImportHandler) APICallback(c *gin.Context) {
	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))
	shopID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("shop_id")), 10, 64)
	if code == "" || shopID <= 0 {
		h.renderShopeeCallback(c, http.StatusBadRequest, "เชื่อมต่อ Shopee ไม่สำเร็จ", "Shopee callback ไม่มี code/shop_id ครบ")
		return
	}

	var createdState *shopeeOAuthState
	var err error
	if state == "" {
		h.logger.Warn("shopee_api: oauth callback missing state; trying latest pending-state fallback", zap.Int64("shop_id", shopID))
		createdState, err = h.consumeLatestPendingShopeeOAuthState(c.Request.Context())
	} else {
		createdState, err = h.consumeShopeeOAuthState(c.Request.Context(), state)
	}
	if err != nil {
		h.logger.Warn("shopee_api: oauth state invalid", zap.Error(err))
		message := "OAuth state หมดอายุหรือถูกใช้ไปแล้ว"
		if state == "" {
			message = "Shopee ไม่ส่ง state กลับมา และไม่พบ session เชื่อมต่อที่ยังใช้ได้ กรุณากลับไป Nexflow แล้วกดเชื่อมต่อ Shopee API ใหม่อีกครั้ง"
		}
		h.renderShopeeCallback(c, http.StatusBadRequest, "เชื่อมต่อ Shopee ไม่สำเร็จ", message)
		return
	}
	tok, err := h.shopeeAPIClient().GetToken(c.Request.Context(), code, shopID)
	if err != nil {
		h.logger.Warn("shopee_api: token exchange failed", zap.Int64("shop_id", shopID), zap.Error(err))
		h.renderShopeeCallback(c, http.StatusBadGateway, "เชื่อมต่อ Shopee ไม่สำเร็จ", err.Error())
		return
	}
	if tok.ShopID > 0 {
		shopID = tok.ShopID
	}
	accessExpires := time.Now().Add(time.Duration(tok.ExpireIn) * time.Second)
	if tok.ExpireIn <= 0 {
		accessExpires = time.Now().Add(4 * time.Hour)
	}
	refreshExpires := time.Now().Add(shopeeAPIRefreshTokenTTL)
	shopName := h.fetchShopeeShopName(c.Request.Context(), tok.AccessToken, shopID)
	if err := h.upsertShopeeAPIConnection(c.Request.Context(), shopID, tok.MerchantID, shopName, tok.AccessToken, tok.RefreshToken, accessExpires, refreshExpires, createdState.UserID, createdState.Environment); err != nil {
		h.logger.Warn("shopee_api: upsert connection failed", zap.Int64("shop_id", shopID), zap.Error(err))
		h.renderShopeeCallback(c, http.StatusInternalServerError, "เชื่อมต่อ Shopee ไม่สำเร็จ", "บันทึก token ไม่สำเร็จ")
		return
	}
	h.renderShopeeCallback(c, http.StatusOK, "เชื่อมต่อ Shopee สำเร็จ", "กลับไปหน้า Nexflow แล้วกดดึงออเดอร์ทดสอบได้เลย")
}

// PreviewFromAPI fetches Shopee orders and returns the same preview shape as
// Shopee Excel. It does not write bills or call SML.
func (h *ShopeeImportHandler) PreviewFromAPI(c *gin.Context) {
	startedAt := time.Now()
	var req ShopeeAPIPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request ไม่ถูกต้อง: " + err.Error()})
		return
	}
	timeFrom, timeTo, err := parseShopeeAPIRange(req.TimeFrom, req.TimeTo)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	conn, err := h.ensureShopeeAPIAccessToken(c.Request.Context(), req.ConnectionID)
	if err != nil {
		msg := shopeeAPIErrorMessage(err, err.Error()).Message
		h.markShopeeAPISync(c.Request.Context(), nil, "error", msg)
		respondShopeeAPIError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = shopeeAPIDefaultPageSize
	}
	if pageSize > shopeeAPIMaxDetailBatchSize {
		pageSize = shopeeAPIMaxDetailBatchSize
	}

	timeField, err := validateShopeeAPITimeField(req.TimeRangeField)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	statusPlan, err := shopeeAPIOrderStatusPlan(req.OrderStatus)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Cursor) != "" && len(statusPlan.RequestStatuses) > 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cursor ใช้ได้เฉพาะเมื่อเลือกสถานะเดียวหรือทุกสถานะ"})
		return
	}

	client := h.shopeeAPIClient()
	orderSNs, more, nextCursor, err := fetchShopeeAPIOrderSNs(
		c.Request.Context(),
		client,
		conn.AccessToken,
		conn.ShopID,
		shopeeapi.OrderListRequest{
			TimeRangeField: timeField,
			TimeFrom:       timeFrom.Unix(),
			TimeTo:         timeTo.Unix(),
			PageSize:       pageSize,
			Cursor:         req.Cursor,
		},
		statusPlan.RequestStatuses,
	)
	if err != nil {
		msg := shopeeAPIErrorMessage(err, "ดึงรายการ order จาก Shopee ไม่สำเร็จ").Message
		h.markShopeeAPISync(c.Request.Context(), &conn.ShopID, "error", msg)
		h.auditShopeeAPIPreview(c, conn, req, timeFrom, timeTo, timeField, statusPlan, "error", msg, time.Since(startedAt), nil)
		respondShopeeAPIError(c, http.StatusBadGateway, err, "ดึงรายการ order จาก Shopee ไม่สำเร็จ")
		return
	}
	detail, err := client.GetOrderDetail(c.Request.Context(), conn.AccessToken, conn.ShopID, orderSNs, shopeeAPIOrderDetailFields())
	if err != nil {
		msg := shopeeAPIErrorMessage(err, "ดึงรายละเอียด order จาก Shopee ไม่สำเร็จ").Message
		h.markShopeeAPISync(c.Request.Context(), &conn.ShopID, "error", msg)
		h.auditShopeeAPIPreview(c, conn, req, timeFrom, timeTo, timeField, statusPlan, "error", msg, time.Since(startedAt), map[string]interface{}{
			"fetched_order_sns": len(orderSNs),
			"more":              more,
			"next_cursor":       nextCursor,
		})
		respondShopeeAPIError(c, http.StatusBadGateway, err, "ดึงรายละเอียด order จาก Shopee ไม่สำเร็จ")
		return
	}
	details := filterShopeeOrderDetailsByStatus(detail.Response.OrderList, statusPlan.LocalStatuses)
	orders, warnings := h.shopeeAPIOrdersToPreview(details)
	if more {
		warnings = append([]string{"Shopee ยังมี order หน้าเพิ่มเติมในช่วงวันที่นี้ กรุณาลดช่วงวันที่หรือเลือกสถานะแยกก่อนยืนยันนำเข้า"}, warnings...)
	}
	shopID := strconv.FormatInt(conn.ShopID, 10)
	dupCount := 0
	for i := range orders {
		orders[i].ShopeeShopID = shopID
		orders[i].ShopeeConnectionID = conn.ID
		orders[i].ShopeeShopLabel = conn.DisplayLabel()
		if billID, exists, _ := h.findShopeeOrderBillIDForShop(orders[i].OrderID, shopID); exists {
			orders[i].Duplicate = true
			orders[i].ExistingBillID = billID
			dupCount++
		}
	}
	preflight := buildShopeePreflight(orders, 0, dupCount)
	importRunID := h.createShopeeImportRun(c, "Shopee API "+time.Now().Format("20060102-150405"), "", orders, warnings, preflight)
	h.markShopeeAPISync(c.Request.Context(), &conn.ShopID, "ok", "")
	h.auditShopeeAPIPreview(c, conn, req, timeFrom, timeTo, timeField, statusPlan, "ok", "", time.Since(startedAt), map[string]interface{}{
		"fetched_order_sns": len(orderSNs),
		"returned_orders":   len(orders),
		"new_orders":        len(orders) - dupCount,
		"duplicate_orders":  dupCount,
		"warnings":          len(warnings),
		"more":              more,
		"next_cursor":       nextCursor,
		"import_run_id":     importRunID,
		"preflight":         preflight,
	})

	c.JSON(http.StatusOK, gin.H{
		"orders":          orders,
		"warnings":        warnings,
		"total_orders":    len(orders),
		"new_count":       len(orders) - dupCount,
		"duplicate_count": dupCount,
		"skipped_count":   0,
		"import_run_id":   importRunID,
		"preflight":       preflight,
		"file_token":      "",
		"more":            more,
		"next_cursor":     nextCursor,
	})
}

func (h *ShopeeImportHandler) auditShopeeAPIPreview(c *gin.Context, conn *ShopeeAPIConnection, req ShopeeAPIPreviewRequest, timeFrom, timeTo time.Time, timeField string, statusPlan shopeeAPIOrderStatusSelection, result, message string, elapsed time.Duration, extra map[string]interface{}) {
	if h.auditRepo == nil || conn == nil {
		return
	}
	var userID *string
	if uid := c.GetString("user_id"); uid != "" {
		userID = &uid
	}
	targetID := conn.ID
	level := "info"
	if result != "ok" {
		level = "error"
	}
	durationMs := int(elapsed.Milliseconds())
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = shopeeAPIDefaultPageSize
	}
	if pageSize > shopeeAPIMaxDetailBatchSize {
		pageSize = shopeeAPIMaxDetailBatchSize
	}
	detail := map[string]interface{}{
		"result":           result,
		"shop_id":          conn.ShopID,
		"shop_label":       conn.DisplayLabel(),
		"connection_id":    conn.ID,
		"time_from":        timeFrom.Format("2006-01-02"),
		"time_to":          timeTo.Format("2006-01-02"),
		"time_range_field": timeField,
		"order_status":     strings.TrimSpace(req.OrderStatus),
		"page_size":        pageSize,
		"cursor_used":      strings.TrimSpace(req.Cursor) != "",
		"request_statuses": statusPlan.RequestStatuses,
		"local_statuses":   statusPlan.LocalStatuses,
	}
	if message != "" {
		detail["message"] = message
	}
	for k, v := range extra {
		detail[k] = v
	}
	if err := h.auditRepo.Log(models.AuditEntry{
		Action:     "shopee_api_preview_requested",
		TargetID:   &targetID,
		UserID:     userID,
		Source:     "shopee_api",
		Level:      level,
		DurationMs: &durationMs,
		TraceID:    c.GetString("trace_id"),
		Detail:     detail,
	}); err != nil {
		h.logger.Warn("shopee_api: audit preview request failed", zap.Error(err))
	}
}

func (c *ShopeeAPIConnection) DisplayLabel() string {
	if c == nil {
		return ""
	}
	if label := strings.TrimSpace(c.Label); label != "" && !isDefaultShopeeShopLabel(label, c.ShopID) {
		return strings.TrimSpace(c.Label)
	}
	if strings.TrimSpace(c.ShopName) != "" {
		return strings.TrimSpace(c.ShopName)
	}
	if strings.TrimSpace(c.Label) != "" {
		return strings.TrimSpace(c.Label)
	}
	if c.ShopID > 0 {
		return defaultShopeeShopLabel(c.ShopID)
	}
	return "Shopee shop"
}

func defaultShopeeShopLabel(shopID int64) string {
	return fmt.Sprintf("Shop %d", shopID)
}

func isDefaultShopeeShopLabel(label string, shopID int64) bool {
	return strings.EqualFold(strings.TrimSpace(label), defaultShopeeShopLabel(shopID))
}

func shopeeAPIConnectionView(conn *ShopeeAPIConnection, now time.Time) ShopeeAPIConnectionView {
	view := ShopeeAPIConnectionView{
		ID:               conn.ID,
		ShopID:           conn.ShopID,
		ShopName:         conn.ShopName,
		Label:            conn.DisplayLabel(),
		Environment:      conn.Environment,
		AccessExpiresAt:  conn.AccessExpiresAt.Format(time.RFC3339),
		RefreshExpiresAt: conn.RefreshExpiresAt.Format(time.RFC3339),
		LastSyncStatus:   conn.LastSyncStatus,
		LastSyncError:    conn.LastSyncError,
		LastErrorCode:    conn.LastErrorCode,
		TokenState:       shopeeTokenState(conn.AccessExpiresAt.Format(time.RFC3339), conn.RefreshExpiresAt.Format(time.RFC3339), now),
		ConnectedAt:      conn.ConnectedAt.Format(time.RFC3339),
		UpdatedAt:        conn.UpdatedAt.Format(time.RFC3339),
	}
	if conn.MerchantID.Valid {
		v := conn.MerchantID.Int64
		view.MerchantID = &v
	}
	if conn.DisabledAt.Valid {
		view.DisabledAt = conn.DisabledAt.Time.Format(time.RFC3339)
	}
	if conn.LastSyncAt.Valid {
		view.LastSyncAt = conn.LastSyncAt.Time.Format(time.RFC3339)
	}
	view.CanFetch = !conn.DisabledAt.Valid && view.TokenState != "refresh_expired"
	return view
}

func (h *ShopeeImportHandler) shopeeAPIStatus() ShopeeAPIStatus {
	env := strings.ToLower(strings.TrimSpace(h.cfg.ShopeeOpenAPIEnv))
	if env == "" {
		env = "sandbox"
	}
	return ShopeeAPIStatus{
		Enabled:     h.cfg.ShopeeOpenAPIEnabled,
		Configured:  h.cfg.ShopeeOpenAPIPartnerID > 0 && strings.TrimSpace(h.cfg.ShopeeOpenAPIPartnerKey) != "",
		Environment: env,
		BaseURL:     h.cfg.ShopeeOpenAPIBaseURL,
		PartnerID:   h.cfg.ShopeeOpenAPIPartnerID,
		RedirectURL: h.shopeeAPIRedirectURL(),
	}
}

func (s *ShopeeAPIStatus) finalizeReadiness(now time.Time) {
	s.Checks = nil
	add := func(key, label string, ok bool, detail string) {
		state := "ok"
		if !ok {
			state = "blocked"
		}
		s.Checks = append(s.Checks, ShopeeAPIReadinessCheck{
			Key:    key,
			Label:  label,
			Status: state,
			Detail: detail,
		})
	}
	addWarning := func(key, label, detail string) {
		s.Checks = append(s.Checks, ShopeeAPIReadinessCheck{
			Key:    key,
			Label:  label,
			Status: "warning",
			Detail: detail,
		})
	}

	add("enabled", "เปิด Shopee Open API บน server", s.Enabled, "ตั้งค่า SHOPEE_OPEN_API_ENABLED=true")
	add("partner_key", "ตั้งค่า Partner ID / Key", s.Configured, "ใส่ Partner ID และ Partner Key ให้ครบใน server env")

	redirectOK, redirectDetail := shopeeRedirectReady(s.RedirectURL)
	add("redirect_url", "Redirect URL พร้อมใช้งาน", redirectOK, redirectDetail)

	baseOK, baseDetail := shopeeBaseURLMatchesEnvironment(s.Environment, s.BaseURL)
	add("base_url", "Base URL ตรงกับ sandbox/live", baseOK, baseDetail)

	if strings.EqualFold(s.Environment, "live") {
		s.Checks = append(s.Checks, ShopeeAPIReadinessCheck{
			Key:    "live_key",
			Label:  "ใช้ Live key หลัง Shopee approve",
			Status: "ok",
			Detail: "environment=live",
		})
	} else {
		addWarning("live_key", "ใช้ Live key หลัง Shopee approve", "ตอนนี้ยังเป็น sandbox ระหว่างรอ Shopee Go-Live approve")
	}

	if s.Connected {
		s.Checks = append(s.Checks, ShopeeAPIReadinessCheck{
			Key:    "shop_connection",
			Label:  "เชื่อมร้านผ่าน OAuth",
			Status: "ok",
			Detail: fmt.Sprintf("shop_id=%d", s.ShopID),
		})
	} else {
		addWarning("shop_connection", "เชื่อมร้านผ่าน OAuth", "ยังไม่มีร้านที่เชื่อมกับ environment นี้")
	}

	if s.Connected {
		s.TokenState = shopeeTokenState(s.AccessExpiresAt, s.RefreshExpiresAt, now)
		tokenStatus := "ok"
		tokenDetail := "access token ยังใช้ได้"
		switch s.TokenState {
		case "access_expiring":
			tokenStatus = "warning"
			tokenDetail = "access token ใกล้หมดอายุ ระบบจะ refresh ก่อนดึงข้อมูล"
		case "refresh_required":
			tokenStatus = "warning"
			tokenDetail = "access token หมดอายุแล้ว แต่ refresh token ยังใช้ได้"
		case "refresh_expired":
			tokenStatus = "blocked"
			tokenDetail = "refresh token หมดอายุ ต้องเชื่อมร้านใหม่"
		}
		s.Checks = append(s.Checks, ShopeeAPIReadinessCheck{
			Key:    "token",
			Label:  "Token พร้อมสำหรับดึง order",
			Status: tokenStatus,
			Detail: tokenDetail,
		})
	}

	if s.LastSyncStatus == "error" && strings.TrimSpace(s.LastSyncError) != "" {
		addWarning("last_sync", "Last sync มี error", s.LastSyncError)
	}

	blockers := []string{}
	for _, check := range s.Checks {
		if check.Status == "blocked" {
			blockers = append(blockers, check.Detail)
		}
	}
	if len(blockers) > 0 {
		s.BlockingReason = blockers[0]
	}
	if s.BlockingReason == "" && !strings.EqualFold(s.Environment, "live") && !s.Connected {
		s.BlockingReason = "รอ Shopee approve แล้วเปลี่ยนเป็น live key ก่อนเชื่อมร้านจริง"
	}
	if s.BlockingReason == "" && !s.Connected {
		s.BlockingReason = "ยังไม่ได้เชื่อมต่อร้าน Shopee"
	}
	if s.TokenState == "refresh_expired" {
		s.BlockingReason = "Shopee refresh token หมดอายุ ต้องเชื่อมร้านใหม่"
	}

	s.CanConnect = s.Enabled && s.Configured && redirectOK && baseOK && (strings.EqualFold(s.Environment, "live") || s.Connected)
	s.CanFetch = s.Enabled && s.Configured && redirectOK && baseOK && s.Connected && s.TokenState != "refresh_expired"
}

func shopeeRedirectReady(raw string) (bool, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, "ตั้งค่า PUBLIC_BASE_URL หรือ SHOPEE_OPEN_API_REDIRECT_URL"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false, "Redirect URL ไม่ถูกต้อง"
	}
	if u.Scheme != "https" {
		return false, "Shopee OAuth ต้องใช้ HTTPS redirect URL"
	}
	if !strings.HasSuffix(u.Path, "/api/shopee-api/callback") {
		return false, "Redirect URL ต้องชี้ไปที่ /api/shopee-api/callback"
	}
	return true, "Redirect URL พร้อม"
}

func shopeeBaseURLMatchesEnvironment(env, raw string) (bool, string) {
	base := strings.ToLower(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if base == "" {
		return false, "SHOPEE_OPEN_API_BASE_URL ว่าง"
	}
	if strings.EqualFold(env, "live") {
		if base != shopeeapi.DefaultLiveBaseURL {
			return false, "environment=live ต้องใช้ https://partner.shopeemobile.com"
		}
		return true, "Live base URL พร้อม"
	}
	if base == shopeeapi.DefaultLiveBaseURL {
		return false, "environment=sandbox แต่ base URL เป็น live"
	}
	return true, "Sandbox base URL พร้อม"
}

func shopeeTokenState(accessRaw, refreshRaw string, now time.Time) string {
	access, _ := time.Parse(time.RFC3339, strings.TrimSpace(accessRaw))
	refresh, _ := time.Parse(time.RFC3339, strings.TrimSpace(refreshRaw))
	if refresh.IsZero() || !now.Before(refresh) {
		return "refresh_expired"
	}
	if access.IsZero() || !now.Before(access) {
		return "refresh_required"
	}
	if !now.Before(access.Add(-shopeeAPIAccessTokenSkew)) {
		return "access_expiring"
	}
	return "access_valid"
}

type shopeeAPIErrorView struct {
	Code      string
	Message   string
	Retryable bool
}

func respondShopeeAPIError(c *gin.Context, status int, err error, fallback string) {
	view := shopeeAPIErrorMessage(err, fallback)
	c.JSON(status, gin.H{
		"error":      view.Message,
		"error_code": view.Code,
		"retryable":  view.Retryable,
	})
}

func shopeeAPIErrorMessage(err error, fallback string) shopeeAPIErrorView {
	raw := strings.TrimSpace(fallback)
	if err != nil {
		raw = strings.TrimSpace(err.Error())
	}
	lower := strings.ToLower(raw)
	switch {
	case lower == "not configured" || strings.Contains(lower, "not configured") || strings.Contains(lower, "partner_id/key") || strings.Contains(lower, "ยังไม่ได้เปิดใช้งาน"):
		return shopeeAPIErrorView{Code: "not_configured", Message: "Shopee Open API ยังไม่ได้ตั้งค่า Partner ID/Key บน server"}
	case strings.Contains(lower, "redirect"):
		return shopeeAPIErrorView{Code: "redirect_not_ready", Message: "Redirect URL ยังไม่พร้อม ให้ตรวจ PUBLIC_BASE_URL และ Shopee Console ว่าตรงกัน"}
	case strings.Contains(lower, "ยังไม่ได้เชื่อมต่อร้าน"):
		return shopeeAPIErrorView{Code: "not_connected", Message: "ยังไม่ได้เชื่อมต่อร้าน Shopee ให้รอ Go-Live approve แล้วกดเชื่อมต่อ API"}
	case strings.Contains(lower, "wrong sign") || strings.Contains(lower, "error_sign") || strings.Contains(lower, "signature"):
		return shopeeAPIErrorView{Code: "bad_signature", Message: "Shopee ปฏิเสธ signature ให้ตรวจ Partner ID/Key และ sandbox/live base URL"}
	case strings.Contains(lower, "access_token") || strings.Contains(lower, "refresh token") || strings.Contains(lower, "token/get") || strings.Contains(lower, "access_token/get"):
		return shopeeAPIErrorView{Code: "token_error", Message: "Shopee token ใช้งานไม่ได้หรือหมดอายุ ให้กดเชื่อมต่อร้านใหม่"}
	case strings.Contains(lower, "permission") || strings.Contains(lower, "access denied") || strings.Contains(lower, "forbidden") || strings.Contains(lower, "not authorized"):
		return shopeeAPIErrorView{Code: "permission_denied", Message: "Shopee ยังไม่อนุญาตสิทธิ์นี้ ให้ตรวจสถานะ Go-Live และ permission ของแอป"}
	case strings.Contains(lower, "rate") || strings.Contains(lower, "too many") || strings.Contains(lower, "http 429"):
		return shopeeAPIErrorView{Code: "rate_limited", Message: "Shopee rate limit ให้รอสักครู่แล้วลองใหม่", Retryable: true}
	case strings.Contains(lower, "deadline") || strings.Contains(lower, "timeout") || strings.Contains(lower, "connection reset") || strings.Contains(lower, "temporary"):
		return shopeeAPIErrorView{Code: "network_timeout", Message: "เชื่อมต่อ Shopee ชั่วคราวไม่สำเร็จ ให้ลองใหม่อีกครั้ง", Retryable: true}
	}
	if raw == "" {
		raw = fallback
	}
	return shopeeAPIErrorView{Code: "unknown", Message: raw}
}

func (h *ShopeeImportHandler) shopeeAPIClient() *shopeeapi.Client {
	baseURL := h.cfg.ShopeeOpenAPIBaseURL
	if strings.TrimSpace(baseURL) == "" {
		if strings.EqualFold(h.cfg.ShopeeOpenAPIEnv, "live") {
			baseURL = shopeeapi.DefaultLiveBaseURL
		} else {
			baseURL = shopeeapi.DefaultSandboxBaseURL
		}
	}
	return shopeeapi.New(shopeeapi.Config{
		BaseURL:    baseURL,
		PartnerID:  h.cfg.ShopeeOpenAPIPartnerID,
		PartnerKey: h.cfg.ShopeeOpenAPIPartnerKey,
	})
}

func (h *ShopeeImportHandler) shopeeAPIRedirectURL() string {
	if strings.TrimSpace(h.cfg.ShopeeOpenAPIRedirect) != "" {
		return strings.TrimSpace(h.cfg.ShopeeOpenAPIRedirect)
	}
	base := strings.TrimRight(strings.TrimSpace(h.cfg.PublicBaseURL), "/")
	if base == "" {
		return ""
	}
	return base + "/api/shopee-api/callback"
}

func (h *ShopeeImportHandler) consumeShopeeOAuthState(ctx context.Context, state string) (*shopeeOAuthState, error) {
	var out shopeeOAuthState
	err := h.billRepo.DB().QueryRowContext(ctx,
		`UPDATE shopee_api_oauth_states
		    SET consumed_at = NOW()
		  WHERE state_hash = $1
		    AND consumed_at IS NULL
		    AND expires_at > NOW()
		  RETURNING user_id::text, environment, redirect_url`,
		hashState(state),
	).Scan(&out.UserID, &out.Environment, &out.RedirectURL)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (h *ShopeeImportHandler) expirePendingShopeeOAuthStates(ctx context.Context, userID, env, redirectURL string) error {
	_, err := h.billRepo.DB().ExecContext(ctx,
		`UPDATE shopee_api_oauth_states
		    SET consumed_at = NOW()
		  WHERE user_id = $1::uuid
		    AND environment = $2
		    AND redirect_url = $3
		    AND consumed_at IS NULL
		    AND expires_at > NOW()`,
		userID, defaultShopeeAPIEnv(env), redirectURL,
	)
	return err
}

func (h *ShopeeImportHandler) consumeLatestPendingShopeeOAuthState(ctx context.Context) (*shopeeOAuthState, error) {
	env := defaultShopeeAPIEnv(h.cfg.ShopeeOpenAPIEnv)
	redirectURL := h.shopeeAPIRedirectURL()
	if redirectURL == "" {
		return nil, fmt.Errorf("redirect URL is required")
	}

	var out shopeeOAuthState
	err := h.billRepo.DB().QueryRowContext(ctx,
		`WITH picked AS (
		    SELECT state_hash
		      FROM shopee_api_oauth_states
		     WHERE consumed_at IS NULL
		       AND expires_at > NOW()
		       AND environment = $1
		       AND redirect_url = $2
		     ORDER BY created_at DESC
		     LIMIT 1
		  )
		  UPDATE shopee_api_oauth_states AS s
		     SET consumed_at = NOW()
		    FROM picked
		   WHERE s.state_hash = picked.state_hash
		     AND s.consumed_at IS NULL
		   RETURNING s.user_id::text, s.environment, s.redirect_url`,
		env, redirectURL,
	).Scan(&out.UserID, &out.Environment, &out.RedirectURL)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (h *ShopeeImportHandler) getShopeeAPIConnection(ctx context.Context) (*ShopeeAPIConnection, error) {
	return h.resolveShopeeAPIConnection(ctx, "")
}

func (h *ShopeeImportHandler) resolveShopeeAPIConnection(ctx context.Context, connectionID string) (*ShopeeAPIConnection, error) {
	connectionID = strings.TrimSpace(connectionID)
	var c ShopeeAPIConnection
	baseSelect := `SELECT id::text, shop_id, merchant_id, shop_name, label, access_token, refresh_token,
		        access_expires_at, refresh_expires_at, environment, disabled_at,
		        last_sync_at, last_sync_status, last_sync_error, last_error_code,
		        connected_at, updated_at
		   FROM shopee_api_connections`
	var err error
	if connectionID != "" {
		err = h.billRepo.DB().QueryRowContext(ctx,
			baseSelect+`
			  WHERE id = $1::uuid
			    AND environment = $2`,
			connectionID, defaultShopeeAPIEnv(h.cfg.ShopeeOpenAPIEnv),
		).Scan(
			&c.ID, &c.ShopID, &c.MerchantID, &c.ShopName, &c.Label, &c.AccessToken, &c.RefreshToken,
			&c.AccessExpiresAt, &c.RefreshExpiresAt, &c.Environment, &c.DisabledAt,
			&c.LastSyncAt, &c.LastSyncStatus, &c.LastSyncError, &c.LastErrorCode,
			&c.ConnectedAt, &c.UpdatedAt,
		)
	} else {
		rows, queryErr := h.billRepo.DB().QueryContext(ctx,
			baseSelect+`
			  WHERE environment = $1
			    AND disabled_at IS NULL
			  ORDER BY updated_at DESC
			  LIMIT 2`,
			defaultShopeeAPIEnv(h.cfg.ShopeeOpenAPIEnv),
		)
		if queryErr != nil {
			return nil, queryErr
		}
		defer rows.Close()
		var conns []ShopeeAPIConnection
		for rows.Next() {
			var row ShopeeAPIConnection
			if scanErr := rows.Scan(
				&row.ID, &row.ShopID, &row.MerchantID, &row.ShopName, &row.Label, &row.AccessToken, &row.RefreshToken,
				&row.AccessExpiresAt, &row.RefreshExpiresAt, &row.Environment, &row.DisabledAt,
				&row.LastSyncAt, &row.LastSyncStatus, &row.LastSyncError, &row.LastErrorCode,
				&row.ConnectedAt, &row.UpdatedAt,
			); scanErr != nil {
				return nil, scanErr
			}
			conns = append(conns, row)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(conns) == 0 {
			return nil, sql.ErrNoRows
		}
		if len(conns) > 1 {
			return nil, fmt.Errorf("พบร้าน Shopee ที่เชื่อมไว้มากกว่า 1 ร้าน กรุณาเลือกร้านก่อนดึง order")
		}
		c = conns[0]
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (h *ShopeeImportHandler) listShopeeAPIConnections(ctx context.Context, includeDisabled bool) ([]ShopeeAPIConnection, error) {
	whereDisabled := "AND disabled_at IS NULL"
	if includeDisabled {
		whereDisabled = ""
	}
	rows, err := h.billRepo.DB().QueryContext(ctx,
		`SELECT id::text, shop_id, merchant_id, shop_name, label, access_token, refresh_token,
		        access_expires_at, refresh_expires_at, environment, disabled_at,
		        last_sync_at, last_sync_status, last_sync_error, last_error_code,
		        connected_at, updated_at
		   FROM shopee_api_connections
		  WHERE environment = $1 `+whereDisabled+`
		  ORDER BY disabled_at NULLS FIRST, updated_at DESC`,
		defaultShopeeAPIEnv(h.cfg.ShopeeOpenAPIEnv),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShopeeAPIConnection{}
	for rows.Next() {
		var c ShopeeAPIConnection
		if err := rows.Scan(
			&c.ID, &c.ShopID, &c.MerchantID, &c.ShopName, &c.Label, &c.AccessToken, &c.RefreshToken,
			&c.AccessExpiresAt, &c.RefreshExpiresAt, &c.Environment, &c.DisabledAt,
			&c.LastSyncAt, &c.LastSyncStatus, &c.LastSyncError, &c.LastErrorCode,
			&c.ConnectedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	h.backfillMissingShopeeShopNames(ctx, out)
	return out, nil
}

func (h *ShopeeImportHandler) patchShopeeAPIConnection(ctx context.Context, id, label string, labelSet bool, disabled bool, disabledSet bool) (*ShopeeAPIConnection, error) {
	var c ShopeeAPIConnection
	err := h.billRepo.DB().QueryRowContext(ctx,
		`UPDATE shopee_api_connections
		    SET label = CASE WHEN $2 THEN $3 ELSE label END,
		        disabled_at = CASE
		          WHEN $4 AND $5 THEN NOW()
		          WHEN $4 AND NOT $5 THEN NULL
		          ELSE disabled_at
		        END,
		        updated_at = NOW()
		  WHERE id = $1::uuid
		    AND environment = $6
		  RETURNING id::text, shop_id, merchant_id, shop_name, label, access_token, refresh_token,
		        access_expires_at, refresh_expires_at, environment, disabled_at,
		        last_sync_at, last_sync_status, last_sync_error, last_error_code,
		        connected_at, updated_at`,
		id, labelSet, label, disabledSet, disabled, defaultShopeeAPIEnv(h.cfg.ShopeeOpenAPIEnv),
	).Scan(
		&c.ID, &c.ShopID, &c.MerchantID, &c.ShopName, &c.Label, &c.AccessToken, &c.RefreshToken,
		&c.AccessExpiresAt, &c.RefreshExpiresAt, &c.Environment, &c.DisabledAt,
		&c.LastSyncAt, &c.LastSyncStatus, &c.LastSyncError, &c.LastErrorCode,
		&c.ConnectedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (h *ShopeeImportHandler) upsertShopeeAPIConnection(ctx context.Context, shopID, merchantID int64, shopName, accessToken, refreshToken string, accessExpires, refreshExpires time.Time, userID, env string) error {
	shopName = strings.TrimSpace(shopName)
	defaultLabel := defaultShopeeShopLabel(shopID)
	label := defaultLabel
	if shopName != "" {
		label = shopName
	}
	_, err := h.billRepo.DB().ExecContext(ctx,
		`INSERT INTO shopee_api_connections
		  (shop_id, merchant_id, shop_name, label, access_token, refresh_token, access_expires_at, refresh_expires_at, environment, connected_by)
		 VALUES ($1, NULLIF($2, 0), $3, $4, $5, $6, $7, $8, $9, NULLIF($10, '')::uuid)
		 ON CONFLICT (shop_id) DO UPDATE
		    SET merchant_id = COALESCE(EXCLUDED.merchant_id, shopee_api_connections.merchant_id),
		        shop_name = COALESCE(NULLIF(EXCLUDED.shop_name, ''), shopee_api_connections.shop_name),
		        label = CASE
		          WHEN shopee_api_connections.label = ''
		            OR shopee_api_connections.label = $11
		          THEN COALESCE(NULLIF(EXCLUDED.shop_name, ''), EXCLUDED.label)
		          ELSE shopee_api_connections.label
		        END,
		        access_token = EXCLUDED.access_token,
		        refresh_token = EXCLUDED.refresh_token,
		        access_expires_at = EXCLUDED.access_expires_at,
		        refresh_expires_at = EXCLUDED.refresh_expires_at,
		        environment = EXCLUDED.environment,
		        connected_by = EXCLUDED.connected_by,
		        connected_at = NOW(),
		        disabled_at = NULL,
		        updated_at = NOW(),
		        last_sync_status = '',
		        last_sync_error = '',
		        last_error_code = ''`,
		shopID, merchantID, shopName, label, accessToken, refreshToken, accessExpires, refreshExpires,
		defaultShopeeAPIEnv(env), userID, defaultLabel,
	)
	return err
}

func (h *ShopeeImportHandler) fetchShopeeShopName(ctx context.Context, accessToken string, shopID int64) string {
	if strings.TrimSpace(accessToken) == "" || shopID <= 0 {
		return ""
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	info, err := h.shopeeAPIClient().GetShopInfo(lookupCtx, accessToken, shopID)
	if err == nil {
		if shopName := strings.TrimSpace(info.Response.ShopName); shopName != "" {
			return shopName
		}
	} else {
		h.logger.Warn("shopee_api: get shop info failed", zap.Int64("shop_id", shopID), zap.Error(err))
	}
	profile, err := h.shopeeAPIClient().GetShopProfile(lookupCtx, accessToken, shopID)
	if err != nil {
		h.logger.Warn("shopee_api: get shop profile failed", zap.Int64("shop_id", shopID), zap.Error(err))
		return ""
	}
	return strings.TrimSpace(profile.Response.ShopName)
}

func (h *ShopeeImportHandler) backfillMissingShopeeShopNames(ctx context.Context, conns []ShopeeAPIConnection) {
	if len(conns) == 0 {
		return
	}
	for i := range conns {
		conn := &conns[i]
		if conn.DisabledAt.Valid || conn.ShopID <= 0 || strings.TrimSpace(conn.AccessToken) == "" {
			continue
		}
		if strings.TrimSpace(conn.ShopName) != "" && !isDefaultShopeeShopLabel(conn.Label, conn.ShopID) {
			continue
		}
		if !time.Now().Before(conn.AccessExpiresAt.Add(-shopeeAPIAccessTokenSkew)) {
			continue
		}
		shopName := h.fetchShopeeShopName(ctx, conn.AccessToken, conn.ShopID)
		if shopName == "" {
			continue
		}
		if err := h.updateShopeeAPIShopName(ctx, conn.ShopID, shopName); err != nil {
			h.logger.Warn("shopee_api: update shop name failed", zap.Int64("shop_id", conn.ShopID), zap.Error(err))
			continue
		}
		conn.ShopName = shopName
		if isDefaultShopeeShopLabel(conn.Label, conn.ShopID) || strings.TrimSpace(conn.Label) == "" {
			conn.Label = shopName
		}
	}
}

func (h *ShopeeImportHandler) updateShopeeAPIShopName(ctx context.Context, shopID int64, shopName string) error {
	shopName = strings.TrimSpace(shopName)
	if shopName == "" || shopID <= 0 {
		return nil
	}
	_, err := h.billRepo.DB().ExecContext(ctx,
		`UPDATE shopee_api_connections
		    SET shop_name = $2,
		        label = CASE
		          WHEN label = '' OR label = $3 THEN $2
		          ELSE label
		        END,
		        updated_at = NOW()
		  WHERE shop_id = $1`,
		shopID, shopName, defaultShopeeShopLabel(shopID),
	)
	return err
}

func (h *ShopeeImportHandler) ensureShopeeAPIAccessToken(ctx context.Context, connectionID string) (*ShopeeAPIConnection, error) {
	status := h.shopeeAPIStatus()
	if !status.Enabled || !status.Configured {
		return nil, fmt.Errorf("Shopee Open API ยังไม่ได้เปิดใช้งานหรือตั้งค่า partner_id/key")
	}
	conn, err := h.resolveShopeeAPIConnection(ctx, connectionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("ยังไม่ได้เชื่อมต่อร้าน Shopee API")
		}
		return nil, err
	}
	if conn.DisabledAt.Valid {
		return nil, fmt.Errorf("ร้าน Shopee นี้ถูกปิดใช้งาน กรุณาเลือกหรือเชื่อมร้านอื่น")
	}
	if time.Now().Before(conn.AccessExpiresAt.Add(-shopeeAPIAccessTokenSkew)) {
		return conn, nil
	}
	tok, err := h.shopeeAPIClient().RefreshToken(ctx, conn.RefreshToken, conn.ShopID)
	if err != nil {
		return nil, err
	}
	conn.AccessToken = tok.AccessToken
	conn.RefreshToken = tok.RefreshToken
	conn.AccessExpiresAt = time.Now().Add(time.Duration(tok.ExpireIn) * time.Second)
	if tok.ExpireIn <= 0 {
		conn.AccessExpiresAt = time.Now().Add(4 * time.Hour)
	}
	conn.RefreshExpiresAt = time.Now().Add(shopeeAPIRefreshTokenTTL)
	_, err = h.billRepo.DB().ExecContext(ctx,
		`UPDATE shopee_api_connections
		    SET access_token = $2,
		        refresh_token = $3,
		        access_expires_at = $4,
		        refresh_expires_at = $5,
		        last_refreshed_at = NOW(),
		        updated_at = NOW()
		  WHERE shop_id = $1`,
		conn.ShopID, conn.AccessToken, conn.RefreshToken, conn.AccessExpiresAt, conn.RefreshExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (h *ShopeeImportHandler) markShopeeAPISync(ctx context.Context, shopID *int64, status, msg string) {
	if shopID == nil {
		return
	}
	if len(msg) > 500 {
		msg = msg[:500]
	}
	_, err := h.billRepo.DB().ExecContext(ctx,
		`UPDATE shopee_api_connections
		    SET last_sync_at = NOW(),
		        last_sync_status = $2,
		        last_sync_error = $3,
		        updated_at = NOW()
		  WHERE shop_id = $1`,
		*shopID, status, msg,
	)
	if err != nil {
		h.logger.Warn("shopee_api: mark sync failed", zap.Error(err))
	}
}

func fetchShopeeAPIOrderSNs(ctx context.Context, client *shopeeapi.Client, accessToken string, shopID int64, baseReq shopeeapi.OrderListRequest, statusFilters []string) ([]string, bool, string, error) {
	if len(statusFilters) == 0 {
		list, err := client.GetOrderList(ctx, accessToken, shopID, baseReq)
		if err != nil {
			return nil, false, "", err
		}
		return orderSNsFromShopeeList(list), list.Response.More, list.Response.NextCursor, nil
	}

	seen := map[string]bool{}
	out := make([]string, 0, baseReq.PageSize)
	more := false
	for _, status := range statusFilters {
		req := baseReq
		req.OrderStatus = status
		req.Cursor = ""
		list, err := client.GetOrderList(ctx, accessToken, shopID, req)
		if err != nil {
			return nil, false, "", err
		}
		if list.Response.More {
			more = true
		}
		for _, sn := range orderSNsFromShopeeList(list) {
			if seen[sn] {
				continue
			}
			seen[sn] = true
			out = append(out, sn)
		}
	}

	limit := baseReq.PageSize
	if limit <= 0 || limit > shopeeAPIMaxDetailBatchSize {
		limit = shopeeAPIDefaultPageSize
	}
	if len(out) > limit {
		out = out[:limit]
		more = true
	}
	return out, more, "", nil
}

func orderSNsFromShopeeList(list *shopeeapi.OrderListResponse) []string {
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list.Response.OrderList))
	for _, o := range list.Response.OrderList {
		if sn := strings.TrimSpace(o.OrderSN); sn != "" {
			out = append(out, sn)
		}
	}
	return out
}

func (h *ShopeeImportHandler) shopeeAPIOrdersToPreview(details []shopeeapi.OrderDetail) ([]ShopeeOrder, []string) {
	orders := make([]ShopeeOrder, 0, len(details))
	warnings := []string{}
	for _, d := range details {
		orderSN := strings.TrimSpace(d.OrderSN)
		if orderSN == "" {
			continue
		}
		docTime := shopeeUnixTime(d.CreateTime)
		payTime := shopeeUnixTime(d.PayTime)
		docDate := time.Now().Format("2006-01-02")
		orderDateTime := ""
		if !docTime.IsZero() {
			docDate = docTime.Format("2006-01-02")
			orderDateTime = docTime.Format(time.RFC3339)
		}
		items := make([]ShopeeExcelItem, 0, len(d.ItemList))
		var totalQty, gross float64
		noSKUCount := 0
		for _, item := range d.ItemList {
			qty := item.ModelQuantityPurchased
			if qty <= 0 {
				qty = 1
			}
			price := item.ModelDiscountedPrice
			if price <= 0 {
				price = item.ModelOriginalPrice
			}
			sku := strings.TrimSpace(item.ModelSKU)
			if sku == "" {
				sku = strings.TrimSpace(item.ItemSKU)
			}
			rawName := shopeeItemRawName(item.ItemName, item.ModelName, "")
			noSKU := sku == ""
			if noSKU {
				noSKUCount++
			}
			items = append(items, ShopeeExcelItem{
				SKU:         sku,
				OrderItemID: strconv.FormatInt(item.ItemID, 10),
				ProductName: strings.TrimSpace(item.ItemName),
				OptionName:  strings.TrimSpace(item.ModelName),
				RawName:     rawName,
				Price:       price,
				Qty:         qty,
				NoSKU:       noSKU,
			})
			totalQty += qty
			gross += price * qty
		}
		if len(items) == 0 {
			warnings = append(warnings, fmt.Sprintf("Order %s: ไม่มีสินค้าใน response — ข้ามไป", orderSN))
			continue
		}
		paymentTime := ""
		if !payTime.IsZero() {
			paymentTime = payTime.Format(time.RFC3339)
		}
		paid := d.TotalAmount
		shippingAmount := d.ActualShippingFee
		if shippingAmount <= 0 {
			shippingAmount = d.EstimatedShippingFee
		}
		shippingAmount = roundFloat(shippingAmount, 2)
		discountAmount := 0.0
		if paid > 0 {
			discountAmount = roundFloat(gross+shippingAmount-paid, 2)
		}
		expectedPaid := roundFloat(gross+shippingAmount-discountAmount, 2)
		trackingNo := strings.TrimSpace(d.TrackingNumber)
		packageNumber := firstShopeePackageNumber(d.PackageList)
		if trackingNo == "" {
			trackingNo = firstShopeePackageTracking(d.PackageList)
		}
		if trackingNo == "" {
			trackingNo = packageNumber
		}
		shippingCarrier := strings.TrimSpace(d.ShippingCarrier)
		if shippingCarrier == "" {
			shippingCarrier = strings.TrimSpace(d.CheckoutShippingCarrier)
		}
		orders = append(orders, ShopeeOrder{
			OrderID:          orderSN,
			DocDate:          docDate,
			OrderDateTime:    orderDateTime,
			PaymentTime:      paymentTime,
			PaymentChannel:   d.PaymentMethod,
			BuyerUsername:    d.BuyerUsername,
			TrackingNo:       trackingNo,
			PackageNumber:    packageNumber,
			ShippingCarrier:  shippingCarrier,
			COD:              d.COD,
			Status:           d.OrderStatus,
			Items:            items,
			ItemCount:        len(items),
			TotalQty:         totalQty,
			PaidAmount:       paid,
			OrderTotalAmount: paid,
			ItemGrossAmount:  gross,
			LinePaidAmount:   paid,
			ShippingAmount:   shippingAmount,
			DiscountAmount:   discountAmount,
			NoSKUItemCount:   noSKUCount,
			HasNoSKU:         noSKUCount > 0,
			MultiLine:        len(items) > 1,
			AmountMismatch:   paid > 0 && math.Abs(expectedPaid-paid) > 0.01,
		})
	}
	if len(orders) == 0 {
		warnings = append(warnings, "ไม่พบ order ที่นำเข้าได้จากช่วงวันที่นี้")
	}
	return orders, warnings
}

func firstShopeePackageNumber(packages []shopeeapi.OrderPackage) string {
	for _, p := range packages {
		if v := strings.TrimSpace(p.PackageNumber); v != "" {
			return v
		}
	}
	return ""
}

func firstShopeePackageTracking(packages []shopeeapi.OrderPackage) string {
	for _, p := range packages {
		if v := strings.TrimSpace(p.TrackingNumber); v != "" {
			return v
		}
	}
	return ""
}

func filterShopeeOrderDetailsByStatus(details []shopeeapi.OrderDetail, allowed map[string]bool) []shopeeapi.OrderDetail {
	if len(allowed) == 0 {
		return details
	}
	out := make([]shopeeapi.OrderDetail, 0, len(details))
	for _, d := range details {
		if allowed[strings.ToUpper(strings.TrimSpace(d.OrderStatus))] {
			out = append(out, d)
		}
	}
	return out
}

func parseShopeeAPIRange(fromRaw, toRaw string) (time.Time, time.Time, error) {
	now := time.Now()
	to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
	from := to.AddDate(0, 0, -7)
	var err error
	if strings.TrimSpace(fromRaw) != "" {
		from, err = time.ParseInLocation("2006-01-02", strings.TrimSpace(fromRaw), now.Location())
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("time_from ต้องเป็น YYYY-MM-DD")
		}
	}
	if strings.TrimSpace(toRaw) != "" {
		parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(toRaw), now.Location())
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("time_to ต้องเป็น YYYY-MM-DD")
		}
		to = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 0, parsed.Location())
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("time_to ต้องมากกว่าหรือเท่ากับ time_from")
	}
	if to.Sub(from) > shopeeAPIMaxRange {
		return time.Time{}, time.Time{}, fmt.Errorf("Shopee API จำกัดช่วงเวลาดึง order ไม่เกิน 15 วันต่อครั้ง")
	}
	return from, to, nil
}

func randomState() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func hashState(state string) string {
	sum := sha256.Sum256([]byte(state))
	return hex.EncodeToString(sum[:])
}

func shopeeUnixTime(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}

func shopeeAPIOrderDetailFields() []string {
	return []string{
		"buyer_username",
		"recipient_address",
		"item_list",
		"pay_time",
		"create_time",
		"update_time",
		"total_amount",
		"payment_method",
		"tracking_number",
		"order_status",
		"actual_shipping_fee",
		"estimated_shipping_fee",
		"reverse_shipping_fee",
		"shipping_carrier",
		"checkout_shipping_carrier",
		"package_list",
		"cod",
	}
}

func validateShopeeAPITimeField(v string) (string, error) {
	switch strings.TrimSpace(v) {
	case "", "create_time":
		return "create_time", nil
	case "update_time":
		return "update_time", nil
	case "pay_time":
		return "", fmt.Errorf("Shopee API ใช้ pay_time ค้นหารายการ order ไม่ได้ กรุณาเลือกวันที่สร้างหรือวันที่อัปเดต")
	default:
		return "", fmt.Errorf("time_range_field ต้องเป็น create_time หรือ update_time")
	}
}

type shopeeAPIOrderStatusSelection struct {
	RequestStatuses []string
	LocalStatuses   map[string]bool
}

func shopeeAPIOrderStatusPlanForStatuses(statuses []string) shopeeAPIOrderStatusSelection {
	allowed := map[string]bool{}
	request := []string{}
	for _, raw := range statuses {
		status := strings.ToUpper(strings.TrimSpace(raw))
		if status == "" {
			continue
		}
		allowed[status] = true
		if !shopeeAPILocalOnlyStatusFilters[status] {
			request = append(request, status)
		}
	}
	if len(request) == len(allowed) {
		return shopeeAPIOrderStatusSelection{RequestStatuses: request}
	}
	return shopeeAPIOrderStatusSelection{LocalStatuses: allowed}
}

func shopeeAPIOrderStatusPlan(v string) (shopeeAPIOrderStatusSelection, error) {
	status := strings.ToUpper(strings.TrimSpace(v))
	switch status {
	case "", "READY_TO_BILL":
		return shopeeAPIOrderStatusPlanForStatuses(shopeeAPIReadyToBillStatuses), nil
	case "ALL":
		return shopeeAPIOrderStatusSelection{}, nil
	case "SHIPPED", "TO_CONFIRM_RECEIVE", "COMPLETED", "READY_TO_SHIP", "PROCESSED":
		return shopeeAPIOrderStatusPlanForStatuses([]string{status}), nil
	default:
		return shopeeAPIOrderStatusSelection{}, fmt.Errorf("order_status ไม่รองรับ: %s", strings.TrimSpace(v))
	}
}

func defaultShopeeAPIEnv(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), "live") {
		return "live"
	}
	return "sandbox"
}

func (h *ShopeeImportHandler) renderShopeeCallback(c *gin.Context, status int, title, message string) {
	statusLabel := "ยังไม่สำเร็จ"
	statusDetail := "ร้าน Shopee ยังไม่ได้ถูกเชื่อมต่อกับ Nexflow"
	statusHint := "ปิดหน้าต่างนี้แล้วกลับไปที่ Nexflow ที่เปิดอยู่ หากยังไม่สำเร็จให้ลองเชื่อมต่อใหม่"
	closeHint := "ปิดหน้าต่างนี้ แล้วกลับไปตรวจสถานะบนหน้า Shopee ใน Nexflow"
	tone := "#b42318"
	statusBg := "#fef3f2"
	border := "#fecdca"
	iconBg := "#fee4e2"
	iconText := "#b42318"
	iconSymbol := "!"
	autoClose := false
	if status >= 200 && status < 300 {
		statusLabel = "สำเร็จ"
		statusDetail = "เชื่อมร้าน Shopee กับ Nexflow เรียบร้อยแล้ว"
		statusHint = "ระบบกำลังปิดหน้าต่างเชื่อมต่อให้อัตโนมัติ หน้า Shopee ใน Nexflow จะ refresh สถานะร้านเอง"
		closeHint = "กำลังปิด popup อัตโนมัติ ถ้า browser ไม่อนุญาตให้กดปิดหน้าต่างนี้"
		tone = "#256f2d"
		statusBg = "#eefae8"
		border = "#b9e8ad"
		iconBg = "#dff7d4"
		iconText = "#256f2d"
		iconSymbol = "✓"
		autoClose = true
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(status, `<!doctype html>
<html lang="th">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Nexflow Shopee</title>
  <style>
    :root {
      color-scheme: light;
      --graphite-950: #121816;
      --graphite-900: #17201d;
      --graphite-700: #34413c;
      --graphite-500: #66736e;
      --graphite-200: #d9dfdc;
      --graphite-100: #eef2ef;
      --graphite-50: #f7f9f6;
      --lime: #a8ff1a;
      --lime-dark: #477218;
      --cobalt: #1f4ea8;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      color: var(--graphite-950);
      background:
        linear-gradient(135deg, rgba(168,255,26,.16), transparent 28rem),
        linear-gradient(180deg, #ffffff 0%%, var(--graphite-50) 100%%);
      line-height: 1.5;
    }
    main {
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 24px;
    }
    .shell {
      width: min(720px, 100%%);
      border: 1px solid var(--graphite-200);
      border-radius: 8px;
      background: rgba(255,255,255,.96);
      box-shadow: 0 24px 70px rgba(18,24,22,.14);
      overflow: hidden;
    }
    .top {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      border-bottom: 1px solid var(--graphite-200);
      background: var(--graphite-950);
      color: #fff;
      padding: 16px 20px;
    }
    .brand {
      display: flex;
      align-items: center;
      gap: 10px;
      min-width: 0;
    }
    .mark {
      display: inline-grid;
      place-items: center;
      width: 34px;
      height: 34px;
      border-radius: 8px;
      background: var(--lime);
      color: var(--graphite-950);
      font-weight: 900;
      letter-spacing: 0;
    }
    .brand-title {
      margin: 0;
      font-size: 15px;
      font-weight: 750;
      line-height: 1.1;
    }
    .brand-sub {
      margin: 2px 0 0;
      color: rgba(255,255,255,.68);
      font-size: 12px;
      font-weight: 600;
    }
    .chip {
      white-space: nowrap;
      border: 1px solid rgba(255,255,255,.16);
      border-radius: 999px;
      padding: 6px 10px;
      color: rgba(255,255,255,.78);
      font-size: 12px;
      font-weight: 650;
    }
    .content {
      padding: 26px;
    }
    .status {
      display: flex;
      align-items: flex-start;
      gap: 14px;
    }
    .status-icon {
      display: inline-grid;
      place-items: center;
      width: 42px;
      height: 42px;
      flex: 0 0 auto;
      border-radius: 8px;
      background: %s;
      color: %s;
      font-weight: 900;
      font-size: 22px;
    }
    .eyebrow {
      display: inline-flex;
      align-items: center;
      width: fit-content;
      border: 1px solid %s;
      border-radius: 999px;
      background: %s;
      color: %s;
      padding: 4px 9px;
      font-size: 12px;
      font-weight: 750;
    }
    h1 {
      margin: 10px 0 0;
      font-size: clamp(24px, 4vw, 34px);
      line-height: 1.16;
      letter-spacing: 0;
    }
    .message {
      margin: 12px 0 0;
      color: var(--graphite-700);
      font-size: 16px;
    }
    .hint {
      margin: 6px 0 0;
      color: var(--graphite-500);
      font-size: 14px;
    }
    .next {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 10px;
      margin: 24px 0 0;
    }
    .step {
      border: 1px solid var(--graphite-200);
      border-radius: 8px;
      background: var(--graphite-50);
      padding: 12px;
    }
    .step strong {
      display: block;
      font-size: 13px;
    }
    .step span {
      display: block;
      margin-top: 2px;
      color: var(--graphite-500);
      font-size: 12px;
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      margin-top: 24px;
    }
    button {
      display: inline-flex;
      min-height: 40px;
      align-items: center;
      justify-content: center;
      border-radius: 8px;
      padding: 10px 14px;
      font: inherit;
      font-size: 14px;
      font-weight: 750;
      text-decoration: none;
      cursor: pointer;
    }
    .primary {
      border: 1px solid var(--lime);
      background: var(--lime);
      color: var(--graphite-950);
    }
    .secondary {
      border: 1px solid var(--graphite-200);
      background: #fff;
      color: var(--graphite-900);
    }
    .footer {
      border-top: 1px solid var(--graphite-200);
      background: var(--graphite-50);
      padding: 12px 20px;
      color: var(--graphite-500);
      font-size: 12px;
    }
    .close-note {
      margin: 10px 0 0;
      color: var(--graphite-500);
      font-size: 12px;
    }
    .close-fallback {
      display: none;
      margin-top: 10px;
      color: var(--graphite-500);
      font-size: 12px;
    }
    .close-fallback.is-visible {
      display: block;
    }
    @media (max-width: 560px) {
      main { padding: 14px; }
      .top { align-items: flex-start; flex-direction: column; padding: 14px; }
      .content { padding: 18px; }
      .status { flex-direction: column; }
      .actions > * { width: 100%%; }
    }
  </style>
</head>
<body>
  <main>
    <section class="shell" aria-labelledby="callback-title">
      <div class="top">
        <div class="brand">
          <div class="mark">N</div>
          <div>
            <p class="brand-title">Nexflow</p>
            <p class="brand-sub">Operations Console</p>
          </div>
        </div>
        <div class="chip">Shopee Open API</div>
      </div>
      <div class="content">
        <div class="status">
          <div class="status-icon" aria-hidden="true">%s</div>
          <div>
            <div class="eyebrow">สถานะ: %s</div>
            <h1 id="callback-title">%s</h1>
            <p class="message">%s</p>
            <p class="hint">%s</p>
          </div>
        </div>

        <div class="actions">
          <button class="primary" type="button" onclick="closePopup()">ปิดหน้าต่างนี้</button>
        </div>
        <p class="close-note">%s</p>
        <p id="close-fallback" class="close-fallback">ถ้าปุ่มปิดไม่ทำงาน ให้ปิด popup/แท็บนี้จาก browser แล้วกลับไป Nexflow ที่เปิดอยู่</p>
      </div>
      <div class="footer">หน้าต่างนี้ใช้สำหรับรับผลจาก Shopee เท่านั้น หลังปิดแล้วให้ทำงานต่อใน Nexflow</div>
    </section>
  </main>
  <script>
    const shouldAutoClose = %t;
    function closePopup() {
      window.close();
      window.setTimeout(function () {
        document.getElementById('close-fallback')?.classList.add('is-visible');
      }, 450);
    }
    if (shouldAutoClose) {
      window.setTimeout(closePopup, 1200);
    }
  </script>
</body>
</html>`,
		iconBg,
		iconText,
		border,
		statusBg,
		tone,
		iconSymbol,
		html.EscapeString(statusLabel),
		html.EscapeString(title),
		html.EscapeString(message),
		html.EscapeString(statusHint+" · "+statusDetail),
		html.EscapeString(closeHint),
		autoClose,
	)
}
