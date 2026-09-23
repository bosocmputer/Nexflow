package handlers

// TikTok Finance settlements are intentionally manual.  This handler imports
// an immutable local snapshot, reconciles every order in a statement, and only
// then allows one confirmed AR receipt write.  It never polls Finance on page
// render and never sends a partial statement.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/events"
	"nexflow/internal/services/tiktokshop"
)

const (
	tikTokSettlementDefaultDays = 15
	tikTokSettlementMaxDays     = 31
	tikTokSettlementMaxPages    = 10
)

var tikTokSettlementBangkok = time.FixedZone("Asia/Bangkok", 7*60*60)

type TikTokSettlementHandler struct {
	db                *sql.DB
	config            *config.Config
	gateway           *tiktokshop.GatewayClient
	routes            *repository.ChannelDefaultRepo
	audit             *repository.AuditLogRepo
	sml               *ShopeeImportHandler // shared SML proxy, never a Shopee route/default
	notifications     *repository.NotificationRepo
	lineNotifications *repository.LineNotificationRepo
	broker            *events.Broker
	logger            *zap.Logger
}

type tikTokSettlementImportRequest struct {
	ShopID   string `json:"shop_id"`
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
}
type tikTokSettlementWithdrawalSearchRequest struct {
	ShopID   string `json:"shop_id"`
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
}
type tikTokSettlementPaymentSearchRequest struct {
	ShopID   string `json:"shop_id"`
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
}

type tikTokSettlementSendRequest struct{ Confirm, ExpectedConfigVersion, DocDate, DocDateReason string }
type tikTokSettlementSettingsRequest struct {
	ReadEnabled           bool `json:"read_enabled"`
	SMLEnabled            bool `json:"sml_send_enabled"`
	ExpectedConfigVersion int  `json:"expected_config_version"`
}

type tikTokSettlementRunView struct {
	ID                    string                     `json:"id"`
	ShopID                string                     `json:"shop_id"`
	ShopLabel             string                     `json:"shop_label"`
	StatementID           string                     `json:"statement_id"`
	PaymentID             string                     `json:"payment_id"`
	PaymentStatus         string                     `json:"payment_status"`
	Currency              string                     `json:"currency"`
	PaymentTime           string                     `json:"payment_time,omitempty"`
	StatementTime         string                     `json:"statement_time,omitempty"`
	TotalSettlementAmount float64                    `json:"total_settlement_amount"`
	InvoiceAmountTotal    float64                    `json:"invoice_amount_total"`
	FeeAmountTotal        float64                    `json:"fee_amount_total"`
	ShippingAmountTotal   float64                    `json:"shipping_amount_total"`
	AdjustmentAmountTotal float64                    `json:"adjustment_amount_total"`
	RefundAmountTotal     float64                    `json:"refund_amount_total"`
	ReserveAmountTotal    float64                    `json:"reserve_amount_total"`
	Status                string                     `json:"status"`
	ConfigVersion         int                        `json:"config_version"`
	RouteConfigVersion    int64                      `json:"-"`
	RCDocNo               string                     `json:"rc_doc_no,omitempty"`
	ErrorMsg              string                     `json:"error_msg,omitempty"`
	AnomalyReason         string                     `json:"anomaly_reason,omitempty"`
	CreatedAt             string                     `json:"created_at"`
	UpdatedAt             string                     `json:"updated_at"`
	Items                 []tikTokSettlementItemView `json:"items"`
	ItemCount             int                        `json:"item_count"`
	BlockedItemCount      int                        `json:"blocked_item_count"`
}
type tikTokSettlementItemView struct {
	ID               string  `json:"id"`
	OrderID          string  `json:"order_id"`
	SMLInvoiceDocNo  string  `json:"sml_invoice_doc_no,omitempty"`
	CustomerCode     string  `json:"customer_code,omitempty"`
	InvoiceAmount    float64 `json:"invoice_amount"`
	SettlementAmount float64 `json:"settlement_amount"`
	FeeAmount        float64 `json:"fee_amount"`
	ShippingAmount   float64 `json:"shipping_amount"`
	AdjustmentAmount float64 `json:"adjustment_amount"`
	RefundAmount     float64 `json:"refund_amount"`
	ReserveAmount    float64 `json:"reserve_amount"`
	Currency         string  `json:"currency"`
	Status           string  `json:"status"`
	BlockReason      string  `json:"block_reason,omitempty"`
	ReceiptDocNo     string  `json:"receipt_doc_no,omitempty"`
}
type tikTokSettlementCounts struct {
	Processing  int `json:"processing"`
	Ready       int `json:"ready"`
	NeedsReview int `json:"needs_review"`
	Sent        int `json:"sent"`
	Failed      int `json:"failed"`
	Total       int `json:"total"`
}

type tikTokSettlementWithdrawalView struct {
	WithdrawalID string `json:"withdrawal_id"`
	Type         string `json:"type"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	Status       string `json:"status"`
	CreateTime   string `json:"create_time"`
}

// tikTokSettlementPaymentView is a temporary read-only evidence surface. It
// never returns an upstream payload wholesale: no buyer, address, token, or
// bank information is exposed.  We need this bounded view to verify which
// official relationship fields TikTok provides before designing the ledger.
type tikTokSettlementPaymentView struct {
	PaymentID      string   `json:"payment_id"`
	Amount         string   `json:"amount,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	Status         string   `json:"status,omitempty"`
	PaymentTime    string   `json:"payment_time,omitempty"`
	CreateTime     string   `json:"create_time,omitempty"`
	OrderCount     int      `json:"order_count"`
	StatementCount int      `json:"statement_count"`
	ObservedFields []string `json:"observed_fields,omitempty"`
}

func NewTikTokSettlementHandler(db *sql.DB, cfg *config.Config, gateway *tiktokshop.GatewayClient, routes *repository.ChannelDefaultRepo, audit *repository.AuditLogRepo, smlBridge *ShopeeImportHandler, notifications *repository.NotificationRepo, lineNotifications *repository.LineNotificationRepo, broker *events.Broker, logger *zap.Logger) *TikTokSettlementHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TikTokSettlementHandler{db: db, config: cfg, gateway: gateway, routes: routes, audit: audit, sml: smlBridge, notifications: notifications, lineNotifications: lineNotifications, broker: broker, logger: logger}
}

func (h *TikTokSettlementHandler) financeEnabled() bool {
	return h != nil && h.config != nil && h.config.TikTokShopFinanceEnabled && h.gateway != nil && h.gateway.Configured()
}
func (h *TikTokSettlementHandler) smlEnabled() bool {
	return h != nil && h.config != nil && h.config.TikTokShopSettlementSMLEnabled
}

// Preflight makes exactly one signed read request. It is not invoked by List.
func (h *TikTokSettlementHandler) Preflight(c *gin.Context) {
	if !h.financeEnabled() {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิดข้อมูลการเงิน TikTok Shop")
		return
	}
	shopID := strings.TrimSpace(c.Query("shop_id"))
	if shopID == "" {
		h.error(c, http.StatusBadRequest, "shop_required", "กรุณาเลือกร้าน TikTok Shop ก่อนตรวจระบบ")
		return
	}
	if ok, err := h.readEnabled(c.Request.Context(), shopID); err != nil {
		h.error(c, 500, "settings_read_failed", "อ่านการตั้งค่าร้านไม่สำเร็จ")
		return
	} else if !ok {
		h.error(c, 403, "read_disabled", "ร้านนี้ยังไม่เปิดอ่านข้อมูลการเงิน TikTok Shop")
		return
	}
	to, from := time.Now(), time.Now().Add(-24*time.Hour)
	result, err := h.gateway.SearchFinanceStatements(c.Request.Context(), tiktokshop.GatewayFinanceStatementsRequest{ShopID: shopID, Search: tikTokSettlementPreflightSearch(from, to)})
	if err != nil {
		h.auditEvent(c, "tiktok_settlement_preflight_failed", "error", map[string]any{"shop_id": shopID, "error_code": financeErrorCode(err)})
		h.error(c, http.StatusBadGateway, financeErrorCode(err), financeThaiError(err))
		return
	}
	_, _ = h.db.ExecContext(c.Request.Context(), `UPDATE tiktok_shop_settlement_settings SET last_successful_preflight_at=NOW(),updated_at=NOW() WHERE shop_id=$1`, shopID)
	h.auditEvent(c, "tiktok_settlement_preflight_completed", "info", map[string]any{"shop_id": shopID, "result_count": len(result.Statements), "request_id": safeRequestID(result.UpstreamRequestID)})
	c.JSON(200, gin.H{"data": gin.H{"status": "ready", "message": "พร้อมใช้: เชื่อมต่อ TikTok Statement และรายการคำสั่งซื้อได้", "statement_count": len(result.Statements)}})
}

func tikTokSettlementPreflightSearch(from, to time.Time) tiktokshop.SearchStatementsRequest {
	return tiktokshop.SearchStatementsRequest{
		PageSize:        1,
		StatementTimeGE: from.Unix(),
		StatementTimeLT: to.Unix(),
	}
}

func (h *TikTokSettlementHandler) Import(c *gin.Context) {
	if !h.financeEnabled() {
		h.error(c, 404, "feature_disabled", "Tenant นี้ยังไม่ได้เปิดข้อมูลการเงิน TikTok Shop")
		return
	}
	var req tikTokSettlementImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.error(c, 400, "invalid_request", "ข้อมูลดึง Statement ไม่ถูกต้อง")
		return
	}
	req.ShopID = strings.TrimSpace(req.ShopID)
	from, to, err := parseTikTokStatementImportRange(req.DateFrom, req.DateTo)
	if req.ShopID == "" || err != nil {
		h.error(c, 400, "invalid_range", firstNonEmpty(errorText(err), "กรุณาเลือกร้าน TikTok Shop"))
		return
	}
	if ok, err := h.readEnabled(c.Request.Context(), req.ShopID); err != nil {
		h.error(c, 500, "settings_read_failed", "อ่านการตั้งค่าร้านไม่สำเร็จ")
		return
	} else if !ok {
		h.error(c, 403, "read_disabled", "ร้านนี้ยังไม่เปิดอ่านข้อมูลการเงิน TikTok Shop")
		return
	}
	importResult, err := h.importStatements(c.Request.Context(), req.ShopID, from, to, c.GetString("user_id"), c.GetString("user_email"))
	if err != nil {
		h.logger.Warn("tiktok_settlement_import_failed",
			zap.String("shop_id", req.ShopID),
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.Error(err))
		h.auditEvent(c, "tiktok_settlement_import_failed", "error", map[string]any{"shop_id": req.ShopID, "error_code": financeErrorCode(err), "failure_stage": tikTokSettlementImportFailureStage(err), "error_reason": safeTikTokSettlementErrorReason(err)})
		h.error(c, 502, financeErrorCode(err), financeThaiError(err))
		return
	}
	_, _ = h.db.ExecContext(c.Request.Context(), `UPDATE tiktok_shop_settlement_settings SET last_import_at=NOW(),updated_at=NOW() WHERE shop_id=$1`, req.ShopID)
	h.auditEvent(c, "tiktok_settlement_import_completed", "info", map[string]any{"shop_id": req.ShopID, "statement_count": importResult.count, "paid_count": importResult.paidCount, "processing_count": importResult.processingCount, "failed_count": importResult.failedCount})
	message := fmt.Sprintf("ดึง Statement %d รายการ: TikTok ยืนยัน settlement แล้ว %d · กำลังดำเนินการ %d · ไม่สำเร็จ %d", importResult.count, importResult.paidCount, importResult.processingCount, importResult.failedCount)
	if importResult.count == 0 {
		message = "ไม่พบ Statement ที่ TikTok API คืนมาในช่วงที่เลือก ระบบยังไม่ได้สร้าง RC"
	}
	c.JSON(http.StatusAccepted, gin.H{"imported_count": importResult.count, "paid_count": importResult.paidCount, "processing_count": importResult.processingCount, "failed_count": importResult.failedCount, "message": message})
}

// SearchWithdrawals is a controlled, read-only UAT surface for TikTok's
// withdrawal rounds.  It intentionally does not create settlement runs or RC
// candidates: TikTok's documented withdrawal response does not establish which
// statement/order belongs to a withdrawal, so membership must be reconciled in
// a later explicit phase rather than guessed from amount or date alone.
func (h *TikTokSettlementHandler) SearchWithdrawals(c *gin.Context) {
	if !h.financeEnabled() {
		h.error(c, 404, "feature_disabled", "Tenant นี้ยังไม่ได้เปิดข้อมูลการเงิน TikTok Shop")
		return
	}
	var req tikTokSettlementWithdrawalSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.error(c, 400, "invalid_request", "ข้อมูลค้นหารอบถอนเงินไม่ถูกต้อง")
		return
	}
	req.ShopID = strings.TrimSpace(req.ShopID)
	from, to, err := parseTikTokSettlementRange(req.DateFrom, req.DateTo)
	if req.ShopID == "" || err != nil {
		h.error(c, 400, "invalid_range", firstNonEmpty(errorText(err), "กรุณาเลือกร้าน TikTok Shop"))
		return
	}
	if ok, err := h.readEnabled(c.Request.Context(), req.ShopID); err != nil {
		h.error(c, 500, "settings_read_failed", "อ่านการตั้งค่าร้านไม่สำเร็จ")
		return
	} else if !ok {
		h.error(c, 403, "read_disabled", "ร้านนี้ยังไม่เปิดอ่านข้อมูลการเงิน TikTok Shop")
		return
	}
	result, err := h.gateway.SearchFinanceWithdrawals(c.Request.Context(), tiktokshop.GatewayFinanceWithdrawalsRequest{
		ShopID: req.ShopID,
		Search: tiktokshop.SearchWithdrawalsRequest{
			Types:        []tiktokshop.WithdrawalType{tiktokshop.WithdrawalTypeWithdraw},
			PageSize:     100,
			CreateTimeGE: from.Unix(),
			CreateTimeLT: to.Unix(),
		},
	})
	if err != nil {
		h.auditEvent(c, "tiktok_settlement_withdrawal_search_failed", "error", map[string]any{"shop_id": req.ShopID, "error_code": financeErrorCode(err)})
		h.error(c, 502, financeErrorCode(err), financeThaiError(err))
		return
	}
	data := make([]tikTokSettlementWithdrawalView, 0, len(result.Withdrawals))
	for _, withdrawal := range result.Withdrawals {
		data = append(data, tikTokSettlementWithdrawalView{
			WithdrawalID: strings.TrimSpace(withdrawal.WithdrawalID),
			Type:         string(withdrawal.Type),
			Amount:       strings.TrimSpace(withdrawal.Amount),
			Currency:     strings.TrimSpace(withdrawal.Currency),
			Status:       strings.TrimSpace(withdrawal.Status),
			CreateTime:   time.Unix(withdrawal.CreateTime, 0).In(tikTokSettlementBangkok).Format(time.RFC3339),
		})
	}
	h.auditEvent(c, "tiktok_settlement_withdrawal_search_completed", "info", map[string]any{"shop_id": req.ShopID, "withdrawal_count": len(data), "request_id": safeRequestID(result.UpstreamRequestID)})
	c.JSON(http.StatusOK, gin.H{
		"data":     data,
		"total":    result.TotalCount,
		"has_more": strings.TrimSpace(result.NextPageToken) != "",
		"message":  "แสดงรอบที่กดถอนเงินจาก TikTok Shop เท่านั้น ยังไม่สร้าง RC และยังไม่จับคู่ออเดอร์โดยการคาดเดา",
	})
}

// SearchPayments is an explicit, manually-triggered discovery call for
// TikTok's official payment/income records. It does not persist a ledger,
// create a Statement, RC, or SML document, and it never matches a withdrawal
// by amount or time.
func (h *TikTokSettlementHandler) SearchPayments(c *gin.Context) {
	if !h.financeEnabled() {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิดข้อมูลการเงิน TikTok Shop")
		return
	}
	var req tikTokSettlementPaymentSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.error(c, http.StatusBadRequest, "invalid_request", "ข้อมูลค้นหารายการรายได้ไม่ถูกต้อง")
		return
	}
	req.ShopID = strings.TrimSpace(req.ShopID)
	from, to, err := parseTikTokSettlementRange(req.DateFrom, req.DateTo)
	if req.ShopID == "" || err != nil {
		h.error(c, http.StatusBadRequest, "invalid_range", firstNonEmpty(errorText(err), "กรุณาเลือกร้าน TikTok Shop"))
		return
	}
	if ok, err := h.readEnabled(c.Request.Context(), req.ShopID); err != nil {
		h.error(c, http.StatusInternalServerError, "settings_read_failed", "อ่านการตั้งค่าร้านไม่สำเร็จ")
		return
	} else if !ok {
		h.error(c, http.StatusForbidden, "read_disabled", "ร้านนี้ยังไม่เปิดอ่านข้อมูลการเงิน TikTok Shop")
		return
	}
	result, err := h.gateway.SearchFinancePayments(c.Request.Context(), tiktokshop.GatewayFinancePaymentsRequest{
		ShopID: req.ShopID,
		Search: tiktokshop.SearchPaymentsRequest{PageSize: 100, CreateTimeGE: from.Unix(), CreateTimeLT: to.Unix(), SortField: "create_time", SortOrder: "DESC"},
	})
	if err != nil {
		h.auditEvent(c, "tiktok_settlement_payment_search_failed", "error", map[string]any{"shop_id": req.ShopID, "error_code": financeErrorCode(err)})
		h.error(c, http.StatusBadGateway, financeErrorCode(err), financeThaiError(err))
		return
	}
	data := make([]tikTokSettlementPaymentView, 0, len(result.Payments))
	fieldSet := map[string]struct{}{}
	for _, payment := range result.Payments {
		for _, field := range payment.ObservedFields {
			fieldSet[field] = struct{}{}
		}
		view := tikTokSettlementPaymentView{
			PaymentID:      strings.TrimSpace(payment.PaymentID),
			Amount:         strings.TrimSpace(payment.Amount),
			Currency:       strings.TrimSpace(payment.Currency),
			Status:         strings.TrimSpace(payment.Status),
			OrderCount:     len(payment.OrderIDs),
			StatementCount: len(payment.StatementIDs),
			ObservedFields: append([]string(nil), payment.ObservedFields...),
		}
		if payment.PaymentTime > 0 {
			view.PaymentTime = time.Unix(payment.PaymentTime, 0).In(tikTokSettlementBangkok).Format(time.RFC3339)
		}
		if payment.CreateTime > 0 {
			view.CreateTime = time.Unix(payment.CreateTime, 0).In(tikTokSettlementBangkok).Format(time.RFC3339)
		}
		data = append(data, view)
	}
	observed := make([]string, 0, len(fieldSet))
	for field := range fieldSet {
		observed = append(observed, field)
	}
	sort.Strings(observed)
	h.auditEvent(c, "tiktok_settlement_payment_search_completed", "info", map[string]any{"shop_id": req.ShopID, "payment_count": len(data), "observed_field_count": len(observed), "request_id": safeRequestID(result.UpstreamRequestID)})
	c.JSON(http.StatusOK, gin.H{
		"data":            data,
		"total":           result.TotalCount,
		"has_more":        strings.TrimSpace(result.NextPageToken) != "",
		"observed_fields": observed,
		"message":         "แสดงรายการรายได้จาก TikTok Shop เพื่อตรวจความเชื่อมโยงกับออเดอร์เท่านั้น ยังไม่สร้าง RC และยังไม่ผูกกับรอบถอนเงิน",
	})
}

// ReconcileWithdrawal remains only as a safe response for a stale browser
// release. The old amount-equality lookup was deliberately removed because it
// could never prove an order belonged to a withdrawal.
func (h *TikTokSettlementHandler) ReconcileWithdrawal(c *gin.Context) {
	h.error(c, http.StatusGone, "withdrawal_reconciliation_replaced", "TikTok ยังไม่ยืนยันความสัมพันธ์ของรอบถอนกับออเดอร์ผ่าน API นี้ ระบบจึงไม่เดาจากยอดเงิน กรุณาใช้ตรวจข้อมูลรายได้ก่อน")
}

func (h *TikTokSettlementHandler) List(c *gin.Context) {
	if !h.financeEnabled() {
		h.error(c, 404, "feature_disabled", "Tenant นี้ยังไม่ได้เปิดข้อมูลการเงิน TikTok Shop")
		return
	}
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	where, args, err := tikTokSettlementListWhere(c, true)
	if err != nil {
		h.error(c, http.StatusBadRequest, "invalid_filter", errorText(err))
		return
	}
	args = append(args, limit)
	rows, err := h.db.QueryContext(c.Request.Context(), `SELECT r.id::text,r.shop_id,r.shop_label,r.statement_id,r.payment_id,r.payment_status,r.currency,r.payment_time,r.statement_time,r.total_settlement_amount,r.invoice_amount_total,r.fee_amount_total,r.shipping_amount_total,r.adjustment_amount_total,r.refund_amount_total,r.reserve_amount_total,r.status,r.config_version,r.route_config_version,r.rc_doc_no,r.error_msg,r.anomaly_reason,r.created_at,r.updated_at,COALESCE((SELECT COUNT(*) FROM tiktok_settlement_items i WHERE i.run_id=r.id),0),COALESCE((SELECT COUNT(*) FROM tiktok_settlement_items i WHERE i.run_id=r.id AND i.status='blocked'),0) FROM tiktok_settlement_runs r `+where+fmt.Sprintf(" ORDER BY r.statement_time DESC NULLS LAST,r.payment_time DESC NULLS LAST,r.created_at DESC LIMIT $%d", len(args)), args...)
	if err != nil {
		h.error(c, 500, "list_failed", "โหลด Statement TikTok Shop ไม่สำเร็จ")
		return
	}
	defer rows.Close()
	data := []tikTokSettlementRunView{}
	for rows.Next() {
		run, err := scanTikTokSettlementRun(rows)
		if err != nil {
			h.error(c, 500, "list_failed", "อ่านข้อมูล Statement ไม่สำเร็จ")
			return
		}
		data = append(data, *run)
	}
	if err := rows.Err(); err != nil {
		h.error(c, 500, "list_failed", "อ่านข้อมูล Statement ไม่สำเร็จ")
		return
	}
	c.JSON(200, gin.H{"data": data})
}

func (h *TikTokSettlementHandler) Counts(c *gin.Context) {
	if !h.financeEnabled() {
		h.error(c, 404, "feature_disabled", "Tenant นี้ยังไม่ได้เปิดข้อมูลการเงิน TikTok Shop")
		return
	}
	where, args, err := tikTokSettlementListWhere(c, false)
	if err != nil {
		h.error(c, http.StatusBadRequest, "invalid_filter", errorText(err))
		return
	}
	var out tikTokSettlementCounts
	rows, err := h.db.QueryContext(c.Request.Context(), `SELECT status,COUNT(*) FROM tiktok_settlement_runs `+where+` GROUP BY status`, args...)
	if err != nil {
		h.error(c, 500, "counts_failed", "โหลดสรุป Statement ไม่สำเร็จ")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			continue
		}
		out.Total += n
		switch s {
		case "importing", "reconciling":
			out.Processing += n
		case "ready":
			out.Ready += n
		case "needs_review":
			out.NeedsReview += n
		case "sent":
			out.Sent += n
		case "failed", "unknown_result":
			out.Failed += n
		}
	}
	c.JSON(200, out)
}

// tikTokSettlementListWhere keeps the page's table and summary bounded to the
// same local snapshot.  It deliberately accepts no free-form SQL and does not
// call TikTok: importing remains an explicit operator action.
func tikTokSettlementListWhere(c *gin.Context, includeStatus bool) (string, []any, error) {
	args := []any{}
	where := "WHERE 1=1"
	if shop := strings.TrimSpace(c.Query("shop_id")); shop != "" {
		args = append(args, shop)
		where += fmt.Sprintf(" AND shop_id=$%d", len(args))
	}
	if includeStatus {
		if status := strings.TrimSpace(c.Query("status")); status != "" {
			args = append(args, status)
			where += fmt.Sprintf(" AND status=$%d", len(args))
		}
	}
	if paymentStatus := strings.TrimSpace(c.Query("payment_status")); paymentStatus != "" {
		switch paymentStatus {
		case string(tiktokshop.StatementStatusPaid), string(tiktokshop.StatementStatusSettled), string(tiktokshop.StatementStatusProcessing), string(tiktokshop.StatementStatusFailed), "BANK_CONFIRMED":
			args = append(args, paymentStatus)
			where += fmt.Sprintf(" AND payment_status=$%d", len(args))
		default:
			return "", nil, errors.New("สถานะการโอน TikTok Shop ไม่ถูกต้อง")
		}
	}
	dateFrom, dateTo := strings.TrimSpace(c.Query("date_from")), strings.TrimSpace(c.Query("date_to"))
	if dateFrom != "" || dateTo != "" {
		from, to, err := parseTikTokStatementImportRange(dateFrom, dateTo)
		if err != nil {
			return "", nil, err
		}
		args = append(args, from, to)
		where += fmt.Sprintf(" AND COALESCE(statement_time,payment_time) >= $%d AND COALESCE(statement_time,payment_time) < $%d", len(args)-1, len(args))
	}
	return where, args, nil
}

func (h *TikTokSettlementHandler) Get(c *gin.Context) {
	run, err := h.loadRun(c.Request.Context(), c.Param("id"), true)
	if err != nil {
		h.error(c, 404, "not_found", "ไม่พบ Statement นี้")
		return
	}
	c.JSON(200, gin.H{"data": run})
}

func (h *TikTokSettlementHandler) Reconcile(c *gin.Context) {
	run, err := h.loadRun(c.Request.Context(), c.Param("id"), false)
	if err != nil {
		h.error(c, 404, "not_found", "ไม่พบ Statement นี้")
		return
	}
	if run.Status == "sent" {
		h.error(c, 409, "sent_immutable", "Statement ที่ส่ง RC แล้วห้ามแก้ไข")
		return
	}
	if err := h.reconcileRun(c.Request.Context(), run.ID); err != nil {
		h.error(c, 500, "reconcile_failed", "ตรวจเทียบ Statement กับ SML ไม่สำเร็จ")
		return
	}
	out, _ := h.loadRun(c.Request.Context(), run.ID, true)
	h.auditEvent(c, "tiktok_settlement_reconciled", "info", map[string]any{"statement_id": out.StatementID, "status": out.Status})
	c.JSON(200, gin.H{"data": out})
}

func (h *TikTokSettlementHandler) Send(c *gin.Context) {
	if !h.smlEnabled() {
		h.error(c, 404, "sml_feature_disabled", "ยังไม่เปิดส่งรับชำระ TikTok Shop เข้า SML")
		return
	}
	var req tikTokSettlementSendRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Confirm) != "CONFIRM_TIKTOK_RC" {
		h.error(c, 400, "confirmation_required", "กรุณายืนยันการสร้าง RC ก่อนส่ง SML")
		return
	}
	run, err := h.loadRun(c.Request.Context(), c.Param("id"), true)
	if err != nil {
		h.error(c, 404, "not_found", "ไม่พบ Statement นี้")
		return
	}
	if run.Status != "ready" {
		h.error(c, 409, "not_ready", "Statement นี้ยังไม่พร้อมส่ง ต้องไม่มีรายการที่ต้องตรวจ")
		return
	}
	if req.ExpectedConfigVersion != "" && req.ExpectedConfigVersion != strconv.Itoa(run.ConfigVersion) {
		h.error(c, 409, "config_changed", "ข้อมูล Statement เปลี่ยนแล้ว กรุณาตรวจใหม่ก่อนส่ง")
		return
	}
	if ok, currentVersion, err := h.smlSendEnabled(c.Request.Context(), run.ShopID); err != nil || !ok || currentVersion != run.ConfigVersion {
		h.error(c, 403, "sml_send_disabled", "ร้านนี้ยังไม่เปิดส่ง RC เข้า SML")
		return
	}
	route, err := h.routes.Get("tiktok_settlement", "ar_receipt")
	if err != nil || route == nil || route.DocFormatCode == "" || route.PassbookCode == "" {
		h.error(c, 409, "route_missing", "กรุณาตั้งค่าเส้นทาง SML สำหรับ TikTok Shop รับชำระหนี้ก่อน")
		return
	}
	if route.ConfigVersion != run.RouteConfigVersion {
		h.error(c, 409, "route_changed", "เส้นทาง SML ถูกแก้ไขแล้ว กรุณาตรวจ Statement ใหม่ก่อนส่ง")
		return
	}
	if math.Abs(run.FeeAmountTotal) > 0.01 && strings.TrimSpace(route.ExpenseCode) == "" {
		h.error(c, 409, "expense_code_missing", "Statement นี้มี Fee/commission กรุณาตั้งรหัสค่าใช้จ่าย TikTok Shop ก่อนส่ง RC")
		return
	}
	if req.DocDate != "" && strings.TrimSpace(req.DocDateReason) == "" {
		h.error(c, 400, "doc_date_reason_required", "หากเปลี่ยนวันเอกสาร กรุณาระบุเหตุผล")
		return
	}
	if req.DocDate != "" {
		if _, err := time.ParseInLocation("2006-01-02", req.DocDate, tikTokSettlementBangkok); err != nil {
			h.error(c, 400, "invalid_doc_date", "วันเอกสารต้องเป็น YYYY-MM-DD")
			return
		}
	}
	if req.DocDate != "" && c.GetString("user_role") != "admin" {
		h.error(c, 403, "doc_date_admin_only", "เปลี่ยนวันเอกสารได้เฉพาะผู้ดูแลระบบ")
		return
	}
	if err := h.lockForSend(c.Request.Context(), run.ID, run.ConfigVersion, req); err != nil {
		h.error(c, 409, "send_locked", "งานนี้กำลังส่งหรือข้อมูลเปลี่ยน กรุณาโหลดใหม่")
		return
	}
	go h.sendRun(run.ID, route, c.GetString("user_id"))
	c.JSON(http.StatusAccepted, gin.H{"message": "เริ่มส่ง RC เข้า SML แล้ว", "run_id": run.ID})
}

func (h *TikTokSettlementHandler) Settings(c *gin.Context) {
	shop := strings.TrimSpace(c.Query("shop_id"))
	if shop == "" {
		h.error(c, 400, "shop_required", "กรุณาเลือกร้าน TikTok Shop")
		return
	}
	s, err := h.loadSettings(c.Request.Context(), shop)
	if err != nil {
		h.error(c, 500, "settings_read_failed", "อ่านการตั้งค่าร้านไม่สำเร็จ")
		return
	}
	c.JSON(200, gin.H{"data": s})
}

// RouteSummary lets staff see the non-secret SML destination that will be
// used for a manual confirmation.  It deliberately does not create a default
// route or expose any tenant credential.
func (h *TikTokSettlementHandler) RouteSummary(c *gin.Context) {
	route, err := h.routes.Get("tiktok_settlement", "ar_receipt")
	if err != nil || route == nil {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"configured": false}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"configured":      strings.TrimSpace(route.DocFormatCode) != "" && strings.TrimSpace(route.PassbookCode) != "",
		"doc_format_code": route.DocFormatCode,
		"passbook_code":   route.PassbookCode,
		"passbook_name":   route.PassbookName,
		"bank_code":       route.BankCode,
		"bank_branch":     route.BankBranch,
		"expense_code":    route.ExpenseCode,
		"expense_name":    route.ExpenseName,
		"config_version":  route.ConfigVersion,
	}})
}
func (h *TikTokSettlementHandler) UpdateSettings(c *gin.Context) {
	shop := strings.TrimSpace(c.Param("shop_id"))
	var req tikTokSettlementSettingsRequest
	if shop == "" || c.ShouldBindJSON(&req) != nil || req.ExpectedConfigVersion < 0 || (req.SMLEnabled && !req.ReadEnabled) {
		h.error(c, 400, "invalid_request", "ข้อมูลการตั้งค่าร้านไม่ถูกต้อง")
		return
	}
	res, err := h.db.ExecContext(c.Request.Context(), `INSERT INTO tiktok_shop_settlement_settings(shop_id,read_enabled,sml_send_enabled,config_version,updated_by,updated_at) SELECT shop_id,$2,$3,1,NULLIF($4,'')::uuid,NOW() FROM tiktok_shop_connections WHERE shop_id=$1 AND disabled_at IS NULL ON CONFLICT(shop_id) DO UPDATE SET read_enabled=EXCLUDED.read_enabled,sml_send_enabled=EXCLUDED.sml_send_enabled,config_version=tiktok_shop_settlement_settings.config_version+1,updated_by=EXCLUDED.updated_by,updated_at=NOW() WHERE tiktok_shop_settlement_settings.config_version=$5`, shop, req.ReadEnabled, req.SMLEnabled, c.GetString("user_id"), req.ExpectedConfigVersion)
	if err != nil {
		h.error(c, 500, "settings_update_failed", "บันทึกการตั้งค่าร้านไม่สำเร็จ")
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		h.error(c, 409, "config_changed", "การตั้งค่าถูกแก้ไขแล้ว กรุณาโหลดใหม่")
		return
	}
	h.auditEvent(c, "tiktok_settlement_settings_updated", "info", map[string]any{"shop_id": shop, "read_enabled": req.ReadEnabled, "sml_send_enabled": req.SMLEnabled})
	s, _ := h.loadSettings(c.Request.Context(), shop)
	c.JSON(200, gin.H{"data": s})
}

type tikTokSettlementImportResult struct {
	count           int
	paidCount       int
	processingCount int
	failedCount     int
}

func (h *TikTokSettlementHandler) importStatements(ctx context.Context, shop string, from, to time.Time, userID, email string) (tikTokSettlementImportResult, error) {
	imported := make(map[string]tiktokshop.StatementStatus)
	if err := h.importStatementPages(ctx, shop, from, to, userID, email, imported); err != nil {
		return tikTokSettlementImportResult{}, err
	}
	result := tikTokSettlementImportResult{count: len(imported)}
	for _, status := range imported {
		switch status {
		case tiktokshop.StatementStatusPaid, tiktokshop.StatementStatusSettled:
			result.paidCount++
		case tiktokshop.StatementStatusProcessing:
			result.processingCount++
		case tiktokshop.StatementStatusFailed:
			result.failedCount++
		}
	}
	return result, nil
}

func tikTokStatementImportSearch(from, to time.Time, pageToken string) tiktokshop.SearchStatementsRequest {
	return tiktokshop.SearchStatementsRequest{
		PageSize:        100,
		PageToken:       pageToken,
		StatementTimeGE: from.Unix(),
		StatementTimeLT: to.Unix(),
		// Omit payment_status: TikTok's documented default is every status.
	}
}

func (h *TikTokSettlementHandler) importStatementPages(ctx context.Context, shop string, from, to time.Time, userID, email string, imported map[string]tiktokshop.StatementStatus) error {
	token := ""
	for page := 0; page < tikTokSettlementMaxPages; page++ {
		r, err := h.gateway.SearchFinanceStatements(ctx, tiktokshop.GatewayFinanceStatementsRequest{ShopID: shop, Search: tikTokStatementImportSearch(from, to, token)})
		if err != nil {
			return fmt.Errorf("read_statement_page: %w", err)
		}
		for _, statement := range r.Statements {
			if err := h.upsertStatement(ctx, shop, statement, r.UpstreamRequestID, userID, email); err != nil {
				return fmt.Errorf("store_statement: %w", err)
			}
			imported[statement.StatementID] = statement.Status
		}
		token = strings.TrimSpace(r.NextPageToken)
		if token == "" {
			break
		}
	}
	return nil
}

func (h *TikTokSettlementHandler) upsertStatement(ctx context.Context, shop string, s tiktokshop.Statement, requestID, userID, email string) error {
	// TikTok may omit optional fee, shipping, adjustment, or refund fields.
	// PostgreSQL NUMERIC columns cannot accept an empty string; an omitted
	// optional component is accounting-neutral, while settlement_amount has
	// already been validated by the Finance client as required evidence.
	s.FeeAmount = tikTokSettlementAmountOrZero(s.FeeAmount)
	s.ShippingAmount = tikTokSettlementAmountOrZero(s.ShippingAmount)
	s.AdjustmentAmount = tikTokSettlementAmountOrZero(s.AdjustmentAmount)
	s.RefundAmount = tikTokSettlementAmountOrZero(s.RefundAmount)
	label, connectionID, err := h.connection(ctx, shop)
	if err != nil {
		return fmt.Errorf("load_connection: %w", err)
	}
	snapshot, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal_statement: %w", err)
	}
	sum := sha256.Sum256(snapshot)
	status := "needs_review"
	if s.Status == tiktokshop.StatementStatusProcessing {
		status = "importing"
	}
	if s.Status == tiktokshop.StatementStatusFailed {
		status = "failed"
	}
	settings, err := h.loadSettings(ctx, shop)
	if err != nil {
		return fmt.Errorf("load_settings: %w", err)
	}
	settingVersion, _ := settings["config_version"].(int)
	routeVersion := int64(0)
	if route, routeErr := h.routes.Get("tiktok_settlement", "ar_receipt"); routeErr == nil && route != nil {
		routeVersion = route.ConfigVersion
	}
	var runID string
	err = h.db.QueryRowContext(ctx, `INSERT INTO tiktok_settlement_runs(connection_id,shop_id,shop_label,statement_id,payment_id,payment_status,currency,payment_time,statement_time,total_settlement_amount,fee_amount_total,shipping_amount_total,adjustment_amount_total,refund_amount_total,content_hash,upstream_request_id,status,config_version,route_config_version,created_by,created_by_email,updated_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,to_timestamp($8),NULLIF(to_timestamp($9),to_timestamp(0)),$10::numeric,$11::numeric,$12::numeric,$13::numeric,$14::numeric,$15,$16,$17,$18,$19,NULLIF($20,'')::uuid,$21,NOW()) ON CONFLICT(shop_id,statement_id) DO UPDATE SET payment_id=EXCLUDED.payment_id,payment_status=EXCLUDED.payment_status,currency=EXCLUDED.currency,payment_time=EXCLUDED.payment_time,statement_time=EXCLUDED.statement_time,total_settlement_amount=EXCLUDED.total_settlement_amount,fee_amount_total=EXCLUDED.fee_amount_total,shipping_amount_total=EXCLUDED.shipping_amount_total,adjustment_amount_total=EXCLUDED.adjustment_amount_total,refund_amount_total=EXCLUDED.refund_amount_total,content_hash=EXCLUDED.content_hash,upstream_request_id=EXCLUDED.upstream_request_id,status=EXCLUDED.status,config_version=EXCLUDED.config_version,route_config_version=EXCLUDED.route_config_version,updated_at=NOW() WHERE tiktok_settlement_runs.status<>'sent' RETURNING id::text`, connectionID, shop, label, s.StatementID, s.PaymentID, s.Status, s.Currency, s.PaymentTime, s.StatementTime, s.SettlementAmount, s.FeeAmount, s.ShippingAmount, s.AdjustmentAmount, s.RefundAmount, hex.EncodeToString(sum[:]), safeRequestID(requestID), status, settingVersion, routeVersion, userID, email).Scan(&runID)
	if err == sql.ErrNoRows {
		// A sent snapshot is immutable.  If TikTok later changes it, retain the
		// original RC evidence and surface an anomaly instead of rewriting it.
		changed, updateErr := h.db.ExecContext(ctx, `UPDATE tiktok_settlement_runs SET anomaly_reason='ข้อมูล TikTok Statement เปลี่ยนหลังส่ง RC แล้ว ต้องตรวจสอบกับธนาคาร',updated_at=NOW() WHERE shop_id=$1 AND statement_id=$2 AND status='sent' AND content_hash<>$3`, shop, s.StatementID, hex.EncodeToString(sum[:]))
		if updateErr != nil {
			return fmt.Errorf("mark_sent_anomaly: %w", updateErr)
		}
		if n, _ := changed.RowsAffected(); n > 0 {
			h.auditDirect(ctx, "tiktok_settlement_anomaly", userID, "warning", map[string]any{"shop_id": shop, "statement_id": s.StatementID, "reason": "upstream_data_changed_after_rc"})
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("save_snapshot: %w", err)
	}
	if tikTokStatementIsSettlementReady(s.Status) {
		if err := h.fetchAndReconcile(ctx, runID, shop, s.StatementID); err != nil {
			return fmt.Errorf("reconcile_statement: %w", err)
		}
	}
	return nil
}

func tikTokSettlementAmountOrZero(value string) string {
	if normalized := strings.TrimSpace(value); normalized != "" {
		return normalized
	}
	return "0"
}

func tikTokStatementTransactionWithZeroOptionalAmounts(value tiktokshop.StatementTransaction) tiktokshop.StatementTransaction {
	value.FeeAmount = tikTokSettlementAmountOrZero(value.FeeAmount)
	value.ShippingAmount = tikTokSettlementAmountOrZero(value.ShippingAmount)
	value.AdjustmentAmount = tikTokSettlementAmountOrZero(value.AdjustmentAmount)
	value.ReserveAmount = tikTokSettlementAmountOrZero(value.ReserveAmount)
	return value
}

// PAID is documented for Statements, while AOY's live response currently uses
// SETTLED.  Both mean TikTok has completed its platform-side settlement; they
// are not a substitute for the operator's bank-evidence confirmation before
// the RC write.
func tikTokStatementIsSettlementReady(status tiktokshop.StatementStatus) bool {
	return status == tiktokshop.StatementStatusPaid || status == tiktokshop.StatementStatusSettled
}

func (h *TikTokSettlementHandler) fetchAndReconcile(ctx context.Context, runID, shop, statementID string) error {
	transactions := []tiktokshop.StatementTransaction{}
	token := ""
	requestID := ""
	reserveTotal, adjustmentTotal := "0", "0"
	for page := 0; page < tikTokSettlementMaxPages; page++ {
		r, err := h.gateway.GetFinanceStatementTransactions(ctx, tiktokshop.GatewayFinanceStatementTransactionsRequest{ShopID: shop, StatementID: statementID, PageToken: token, PageSize: 100})
		if err != nil {
			return err
		}
		transactions = append(transactions, r.Transactions...)
		if strings.TrimSpace(r.TotalReserveAmount) != "" {
			reserveTotal = r.TotalReserveAmount
		}
		if strings.TrimSpace(r.TotalSettlementBreakdown.TotalAdjustmentAmount) != "" {
			adjustmentTotal = r.TotalSettlementBreakdown.TotalAdjustmentAmount
		}
		requestID = r.UpstreamRequestID
		token = strings.TrimSpace(r.NextPageToken)
		if token == "" {
			break
		}
		if page == tikTokSettlementMaxPages-1 {
			return errors.New("TikTok Shop statement has too many pages")
		}
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM tiktok_settlement_items WHERE run_id=$1::uuid`, runID); err != nil {
		return err
	}
	for index, v := range transactions {
		// TikTok omits optional transaction components when they are zero. The
		// accounting schema intentionally uses NUMERIC, so normalize those empty
		// optional values before both persisting the snapshot and binding SQL.
		// settlement_amount itself remains required Finance evidence.
		v = tikTokStatementTransactionWithZeroOptionalAmounts(v)
		snapshot, _ := json.Marshal(v)
		orderID := firstNonEmpty(strings.TrimSpace(v.OrderID), strings.TrimSpace(v.AssociatedOrderID))
		if orderID == "" {
			orderID = fmt.Sprintf("__tiktok_finance_%d", index+1)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tiktok_settlement_items(run_id,order_id,settlement_amount,fee_amount,shipping_amount,adjustment_amount,refund_amount,reserve_amount,currency,snapshot,updated_at) VALUES($1::uuid,$2,$3::numeric,$4::numeric,$5::numeric,$6::numeric,$7::numeric,$8::numeric,$9,$10::jsonb,NOW())`, runID, orderID, v.SettlementAmount, v.FeeAmount, v.ShippingAmount, v.AdjustmentAmount, "0", v.ReserveAmount, v.Currency, snapshot); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tiktok_settlement_runs SET upstream_request_id=$2,reserve_amount_total=$3::numeric,adjustment_amount_total=$4::numeric,status='reconciling',updated_at=NOW() WHERE id=$1::uuid`, runID, safeRequestID(requestID), reserveTotal, adjustmentTotal); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return h.reconcileRun(ctx, runID)
}

func (h *TikTokSettlementHandler) reconcileRun(ctx context.Context, runID string) error {
	run, err := h.loadRun(ctx, runID, true)
	if err != nil {
		return err
	}
	if run.Status == "sent" {
		return errors.New("sent statement immutable")
	}
	settings, err := h.loadSettings(ctx, run.ShopID)
	if err != nil {
		return err
	}
	settingVersion, _ := settings["config_version"].(int)
	routeVersion := int64(0)
	if route, routeErr := h.routes.Get("tiktok_settlement", "ar_receipt"); routeErr == nil && route != nil {
		routeVersion = route.ConfigVersion
	}
	if _, err = h.db.ExecContext(ctx, `UPDATE tiktok_settlement_runs SET config_version=$2,route_config_version=$3,status='reconciling',updated_at=NOW() WHERE id=$1::uuid`, runID, settingVersion, routeVersion); err != nil {
		return err
	}
	run.ConfigVersion, run.RouteConfigVersion = settingVersion, routeVersion
	if !tikTokStatementIsSettlementReady(tiktokshop.StatementStatus(run.PaymentStatus)) && run.PaymentStatus != "BANK_CONFIRMED" {
		_, err = h.db.ExecContext(ctx, `UPDATE tiktok_settlement_runs SET status='needs_review',error_msg='Statement ยังไม่อยู่ในสถานะที่ TikTok ยืนยัน settlement',updated_at=NOW() WHERE id=$1::uuid`, runID)
		return err
	}
	items := run.Items
	orders := make([]string, 0, len(items))
	for _, i := range items {
		orders = append(orders, i.OrderID)
	}
	candidates, err := h.sml.fetchSettlementCandidates(ctx, orders)
	if err != nil {
		return err
	}
	customer := ""
	blocked := 0
	invoiceTotal := 0.0
	feeTotal := 0.0
	for _, item := range items {
		cand := candidates[item.OrderID]
		status, reason := "ready", ""
		item.InvoiceAmount = cand.InvoiceAmount
		item.SMLInvoiceDocNo = cand.InvoiceDocNo
		item.CustomerCode = cand.CustCode
		invoiceTotal += item.InvoiceAmount
		feeTotal += item.FeeAmount
		if strings.HasPrefix(item.OrderID, "__tiktok_finance_") {
			status, reason = "blocked", "มีรายการการเงินที่ไม่ผูกคำสั่งซื้อ ต้องตรวจ Statement"
		} else if cand.Status == "" || cand.Status == "not_found" {
			status, reason = "blocked", "ไม่พบใบขาย SML ที่อ้างอิงคำสั่งซื้อ TikTok Shop"
		} else if cand.AlreadyReceived {
			status, reason = "blocked", "ใบขายนี้เคยรับชำระแล้วในเอกสาร "+cand.ExistingReceiptDocNo
		} else if strings.ToUpper(item.Currency) != "THB" {
			status, reason = "blocked", "Statement ไม่ใช่สกุล THB"
		} else if item.RefundAmount != 0 {
			status, reason = "blocked", "มี refund ต้องตรวจเอกสารลดหนี้ก่อน"
		} else if item.ReserveAmount != 0 {
			status, reason = "blocked", "มี reserve ต้องตรวจ Statement ก่อน"
		} else if item.AdjustmentAmount != 0 {
			status, reason = "blocked", "มี adjustment ที่ต้องตรวจเอกสารก่อน"
		} else if item.SettlementAmount > item.InvoiceAmount+0.01 {
			status, reason = "blocked", "ยอด TikTok มากกว่ายอดใบขาย SML"
		} else if customer != "" && customer != item.CustomerCode {
			status, reason = "blocked", "Statement มีลูกหนี้มากกว่าหนึ่งราย"
		} else if item.CustomerCode == "" {
			status, reason = "blocked", "ยังหาลูกหนี้จากใบขาย SML ไม่ได้"
		} else {
			customer = item.CustomerCode
		}
		if status != "ready" {
			blocked++
		}
		_, err = h.db.ExecContext(ctx, `UPDATE tiktok_settlement_items SET sml_invoice_doc_no=$2,customer_code=$3,invoice_amount=$4::numeric,status=$5,block_reason=$6,updated_at=NOW() WHERE id=$1::uuid`, item.ID, item.SMLInvoiceDocNo, item.CustomerCode, item.InvoiceAmount, status, reason)
		if err != nil {
			return err
		}
	}
	var total float64
	for _, i := range items {
		total += i.SettlementAmount
	}
	status := "ready"
	reason := ""
	if blocked > 0 {
		status, reason = "needs_review", "มีรายการที่ต้องตรวจ"
	} else if strings.ToUpper(run.Currency) != "THB" {
		status, reason = "needs_review", "Statement ไม่ใช่สกุล THB"
	} else if run.PaymentTime == "" || strings.TrimSpace(run.PaymentID) == "" {
		status, reason = "needs_review", "Statement PAID ยังไม่มี Payment ID หรือเวลาที่ TikTok แจ้งว่าโอน"
	} else if run.RefundAmountTotal != 0 || run.AdjustmentAmountTotal != 0 || run.ReserveAmountTotal != 0 {
		status, reason = "needs_review", "มี refund, reserve หรือ adjustment ที่ต้องตรวจ"
	} else if math.Abs(total-run.TotalSettlementAmount) > 0.01 {
		status, reason = "needs_review", "ยอด Statement ไม่สมดุลกับรายการคำสั่งซื้อ"
	}
	_, err = h.db.ExecContext(ctx, `UPDATE tiktok_settlement_runs SET status=$2,invoice_amount_total=$3::numeric,fee_amount_total=$4::numeric,error_msg='',anomaly_reason=$5,finished_at=NOW(),updated_at=NOW() WHERE id=$1::uuid`, runID, status, invoiceTotal, feeTotal, reason)
	return err
}

func (h *TikTokSettlementHandler) lockForSend(ctx context.Context, runID string, version int, req tikTokSettlementSendRequest) error {
	fingerprint := sha256.Sum256([]byte(runID + "|" + strconv.Itoa(version) + "|" + req.DocDate + "|" + req.DocDateReason))
	res, err := h.db.ExecContext(ctx, `UPDATE tiktok_settlement_runs SET status='sending',request_fingerprint=$2,doc_date_override=NULLIF($3,'')::date,doc_date_override_reason=$4,lease_until=NOW()+INTERVAL '60 seconds',started_at=COALESCE(started_at,NOW()),updated_at=NOW() WHERE id=$1::uuid AND status='ready' AND config_version=$5`, runID, hex.EncodeToString(fingerprint[:]), req.DocDate, strings.TrimSpace(req.DocDateReason), version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("settlement stale")
	}
	return nil
}

func (h *TikTokSettlementHandler) sendRun(runID string, route *models.ChannelDefault, userID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run, err := h.loadRun(ctx, runID, true)
	if err != nil || run.Status != "sending" {
		return
	}
	if latest, routeErr := h.routes.Get("tiktok_settlement", "ar_receipt"); routeErr != nil || latest == nil || latest.ConfigVersion != route.ConfigVersion {
		h.failSettlementRun(ctx, run, userID, "เส้นทาง SML ถูกแก้ไขระหว่างส่ง กรุณาตรวจ Statement ใหม่")
		return
	}
	if enabled, version, settingErr := h.smlSendEnabled(ctx, run.ShopID); settingErr != nil || !enabled || version != run.ConfigVersion {
		h.failSettlementRun(ctx, run, userID, "การตั้งค่าร้านถูกแก้ไขระหว่างส่ง กรุณาตรวจ Statement ใหม่")
		return
	}
	items := run.Items
	if len(items) == 0 {
		h.failSettlementRun(ctx, run, userID, "ไม่มีรายการรับชำระ")
		return
	}
	lines := make([]map[string]any, 0, len(items))
	for _, i := range items {
		if i.Status != "ready" {
			h.failSettlementRun(ctx, run, userID, "Statement เปลี่ยนระหว่างส่ง")
			return
		}
		lines = append(lines, map[string]any{"order_sn": i.OrderID, "invoice_doc_no": i.SMLInvoiceDocNo, "payout_amount": i.SettlementAmount})
	}
	docDate := time.Now().In(tikTokSettlementBangkok).Format("2006-01-02")
	if run.PaymentTime != "" {
		if t, err := time.Parse(time.RFC3339, run.PaymentTime); err == nil {
			docDate = t.In(tikTokSettlementBangkok).Format("2006-01-02")
		}
	}
	var override sql.NullTime
	if err := h.db.QueryRowContext(ctx, `SELECT doc_date_override FROM tiktok_settlement_runs WHERE id=$1::uuid`, runID).Scan(&override); err == nil && override.Valid {
		docDate = override.Time.In(tikTokSettlementBangkok).Format("2006-01-02")
	}
	remark := fmt.Sprintf("TikTok Statement %s Payment %s", run.StatementID, run.PaymentID)
	payload := map[string]any{"doc_date": docDate, "doc_time": time.Now().In(tikTokSettlementBangkok).Format("15:04"), "doc_format_code": route.DocFormatCode, "passbook_code": route.PassbookCode, "expense_code": route.ExpenseCode, "remark": remark, "lines": lines}
	var out struct {
		Success bool                      `json:"success"`
		Data    settlementReceiptResponse `json:"data"`
		Error   *smlProxyErrorBody        `json:"error"`
		Message string                    `json:"message"`
	}
	if err := h.sml.callSMLAPI(ctx, http.MethodPost, "/api/v1/ar/receipts", payload, &out); err != nil {
		h.unknownRun(ctx, runID, "ไม่ทราบผลการส่ง SML กรุณาตรวจ RC ก่อนลองใหม่")
		h.auditDirect(ctx, "tiktok_settlement_sml_unknown_result", userID, "warning", map[string]any{"run_id": runID, "statement_id": run.StatementID, "payment_id": run.PaymentID})
		h.notifySettlementResult(ctx, run, "warning", "ต้องตรวจผล RC TikTok Shop", "ไม่ทราบผลการส่ง SML กรุณาตรวจเอกสาร RC ก่อนลองใหม่", "unknown_result")
		return
	}
	if !out.Success || strings.TrimSpace(out.Data.DocNo) == "" {
		message := firstNonEmpty(out.Message, "SML ปฏิเสธการสร้าง RC")
		if out.Error != nil {
			message = firstNonEmpty(out.Error.Message, message)
		}
		h.failRun(ctx, runID, message)
		h.auditDirect(ctx, "tiktok_settlement_sml_failed", userID, "error", map[string]any{"run_id": runID, "statement_id": run.StatementID, "payment_id": run.PaymentID})
		h.notifySettlementResult(ctx, run, "error", "ส่ง RC TikTok Shop ไม่สำเร็จ", "SML ปฏิเสธการสร้างรับชำระหนี้ กรุณาตรวจ Statement แล้วลองใหม่", "failed")
		return
	}
	// The SML receipt response is the first read-back evidence available from
	// the shared Gateway.  A document number alone is not enough: if its
	// returned invoice/payout amounts differ from this immutable statement
	// snapshot, stop immediately and require an operator to inspect SML.
	if math.Abs(out.Data.InvoiceAmount-run.InvoiceAmountTotal) > 0.01 || math.Abs(out.Data.PayoutAmount-run.TotalSettlementAmount) > 0.01 {
		message := "ยอด RC ที่ SML ตอบกลับไม่ตรงกับ Statement ต้องตรวจเอกสารก่อนลองใหม่"
		_, _ = h.db.ExecContext(ctx, `UPDATE tiktok_settlement_runs SET status='unknown_result',rc_doc_no=$2,anomaly_reason=$3,lease_until=NULL,finished_at=NOW(),updated_at=NOW() WHERE id=$1::uuid`, runID, out.Data.DocNo, message)
		h.auditDirect(ctx, "tiktok_settlement_anomaly", userID, "warning", map[string]any{"run_id": runID, "statement_id": run.StatementID, "payment_id": run.PaymentID, "rc_doc_no": out.Data.DocNo, "reason": "sml_amount_mismatch"})
		h.notifySettlementResult(ctx, run, "warning", "ต้องตรวจยอด RC TikTok Shop", message, "unknown_result")
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		h.unknownRun(ctx, runID, "บันทึกผล SML ไม่สำเร็จ กรุณาตรวจ RC")
		h.auditDirect(ctx, "tiktok_settlement_sml_unknown_result", userID, "warning", map[string]any{"run_id": runID, "statement_id": run.StatementID, "payment_id": run.PaymentID})
		h.notifySettlementResult(ctx, run, "warning", "ต้องตรวจผล RC TikTok Shop", "SML อาจสร้าง RC แล้ว แต่ระบบบันทึกผลไม่ครบ กรุณาตรวจเอกสารก่อนลองใหม่", "unknown_result")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE tiktok_settlement_items SET status='sent',receipt_doc_no=$2,updated_at=NOW() WHERE run_id=$1::uuid AND status='ready'`, runID, out.Data.DocNo); err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE tiktok_settlement_runs SET status='sent',rc_doc_no=$2,error_msg='',lease_until=NULL,finished_at=NOW(),updated_at=NOW() WHERE id=$1::uuid AND status='sending'`, runID, out.Data.DocNo)
	}
	if err != nil || tx.Commit() != nil {
		h.unknownRun(ctx, runID, "บันทึกผล SML ไม่สำเร็จ กรุณาตรวจ RC")
		h.auditDirect(ctx, "tiktok_settlement_sml_unknown_result", userID, "warning", map[string]any{"run_id": runID, "statement_id": run.StatementID, "payment_id": run.PaymentID})
		h.notifySettlementResult(ctx, run, "warning", "ต้องตรวจผล RC TikTok Shop", "SML อาจสร้าง RC แล้ว แต่ระบบบันทึกผลไม่ครบ กรุณาตรวจเอกสารก่อนลองใหม่", "unknown_result")
		return
	}
	h.auditDirect(ctx, "tiktok_settlement_sent", userID, "info", map[string]any{"run_id": runID, "statement_id": run.StatementID, "payment_id": run.PaymentID, "rc_doc_no": out.Data.DocNo, "order_count": len(items)})
	h.notifySettlementResult(ctx, run, "success", "ส่ง RC TikTok Shop แล้ว", "สร้างรับชำระหนี้ใน SML เลขที่ "+out.Data.DocNo, "sent")
}

func (h *TikTokSettlementHandler) failSettlementRun(ctx context.Context, run *tikTokSettlementRunView, userID, message string) {
	if run == nil {
		return
	}
	h.failRun(ctx, run.ID, message)
	h.auditDirect(ctx, "tiktok_settlement_sml_failed", userID, "error", map[string]any{"run_id": run.ID, "statement_id": run.StatementID, "payment_id": run.PaymentID})
	h.notifySettlementResult(ctx, run, "error", "ส่ง RC TikTok Shop ไม่สำเร็จ", message, "failed")
}

// notifySettlementResult sends only terminal outcomes.  Payloads contain the
// Statement/Payment identity and no buyer data, so both Topbar and LINE are
// useful operational evidence without becoming a PII channel.
func (h *TikTokSettlementHandler) notifySettlementResult(ctx context.Context, run *tikTokSettlementRunView, severity, title, body, outcome string) {
	if h == nil || run == nil {
		return
	}
	entityID := strings.TrimSpace(run.ID)
	if entityID == "" {
		return
	}
	dedupe := "tiktok:settlement:" + entityID + ":" + strings.TrimSpace(outcome)
	if h.lineNotifications != nil {
		_, err := h.lineNotifications.Enqueue(ctx, models.LineNotificationMessageInput{
			Source: "tiktok_settlement", Severity: severity, Title: title,
			Body: body, ActionURL: "/tiktok-settlements", EntityType: "tiktok_settlement", EntityID: entityID,
			DedupeKey: dedupe, MessageText: title + "\nStatement: " + run.StatementID + "\nPayment ID: " + run.PaymentID + "\n" + body,
		})
		if err != nil && h.logger != nil {
			h.logger.Warn("enqueue TikTok settlement LINE notification failed", zap.String("run_id", entityID), zap.Error(err))
		}
	}
	if h.notifications == nil {
		return
	}
	created, err := h.notifications.CreateForRoles(ctx, []string{"admin", "staff"}, models.NotificationInput{
		Source: "tiktok_settlement", Severity: severity, Title: title, Body: body, ActionURL: "/tiktok-settlements",
		EntityType: "tiktok_settlement", EntityID: entityID, DedupeKey: dedupe,
	})
	if err != nil {
		if h.logger != nil {
			h.logger.Warn("create TikTok settlement notification failed", zap.String("run_id", entityID), zap.Error(err))
		}
		return
	}
	for _, notification := range created {
		if h.broker == nil {
			continue
		}
		unread, _ := h.notifications.UnreadCount(ctx, notification.RecipientID)
		bySource, _ := h.notifications.UnreadCountsBySource(ctx, notification.RecipientID)
		if bySource == nil {
			bySource = map[string]int{}
		}
		h.broker.Publish(events.Event{Type: events.TypeNotificationCreated, TargetUserID: notification.RecipientID, Payload: map[string]any{"notification": notification, "unread_count": unread, "unread_by_source": bySource}})
		h.broker.Publish(events.Event{Type: events.TypeNotificationUnreadChanged, TargetUserID: notification.RecipientID, Payload: map[string]any{"total": unread, "unread_by_source": bySource}})
	}
}
func (h *TikTokSettlementHandler) failRun(ctx context.Context, id, msg string) {
	_, _ = h.db.ExecContext(ctx, `UPDATE tiktok_settlement_runs SET status='failed',error_msg=$2,lease_until=NULL,finished_at=NOW(),updated_at=NOW() WHERE id=$1::uuid`, id, msg)
}
func (h *TikTokSettlementHandler) unknownRun(ctx context.Context, id, msg string) {
	_, _ = h.db.ExecContext(ctx, `UPDATE tiktok_settlement_runs SET status='unknown_result',error_msg=$2,lease_until=NULL,finished_at=NOW(),updated_at=NOW() WHERE id=$1::uuid`, id, msg)
}

func (h *TikTokSettlementHandler) connection(ctx context.Context, shop string) (string, string, error) {
	var label, id string
	err := h.db.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(BTRIM(label),''),NULLIF(BTRIM(shop_name),''),shop_id),gateway_connection_id::text FROM tiktok_shop_connections WHERE shop_id=$1 AND disabled_at IS NULL`, shop).Scan(&label, &id)
	return label, id, err
}
func (h *TikTokSettlementHandler) readEnabled(ctx context.Context, shop string) (bool, error) {
	var enabled bool
	err := h.db.QueryRowContext(ctx, `SELECT COALESCE(s.read_enabled,FALSE) FROM tiktok_shop_connections c LEFT JOIN tiktok_shop_settlement_settings s ON s.shop_id=c.shop_id WHERE c.shop_id=$1 AND c.disabled_at IS NULL`, shop).Scan(&enabled)
	return enabled, err
}
func (h *TikTokSettlementHandler) smlSendEnabled(ctx context.Context, shop string) (bool, int, error) {
	var enabled bool
	var version int
	err := h.db.QueryRowContext(ctx, `SELECT COALESCE(s.sml_send_enabled,FALSE),COALESCE(s.config_version,0) FROM tiktok_shop_connections c LEFT JOIN tiktok_shop_settlement_settings s ON s.shop_id=c.shop_id WHERE c.shop_id=$1 AND c.disabled_at IS NULL`, shop).Scan(&enabled, &version)
	return enabled, version, err
}
func (h *TikTokSettlementHandler) loadSettings(ctx context.Context, shop string) (gin.H, error) {
	var read, sml bool
	var version int
	err := h.db.QueryRowContext(ctx, `SELECT COALESCE(s.read_enabled,FALSE),COALESCE(s.sml_send_enabled,FALSE),COALESCE(s.config_version,0) FROM tiktok_shop_connections c LEFT JOIN tiktok_shop_settlement_settings s ON s.shop_id=c.shop_id WHERE c.shop_id=$1 AND c.disabled_at IS NULL`, shop).Scan(&read, &sml, &version)
	return gin.H{"shop_id": shop, "read_enabled": read, "sml_send_enabled": sml, "config_version": version}, err
}
func (h *TikTokSettlementHandler) loadRun(ctx context.Context, id string, withItems bool) (*tikTokSettlementRunView, error) {
	row := h.db.QueryRowContext(ctx, `SELECT id::text,shop_id,shop_label,statement_id,payment_id,payment_status,currency,payment_time,statement_time,total_settlement_amount,invoice_amount_total,fee_amount_total,shipping_amount_total,adjustment_amount_total,refund_amount_total,reserve_amount_total,status,config_version,route_config_version,rc_doc_no,error_msg,anomaly_reason,created_at,updated_at,COALESCE((SELECT COUNT(*) FROM tiktok_settlement_items i WHERE i.run_id=tiktok_settlement_runs.id),0),COALESCE((SELECT COUNT(*) FROM tiktok_settlement_items i WHERE i.run_id=tiktok_settlement_runs.id AND i.status='blocked'),0) FROM tiktok_settlement_runs WHERE id=$1::uuid`, id)
	run, err := scanTikTokSettlementRun(row)
	if err != nil {
		return nil, err
	}
	if withItems {
		rows, err := h.db.QueryContext(ctx, `SELECT id::text,order_id,sml_invoice_doc_no,customer_code,invoice_amount,settlement_amount,fee_amount,shipping_amount,adjustment_amount,refund_amount,reserve_amount,currency,status,block_reason,receipt_doc_no FROM tiktok_settlement_items WHERE run_id=$1::uuid ORDER BY order_id`, id)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		run.Items = []tikTokSettlementItemView{}
		for rows.Next() {
			var item tikTokSettlementItemView
			if err := rows.Scan(&item.ID, &item.OrderID, &item.SMLInvoiceDocNo, &item.CustomerCode, &item.InvoiceAmount, &item.SettlementAmount, &item.FeeAmount, &item.ShippingAmount, &item.AdjustmentAmount, &item.RefundAmount, &item.ReserveAmount, &item.Currency, &item.Status, &item.BlockReason, &item.ReceiptDocNo); err != nil {
				return nil, err
			}
			run.Items = append(run.Items, item)
		}
	}
	return run, nil
}
func scanTikTokSettlementRun(row interface{ Scan(...any) error }) (*tikTokSettlementRunView, error) {
	var r tikTokSettlementRunView
	var payment, statement sql.NullTime
	var created, updated time.Time
	err := row.Scan(&r.ID, &r.ShopID, &r.ShopLabel, &r.StatementID, &r.PaymentID, &r.PaymentStatus, &r.Currency, &payment, &statement, &r.TotalSettlementAmount, &r.InvoiceAmountTotal, &r.FeeAmountTotal, &r.ShippingAmountTotal, &r.AdjustmentAmountTotal, &r.RefundAmountTotal, &r.ReserveAmountTotal, &r.Status, &r.ConfigVersion, &r.RouteConfigVersion, &r.RCDocNo, &r.ErrorMsg, &r.AnomalyReason, &created, &updated, &r.ItemCount, &r.BlockedItemCount)
	if err != nil {
		return nil, err
	}
	if payment.Valid {
		r.PaymentTime = payment.Time.UTC().Format(time.RFC3339)
	}
	if statement.Valid {
		r.StatementTime = statement.Time.UTC().Format(time.RFC3339)
	}
	r.CreatedAt = created.UTC().Format(time.RFC3339)
	r.UpdatedAt = updated.UTC().Format(time.RFC3339)
	return &r, nil
}
func parseTikTokSettlementRange(fromRaw, toRaw string) (time.Time, time.Time, error) {
	loc := tikTokSettlementBangkok
	now := time.Now().In(loc)
	if strings.TrimSpace(toRaw) == "" {
		toRaw = now.Format("2006-01-02")
	}
	if strings.TrimSpace(fromRaw) == "" {
		fromRaw = now.AddDate(0, 0, -tikTokSettlementDefaultDays+1).Format("2006-01-02")
	}
	from, err := time.ParseInLocation("2006-01-02", fromRaw, loc)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("วันที่เริ่มต้นต้องเป็น YYYY-MM-DD")
	}
	to, err := time.ParseInLocation("2006-01-02", toRaw, loc)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("วันที่สิ้นสุดต้องเป็น YYYY-MM-DD")
	}
	to = to.AddDate(0, 0, 1)
	if !from.Before(to) || to.Sub(from) > time.Duration(tikTokSettlementMaxDays)*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("ช่วงวันที่ต้องไม่เกิน %d วัน", tikTokSettlementMaxDays)
	}
	return from, to, nil
}

// parseTikTokStatementImportRange accepts business transaction dates and turns
// them into TikTok's UTC Statement-generation window. TikTok generates the
// Statement for a transaction day at 00:00 UTC on the following day.
func parseTikTokStatementImportRange(fromRaw, toRaw string) (time.Time, time.Time, error) {
	fromRaw, toRaw = strings.TrimSpace(fromRaw), strings.TrimSpace(toRaw)
	if fromRaw == "" || toRaw == "" {
		return time.Time{}, time.Time{}, errors.New("กรุณาเลือกช่วงวันที่รายการขาย")
	}
	fromDate, err := time.ParseInLocation("2006-01-02", fromRaw, time.UTC)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("วันที่เริ่มต้นต้องเป็น YYYY-MM-DD")
	}
	toDate, err := time.ParseInLocation("2006-01-02", toRaw, time.UTC)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("วันที่สิ้นสุดต้องเป็น YYYY-MM-DD")
	}
	if toDate.Before(fromDate) || toDate.Sub(fromDate) >= time.Duration(tikTokSettlementMaxDays)*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("ช่วงวันที่ต้องไม่เกิน %d วัน", tikTokSettlementMaxDays)
	}
	return fromDate.AddDate(0, 0, 1), toDate.AddDate(0, 0, 1).Add(time.Second), nil
}

func financeErrorCode(err error) string {
	var e *tiktokshop.GatewayError
	if errors.As(err, &e) {
		return firstNonEmpty(strings.TrimSpace(e.Code), "gateway_request_failed")
	}
	return "gateway_request_failed"
}

func tikTokSettlementImportFailureStage(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	switch {
	case strings.HasPrefix(message, "read_statement_page:"):
		return "read_statement_page"
	case strings.HasPrefix(message, "store_statement:"):
		switch {
		case strings.Contains(message, "load_connection:"):
			return "load_connection"
		case strings.Contains(message, "load_settings:"):
			return "load_settings"
		case strings.Contains(message, "save_snapshot:"):
			return "save_snapshot"
		case strings.Contains(message, "reconcile_statement:"):
			return "reconcile_statement"
		default:
			return "store_statement"
		}
	default:
		return "unknown"
	}
}

func safeTikTokSettlementErrorReason(err error) string {
	if err == nil {
		return ""
	}
	value := strings.TrimSpace(err.Error())
	if len(value) > 180 {
		return value[:180]
	}
	return value
}

func financeThaiError(err error) string {
	switch financeErrorCode(err) {
	case "reconnect_required", "token_expired", "invalid_token":
		return "สิทธิ์ TikTok Shop หมดอายุ กรุณาเชื่อมต่อร้านใหม่"
	case "scope_unavailable", "permission_denied":
		return "ยังไม่ได้เปิดสิทธิ์ Finance Information ของ TikTok Shop"
	case "rate_limited":
		return "TikTok Shop จำกัดการเรียกข้อมูล กรุณาลองใหม่ภายหลัง"
	default:
		return "ดึง Statement จาก TikTok Shop ไม่สำเร็จ กรุณาลองใหม่ภายหลัง"
	}
}
func safeRequestID(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 128 {
		v = v[:128]
	}
	return v
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func (h *TikTokSettlementHandler) error(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
func (h *TikTokSettlementHandler) auditEvent(c *gin.Context, action, level string, detail map[string]any) {
	h.auditDirect(c.Request.Context(), action, c.GetString("user_id"), level, detail)
}
func (h *TikTokSettlementHandler) auditDirect(ctx context.Context, action, userID, level string, detail map[string]any) {
	if h.audit == nil {
		return
	}
	var id *string
	if strings.TrimSpace(userID) != "" {
		id = &userID
	}
	_ = h.audit.Log(models.AuditEntry{Action: action, UserID: id, Source: "tiktok_settlement", Level: level, Detail: detail})
}
