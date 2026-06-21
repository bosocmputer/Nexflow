package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/lib/pq"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/services/shopeeapi"
	"nexflow/internal/services/sml"
)

const shopeeSettlementMaxEscrows = 1000

type ShopeeSettlementDefaults struct {
	DocFormatCode string `json:"doc_format_code"`
	PassbookCode  string `json:"passbook_code"`
	PassbookName  string `json:"passbook_name"`
	BankCode      string `json:"bank_code"`
	BankBranch    string `json:"bank_branch"`
	ExpenseCode   string `json:"expense_code"`
	ExpenseName   string `json:"expense_name"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

type shopeeSettlementPreviewRequest struct {
	ConnectionID    string `json:"connection_id"`
	ReleaseTimeFrom string `json:"release_time_from"`
	ReleaseTimeTo   string `json:"release_time_to"`
	PageSize        int    `json:"page_size"`
}

type shopeeSettlementSendRequest struct {
	DocFormatCode string `json:"doc_format_code"`
	PassbookCode  string `json:"passbook_code"`
	PassbookName  string `json:"passbook_name"`
	BankCode      string `json:"bank_code"`
	BankBranch    string `json:"bank_branch"`
	ExpenseCode   string `json:"expense_code"`
	ExpenseName   string `json:"expense_name"`
	Remark        string `json:"remark"`
	DocDate       string `json:"doc_date"`
	DocTime       string `json:"doc_time"`
}

type shopeeSettlementRunView struct {
	ID                      string                     `json:"id"`
	ConnectionID            string                     `json:"connection_id,omitempty"`
	ShopID                  int64                      `json:"shop_id"`
	ShopLabel               string                     `json:"shop_label"`
	ReleaseTimeFrom         string                     `json:"release_time_from"`
	ReleaseTimeTo           string                     `json:"release_time_to"`
	ReleaseDateFrom         string                     `json:"release_date_from"`
	ReleaseDateTo           string                     `json:"release_date_to"`
	Status                  string                     `json:"status"`
	TotalCount              int                        `json:"total_count"`
	ReadyCount              int                        `json:"ready_count"`
	BlockedCount            int                        `json:"blocked_count"`
	SentCount               int                        `json:"sent_count"`
	InvoiceAmountTotal      float64                    `json:"invoice_amount_total"`
	PayoutAmountTotal       float64                    `json:"payout_amount_total"`
	DifferenceAmountTotal   float64                    `json:"difference_amount_total"`
	ReadyInvoiceAmount      float64                    `json:"ready_invoice_amount"`
	ReadyPayoutAmount       float64                    `json:"ready_payout_amount"`
	ReadyDifferenceAmount   float64                    `json:"ready_difference_amount"`
	BlockedInvoiceAmount    float64                    `json:"blocked_invoice_amount"`
	BlockedPayoutAmount     float64                    `json:"blocked_payout_amount"`
	BlockedDifferenceAmount float64                    `json:"blocked_difference_amount"`
	RCDocNo                 string                     `json:"rc_doc_no,omitempty"`
	ErrorMsg                string                     `json:"error_msg,omitempty"`
	SelectedDocFormatCode   string                     `json:"selected_doc_format_code,omitempty"`
	SelectedPassbookCode    string                     `json:"selected_passbook_code,omitempty"`
	SelectedPassbookName    string                     `json:"selected_passbook_name,omitempty"`
	SelectedBankCode        string                     `json:"selected_bank_code,omitempty"`
	SelectedBankBranch      string                     `json:"selected_bank_branch,omitempty"`
	SelectedExpenseCode     string                     `json:"selected_expense_code,omitempty"`
	SelectedExpenseName     string                     `json:"selected_expense_name,omitempty"`
	CreatedAt               string                     `json:"created_at"`
	UpdatedAt               string                     `json:"updated_at"`
	StartedAt               string                     `json:"started_at,omitempty"`
	FinishedAt              string                     `json:"finished_at,omitempty"`
	HiddenAt                string                     `json:"hidden_at,omitempty"`
	HiddenBy                string                     `json:"hidden_by,omitempty"`
	HiddenReason            string                     `json:"hidden_reason,omitempty"`
	Items                   []shopeeSettlementItemView `json:"items"`
}

type shopeeSettlementItemView struct {
	ID                   string  `json:"id"`
	ShopID               int64   `json:"-"`
	OrderSN              string  `json:"order_sn"`
	EscrowReleaseTime    string  `json:"escrow_release_time,omitempty"`
	PayoutAmount         float64 `json:"payout_amount"`
	EscrowAmount         float64 `json:"escrow_amount"`
	BuyerTotalAmount     float64 `json:"buyer_total_amount"`
	InvoiceDocNo         string  `json:"invoice_doc_no,omitempty"`
	InvoiceDocDate       string  `json:"invoice_doc_date,omitempty"`
	CustCode             string  `json:"cust_code,omitempty"`
	InvoiceAmount        float64 `json:"invoice_amount"`
	DifferenceAmount     float64 `json:"difference_amount"`
	Status               string  `json:"status"`
	BlockReason          string  `json:"block_reason,omitempty"`
	ReceiptDocNo         string  `json:"receipt_doc_no,omitempty"`
	ExistingReceiptDocNo string  `json:"existing_receipt_doc_no,omitempty"`
}

type settlementCandidateResponse struct {
	Items []settlementCandidate `json:"items"`
}

type settlementCandidate struct {
	OrderSN              string  `json:"order_sn"`
	InvoiceDocNo         string  `json:"invoice_doc_no"`
	InvoiceDocDate       string  `json:"invoice_doc_date"`
	CustCode             string  `json:"cust_code"`
	InvoiceAmount        float64 `json:"invoice_amount"`
	AlreadyReceived      bool    `json:"already_received"`
	ExistingReceiptDocNo string  `json:"existing_receipt_doc_no"`
	Status               string  `json:"status"`
	Message              string  `json:"message"`
}

type settlementReceiptResponse struct {
	DocNo            string  `json:"doc_no"`
	Status           string  `json:"status"`
	InvoiceAmount    float64 `json:"invoice_amount"`
	PayoutAmount     float64 `json:"payout_amount"`
	DifferenceAmount float64 `json:"difference_amount"`
}

type settlementReconcileResult struct {
	TotalCount    int      `json:"total_count"`
	ReadyCount    int      `json:"ready_count"`
	BlockedCount  int      `json:"blocked_count"`
	SentCount     int      `json:"sent_count"`
	NewlyBlocked  int      `json:"newly_blocked"`
	NewStatus     string   `json:"new_status"`
	BlockedDocNos []string `json:"blocked_doc_nos,omitempty"`
}

// GET /api/settings/shopee-settlement-defaults
func (h *ShopeeImportHandler) GetSettlementDefaults(c *gin.Context) {
	defaults, err := h.loadSettlementDefaults(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดค่าเริ่มต้นรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": defaults})
}

// PUT /api/settings/shopee-settlement-defaults
func (h *ShopeeImportHandler) UpdateSettlementDefaults(c *gin.Context) {
	var req ShopeeSettlementDefaults
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload ไม่ถูกต้อง"})
		return
	}
	req.DocFormatCode = strings.TrimSpace(req.DocFormatCode)
	req.PassbookCode = strings.TrimSpace(req.PassbookCode)
	req.PassbookName = strings.TrimSpace(req.PassbookName)
	req.BankCode = strings.TrimSpace(req.BankCode)
	req.BankBranch = strings.TrimSpace(req.BankBranch)
	req.ExpenseCode = strings.TrimSpace(req.ExpenseCode)
	req.ExpenseName = strings.TrimSpace(req.ExpenseName)
	if req.DocFormatCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาเลือกรูปแบบเอกสารรับชำระ (screen_code=EE)"})
		return
	}
	if req.PassbookCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาเลือกบัญชีรับเงิน"})
		return
	}
	var userID any
	if uid := c.GetString("user_id"); uid != "" {
		userID = uid
	}
	tx, err := h.sqlDB().BeginTx(c.Request.Context(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกค่าเริ่มต้นรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(c.Request.Context(), `
		INSERT INTO channel_defaults (
			channel, bill_type, party_code, party_name, doc_format_code, endpoint,
			doc_prefix, doc_running_format, passbook_code, passbook_name, bank_code, bank_branch,
			expense_code, expense_name, updated_by, updated_at
		) VALUES (
			'shopee_settlement','ar_receipt','','',$1,'/api/v1/ar/receipts',
			$1,'@YYMM####',$2,$3,$4,$5,$6,$7,$8,NOW()
		)
		ON CONFLICT (channel, bill_type) DO UPDATE SET
			doc_format_code=EXCLUDED.doc_format_code,
			endpoint=EXCLUDED.endpoint,
			doc_prefix=EXCLUDED.doc_prefix,
			doc_running_format=EXCLUDED.doc_running_format,
			passbook_code=EXCLUDED.passbook_code,
			passbook_name=EXCLUDED.passbook_name,
			bank_code=EXCLUDED.bank_code,
			bank_branch=EXCLUDED.bank_branch,
			expense_code=EXCLUDED.expense_code,
			expense_name=EXCLUDED.expense_name,
			updated_by=EXCLUDED.updated_by,
			updated_at=NOW()`,
		req.DocFormatCode, req.PassbookCode, req.PassbookName, req.BankCode, req.BankBranch,
		req.ExpenseCode, req.ExpenseName, userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกค่าเริ่มต้นรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	_, err = tx.ExecContext(c.Request.Context(), `
		INSERT INTO shopee_settlement_defaults (
			id, doc_format_code, passbook_code, passbook_name, bank_code, bank_branch,
			expense_code, expense_name, updated_by, updated_at
		) VALUES (1,$1,$2,$3,$4,$5,$6,$7,$8,NOW())
		ON CONFLICT (id) DO UPDATE SET
			doc_format_code=EXCLUDED.doc_format_code,
			passbook_code=EXCLUDED.passbook_code,
			passbook_name=EXCLUDED.passbook_name,
			bank_code=EXCLUDED.bank_code,
			bank_branch=EXCLUDED.bank_branch,
			expense_code=EXCLUDED.expense_code,
			expense_name=EXCLUDED.expense_name,
			updated_by=EXCLUDED.updated_by,
			updated_at=NOW()`,
		req.DocFormatCode, req.PassbookCode, req.PassbookName, req.BankCode, req.BankBranch,
		req.ExpenseCode, req.ExpenseName, userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกค่าเริ่มต้นรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกค่าเริ่มต้นรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action: "shopee_settlement_defaults_updated",
		UserID: stringPtrIfNotEmpty(c.GetString("user_id")),
		Source: "shopee_settlement",
		Detail: map[string]interface{}{
			"doc_format_code": req.DocFormatCode,
			"passbook_code":   req.PassbookCode,
			"passbook_name":   req.PassbookName,
			"expense_code":    req.ExpenseCode,
			"expense_name":    req.ExpenseName,
		},
	})
	c.JSON(http.StatusOK, gin.H{"data": req})
}

// POST /api/shopee-settlements/preview
func (h *ShopeeImportHandler) CreateSettlementPreview(c *gin.Context) {
	var req shopeeSettlementPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload ไม่ถูกต้อง"})
		return
	}
	from, to, err := parseShopeeSettlementReleaseRange(req.ReleaseTimeFrom, req.ReleaseTimeTo)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	userID := stringPtrIfNotEmpty(c.GetString("user_id"))
	conn, err := h.ensureShopeeAPIAccessToken(c.Request.Context(), req.ConnectionID)
	if err != nil {
		msg := humanizeSettlementError(err)
		h.auditSettlementRun(c.Request.Context(), "shopee_settlement_preview_failed", "", userID, "error", map[string]interface{}{
			"connection_id":     strings.TrimSpace(req.ConnectionID),
			"release_date_from": from.Format("2006-01-02"),
			"release_date_to":   to.Format("2006-01-02"),
			"error":             msg,
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": msg})
		return
	}
	runID, err := h.createSettlementRun(c.Request.Context(), conn, from, to, c.GetString("user_id"), c.GetString("user_email"))
	if err != nil {
		msg := "สร้างงานดึงรับชำระ Shopee ไม่สำเร็จ"
		h.auditSettlementRun(c.Request.Context(), "shopee_settlement_preview_failed", "", userID, "error", map[string]interface{}{
			"shop_id":           conn.ShopID,
			"shop_label":        conn.DisplayLabel(),
			"release_date_from": from.Format("2006-01-02"),
			"release_date_to":   to.Format("2006-01-02"),
			"error":             msg,
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "สร้างงานดึงรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	h.auditSettlementRun(c.Request.Context(), "shopee_settlement_preview_started", runID, userID, "info", nil)
	go h.runSettlementPreview(runID, conn.ID, from, to, req.PageSize, userID)
	run, _ := h.loadSettlementRun(c.Request.Context(), runID)
	c.JSON(http.StatusAccepted, gin.H{"run_id": runID, "run": run})
}

// GET /api/shopee-settlements
func (h *ShopeeImportHandler) ListSettlementRuns(c *gin.Context) {
	filters := parseSettlementListFilters(c)
	result, err := h.listSettlementRuns(c.Request.Context(), filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดประวัติงานรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":     result.Runs,
		"total":    result.Total,
		"page":     result.Page,
		"per_page": result.PerPage,
		"has_more": result.Page*result.PerPage < result.Total,
	})
}

// GET /api/shopee-settlements/counts
func (h *ShopeeImportHandler) SettlementRunCounts(c *gin.Context) {
	filters := parseSettlementListFilters(c)
	filters.Status = ""
	if err := h.refreshStaleSettlementRunStatuses(c.Request.Context()); err != nil && h.logger != nil {
		h.logger.Warn("shopee_settlement_refresh_stale_counts_failed", zap.Error(err))
	}
	counts, err := h.countSettlementRuns(c.Request.Context(), filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดจำนวนงานรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, counts)
}

// GET /api/shopee-settlements/:id
func (h *ShopeeImportHandler) GetSettlementRun(c *gin.Context) {
	run, err := h.loadSettlementRun(c.Request.Context(), c.Param("id"))
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบงานรับชำระ Shopee"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดงานรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": run})
}

// POST /api/shopee-settlements/:id/reconcile
func (h *ShopeeImportHandler) ReconcileSettlementRun(c *gin.Context) {
	runID := c.Param("id")
	result, err := h.reconcileSettlementRun(c.Request.Context(), runID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": humanizeSettlementError(err)})
		return
	}
	run, err := h.loadSettlementRun(c.Request.Context(), runID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบงานรับชำระ Shopee"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดงานรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	h.auditSettlementRun(c.Request.Context(), "shopee_settlement_reconciled", runID, stringPtrIfNotEmpty(c.GetString("user_id")), "info", map[string]interface{}{
		"newly_blocked":   result.NewlyBlocked,
		"new_status":      result.NewStatus,
		"blocked_doc_nos": result.BlockedDocNos,
		"message":         settlementReconcileAuditMessage(result),
	})
	h.notifySettlementLine(c.Request.Context(), runID, "reconcile")
	c.JSON(http.StatusOK, gin.H{"data": run, "reconcile": result})
}

// POST /api/shopee-settlements/:id/send
func (h *ShopeeImportHandler) SendSettlementRun(c *gin.Context) {
	runID := c.Param("id")
	userID := stringPtrIfNotEmpty(c.GetString("user_id"))
	var req shopeeSettlementSendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload ไม่ถูกต้อง"})
		return
	}
	req = normalizeSettlementSendRequest(req)
	defaults, err := h.loadSettlementDefaults(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดค่าเริ่มต้นรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	req = applySettlementDefaults(req, defaults)
	if req.DocFormatCode == "" || req.PassbookCode == "" {
		msg := "กรุณาตั้งค่ารูปแบบเอกสารรับชำระและบัญชีรับเงินที่หน้าเส้นทางเอกสาร SML"
		h.auditSettlementRun(c.Request.Context(), "shopee_settlement_send_blocked", runID, userID, "warn", map[string]interface{}{"message": msg})
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	if req.DocDate == "" {
		req.DocDate = time.Now().Format("2006-01-02")
	}
	if req.DocTime == "" {
		req.DocTime = time.Now().Format("15:04")
	}
	reconcileResult, err := h.reconcileSettlementRun(c.Request.Context(), runID)
	if err != nil {
		msg := humanizeSettlementError(err)
		h.auditSettlementRun(c.Request.Context(), "shopee_settlement_send_blocked", runID, userID, "warn", map[string]interface{}{"message": msg})
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	if reconcileResult.ReadyCount == 0 {
		msg := "รายการในรอบนี้ถูกส่งรับชำระแล้ว กรุณารีเฟรชผล"
		h.auditSettlementRun(c.Request.Context(), "shopee_settlement_send_blocked", runID, userID, "warn", map[string]interface{}{
			"message":         msg,
			"newly_blocked":   reconcileResult.NewlyBlocked,
			"blocked_doc_nos": reconcileResult.BlockedDocNos,
		})
		c.JSON(http.StatusConflict, gin.H{"error": msg})
		return
	}
	if err := h.prepareSettlementSend(c.Request.Context(), runID, req); err != nil {
		msg := humanizeSettlementError(err)
		h.auditSettlementRun(c.Request.Context(), "shopee_settlement_send_blocked", runID, userID, "warn", map[string]interface{}{
			"message":         msg,
			"doc_format_code": req.DocFormatCode,
			"passbook_code":   req.PassbookCode,
			"expense_code":    req.ExpenseCode,
		})
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	go h.runSettlementSend(runID, req, userID)
	run, _ := h.loadSettlementRun(c.Request.Context(), runID)
	c.JSON(http.StatusAccepted, gin.H{"run_id": runID, "run": run})
}

type shopeeSettlementHideRequest struct {
	Reason string `json:"reason"`
}

// POST /api/shopee-settlements/:id/hide
func (h *ShopeeImportHandler) HideSettlementRun(c *gin.Context) {
	runID := c.Param("id")
	run, err := h.loadSettlementRun(c.Request.Context(), runID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบงานรับชำระ Shopee"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดงานรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	if err := canHideSettlementRun(run); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req shopeeSettlementHideRequest
	_ = c.ShouldBindJSON(&req)
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = settlementDefaultHideReason(run)
	}
	userID := stringPtrIfNotEmpty(c.GetString("user_id"))
	_, err = h.sqlDB().ExecContext(c.Request.Context(), `
		UPDATE shopee_settlement_runs
		   SET hidden_at=COALESCE(hidden_at,NOW()),
		       hidden_by=$2,
		       hidden_reason=$3,
		       updated_at=NOW()
		 WHERE id=$1::uuid`,
		runID, userID, reason,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ซ่อนงานรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	h.auditSettlementRun(c.Request.Context(), "shopee_settlement_hidden", runID, userID, "info", map[string]interface{}{
		"hidden_reason": reason,
		"message":       "ซ่อนงานรับชำระ Shopee จากรายการปกติ",
	})
	run, _ = h.loadSettlementRun(c.Request.Context(), runID)
	c.JSON(http.StatusOK, gin.H{"data": run})
}

// POST /api/shopee-settlements/:id/restore
func (h *ShopeeImportHandler) RestoreSettlementRun(c *gin.Context) {
	runID := c.Param("id")
	if _, err := h.loadSettlementRun(c.Request.Context(), runID); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบงานรับชำระ Shopee"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดงานรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	_, err := h.sqlDB().ExecContext(c.Request.Context(), `
		UPDATE shopee_settlement_runs
		   SET hidden_at=NULL,
		       hidden_by=NULL,
		       hidden_reason='',
		       updated_at=NOW()
		 WHERE id=$1::uuid`, runID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "กู้คืนงานรับชำระ Shopee ไม่สำเร็จ"})
		return
	}
	userID := stringPtrIfNotEmpty(c.GetString("user_id"))
	h.auditSettlementRun(c.Request.Context(), "shopee_settlement_restored", runID, userID, "info", map[string]interface{}{
		"message": "กู้คืนงานรับชำระ Shopee กลับมาแสดงในรายการปกติ",
	})
	run, _ := h.loadSettlementRun(c.Request.Context(), runID)
	c.JSON(http.StatusOK, gin.H{"data": run})
}

func (h *ShopeeImportHandler) sqlDB() *sql.DB {
	if h.db != nil {
		return h.db
	}
	if h.billRepo != nil {
		return h.billRepo.DB()
	}
	return nil
}

func (h *ShopeeImportHandler) createSettlementRun(ctx context.Context, conn *ShopeeAPIConnection, from, to time.Time, userID, userEmail string) (string, error) {
	var id string
	var uid any
	if strings.TrimSpace(userID) != "" {
		uid = strings.TrimSpace(userID)
	}
	err := h.sqlDB().QueryRowContext(ctx, `
		INSERT INTO shopee_settlement_runs (
			connection_id, shop_id, shop_label, release_time_from, release_time_to,
			status, created_by, created_by_email, started_at, updated_at
		) VALUES ($1::uuid,$2,$3,$4,$5,'running',$6,$7,NOW(),NOW())
		RETURNING id::text`,
		conn.ID, conn.ShopID, conn.DisplayLabel(), from, to, uid, strings.TrimSpace(userEmail),
	).Scan(&id)
	return id, err
}

func (h *ShopeeImportHandler) runSettlementPreview(runID, connectionID string, from, to time.Time, pageSize int, userID *string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn, err := h.ensureShopeeAPIAccessToken(ctx, connectionID)
	if err != nil {
		h.failSettlementPreviewRun(ctx, runID, userID, humanizeSettlementError(err))
		return
	}
	items, err := h.fetchSettlementEscrows(ctx, conn, from, to, pageSize)
	if err != nil {
		h.failSettlementPreviewRun(ctx, runID, userID, humanizeSettlementError(err))
		return
	}
	if len(items) == 0 {
		_, _ = h.sqlDB().ExecContext(ctx, `UPDATE shopee_settlement_runs SET status='partial', total_count=0, ready_count=0, blocked_count=0, finished_at=NOW(), updated_at=NOW() WHERE id=$1::uuid`, runID)
		h.auditSettlementRun(ctx, "shopee_settlement_preview_completed", runID, userID, "info", map[string]interface{}{"message": "ไม่พบรายการ Shopee ที่ release เงินในช่วงนี้"})
		return
	}
	orderSNs := make([]string, 0, len(items))
	for _, item := range items {
		orderSNs = append(orderSNs, item.OrderSN)
	}
	candidates, err := h.fetchSettlementCandidates(ctx, orderSNs)
	if err != nil {
		h.failSettlementPreviewRun(ctx, runID, userID, humanizeSettlementError(err))
		return
	}
	if err := h.insertSettlementItems(ctx, runID, conn.ShopID, items, candidates); err != nil {
		h.failSettlementPreviewRun(ctx, runID, userID, "บันทึกผล preview รับชำระ Shopee ไม่สำเร็จ")
		if h.logger != nil {
			h.logger.Warn("shopee_settlement_preview_insert_failed", zap.String("run_id", runID), zap.Error(err))
		}
		return
	}
	h.auditSettlementRun(ctx, "shopee_settlement_preview_completed", runID, userID, "info", nil)
	h.notifySettlementLine(ctx, runID, "preview")
}

type settlementEscrow struct {
	OrderSN           string
	PayoutAmount      float64
	EscrowReleaseTime time.Time
	EscrowAmount      float64
	BuyerTotalAmount  float64
	Raw               json.RawMessage
}

func (h *ShopeeImportHandler) fetchSettlementEscrows(ctx context.Context, conn *ShopeeAPIConnection, from, to time.Time, pageSize int) ([]settlementEscrow, error) {
	client := h.shopeeAPIClient()
	if pageSize <= 0 || pageSize > 100 {
		pageSize = shopeeAPIDefaultPageSize
	}
	out := make([]settlementEscrow, 0)
	for page := 1; ; page++ {
		list, err := client.GetEscrowList(ctx, conn.AccessToken, conn.ShopID, shopeeapi.EscrowListRequest{
			ReleaseTimeFrom: from.Unix(),
			ReleaseTimeTo:   to.Unix(),
			PageNo:          page,
			PageSize:        pageSize,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range list.Response.EscrowList {
			if strings.TrimSpace(row.OrderSN) == "" {
				continue
			}
			out = append(out, settlementEscrow{
				OrderSN:           strings.TrimSpace(row.OrderSN),
				PayoutAmount:      roundSettlement(row.PayoutAmount),
				EscrowReleaseTime: time.Unix(row.EscrowReleaseTime, 0),
			})
		}
		if !list.Response.More || len(out) >= shopeeSettlementMaxEscrows {
			break
		}
	}
	if len(out) == 0 {
		return out, nil
	}

	type detailResult struct {
		index int
		resp  *shopeeapi.EscrowDetailResponse
		err   error
		raw   json.RawMessage
	}
	jobs := make(chan int)
	results := make(chan detailResult, len(out))
	workers := 4
	if len(out) < workers {
		workers = len(out)
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				resp, err := client.GetEscrowDetail(ctx, conn.AccessToken, conn.ShopID, out[idx].OrderSN)
				var raw json.RawMessage
				if resp != nil {
					raw, _ = json.Marshal(resp.Response)
				}
				results <- detailResult{index: idx, resp: resp, err: err, raw: raw}
			}
		}()
	}
	for i := range out {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(results)
	detailFailures := 0
	for res := range results {
		if res.err != nil {
			detailFailures++
			if h.logger != nil {
				h.logger.Warn(
					"shopee_settlement_escrow_detail_failed",
					zap.String("order_sn", out[res.index].OrderSN),
					zap.Error(res.err),
				)
			}
			continue
		}
		if res.resp != nil {
			income := res.resp.Response.OrderIncome
			out[res.index].EscrowAmount = roundSettlement(income.EscrowAmount)
			out[res.index].BuyerTotalAmount = roundSettlement(income.BuyerTotalAmount)
			out[res.index].Raw = res.raw
			if out[res.index].PayoutAmount == 0 && income.EscrowAmount > 0 {
				out[res.index].PayoutAmount = roundSettlement(income.EscrowAmount)
			}
		}
	}
	if detailFailures > 0 && h.logger != nil {
		h.logger.Warn(
			"shopee_settlement_escrow_detail_partial",
			zap.Int("failed_count", detailFailures),
			zap.Int("total_count", len(out)),
		)
	}
	return out, nil
}

func (h *ShopeeImportHandler) fetchSettlementCandidates(ctx context.Context, orderSNs []string) (map[string]settlementCandidate, error) {
	payload := map[string]interface{}{"order_sns": orderSNs}
	var out struct {
		Success bool                        `json:"success"`
		Data    settlementCandidateResponse `json:"data"`
		Error   *smlProxyErrorBody          `json:"error"`
		Message string                      `json:"message"`
	}
	if err := h.callSMLAPI(ctx, http.MethodPost, "/api/v1/ar/receipt-candidates", payload, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		msg := out.Message
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		return nil, fmt.Errorf("sml receipt candidates: %s", msg)
	}
	m := make(map[string]settlementCandidate, len(out.Data.Items))
	for _, item := range out.Data.Items {
		m[item.OrderSN] = item
	}
	return m, nil
}

func (h *ShopeeImportHandler) insertSettlementItems(ctx context.Context, runID string, shopID int64, escrows []settlementEscrow, candidates map[string]settlementCandidate) error {
	tx, err := h.sqlDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, _ = tx.ExecContext(ctx, `DELETE FROM shopee_settlement_items WHERE run_id=$1::uuid`, runID)
	ready, blocked := 0, 0
	for _, escrow := range escrows {
		cand := candidates[escrow.OrderSN]
		status := "ready"
		reason := ""
		if cand.Status == "" || cand.Status == "not_found" {
			status = "blocked"
			reason = "ไม่พบใบขาย SML ที่ doc_ref ตรงกับคำสั่งซื้อ Shopee"
		} else if cand.AlreadyReceived {
			status = "blocked"
			reason = "ใบขายนี้เคยรับชำระแล้วในเอกสาร " + cand.ExistingReceiptDocNo
		} else if escrow.PayoutAmount > roundSettlement(cand.InvoiceAmount) {
			status = "blocked"
			reason = "Shopee payout มากกว่ายอดใบขาย SML รอบนี้ยังไม่รองรับ"
		} else if docNo, err := h.findSentSettlementReceipt(ctx, shopID, escrow.OrderSN); err != nil {
			return err
		} else if docNo != "" {
			status = "blocked"
			reason = "คำสั่งซื้อนี้เคยส่งรับชำระจาก Nexflow แล้วในเอกสาร " + docNo
		}
		if status == "ready" {
			ready++
		} else {
			blocked++
		}
		var releaseAt any
		if !escrow.EscrowReleaseTime.IsZero() {
			releaseAt = escrow.EscrowReleaseTime
		}
		var invoiceDate any
		if cand.InvoiceDocDate != "" {
			if d, err := time.Parse("2006-01-02", cand.InvoiceDocDate); err == nil {
				invoiceDate = d
			}
		}
		raw := escrow.Raw
		if len(raw) == 0 {
			raw = json.RawMessage(`{}`)
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO shopee_settlement_items (
				run_id, shop_id, order_sn, escrow_release_time, payout_amount, escrow_amount, buyer_total_amount,
				invoice_doc_no, invoice_doc_date, cust_code, invoice_amount, difference_amount,
				status, block_reason, existing_receipt_doc_no, raw_escrow, updated_at
			) VALUES (
				$1::uuid,$2,$3,$4,$5,$6,$7,
				$8,$9,$10,$11,$12,
				$13,$14,$15,$16,NOW()
			)`,
			runID, shopID, escrow.OrderSN, releaseAt, escrow.PayoutAmount, escrow.EscrowAmount, escrow.BuyerTotalAmount,
			cand.InvoiceDocNo, invoiceDate, cand.CustCode, cand.InvoiceAmount, roundSettlement(cand.InvoiceAmount-escrow.PayoutAmount),
			status, reason, cand.ExistingReceiptDocNo, raw,
		)
		if err != nil {
			return err
		}
	}
	status := settlementRunStatusFromCounts(ready, blocked, 0, "")
	_, err = tx.ExecContext(ctx, `
		UPDATE shopee_settlement_runs
		   SET status=$2, total_count=$3, ready_count=$4, blocked_count=$5,
		       finished_at=NOW(), updated_at=NOW()
		 WHERE id=$1::uuid`,
		runID, status, len(escrows), ready, blocked,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (h *ShopeeImportHandler) findSentSettlementReceipt(ctx context.Context, shopID int64, orderSN string) (string, error) {
	var docNo string
	err := h.sqlDB().QueryRowContext(ctx, `
		SELECT receipt_doc_no
		  FROM shopee_settlement_items
		 WHERE shop_id=$1 AND order_sn=$2 AND status='sent'
		 ORDER BY updated_at DESC
		 LIMIT 1`, shopID, orderSN).Scan(&docNo)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return strings.TrimSpace(docNo), err
}

func (h *ShopeeImportHandler) reconcileSettlementRun(ctx context.Context, runID string) (settlementReconcileResult, error) {
	var currentStatus string
	if err := h.sqlDB().QueryRowContext(ctx, `SELECT status FROM shopee_settlement_runs WHERE id=$1::uuid`, runID).Scan(&currentStatus); err != nil {
		if err == sql.ErrNoRows {
			return settlementReconcileResult{}, fmt.Errorf("ไม่พบงานรับชำระ Shopee")
		}
		return settlementReconcileResult{}, err
	}
	if currentStatus == "pending" || currentStatus == "running" || currentStatus == "sending" {
		return settlementReconcileResult{}, fmt.Errorf("งานนี้กำลังทำงานอยู่ กรุณารอให้เสร็จก่อน")
	}
	items, err := h.loadSettlementReadyItems(ctx, runID)
	if err != nil {
		return settlementReconcileResult{}, err
	}
	blockedDocNos := map[string]bool{}
	newlyBlocked := 0
	if len(items) > 0 {
		orderSNs := make([]string, 0, len(items))
		for _, item := range items {
			orderSNs = append(orderSNs, item.OrderSN)
		}
		candidates, err := h.fetchSettlementCandidates(ctx, orderSNs)
		if err != nil {
			return settlementReconcileResult{}, err
		}
		tx, err := h.sqlDB().BeginTx(ctx, nil)
		if err != nil {
			return settlementReconcileResult{}, err
		}
		defer tx.Rollback()
		for _, item := range items {
			status, reason, receiptDocNo, err := h.reconcileSettlementItemStatus(ctx, item, candidates[item.OrderSN])
			if err != nil {
				return settlementReconcileResult{}, err
			}
			if status != "blocked" {
				continue
			}
			res, err := tx.ExecContext(ctx, `
				UPDATE shopee_settlement_items
				   SET status='blocked',
				       block_reason=$2,
				       existing_receipt_doc_no=$3,
				       updated_at=NOW()
				 WHERE id=$1::uuid
				   AND status='ready'`,
				item.ID, reason, receiptDocNo,
			)
			if err != nil {
				return settlementReconcileResult{}, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				newlyBlocked++
				if receiptDocNo != "" {
					blockedDocNos[receiptDocNo] = true
				}
			}
		}
		result, err := h.refreshSettlementRunCountsTx(ctx, tx, runID)
		if err != nil {
			return settlementReconcileResult{}, err
		}
		result.NewlyBlocked = newlyBlocked
		result.BlockedDocNos = sortedSettlementDocNos(blockedDocNos)
		if err := tx.Commit(); err != nil {
			return settlementReconcileResult{}, err
		}
		return result, nil
	}
	tx, err := h.sqlDB().BeginTx(ctx, nil)
	if err != nil {
		return settlementReconcileResult{}, err
	}
	defer tx.Rollback()
	result, err := h.refreshSettlementRunCountsTx(ctx, tx, runID)
	if err != nil {
		return settlementReconcileResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return settlementReconcileResult{}, err
	}
	return result, nil
}

func (h *ShopeeImportHandler) reconcileSettlementItemStatus(ctx context.Context, item shopeeSettlementItemView, cand settlementCandidate) (status, reason, receiptDocNo string, err error) {
	if cand.Status == "" || cand.Status == "not_found" {
		return "blocked", "ไม่พบใบขาย SML ที่ doc_ref ตรงกับคำสั่งซื้อ Shopee", "", nil
	}
	if cand.AlreadyReceived {
		docNo := strings.TrimSpace(cand.ExistingReceiptDocNo)
		return "blocked", "ใบขายนี้เคยรับชำระแล้วในเอกสาร " + docNo, docNo, nil
	}
	invoiceAmount := roundSettlement(cand.InvoiceAmount)
	if invoiceAmount == 0 {
		invoiceAmount = roundSettlement(item.InvoiceAmount)
	}
	if item.PayoutAmount > invoiceAmount {
		return "blocked", "Shopee payout มากกว่ายอดใบขาย SML รอบนี้ยังไม่รองรับ", "", nil
	}
	if docNo, err := h.findSentSettlementReceipt(ctx, item.ShopID, item.OrderSN); err != nil {
		return "", "", "", err
	} else if docNo != "" {
		return "blocked", "คำสั่งซื้อนี้เคยส่งรับชำระจาก Nexflow แล้วในเอกสาร " + docNo, docNo, nil
	}
	return "ready", "", "", nil
}

func (h *ShopeeImportHandler) refreshSettlementRunCountsTx(ctx context.Context, tx *sql.Tx, runID string) (settlementReconcileResult, error) {
	var result settlementReconcileResult
	err := tx.QueryRowContext(ctx, `
		WITH counts AS (
			SELECT COUNT(*)::int AS total_count,
			       COUNT(*) FILTER (WHERE status='ready')::int AS ready_count,
			       COUNT(*) FILTER (WHERE status='blocked')::int AS blocked_count,
			       COUNT(*) FILTER (WHERE status='sent')::int AS sent_count
			  FROM shopee_settlement_items
			 WHERE run_id=$1::uuid
		)
		UPDATE shopee_settlement_runs r
		   SET total_count=counts.total_count,
		       ready_count=counts.ready_count,
		       blocked_count=counts.blocked_count,
		       sent_count=counts.sent_count,
		       status=CASE
		         WHEN r.status='sent' THEN 'sent'
		         WHEN r.status='sending' THEN 'sending'
		         WHEN r.status='failed' THEN 'failed'
		         WHEN counts.ready_count > 0 THEN 'ready'
		         WHEN counts.blocked_count > 0 THEN 'partial'
		         ELSE 'partial'
		       END,
		       updated_at=NOW()
		  FROM counts
		 WHERE r.id=$1::uuid
		 RETURNING r.total_count, r.ready_count, r.blocked_count, r.sent_count, r.status`,
		runID,
	).Scan(&result.TotalCount, &result.ReadyCount, &result.BlockedCount, &result.SentCount, &result.NewStatus)
	if err == sql.ErrNoRows {
		return result, fmt.Errorf("ไม่พบงานรับชำระ Shopee")
	}
	return result, err
}

func settlementRunStatusFromCounts(readyCount, blockedCount, sentCount int, currentStatus string) string {
	switch currentStatus {
	case "sent", "sending", "failed":
		return currentStatus
	}
	if sentCount > 0 && readyCount == 0 {
		return "sent"
	}
	if readyCount > 0 {
		return "ready"
	}
	if blockedCount > 0 {
		return "partial"
	}
	return "partial"
}

func (h *ShopeeImportHandler) blockOtherSettlementRunsAfterSend(ctx context.Context, runID string, shopID int64, orderSNs []string, receiptDocNo string) (int, error) {
	if shopID == 0 || len(orderSNs) == 0 || strings.TrimSpace(receiptDocNo) == "" {
		return 0, nil
	}
	tx, err := h.sqlDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		UPDATE shopee_settlement_items
		   SET status='blocked',
		       existing_receipt_doc_no=$4,
		       block_reason=$5,
		       updated_at=NOW()
		 WHERE shop_id=$1
		   AND run_id<>$2::uuid
		   AND status='ready'
		   AND order_sn=ANY($3)
		 RETURNING run_id::text`,
		shopID,
		runID,
		pq.Array(orderSNs),
		receiptDocNo,
		"คำสั่งซื้อนี้ถูกส่งรับชำระจาก Nexflow แล้วในเอกสาร "+receiptDocNo,
	)
	if err != nil {
		return 0, err
	}
	affectedRuns := map[string]bool{}
	affectedItems := 0
	for rows.Next() {
		var affectedRunID string
		if err := rows.Scan(&affectedRunID); err != nil {
			rows.Close()
			return 0, err
		}
		affectedRuns[affectedRunID] = true
		affectedItems++
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for affectedRunID := range affectedRuns {
		if _, err := h.refreshSettlementRunCountsTx(ctx, tx, affectedRunID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return affectedItems, nil
}

func sortedSettlementDocNos(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for v := range values {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func (h *ShopeeImportHandler) prepareSettlementSend(ctx context.Context, runID string, req shopeeSettlementSendRequest) error {
	items, err := h.loadSettlementReadyItems(ctx, runID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("ไม่มีรายการพร้อมส่งรับชำระ")
	}
	cust := ""
	needsExpense := false
	for _, item := range items {
		if item.Status != "ready" {
			continue
		}
		if cust == "" {
			cust = item.CustCode
		}
		if item.CustCode != cust {
			return fmt.Errorf("รายการที่พร้อมส่งมีลูกค้าหลายรหัส กรุณาแยกส่ง")
		}
		if item.PayoutAmount > item.InvoiceAmount {
			return fmt.Errorf("Shopee payout มากกว่ายอดใบขาย SML รอบนี้ยังไม่รองรับ")
		}
		if item.DifferenceAmount > 0 {
			needsExpense = true
		}
	}
	if needsExpense && strings.TrimSpace(req.ExpenseCode) == "" {
		return fmt.Errorf("กรุณาเลือกค่าใช้จ่าย Shopee สำหรับส่วนต่าง")
	}
	res, err := h.sqlDB().ExecContext(ctx, `
		UPDATE shopee_settlement_runs
		   SET status='sending',
		       selected_doc_format_code=$2,
		       selected_passbook_code=$3,
		       selected_passbook_name=$4,
		       selected_bank_code=$5,
		       selected_bank_branch=$6,
		       selected_expense_code=$7,
		       selected_expense_name=$8,
		       error_msg='',
		       started_at=COALESCE(started_at,NOW()),
		       updated_at=NOW()
		 WHERE id=$1::uuid
		   AND status IN ('ready','failed','partial')`,
		runID, req.DocFormatCode, req.PassbookCode, req.PassbookName, req.BankCode, req.BankBranch, req.ExpenseCode, req.ExpenseName,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("งานนี้กำลังส่งหรือส่งไปแล้ว กรุณารีเฟรชผล")
	}
	return nil
}

func (h *ShopeeImportHandler) runSettlementSend(runID string, req shopeeSettlementSendRequest, userID *string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	items, err := h.loadSettlementReadyItems(ctx, runID)
	if err != nil {
		h.failSettlementRun(ctx, runID, humanizeSettlementError(err))
		return
	}
	lines := make([]map[string]interface{}, 0, len(items))
	orderSNs := make([]string, 0, len(items))
	var shopID int64
	for _, item := range items {
		if shopID == 0 {
			shopID = item.ShopID
		}
		orderSNs = append(orderSNs, item.OrderSN)
		lines = append(lines, map[string]interface{}{
			"order_sn":       item.OrderSN,
			"invoice_doc_no": item.InvoiceDocNo,
			"payout_amount":  item.PayoutAmount,
		})
	}
	payload := map[string]interface{}{
		"doc_date":        req.DocDate,
		"doc_time":        req.DocTime,
		"doc_format_code": req.DocFormatCode,
		"passbook_code":   req.PassbookCode,
		"expense_code":    req.ExpenseCode,
		"remark":          firstNonEmpty(req.Remark, "Nexflow Shopee settlement"),
		"lines":           lines,
	}
	var out struct {
		Success bool                      `json:"success"`
		Data    settlementReceiptResponse `json:"data"`
		Error   *smlProxyErrorBody        `json:"error"`
		Message string                    `json:"message"`
	}
	if err := h.callSMLAPI(ctx, http.MethodPost, "/api/v1/ar/receipts", payload, &out); err != nil {
		h.failSettlementRun(ctx, runID, humanizeSettlementError(err))
		return
	}
	if !out.Success {
		msg := out.Message
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		h.failSettlementRun(ctx, runID, humanizeSettlementError(fmt.Errorf("%s", msg)))
		return
	}
	docNo := out.Data.DocNo
	tx, err := h.sqlDB().BeginTx(ctx, nil)
	if err != nil {
		h.failSettlementRun(ctx, runID, "บันทึกผลส่งรับชำระไม่สำเร็จ")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE shopee_settlement_items SET status='sent', receipt_doc_no=$2, updated_at=NOW() WHERE run_id=$1::uuid AND status='ready'`, runID, docNo); err != nil {
		h.failSettlementRun(ctx, runID, "บันทึกผลรายการรับชำระไม่สำเร็จ")
		return
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE shopee_settlement_runs
		   SET status='sent', rc_doc_no=$2, sent_count=ready_count, error_msg='', finished_at=NOW(), updated_at=NOW()
		 WHERE id=$1::uuid`, runID, docNo); err != nil {
		h.failSettlementRun(ctx, runID, "บันทึกผลเอกสารรับชำระไม่สำเร็จ")
		return
	}
	if err := tx.Commit(); err != nil {
		h.failSettlementRun(ctx, runID, "บันทึกผลเอกสารรับชำระไม่สำเร็จ")
		return
	}
	blockedAfterReconcile, blockErr := h.blockOtherSettlementRunsAfterSend(ctx, runID, shopID, orderSNs, docNo)
	if blockErr != nil && h.logger != nil {
		h.logger.Warn("shopee_settlement_reconcile_other_runs_failed",
			zap.String("run_id", runID),
			zap.String("doc_no", docNo),
			zap.Error(blockErr),
		)
	}
	h.auditSettlementRun(ctx, "shopee_settlement_sent", runID, userID, "info", map[string]interface{}{
		"rc_doc_no":                     docNo,
		"sent_count":                    len(items),
		"blocked_after_reconcile_count": blockedAfterReconcile,
		"doc_format_code":               req.DocFormatCode,
		"passbook_code":                 req.PassbookCode,
		"passbook_name":                 req.PassbookName,
		"expense_code":                  req.ExpenseCode,
		"expense_name":                  req.ExpenseName,
	})
}

const (
	settlementRunDefaultPage    = 1
	settlementRunDefaultPerPage = 20
	settlementRunMaxPerPage     = 100
)

type settlementListFilters struct {
	Status   string
	ShopID   string
	Search   string
	DateFrom string
	DateTo   string
	Hidden   string
	Page     int
	PerPage  int
}

func parseSettlementListFilters(c *gin.Context) settlementListFilters {
	page := settlementRunDefaultPage
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			page = n
		}
	}
	perPage := settlementRunDefaultPerPage
	if raw := strings.TrimSpace(firstNonEmpty(c.Query("per_page"), c.Query("limit"))); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			perPage = n
		}
	}
	if perPage > settlementRunMaxPerPage {
		perPage = settlementRunMaxPerPage
	}
	return settlementListFilters{
		Status:   strings.TrimSpace(c.Query("status")),
		ShopID:   strings.TrimSpace(c.Query("shop_id")),
		Search:   strings.TrimSpace(c.Query("search")),
		DateFrom: strings.TrimSpace(c.Query("date_from")),
		DateTo:   strings.TrimSpace(c.Query("date_to")),
		Hidden:   strings.TrimSpace(c.Query("hidden")),
		Page:     page,
		PerPage:  perPage,
	}
}

func (f settlementListFilters) offset() int {
	if f.Page <= 1 {
		return 0
	}
	return (f.Page - 1) * f.PerPage
}

func appendSettlementRunFilters(sb *strings.Builder, args *[]any, filters settlementListFilters, includeStatus bool) {
	switch filters.Hidden {
	case "all":
		// Include both visible and hidden runs.
	case "only":
		sb.WriteString(" AND r.hidden_at IS NOT NULL")
	default:
		sb.WriteString(" AND r.hidden_at IS NULL")
	}
	if includeStatus && filters.Status != "" && filters.Status != "all" {
		*args = append(*args, filters.Status)
		fmt.Fprintf(sb, " AND r.status=$%d", len(*args))
	}
	if filters.ShopID != "" && filters.ShopID != "all" {
		if shopID, err := strconv.ParseInt(filters.ShopID, 10, 64); err == nil {
			*args = append(*args, shopID)
			fmt.Fprintf(sb, " AND r.shop_id=$%d", len(*args))
		}
	}
	if filters.DateFrom != "" {
		if from, err := time.Parse("2006-01-02", filters.DateFrom); err == nil {
			*args = append(*args, from)
			fmt.Fprintf(sb, " AND r.release_time_from >= $%d", len(*args))
		}
	}
	if filters.DateTo != "" {
		if to, err := time.Parse("2006-01-02", filters.DateTo); err == nil {
			*args = append(*args, to.AddDate(0, 0, 1))
			fmt.Fprintf(sb, " AND r.release_time_to < $%d", len(*args))
		}
	}
	if filters.Search != "" {
		*args = append(*args, "%"+filters.Search+"%")
		fmt.Fprintf(sb, ` AND (
			r.shop_label ILIKE $%d OR r.rc_doc_no ILIKE $%d OR EXISTS (
				SELECT 1 FROM shopee_settlement_items si
				 WHERE si.run_id = r.id
				   AND (si.order_sn ILIKE $%d OR si.invoice_doc_no ILIKE $%d OR si.receipt_doc_no ILIKE $%d)
			)
		)`, len(*args), len(*args), len(*args), len(*args), len(*args))
	}
}

type settlementRunListResult struct {
	Runs    []shopeeSettlementRunView
	Total   int
	Page    int
	PerPage int
}

func (h *ShopeeImportHandler) listSettlementRuns(ctx context.Context, filters settlementListFilters) (settlementRunListResult, error) {
	if err := h.refreshStaleSettlementRunStatuses(ctx); err != nil && h.logger != nil {
		h.logger.Warn("shopee_settlement_refresh_stale_status_failed", zap.Error(err))
	}
	result := settlementRunListResult{
		Page:    filters.Page,
		PerPage: filters.PerPage,
		Runs:    make([]shopeeSettlementRunView, 0),
	}
	total, err := h.countSettlementRunsTotal(ctx, filters)
	if err != nil {
		return result, err
	}
	result.Total = total

	var sb strings.Builder
	args := make([]any, 0, 8)
	sb.WriteString(`
		SELECT r.id::text, COALESCE(r.connection_id::text,''), r.shop_id, r.shop_label,
		       r.release_time_from, r.release_time_to, r.status, r.total_count, r.ready_count, r.blocked_count, r.sent_count,
		       COALESCE(SUM(i.invoice_amount),0), COALESCE(SUM(i.payout_amount),0), COALESCE(SUM(GREATEST(i.difference_amount,0)),0),
		       COALESCE(SUM(i.invoice_amount) FILTER (WHERE i.status IN ('ready','sent')),0),
		       COALESCE(SUM(i.payout_amount) FILTER (WHERE i.status IN ('ready','sent')),0),
		       COALESCE(SUM(GREATEST(i.difference_amount,0)) FILTER (WHERE i.status IN ('ready','sent')),0),
		       COALESCE(SUM(i.invoice_amount) FILTER (WHERE i.status='blocked'),0),
		       COALESCE(SUM(i.payout_amount) FILTER (WHERE i.status='blocked'),0),
		       COALESCE(SUM(GREATEST(i.difference_amount,0)) FILTER (WHERE i.status='blocked'),0),
		       r.rc_doc_no, r.error_msg, r.selected_doc_format_code, r.selected_passbook_code, r.selected_passbook_name,
		       r.selected_bank_code, r.selected_bank_branch, r.selected_expense_code, r.selected_expense_name,
		       r.started_at, r.finished_at, r.created_at, r.updated_at, r.hidden_at, COALESCE(r.hidden_by::text,''), r.hidden_reason
		  FROM shopee_settlement_runs r
		  LEFT JOIN shopee_settlement_items i ON i.run_id = r.id
		 WHERE 1=1`)
	appendSettlementRunFilters(&sb, &args, filters, true)
	sb.WriteString(`
		 GROUP BY r.id
		 ORDER BY r.created_at DESC
		 LIMIT $`)
	args = append(args, filters.PerPage)
	sb.WriteString(strconv.Itoa(len(args)))
	sb.WriteString(" OFFSET $")
	args = append(args, filters.offset())
	sb.WriteString(strconv.Itoa(len(args)))

	rows, err := h.sqlDB().QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		run, err := scanSettlementRunSummary(rows)
		if err != nil {
			return result, err
		}
		result.Runs = append(result.Runs, run)
	}
	return result, rows.Err()
}

func (h *ShopeeImportHandler) countSettlementRunsTotal(ctx context.Context, filters settlementListFilters) (int, error) {
	var sb strings.Builder
	args := make([]any, 0, 8)
	sb.WriteString(`
		SELECT COUNT(*)
		  FROM shopee_settlement_runs r
		 WHERE 1=1`)
	appendSettlementRunFilters(&sb, &args, filters, true)
	var total int
	if err := h.sqlDB().QueryRowContext(ctx, sb.String(), args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (h *ShopeeImportHandler) refreshStaleSettlementRunStatuses(ctx context.Context) error {
	_, err := h.sqlDB().ExecContext(ctx, `
		WITH counts AS (
			SELECT run_id,
			       COUNT(*)::int AS total_count,
			       COUNT(*) FILTER (WHERE status='ready')::int AS ready_count,
			       COUNT(*) FILTER (WHERE status='blocked')::int AS blocked_count,
			       COUNT(*) FILTER (WHERE status='sent')::int AS sent_count
			  FROM shopee_settlement_items
			 GROUP BY run_id
		), stale AS (
			SELECT r.id,
			       COALESCE(counts.total_count,0)::int AS total_count,
			       COALESCE(counts.ready_count,0)::int AS ready_count,
			       COALESCE(counts.blocked_count,0)::int AS blocked_count,
			       COALESCE(counts.sent_count,0)::int AS sent_count
			  FROM shopee_settlement_runs r
			  LEFT JOIN counts ON counts.run_id = r.id
			 WHERE r.status='ready'
			   AND COALESCE(counts.ready_count,0)=0
		)
		UPDATE shopee_settlement_runs r
		   SET total_count=stale.total_count,
		       ready_count=stale.ready_count,
		       blocked_count=stale.blocked_count,
		       sent_count=stale.sent_count,
		       status=CASE
		         WHEN stale.sent_count > 0 THEN 'sent'
		         WHEN stale.blocked_count > 0 THEN 'partial'
		         ELSE 'partial'
		       END,
		       updated_at=NOW()
		  FROM stale
		 WHERE r.id=stale.id`)
	return err
}

type settlementRunCounts struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Ready   int `json:"ready"`
	Sending int `json:"sending"`
	Sent    int `json:"sent"`
	Failed  int `json:"failed"`
	Partial int `json:"partial"`
}

func (h *ShopeeImportHandler) countSettlementRuns(ctx context.Context, filters settlementListFilters) (settlementRunCounts, error) {
	var sb strings.Builder
	args := make([]any, 0, 8)
	sb.WriteString(`
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE status IN ('pending','running')),
		       COUNT(*) FILTER (WHERE status='ready'),
		       COUNT(*) FILTER (WHERE status='sending'),
		       COUNT(*) FILTER (WHERE status='sent'),
		       COUNT(*) FILTER (WHERE status='failed'),
		       COUNT(*) FILTER (WHERE status='partial')
		  FROM shopee_settlement_runs r
		 WHERE 1=1`)
	appendSettlementRunFilters(&sb, &args, filters, false)
	var counts settlementRunCounts
	err := h.sqlDB().QueryRowContext(ctx, sb.String(), args...).Scan(
		&counts.Total, &counts.Running, &counts.Ready, &counts.Sending,
		&counts.Sent, &counts.Failed, &counts.Partial,
	)
	return counts, err
}

type settlementRunScanner interface {
	Scan(dest ...any) error
}

func scanSettlementRunSummary(s settlementRunScanner) (shopeeSettlementRunView, error) {
	var run shopeeSettlementRunView
	var connectionID sql.NullString
	var releaseFrom, releaseTo time.Time
	var startedAt, finishedAt sql.NullTime
	var hiddenAt sql.NullTime
	var createdAt, updatedAt time.Time
	err := s.Scan(&run.ID, &connectionID, &run.ShopID, &run.ShopLabel,
		&releaseFrom, &releaseTo, &run.Status, &run.TotalCount, &run.ReadyCount, &run.BlockedCount, &run.SentCount,
		&run.InvoiceAmountTotal, &run.PayoutAmountTotal, &run.DifferenceAmountTotal,
		&run.ReadyInvoiceAmount, &run.ReadyPayoutAmount, &run.ReadyDifferenceAmount,
		&run.BlockedInvoiceAmount, &run.BlockedPayoutAmount, &run.BlockedDifferenceAmount,
		&run.RCDocNo, &run.ErrorMsg, &run.SelectedDocFormatCode, &run.SelectedPassbookCode, &run.SelectedPassbookName,
		&run.SelectedBankCode, &run.SelectedBankBranch, &run.SelectedExpenseCode, &run.SelectedExpenseName,
		&startedAt, &finishedAt, &createdAt, &updatedAt, &hiddenAt, &run.HiddenBy, &run.HiddenReason)
	if err != nil {
		return run, err
	}
	if connectionID.Valid {
		run.ConnectionID = connectionID.String
	}
	run.ReleaseTimeFrom = releaseFrom.Format(time.RFC3339)
	run.ReleaseTimeTo = releaseTo.Format(time.RFC3339)
	run.ReleaseDateFrom = releaseFrom.Format("2006-01-02")
	run.ReleaseDateTo = releaseTo.Format("2006-01-02")
	run.Items = []shopeeSettlementItemView{}
	if startedAt.Valid {
		run.StartedAt = startedAt.Time.Format(time.RFC3339)
	}
	if finishedAt.Valid {
		run.FinishedAt = finishedAt.Time.Format(time.RFC3339)
	}
	if hiddenAt.Valid {
		run.HiddenAt = hiddenAt.Time.Format(time.RFC3339)
	}
	run.CreatedAt = createdAt.Format(time.RFC3339)
	run.UpdatedAt = updatedAt.Format(time.RFC3339)
	return run, nil
}

func (h *ShopeeImportHandler) loadSettlementReadyItems(ctx context.Context, runID string) ([]shopeeSettlementItemView, error) {
	rows, err := h.sqlDB().QueryContext(ctx, `
		SELECT id::text, shop_id, order_sn, COALESCE(payout_amount,0), COALESCE(invoice_doc_no,''), COALESCE(cust_code,''),
		       COALESCE(invoice_amount,0), COALESCE(difference_amount,0), status
		  FROM shopee_settlement_items
		 WHERE run_id=$1::uuid AND status='ready'
		 ORDER BY escrow_release_time NULLS LAST, order_sn`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []shopeeSettlementItemView
	for rows.Next() {
		var item shopeeSettlementItemView
		if err := rows.Scan(&item.ID, &item.ShopID, &item.OrderSN, &item.PayoutAmount, &item.InvoiceDocNo, &item.CustCode, &item.InvoiceAmount, &item.DifferenceAmount, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *ShopeeImportHandler) loadSettlementRun(ctx context.Context, runID string) (*shopeeSettlementRunView, error) {
	var run shopeeSettlementRunView
	var connectionID sql.NullString
	var releaseFrom, releaseTo time.Time
	var startedAt, finishedAt sql.NullTime
	var hiddenAt sql.NullTime
	var createdAt, updatedAt time.Time
	err := h.sqlDB().QueryRowContext(ctx, `
		SELECT id::text, COALESCE(connection_id::text,''), shop_id, shop_label,
		       release_time_from, release_time_to, status, total_count, ready_count, blocked_count, sent_count,
		       rc_doc_no, error_msg, selected_doc_format_code, selected_passbook_code, selected_passbook_name,
		       selected_bank_code, selected_bank_branch, selected_expense_code, selected_expense_name,
		       started_at, finished_at, created_at, updated_at, hidden_at, COALESCE(hidden_by::text,''), hidden_reason
		  FROM shopee_settlement_runs
		 WHERE id=$1::uuid`, runID).
		Scan(&run.ID, &connectionID, &run.ShopID, &run.ShopLabel,
			&releaseFrom, &releaseTo, &run.Status, &run.TotalCount, &run.ReadyCount, &run.BlockedCount, &run.SentCount,
			&run.RCDocNo, &run.ErrorMsg, &run.SelectedDocFormatCode, &run.SelectedPassbookCode, &run.SelectedPassbookName,
			&run.SelectedBankCode, &run.SelectedBankBranch, &run.SelectedExpenseCode, &run.SelectedExpenseName,
			&startedAt, &finishedAt, &createdAt, &updatedAt, &hiddenAt, &run.HiddenBy, &run.HiddenReason)
	if err != nil {
		return nil, err
	}
	if connectionID.Valid {
		run.ConnectionID = connectionID.String
	}
	run.ReleaseTimeFrom = releaseFrom.Format(time.RFC3339)
	run.ReleaseTimeTo = releaseTo.Format(time.RFC3339)
	run.ReleaseDateFrom = releaseFrom.Format("2006-01-02")
	run.ReleaseDateTo = releaseTo.Format("2006-01-02")
	run.Items = []shopeeSettlementItemView{}
	if startedAt.Valid {
		run.StartedAt = startedAt.Time.Format(time.RFC3339)
	}
	if finishedAt.Valid {
		run.FinishedAt = finishedAt.Time.Format(time.RFC3339)
	}
	if hiddenAt.Valid {
		run.HiddenAt = hiddenAt.Time.Format(time.RFC3339)
	}
	run.CreatedAt = createdAt.Format(time.RFC3339)
	run.UpdatedAt = updatedAt.Format(time.RFC3339)

	rows, err := h.sqlDB().QueryContext(ctx, `
		SELECT id::text, order_sn, escrow_release_time, COALESCE(payout_amount,0), COALESCE(escrow_amount,0), COALESCE(buyer_total_amount,0),
		       invoice_doc_no, invoice_doc_date, cust_code, COALESCE(invoice_amount,0), COALESCE(difference_amount,0),
		       status, block_reason, receipt_doc_no, existing_receipt_doc_no
		  FROM shopee_settlement_items
		 WHERE run_id=$1::uuid
		 ORDER BY escrow_release_time NULLS LAST, order_sn`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item shopeeSettlementItemView
		var releaseAt, invoiceDate sql.NullTime
		if err := rows.Scan(&item.ID, &item.OrderSN, &releaseAt, &item.PayoutAmount, &item.EscrowAmount, &item.BuyerTotalAmount,
			&item.InvoiceDocNo, &invoiceDate, &item.CustCode, &item.InvoiceAmount, &item.DifferenceAmount,
			&item.Status, &item.BlockReason, &item.ReceiptDocNo, &item.ExistingReceiptDocNo); err != nil {
			return nil, err
		}
		if releaseAt.Valid {
			item.EscrowReleaseTime = releaseAt.Time.Format(time.RFC3339)
		}
		if invoiceDate.Valid {
			item.InvoiceDocDate = invoiceDate.Time.Format("2006-01-02")
		}
		run.InvoiceAmountTotal = roundSettlement(run.InvoiceAmountTotal + item.InvoiceAmount)
		run.PayoutAmountTotal = roundSettlement(run.PayoutAmountTotal + item.PayoutAmount)
		if item.DifferenceAmount > 0 {
			run.DifferenceAmountTotal = roundSettlement(run.DifferenceAmountTotal + item.DifferenceAmount)
		}
		if item.Status == "ready" || item.Status == "sent" {
			run.ReadyInvoiceAmount = roundSettlement(run.ReadyInvoiceAmount + item.InvoiceAmount)
			run.ReadyPayoutAmount = roundSettlement(run.ReadyPayoutAmount + item.PayoutAmount)
			if item.DifferenceAmount > 0 {
				run.ReadyDifferenceAmount = roundSettlement(run.ReadyDifferenceAmount + item.DifferenceAmount)
			}
		}
		if item.Status == "blocked" {
			run.BlockedInvoiceAmount = roundSettlement(run.BlockedInvoiceAmount + item.InvoiceAmount)
			run.BlockedPayoutAmount = roundSettlement(run.BlockedPayoutAmount + item.PayoutAmount)
			if item.DifferenceAmount > 0 {
				run.BlockedDifferenceAmount = roundSettlement(run.BlockedDifferenceAmount + item.DifferenceAmount)
			}
		}
		run.Items = append(run.Items, item)
	}
	return &run, rows.Err()
}

func (h *ShopeeImportHandler) notifySettlementLine(ctx context.Context, runID, reason string) {
	if h == nil || h.settlementLineNotifier == nil || h.cfg == nil || !h.cfg.ShopeeSettlementLineAlertsEnabled {
		return
	}
	run, err := h.loadSettlementLineRun(ctx, runID)
	if err != nil {
		if h.logger != nil {
			h.logger.Warn("shopee_settlement_line_load_failed",
				zap.String("run_id", runID),
				zap.String("reason", reason),
				zap.Error(err),
			)
		}
		return
	}
	if run.TotalCount <= 0 {
		return
	}
	key := "shopee:settlement:" + strings.TrimSpace(runID)
	if _, err := h.settlementLineNotifier.EnqueueShopeeSettlementReady(ctx, run, key); err != nil && h.logger != nil {
		h.logger.Warn("shopee_settlement_line_enqueue_failed",
			zap.String("run_id", runID),
			zap.String("reason", reason),
			zap.Int64("shop_id", run.ShopID),
			zap.Error(err),
		)
	}
}

func (h *ShopeeImportHandler) loadSettlementLineRun(ctx context.Context, runID string) (models.ShopeeSettlementLineRun, error) {
	var run models.ShopeeSettlementLineRun
	var releaseFrom, releaseTo time.Time
	if err := h.sqlDB().QueryRowContext(ctx, `
		SELECT id::text, shop_id, shop_label, release_time_from, release_time_to,
		       status, total_count, ready_count, blocked_count, sent_count
		  FROM shopee_settlement_runs
		 WHERE id=$1::uuid`, strings.TrimSpace(runID)).
		Scan(&run.ID, &run.ShopID, &run.ShopLabel, &releaseFrom, &releaseTo,
			&run.Status, &run.TotalCount, &run.ReadyCount, &run.BlockedCount, &run.SentCount); err != nil {
		return run, err
	}
	run.ReleaseDateFrom = releaseFrom.Format("2006-01-02")
	run.ReleaseDateTo = releaseTo.Format("2006-01-02")
	rows, err := h.sqlDB().QueryContext(ctx, `
		SELECT order_sn, escrow_release_time, COALESCE(payout_amount,0), COALESCE(escrow_amount,0),
		       COALESCE(buyer_total_amount,0), COALESCE(invoice_amount,0), COALESCE(difference_amount,0),
		       status, block_reason, raw_escrow
		  FROM shopee_settlement_items
		 WHERE run_id=$1::uuid
		 ORDER BY escrow_release_time NULLS LAST, order_sn`, strings.TrimSpace(runID))
	if err != nil {
		return run, err
	}
	defer rows.Close()
	for rows.Next() {
		var item models.ShopeeSettlementLineItem
		var releaseAt sql.NullTime
		var raw []byte
		if err := rows.Scan(&item.OrderSN, &releaseAt, &item.PayoutAmount, &item.EscrowAmount,
			&item.BuyerTotalAmount, &item.InvoiceAmount, &item.DifferenceAmount,
			&item.Status, &item.BlockReason, &raw); err != nil {
			return run, err
		}
		if releaseAt.Valid {
			item.EscrowReleaseTime = releaseAt.Time.Format(time.RFC3339)
		}
		if len(raw) > 0 {
			item.RawEscrow = json.RawMessage(raw)
		} else {
			item.RawEscrow = json.RawMessage(`{}`)
		}
		if item.BuyerTotalAmount > 0 {
			item.DeductionAmount = roundSettlement(item.BuyerTotalAmount - item.PayoutAmount)
		}
		run.BuyerTotalAmountTotal = roundSettlement(run.BuyerTotalAmountTotal + item.BuyerTotalAmount)
		run.PayoutAmountTotal = roundSettlement(run.PayoutAmountTotal + item.PayoutAmount)
		run.DeductionAmountTotal = roundSettlement(run.DeductionAmountTotal + item.DeductionAmount)
		run.Items = append(run.Items, item)
	}
	return run, rows.Err()
}

func parseShopeeSettlementReleaseRange(fromRaw, toRaw string) (time.Time, time.Time, error) {
	from, to, err := parseShopeeAPIRange(fromRaw, toRaw)
	if err == nil {
		return from, to, nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "ไม่เกิน 15 วัน"):
		return time.Time{}, time.Time{}, fmt.Errorf("ช่วงวันที่ Shopee release เงินต้องไม่เกิน 15 วันต่อครั้ง")
	case strings.Contains(msg, "time_from ต้องเป็น"):
		return time.Time{}, time.Time{}, fmt.Errorf("วันที่เริ่มต้น Shopee release เงินต้องเป็น YYYY-MM-DD")
	case strings.Contains(msg, "time_to ต้องเป็น"):
		return time.Time{}, time.Time{}, fmt.Errorf("วันที่สิ้นสุด Shopee release เงินต้องเป็น YYYY-MM-DD")
	case strings.Contains(msg, "time_to ต้องมากกว่า"):
		return time.Time{}, time.Time{}, fmt.Errorf("วันที่สิ้นสุด Shopee release เงินต้องมากกว่าหรือเท่ากับวันที่เริ่มต้น")
	default:
		return time.Time{}, time.Time{}, err
	}
}

func (h *ShopeeImportHandler) loadSettlementDefaults(ctx context.Context) (ShopeeSettlementDefaults, error) {
	var d ShopeeSettlementDefaults
	var updatedAt sql.NullTime
	if h.channelDefaults != nil {
		if def, err := h.channelDefaults.Get("shopee_settlement", "ar_receipt"); err != nil {
			return d, err
		} else if def != nil {
			d = ShopeeSettlementDefaults{
				DocFormatCode: strings.TrimSpace(def.DocFormatCode),
				PassbookCode:  strings.TrimSpace(def.PassbookCode),
				PassbookName:  strings.TrimSpace(def.PassbookName),
				BankCode:      strings.TrimSpace(def.BankCode),
				BankBranch:    strings.TrimSpace(def.BankBranch),
				ExpenseCode:   strings.TrimSpace(def.ExpenseCode),
				ExpenseName:   strings.TrimSpace(def.ExpenseName),
				UpdatedAt:     def.UpdatedAt.Format(time.RFC3339),
			}
			return d, nil
		}
	}
	err := h.sqlDB().QueryRowContext(ctx, `
		SELECT doc_format_code, passbook_code, passbook_name, bank_code, bank_branch,
		       expense_code, expense_name, updated_at
		  FROM shopee_settlement_defaults
		 WHERE id=1`).Scan(&d.DocFormatCode, &d.PassbookCode, &d.PassbookName, &d.BankCode, &d.BankBranch, &d.ExpenseCode, &d.ExpenseName, &updatedAt)
	if err == sql.ErrNoRows {
		return d, nil
	}
	if updatedAt.Valid {
		d.UpdatedAt = updatedAt.Time.Format(time.RFC3339)
	}
	return d, err
}

func (h *ShopeeImportHandler) failSettlementRun(ctx context.Context, runID, msg string) {
	_, _ = h.sqlDB().ExecContext(ctx, `
		UPDATE shopee_settlement_runs
		   SET status='failed', error_msg=$2, finished_at=NOW(), updated_at=NOW()
		 WHERE id=$1::uuid`, runID, msg)
}

func (h *ShopeeImportHandler) failSettlementPreviewRun(ctx context.Context, runID string, userID *string, msg string) {
	h.failSettlementRun(ctx, runID, msg)
	h.auditSettlementRun(ctx, "shopee_settlement_preview_failed", runID, userID, "error", map[string]interface{}{
		"error": msg,
	})
}

func (h *ShopeeImportHandler) auditSettlementRun(ctx context.Context, action, runID string, userID *string, level string, extra map[string]interface{}) {
	if h.auditRepo == nil {
		return
	}
	detail := map[string]interface{}{}
	var targetID *string
	if strings.TrimSpace(runID) != "" {
		detail["run_id"] = strings.TrimSpace(runID)
		targetID = stringPtrIfNotEmpty(runID)
		if run, err := h.loadSettlementRun(ctx, runID); err == nil && run != nil {
			detail = settlementAuditDetail(run)
			targetID = stringPtrIfNotEmpty(run.ID)
		}
	}
	for k, v := range extra {
		if settlementAuditValueEmpty(v) {
			continue
		}
		detail[k] = v
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action:   action,
		TargetID: targetID,
		UserID:   userID,
		Source:   "shopee_settlement",
		Level:    level,
		Detail:   detail,
	})
}

func settlementAuditDetail(run *shopeeSettlementRunView) map[string]interface{} {
	detail := map[string]interface{}{
		"run_id":               run.ID,
		"shop_id":              run.ShopID,
		"shop_label":           run.ShopLabel,
		"release_date_from":    run.ReleaseDateFrom,
		"release_date_to":      run.ReleaseDateTo,
		"status":               run.Status,
		"total_count":          run.TotalCount,
		"ready_count":          run.ReadyCount,
		"blocked_count":        run.BlockedCount,
		"sent_count":           run.SentCount,
		"rc_doc_no":            run.RCDocNo,
		"doc_format_code":      run.SelectedDocFormatCode,
		"passbook_code":        run.SelectedPassbookCode,
		"passbook_name":        run.SelectedPassbookName,
		"expense_code":         run.SelectedExpenseCode,
		"expense_name":         run.SelectedExpenseName,
		"invoice_amount_total": run.InvoiceAmountTotal,
		"payout_amount_total":  run.PayoutAmountTotal,
		"difference_amount":    run.DifferenceAmountTotal,
		"hidden_at":            run.HiddenAt,
		"hidden_reason":        run.HiddenReason,
	}
	for k, v := range detail {
		if settlementAuditValueEmpty(v) {
			delete(detail, k)
		}
	}
	return detail
}

func canHideSettlementRun(run *shopeeSettlementRunView) error {
	if run == nil {
		return fmt.Errorf("ไม่พบงานรับชำระ Shopee")
	}
	switch run.Status {
	case "pending", "running", "sending":
		return fmt.Errorf("งานนี้กำลังทำงานอยู่ กรุณารอให้เสร็จก่อนซ่อน")
	case "sent":
		return fmt.Errorf("งานที่ส่งรับชำระสำเร็จแล้วต้องเก็บไว้เป็นประวัติ ไม่สามารถซ่อนจากรายการปกติได้")
	}
	if run.ReadyCount > 0 {
		return fmt.Errorf("งานนี้ยังมีรายการพร้อมส่ง จึงยังซ่อนไม่ได้ กรุณาส่งหรือรีเฟรชผลก่อน")
	}
	return nil
}

func settlementDefaultHideReason(run *shopeeSettlementRunView) string {
	if run == nil {
		return "ซ่อนจากรายการรับชำระ Shopee"
	}
	if run.Status == "failed" {
		return "ซ่อนงานที่ดึงหรือส่งไม่สำเร็จ"
	}
	if run.ReadyCount == 0 && run.BlockedCount > 0 {
		return "ซ่อนงานที่ไม่มีรายการพร้อมส่ง"
	}
	if run.TotalCount == 0 {
		return "ซ่อนงานที่ไม่พบรายการ Shopee ในช่วง release"
	}
	return "ซ่อนจากรายการรับชำระ Shopee"
}

func settlementAuditValueEmpty(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []string:
		return len(x) == 0
	default:
		return false
	}
}

func settlementReconcileAuditMessage(result settlementReconcileResult) string {
	if result.NewlyBlocked > 0 {
		return fmt.Sprintf("รีเฟรชผลแล้วพบรายการที่ต้อง block เพิ่ม %d รายการ", result.NewlyBlocked)
	}
	return "รีเฟรชผลแล้ว ไม่พบรายการที่ต้อง block เพิ่ม"
}

func (h *ShopeeImportHandler) callSMLAPI(ctx context.Context, method, path string, payload interface{}, out interface{}) error {
	baseURL := strings.TrimRight(h.cfg.ShopeeSMLURL, "/")
	if baseURL == "" || strings.TrimSpace(h.cfg.ShopeeSMLGUID) == "" {
		return fmt.Errorf("SML REST URL ยังไม่ได้ตั้งค่า")
	}
	var body io.Reader
	if payload != nil {
		b, err := sml.MarshalASCII(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", h.cfg.ShopeeSMLGUID)
	if h.cfg.ShopeeSMLDatabase != "" {
		req.Header.Set("x-tenant", h.cfg.ShopeeSMLDatabase)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("เรียก SML ไม่สำเร็จ: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var body struct {
			Error   *smlProxyErrorBody `json:"error"`
			Message string             `json:"message"`
		}
		_ = json.Unmarshal(b, &body)
		msg := strings.TrimSpace(body.Message)
		if body.Error != nil && body.Error.Message != "" {
			msg = strings.TrimSpace(body.Error.Message)
		}
		if msg == "" {
			msg = string(b)
		}
		return fmt.Errorf("SML HTTP %d: %s", resp.StatusCode, msg)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("อ่านผลลัพธ์ SML ไม่สำเร็จ: %w", err)
		}
	}
	return nil
}

func normalizeSettlementSendRequest(req shopeeSettlementSendRequest) shopeeSettlementSendRequest {
	req.DocFormatCode = strings.TrimSpace(req.DocFormatCode)
	req.PassbookCode = strings.TrimSpace(req.PassbookCode)
	req.PassbookName = strings.TrimSpace(req.PassbookName)
	req.BankCode = strings.TrimSpace(req.BankCode)
	req.BankBranch = strings.TrimSpace(req.BankBranch)
	req.ExpenseCode = strings.TrimSpace(req.ExpenseCode)
	req.ExpenseName = strings.TrimSpace(req.ExpenseName)
	req.Remark = strings.TrimSpace(req.Remark)
	req.DocDate = strings.TrimSpace(req.DocDate)
	req.DocTime = strings.TrimSpace(req.DocTime)
	return req
}

func applySettlementDefaults(req shopeeSettlementSendRequest, defaults ShopeeSettlementDefaults) shopeeSettlementSendRequest {
	if req.DocFormatCode == "" {
		req.DocFormatCode = strings.TrimSpace(defaults.DocFormatCode)
	}
	if req.PassbookCode == "" {
		req.PassbookCode = strings.TrimSpace(defaults.PassbookCode)
	}
	if req.PassbookName == "" {
		req.PassbookName = strings.TrimSpace(defaults.PassbookName)
	}
	if req.BankCode == "" {
		req.BankCode = strings.TrimSpace(defaults.BankCode)
	}
	if req.BankBranch == "" {
		req.BankBranch = strings.TrimSpace(defaults.BankBranch)
	}
	if req.ExpenseCode == "" {
		req.ExpenseCode = strings.TrimSpace(defaults.ExpenseCode)
	}
	if req.ExpenseName == "" {
		req.ExpenseName = strings.TrimSpace(defaults.ExpenseName)
	}
	return req
}

func humanizeSettlementError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "context deadline exceeded"), strings.Contains(lower, "timeout"):
		return "เชื่อมต่อ Shopee/SML ใช้เวลานานเกินกำหนด กรุณาลองใหม่อีกครั้ง"
	case strings.Contains(lower, "connection refused"), strings.Contains(lower, "no route to host"), strings.Contains(lower, "connect"):
		return "เชื่อมต่อ Shopee/SML ไม่สำเร็จ กรุณาตรวจการเชื่อมต่อหรือเครื่อง SML"
	case strings.Contains(lower, "token"):
		return "Shopee token มีปัญหา กรุณาเชื่อมต่อร้าน Shopee ใหม่"
	default:
		return sml.HumanizeConnectionError(msg)
	}
}

func roundSettlement(v float64) float64 {
	return math.Round(v*100) / 100
}

func stringPtrIfNotEmpty(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}
