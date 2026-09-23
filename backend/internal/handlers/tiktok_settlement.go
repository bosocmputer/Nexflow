package handlers

// TikTok Finance settlements are intentionally manual.  This handler imports
// an immutable local snapshot, reconciles every order in a statement, and only
// then allows one confirmed AR receipt write.  It never polls Finance on page
// render and never sends a partial statement.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	pendingIncomeMu   sync.Mutex
	pendingIncome     map[string]*tikTokIncomePendingPreview
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

const (
	tikTokIncomePreviewTTL     = 15 * time.Minute
	tikTokIncomeMaxFileBytes   = 20 * 1024 * 1024
	tikTokIncomeMaxPreviews    = 16
	tikTokIncomeEvidencePrefix = "income-export:"
)

type tikTokIncomePendingPreview struct {
	export    *tikTokIncomeExport
	shopID    string
	userID    string
	fileHash  string
	createdAt time.Time
}

type tikTokIncomeReceiptConfirmRequest struct {
	PreviewToken   string `json:"preview_token"`
	WithdrawalID   string `json:"withdrawal_id"`
	BankReceivedAt string `json:"bank_received_at"`
	Confirmation   string `json:"confirmation"`
}

func NewTikTokSettlementHandler(db *sql.DB, cfg *config.Config, gateway *tiktokshop.GatewayClient, routes *repository.ChannelDefaultRepo, audit *repository.AuditLogRepo, smlBridge *ShopeeImportHandler, notifications *repository.NotificationRepo, lineNotifications *repository.LineNotificationRepo, broker *events.Broker, logger *zap.Logger) *TikTokSettlementHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TikTokSettlementHandler{db: db, config: cfg, gateway: gateway, routes: routes, audit: audit, sml: smlBridge, notifications: notifications, lineNotifications: lineNotifications, broker: broker, logger: logger, pendingIncome: make(map[string]*tikTokIncomePendingPreview)}
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
	result, err := h.gateway.SearchFinanceWithdrawals(c.Request.Context(), tiktokshop.GatewayFinanceWithdrawalsRequest{ShopID: shopID, Search: tiktokshop.SearchWithdrawalsRequest{Types: []tiktokshop.WithdrawalType{tiktokshop.WithdrawalTypeWithdraw}, PageSize: 1, CreateTimeGE: from.Unix(), CreateTimeLT: to.Unix()}})
	if err != nil {
		h.auditEvent(c, "tiktok_settlement_preflight_failed", "error", map[string]any{"shop_id": shopID, "error_code": financeErrorCode(err)})
		h.error(c, http.StatusBadGateway, financeErrorCode(err), financeThaiError(err))
		return
	}
	_, _ = h.db.ExecContext(c.Request.Context(), `UPDATE tiktok_shop_settlement_settings SET last_successful_preflight_at=NOW(),updated_at=NOW() WHERE shop_id=$1`, shopID)
	h.auditEvent(c, "tiktok_settlement_preflight_completed", "info", map[string]any{"shop_id": shopID, "result_count": len(result.Withdrawals), "request_id": safeRequestID(result.UpstreamRequestID)})
	c.JSON(200, gin.H{"data": gin.H{"status": "ready", "message": "พร้อมใช้: เชื่อมต่อข้อมูลรอบถอนเงิน TikTok Shop ได้", "withdrawal_count": len(result.Withdrawals)}})
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
	importResult, err := h.importStatements(c.Request.Context(), req.ShopID, from, to, c.GetString("user_id"), c.GetString("user_email"))
	if err != nil {
		h.auditEvent(c, "tiktok_settlement_import_failed", "error", map[string]any{"shop_id": req.ShopID, "error_code": financeErrorCode(err)})
		h.error(c, 502, financeErrorCode(err), financeThaiError(err))
		return
	}
	_, _ = h.db.ExecContext(c.Request.Context(), `UPDATE tiktok_shop_settlement_settings SET last_import_at=NOW(),updated_at=NOW() WHERE shop_id=$1`, req.ShopID)
	h.auditEvent(c, "tiktok_settlement_import_completed", "info", map[string]any{"shop_id": req.ShopID, "statement_count": importResult.count, "all_status_fallback": importResult.usedAllStatusFallback, "unavailable_statuses": importResult.unavailableStatuses})
	message := "ดึง Statement แล้ว ระบบตรวจเทียบกับใบขาย SML เรียบร้อย"
	if importResult.usedAllStatusFallback {
		message = "ดึง Statement ครบแล้ว โดยใช้รายการรวมทุกสถานะหลัง TikTok ไม่ตอบการกรองสถานะย่อย"
	}
	if len(importResult.unavailableStatuses) > 0 {
		message = "ดึง Statement ที่ TikTok แจ้งว่าโอนแล้วแล้ว แต่ TikTok ยังตอบสถานะรอโอน/โอนไม่สำเร็จไม่ได้ในขณะนี้ จึงยังไม่แสดงสองสถานะดังกล่าว"
	}
	c.JSON(http.StatusAccepted, gin.H{"imported_count": importResult.count, "partial": len(importResult.unavailableStatuses) > 0, "unavailable_statuses": importResult.unavailableStatuses, "message": message})
}

// PreviewIncomeExport parses an official Seller Center Income export locally.
// It deliberately stores no raw workbook and writes no finance/SML record. The
// result expires, is user-scoped, and requires an explicit bank attestation in
// the next step because TikTok does not expose withdrawal membership in TH.
func (h *TikTokSettlementHandler) PreviewIncomeExport(c *gin.Context) {
	if !h.financeEnabled() {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิดข้อมูลการเงิน TikTok Shop")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, tikTokIncomeMaxFileBytes)
	shopID := strings.TrimSpace(c.PostForm("shop_id"))
	if shopID == "" {
		h.error(c, http.StatusBadRequest, "shop_required", "กรุณาเลือกร้าน TikTok Shop ก่อนนำเข้าไฟล์รายได้")
		return
	}
	if ok, err := h.readEnabled(c.Request.Context(), shopID); err != nil {
		h.error(c, http.StatusInternalServerError, "settings_read_failed", "อ่านการตั้งค่าร้านไม่สำเร็จ")
		return
	} else if !ok {
		h.error(c, http.StatusForbidden, "read_disabled", "ร้านนี้ยังไม่เปิดอ่านข้อมูลการเงิน TikTok Shop")
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		h.error(c, http.StatusBadRequest, "file_required", "กรุณาเลือกไฟล์ Excel รายได้จาก TikTok Shop")
		return
	}
	if file.Size <= 0 || file.Size > tikTokIncomeMaxFileBytes || !strings.HasSuffix(strings.ToLower(strings.TrimSpace(file.Filename)), ".xlsx") {
		h.error(c, http.StatusBadRequest, "invalid_file", "รองรับเฉพาะไฟล์ .xlsx จาก TikTok Shop ขนาดไม่เกิน 20 MB")
		return
	}
	src, err := file.Open()
	if err != nil {
		h.error(c, http.StatusBadRequest, "file_open_failed", "เปิดไฟล์รายได้ TikTok ไม่สำเร็จ")
		return
	}
	defer src.Close()
	data, err := io.ReadAll(io.LimitReader(src, tikTokIncomeMaxFileBytes+1))
	if err != nil || len(data) == 0 || len(data) > tikTokIncomeMaxFileBytes {
		h.error(c, http.StatusBadRequest, "invalid_file", "อ่านไฟล์รายได้ TikTok ไม่สำเร็จ หรือไฟล์มีขนาดเกินกำหนด")
		return
	}
	export, err := parseTikTokIncomeExport(bytes.NewReader(data))
	if err != nil {
		h.error(c, http.StatusUnprocessableEntity, "income_export_invalid", errorText(err))
		return
	}
	fileHash := sha256.Sum256(data)
	token, err := randomTikTokPreviewToken()
	if err != nil {
		h.error(c, http.StatusInternalServerError, "preview_unavailable", "สร้างตัวอย่างไฟล์ไม่สำเร็จ กรุณาลองใหม่")
		return
	}
	if !h.storeIncomePreview(token, &tikTokIncomePendingPreview{
		export: export, shopID: shopID, userID: c.GetString("user_id"),
		fileHash: hex.EncodeToString(fileHash[:]), createdAt: time.Now(),
	}, time.Now()) {
		h.error(c, http.StatusTooManyRequests, "income_preview_limit", "มีการตรวจไฟล์รายได้ค้างอยู่มากเกินไป กรุณารอ 15 นาทีแล้วลองใหม่")
		return
	}
	h.auditEvent(c, "tiktok_income_export_previewed", "info", map[string]any{
		"shop_id": shopID, "order_count": len(export.Orders), "withdrawal_count": len(export.Withdrawals),
	})
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"preview_token": token, "currency": export.Currency,
		"order_count": len(export.Orders), "order_payment_total": moneyFromCents(export.OrderPaymentTotalCents),
		"report_payment_total": moneyFromCents(export.ReportPaymentTotalCents), "withdrawals": export.Withdrawals,
		"message": "อ่านไฟล์แล้ว เลือกรอบถอนและยืนยันว่าเงินเข้าบัญชีจริงก่อนสร้างร่าง RC",
	}})
}

// CreateIncomeReceiptCandidate persists an immutable, locally-attested RC
// candidate. It does not call SML. A separate existing confirmed-send action
// remains the only SML write path.
func (h *TikTokSettlementHandler) CreateIncomeReceiptCandidate(c *gin.Context) {
	if !h.financeEnabled() {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิดข้อมูลการเงิน TikTok Shop")
		return
	}
	var req tikTokIncomeReceiptConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.error(c, http.StatusBadRequest, "invalid_request", "ข้อมูลยืนยันรอบถอนเงินไม่ถูกต้อง")
		return
	}
	pending, ok := h.consumeIncomePreview(req.PreviewToken, c.GetString("user_id"), time.Now())
	if !ok {
		h.error(c, http.StatusConflict, "preview_expired", "ตัวอย่างไฟล์หมดอายุหรือถูกใช้แล้ว กรุณาอัปโหลดใหม่")
		return
	}
	if err := validateTikTokIncomeReceiptAttestation(*pending.export, req.WithdrawalID, req.Confirmation); err != nil {
		h.error(c, http.StatusUnprocessableEntity, "income_receipt_blocked", errorText(err))
		return
	}
	bankReceivedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(req.BankReceivedAt))
	if err != nil || bankReceivedAt.After(time.Now().Add(5*time.Minute)) {
		h.error(c, http.StatusBadRequest, "bank_received_at_invalid", "กรุณาระบุวันและเวลาที่เงินเข้าบัญชีจริง")
		return
	}
	if ok, err := h.readEnabled(c.Request.Context(), pending.shopID); err != nil {
		h.error(c, http.StatusInternalServerError, "settings_read_failed", "อ่านการตั้งค่าร้านไม่สำเร็จ")
		return
	} else if !ok {
		h.error(c, http.StatusForbidden, "read_disabled", "ร้านนี้ยังไม่เปิดอ่านข้อมูลการเงิน TikTok Shop")
		return
	}
	withdrawal, _ := pending.export.SelectWithdrawal(req.WithdrawalID)
	run, err := h.createIncomeExportRun(c.Request.Context(), pending, withdrawal.Withdrawal, bankReceivedAt, c.GetString("user_id"), c.GetString("user_email"))
	if err != nil {
		if strings.Contains(err.Error(), "already imported") {
			h.error(c, http.StatusConflict, "withdrawal_already_imported", "รอบถอนนี้ถูกนำเข้าแล้ว ระบบจะไม่สร้างร่าง RC ซ้ำ")
			return
		}
		h.logger.Warn("create TikTok income export receipt candidate failed", zap.String("shop_id", pending.shopID), zap.Error(err))
		h.error(c, http.StatusInternalServerError, "income_receipt_create_failed", "สร้างร่าง RC จากไฟล์รายได้ไม่สำเร็จ")
		return
	}
	h.auditEvent(c, "tiktok_income_export_receipt_candidate_created", "info", map[string]any{
		"shop_id": pending.shopID, "withdrawal_id": withdrawal.Withdrawal.ID, "run_id": run.ID, "order_count": run.ItemCount,
	})
	c.JSON(http.StatusCreated, gin.H{"data": run, "message": "สร้างร่าง RC แล้ว กรุณาตรวจรายการก่อนกดยืนยันส่งเข้า SML"})
}

func (h *TikTokSettlementHandler) consumeIncomePreview(token, userID string, now time.Time) (*tikTokIncomePendingPreview, bool) {
	h.pendingIncomeMu.Lock()
	defer h.pendingIncomeMu.Unlock()
	if h.pendingIncome == nil {
		return nil, false
	}
	token = strings.TrimSpace(token)
	pending, ok := h.pendingIncome[token]
	delete(h.pendingIncome, token)
	if !ok {
		return nil, false
	}
	if pending == nil || pending.export == nil || pending.userID != strings.TrimSpace(userID) || now.Sub(pending.createdAt) > tikTokIncomePreviewTTL {
		return nil, false
	}
	return pending, true
}

func (h *TikTokSettlementHandler) storeIncomePreview(token string, preview *tikTokIncomePendingPreview, now time.Time) bool {
	h.pendingIncomeMu.Lock()
	defer h.pendingIncomeMu.Unlock()
	if h.pendingIncome == nil {
		h.pendingIncome = make(map[string]*tikTokIncomePendingPreview)
	}
	for key, existing := range h.pendingIncome {
		if existing == nil || now.Sub(existing.createdAt) > tikTokIncomePreviewTTL {
			delete(h.pendingIncome, key)
		}
	}
	if len(h.pendingIncome) >= tikTokIncomeMaxPreviews {
		return false
	}
	h.pendingIncome[token] = preview
	return true
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
	args := []any{}
	where := "WHERE 1=1"
	if shop := strings.TrimSpace(c.Query("shop_id")); shop != "" {
		args = append(args, shop)
		where += fmt.Sprintf(" AND shop_id=$%d", len(args))
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND status=$%d", len(args))
	}
	if paymentStatus := strings.TrimSpace(c.Query("payment_status")); paymentStatus != "" {
		switch paymentStatus {
		case string(tiktokshop.StatementStatusPaid), string(tiktokshop.StatementStatusProcessing), string(tiktokshop.StatementStatusFailed), "BANK_CONFIRMED":
			args = append(args, paymentStatus)
			where += fmt.Sprintf(" AND payment_status=$%d", len(args))
		default:
			h.error(c, http.StatusBadRequest, "invalid_payment_status", "สถานะการโอน TikTok Shop ไม่ถูกต้อง")
			return
		}
	}
	for _, filter := range []struct {
		raw    string
		column string
		end    bool
	}{{raw: c.Query("date_from"), column: ">=", end: false}, {raw: c.Query("date_to"), column: "<", end: true}} {
		if strings.TrimSpace(filter.raw) == "" {
			continue
		}
		date, err := time.ParseInLocation("2006-01-02", filter.raw, tikTokSettlementBangkok)
		if err != nil {
			h.error(c, http.StatusBadRequest, "invalid_date", "ช่วงวันที่ไม่ถูกต้อง")
			return
		}
		if filter.end {
			date = date.AddDate(0, 0, 1)
		}
		args = append(args, date)
		where += fmt.Sprintf(" AND COALESCE(payment_time,statement_time) %s $%d", filter.column, len(args))
	}
	args = append(args, limit)
	rows, err := h.db.QueryContext(c.Request.Context(), `SELECT r.id::text,r.shop_id,r.shop_label,r.statement_id,r.payment_id,r.payment_status,r.currency,r.payment_time,r.statement_time,r.total_settlement_amount,r.invoice_amount_total,r.fee_amount_total,r.shipping_amount_total,r.adjustment_amount_total,r.refund_amount_total,r.reserve_amount_total,r.status,r.config_version,r.route_config_version,r.rc_doc_no,r.error_msg,r.anomaly_reason,r.created_at,r.updated_at,COALESCE((SELECT COUNT(*) FROM tiktok_settlement_items i WHERE i.run_id=r.id),0) FROM tiktok_settlement_runs r `+where+fmt.Sprintf(" ORDER BY r.payment_time DESC NULLS LAST,r.created_at DESC LIMIT $%d", len(args)), args...)
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
	var out tikTokSettlementCounts
	rows, err := h.db.QueryContext(c.Request.Context(), `SELECT status,COUNT(*) FROM tiktok_settlement_runs GROUP BY status`)
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
	count                 int
	usedAllStatusFallback bool
	unavailableStatuses   []tiktokshop.StatementStatus
}

func (h *TikTokSettlementHandler) importStatements(ctx context.Context, shop string, from, to time.Time, userID, email string) (tikTokSettlementImportResult, error) {
	imported := make(map[string]struct{})
	statuses := []tiktokshop.StatementStatus{tiktokshop.StatementStatusPaid, tiktokshop.StatementStatusProcessing, tiktokshop.StatementStatusFailed}
	paidLoaded := false
	for index, status := range statuses {
		if err := h.importStatementPages(ctx, shop, from, to, status, userID, email, imported); err != nil {
			if !shouldUseAllTikTokStatementStatuses(err) {
				return tikTokSettlementImportResult{}, err
			}
			if fallbackErr := h.importStatementPages(ctx, shop, from, to, "", userID, email, imported); fallbackErr != nil {
				if shouldCompleteTikTokSettlementPaidOnlyImport(status, paidLoaded, fallbackErr) {
					return tikTokSettlementImportResult{count: len(imported), unavailableStatuses: append([]tiktokshop.StatementStatus(nil), statuses[index:]...)}, nil
				}
				return tikTokSettlementImportResult{}, fallbackErr
			}
			return tikTokSettlementImportResult{count: len(imported), usedAllStatusFallback: true}, nil
		}
		if status == tiktokshop.StatementStatusPaid {
			paidLoaded = true
		}
	}
	return tikTokSettlementImportResult{count: len(imported)}, nil
}

func (h *TikTokSettlementHandler) importStatementPages(ctx context.Context, shop string, from, to time.Time, status tiktokshop.StatementStatus, userID, email string, imported map[string]struct{}) error {
	token := ""
	for page := 0; page < tikTokSettlementMaxPages; page++ {
		r, err := h.gateway.SearchFinanceStatements(ctx, tiktokshop.GatewayFinanceStatementsRequest{ShopID: shop, Search: tiktokshop.SearchStatementsRequest{PageSize: 100, PageToken: token, StatementTimeGE: from.Unix(), StatementTimeLT: to.Unix(), StatementStatus: status}})
		if err != nil {
			return err
		}
		for _, statement := range r.Statements {
			if err := h.upsertStatement(ctx, shop, statement, r.UpstreamRequestID, userID, email); err != nil {
				return err
			}
			imported[statement.StatementID] = struct{}{}
		}
		token = strings.TrimSpace(r.NextPageToken)
		if token == "" {
			break
		}
	}
	return nil
}

func shouldUseAllTikTokStatementStatuses(err error) bool {
	var gatewayErr *tiktokshop.GatewayError
	if !errors.As(err, &gatewayErr) || !gatewayErr.Retryable {
		return false
	}
	return gatewayErr.Code == "internal_error" || gatewayErr.Code == "tiktok_api_error"
}

// TikTok PAID is the only status eligible for settlement reconciliation. When
// PAID has been read successfully, a retryable upstream failure for an
// informational status may complete as a visibly partial import. Permission,
// rate-limit, and PAID failures stay fail-closed.
func shouldCompleteTikTokSettlementPaidOnlyImport(status tiktokshop.StatementStatus, paidLoaded bool, err error) bool {
	return paidLoaded && status != tiktokshop.StatementStatusPaid && shouldUseAllTikTokStatementStatuses(err)
}

func (h *TikTokSettlementHandler) upsertStatement(ctx context.Context, shop string, s tiktokshop.Statement, requestID, userID, email string) error {
	label, connectionID, err := h.connection(ctx, shop)
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(s)
	if err != nil {
		return err
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
		return err
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
			return updateErr
		}
		if n, _ := changed.RowsAffected(); n > 0 {
			h.auditDirect(ctx, "tiktok_settlement_anomaly", userID, "warning", map[string]any{"shop_id": shop, "statement_id": s.StatementID, "reason": "upstream_data_changed_after_rc"})
		}
		return nil
	}
	if err != nil {
		return err
	}
	if s.Status == tiktokshop.StatementStatusPaid {
		return h.fetchAndReconcile(ctx, runID, shop, s.StatementID)
	}
	return nil
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

// createIncomeExportRun turns one operator-attested Income export into the
// existing immutable settlement-run model. The input was validated before this
// function is called; still, the database uniqueness constraint is the final
// duplicate guard. This function only writes Nexflow's local reconciliation
// snapshot and makes read-only SML candidate lookups through reconcileRun.
func (h *TikTokSettlementHandler) createIncomeExportRun(ctx context.Context, pending *tikTokIncomePendingPreview, withdrawal tikTokIncomeWithdrawal, bankReceivedAt time.Time, userID, email string) (*tikTokSettlementRunView, error) {
	if pending == nil || pending.export == nil {
		return nil, errors.New("income preview unavailable")
	}
	label, connectionID, err := h.connection(ctx, pending.shopID)
	if err != nil {
		return nil, err
	}
	settings, err := h.loadSettings(ctx, pending.shopID)
	if err != nil {
		return nil, err
	}
	settingVersion, _ := settings["config_version"].(int)
	routeVersion := int64(0)
	if route, routeErr := h.routes.Get("tiktok_settlement", "ar_receipt"); routeErr == nil && route != nil {
		routeVersion = route.ConfigVersion
	}

	statementID := tikTokIncomeEvidencePrefix + strings.TrimSpace(withdrawal.ID)
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var runID string
	err = tx.QueryRowContext(ctx, `INSERT INTO tiktok_settlement_runs(connection_id,shop_id,shop_label,statement_id,payment_id,payment_status,currency,payment_time,total_settlement_amount,fee_amount_total,shipping_amount_total,adjustment_amount_total,refund_amount_total,reserve_amount_total,content_hash,upstream_request_id,status,config_version,route_config_version,created_by,created_by_email,updated_at) VALUES($1::uuid,$2,$3,$4,$5,'BANK_CONFIRMED','THB',$6,$7::numeric,$8::numeric,$9::numeric,'0'::numeric,'0'::numeric,'0'::numeric,$10,'income_export_v1','reconciling',$11,$12,NULLIF($13,'')::uuid,$14,NOW()) ON CONFLICT(shop_id,statement_id) DO NOTHING RETURNING id::text`, connectionID, pending.shopID, label, statementID, withdrawal.ID, bankReceivedAt, tikTokIncomeCentsDecimal(pending.export.OrderPaymentTotalCents), tikTokIncomeCentsDecimal(sumTikTokIncomeCents(pending.export.Orders, func(order tikTokIncomeOrder) int64 { return order.FeeCents })), tikTokIncomeCentsDecimal(sumTikTokIncomeCents(pending.export.Orders, func(order tikTokIncomeOrder) int64 { return order.ShippingCents })), pending.fileHash, settingVersion, routeVersion, userID, email).Scan(&runID)
	if err == sql.ErrNoRows {
		return nil, errors.New("withdrawal already imported")
	}
	if err != nil {
		return nil, err
	}
	for _, order := range pending.export.Orders {
		snapshot, marshalErr := json.Marshal(map[string]any{
			"source": "tiktok_income_export_v1", "transaction_type": strings.TrimSpace(order.TransactionType),
			"payment_time": strings.TrimSpace(order.PaymentTime), "income_cents": order.IncomeCents,
			"fee_cents": order.FeeCents, "shipping_cents": order.ShippingCents, "refund_cents": order.RefundCents,
		})
		if marshalErr != nil {
			return nil, marshalErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tiktok_settlement_items(run_id,order_id,settlement_amount,fee_amount,shipping_amount,adjustment_amount,refund_amount,reserve_amount,currency,snapshot,updated_at) VALUES($1::uuid,$2,$3::numeric,$4::numeric,$5::numeric,'0'::numeric,'0'::numeric,'0'::numeric,'THB',$6::jsonb,NOW())`, runID, strings.TrimSpace(order.OrderID), tikTokIncomeCentsDecimal(order.PaymentCents), tikTokIncomeCentsDecimal(order.FeeCents), tikTokIncomeCentsDecimal(order.ShippingCents), snapshot); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if err = h.reconcileRun(ctx, runID); err != nil {
		return nil, err
	}
	return h.loadRun(ctx, runID, true)
}

func sumTikTokIncomeCents(orders []tikTokIncomeOrder, value func(tikTokIncomeOrder) int64) int64 {
	var total int64
	for _, order := range orders {
		total += value(order)
	}
	return total
}

func tikTokIncomeCentsDecimal(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
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
	if run.PaymentStatus != "PAID" && run.PaymentStatus != "BANK_CONFIRMED" {
		_, err = h.db.ExecContext(ctx, `UPDATE tiktok_settlement_runs SET status=CASE WHEN payment_status='FAILED' THEN 'failed' ELSE 'needs_review' END,error_msg=CASE WHEN payment_status='FAILED' THEN 'TikTok Shop แจ้งว่าโอนไม่สำเร็จ' ELSE 'TikTok Shop ยังไม่แจ้งว่าโอนสำเร็จ' END,updated_at=NOW() WHERE id=$1::uuid`, runID)
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
	if strings.HasPrefix(run.StatementID, tikTokIncomeEvidencePrefix) {
		remark = fmt.Sprintf("TikTok Income Export Withdrawal %s; bank receipt confirmed", run.PaymentID)
	}
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
	row := h.db.QueryRowContext(ctx, `SELECT id::text,shop_id,shop_label,statement_id,payment_id,payment_status,currency,payment_time,statement_time,total_settlement_amount,invoice_amount_total,fee_amount_total,shipping_amount_total,adjustment_amount_total,refund_amount_total,reserve_amount_total,status,config_version,route_config_version,rc_doc_no,error_msg,anomaly_reason,created_at,updated_at,COALESCE((SELECT COUNT(*) FROM tiktok_settlement_items i WHERE i.run_id=tiktok_settlement_runs.id),0) FROM tiktok_settlement_runs WHERE id=$1::uuid`, id)
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
	err := row.Scan(&r.ID, &r.ShopID, &r.ShopLabel, &r.StatementID, &r.PaymentID, &r.PaymentStatus, &r.Currency, &payment, &statement, &r.TotalSettlementAmount, &r.InvoiceAmountTotal, &r.FeeAmountTotal, &r.ShippingAmountTotal, &r.AdjustmentAmountTotal, &r.RefundAmountTotal, &r.ReserveAmountTotal, &r.Status, &r.ConfigVersion, &r.RouteConfigVersion, &r.RCDocNo, &r.ErrorMsg, &r.AnomalyReason, &created, &updated, &r.ItemCount)
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
func financeErrorCode(err error) string {
	var e *tiktokshop.GatewayError
	if errors.As(err, &e) {
		return firstNonEmpty(strings.TrimSpace(e.Code), "gateway_request_failed")
	}
	return "gateway_request_failed"
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
