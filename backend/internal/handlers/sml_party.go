package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
)

// SMLPartyHandler proxies the SML 248 party master through an in-memory cache.
// Admin-only — used by /settings/channels picker and any future supplier UI.
type SMLPartyHandler struct {
	cache      *sml.PartyCache
	client     *sml.PartyClient
	auditRepo  *repository.AuditLogRepo
	smlBaseURL string // sml-api-byboss base URL (e.g. http://172.24.0.1:8200)
	smlGUID    string // API key for sml-api-byboss (used as guid header)
	smlTenant  string // database/tenant name for sml-api-byboss (X-Tenant header)
	logger     *zap.Logger
}

func NewSMLPartyHandler(cache *sml.PartyCache, client *sml.PartyClient, auditRepo *repository.AuditLogRepo, logger *zap.Logger) *SMLPartyHandler {
	return &SMLPartyHandler{cache: cache, client: client, auditRepo: auditRepo, logger: logger}
}

// SetSMLConfig injects the sml-api-byboss connection details needed for
// endpoints that call sml-api-byboss directly (e.g. doc-formats).
func (h *SMLPartyHandler) SetSMLConfig(baseURL, guid, tenant string) {
	h.smlBaseURL = strings.TrimRight(baseURL, "/")
	h.smlGUID = guid
	h.smlTenant = tenant
}

// GET /api/sml/customers?search=&limit=20
func (h *SMLPartyHandler) SearchCustomers(c *gin.Context) {
	h.search(c, "sale")
}

// GET /api/sml/suppliers?search=&limit=20
func (h *SMLPartyHandler) SearchSuppliers(c *gin.Context) {
	h.search(c, "purchase")
}

type createPartyRequest struct {
	Code       string `json:"code" binding:"required"`
	Name1      string `json:"name_1"`
	ARStatus   *int   `json:"ar_status"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	APStatus   *int   `json:"ap_status"`
	Firstname  string `json:"firstname"`
	Lastname   string `json:"lastname"`
	NameEng1   string `json:"name_eng_1"`
	Address    string `json:"address"`
	Remark     string `json:"remark"`
	TaxID      string `json:"tax_id"`
	BranchType *int   `json:"branch_type"`
	BranchCode string `json:"branch_code"`
	CardID     string `json:"card_id"`
}

func (h *SMLPartyHandler) search(c *gin.Context, billType string) {
	if h.cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "party cache not configured"})
		return
	}
	q := c.Query("search")
	limit := 20
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	results := h.cache.Search(billType, q, limit)
	status := h.cache.Status()
	total := status.Customers
	if billType == "purchase" {
		total = status.Suppliers
	}
	out := gin.H{
		"data":         results,
		"total":        total,
		"last_sync":    nullableTime(status.LastSync),
		"last_attempt": nullableTime(status.LastAttempt),
		"status":       status.Status,
	}
	if status.Error != "" {
		out["error"] = sml.HumanizeConnectionError(status.Error)
	}
	c.JSON(http.StatusOK, out)
}

func (h *SMLPartyHandler) CreateCustomer(c *gin.Context) {
	h.create(c, "sale")
}

func (h *SMLPartyHandler) CreateSupplier(c *gin.Context) {
	h.create(c, "purchase")
}

func (h *SMLPartyHandler) create(c *gin.Context, billType string) {
	if h.client == nil || !h.client.IsConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SML party client not configured"})
		return
	}
	var req createPartyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Code != strings.TrimSpace(req.Code) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "รหัสห้ามมีช่องว่างหน้า/หลัง"})
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	req.Name1 = strings.TrimSpace(req.Name1)
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.Firstname = strings.TrimSpace(req.Firstname)
	req.Lastname = strings.TrimSpace(req.Lastname)
	req.NameEng1 = strings.TrimSpace(req.NameEng1)
	req.Address = strings.TrimSpace(req.Address)
	req.Remark = strings.TrimSpace(req.Remark)
	req.TaxID = strings.TrimSpace(req.TaxID)
	req.BranchCode = strings.TrimSpace(req.BranchCode)
	req.CardID = strings.TrimSpace(req.CardID)
	if req.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณากรอกรหัส"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var party *sml.Party
	var statusCode int
	var err error
	if billType == "purchase" {
		statusCode, party, err = h.client.CreateSupplier(ctx, sml.SupplierCreateInput{
			Code:       req.Code,
			APStatus:   req.APStatus,
			Firstname:  req.Firstname,
			Lastname:   req.Lastname,
			Name1:      req.Name1,
			NameEng1:   req.NameEng1,
			Address:    req.Address,
			Remark:     req.Remark,
			TaxID:      req.TaxID,
			BranchType: req.BranchType,
			BranchCode: req.BranchCode,
			CardID:     req.CardID,
		})
	} else {
		statusCode, party, err = h.client.CreateCustomer(ctx, sml.CustomerCreateInput{
			Code:       req.Code,
			ARStatus:   req.ARStatus,
			FirstName:  req.FirstName,
			LastName:   req.LastName,
			Name1:      req.Name1,
			NameEng1:   req.NameEng1,
			Address:    req.Address,
			Remark:     req.Remark,
			TaxID:      req.TaxID,
			BranchType: req.BranchType,
			BranchCode: req.BranchCode,
			CardID:     req.CardID,
		})
	}
	if err != nil {
		if statusCode == http.StatusConflict {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": req.Code})
			return
		}
		h.logger.Warn("sml_party_create_failed",
			zap.String("bill_type", billType),
			zap.String("code", req.Code),
			zap.Int("status_code", statusCode),
			zap.Error(err),
		)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if party == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "SML returned empty party"})
		return
	}
	if h.cache != nil {
		h.cache.Upsert(billType, *party)
	}
	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		action := "sml_customer_created"
		if billType == "purchase" {
			action = "sml_supplier_created"
		}
		detail := map[string]interface{}{
			"code": party.Code,
			"name": party.Name,
		}
		if billType == "purchase" {
			detail["ap_status"] = party.APStatus
			detail["branch_type"] = party.BranchType
		} else {
			detail["ar_status"] = party.ARStatus
			detail["branch_type"] = party.BranchType
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:  action,
			UserID:  userID,
			Source:  "ui",
			Level:   "info",
			TraceID: c.GetString("trace_id"),
			Detail:  detail,
		})
	}
	c.JSON(http.StatusCreated, gin.H{"party": party})
}

// POST /api/sml/refresh-parties — re-fetch both lists from SML.
func (h *SMLPartyHandler) Refresh(c *gin.Context) {
	if h.cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "party cache not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.cache.RefreshNow(ctx); err != nil {
		status := h.cache.Status()
		msg := sml.HumanizeConnectionError(err.Error())
		c.JSON(http.StatusBadGateway, gin.H{
			"error":        "ดึงรายชื่อลูกค้า/ผู้ขายจาก SML ไม่สำเร็จ: " + msg,
			"customers":    status.Customers,
			"suppliers":    status.Suppliers,
			"last_sync":    nullableTime(status.LastSync),
			"last_attempt": nullableTime(status.LastAttempt),
			"status":       status.Status,
		})
		return
	}
	status := h.cache.Status()
	c.JSON(http.StatusOK, gin.H{
		"customers":    status.Customers,
		"suppliers":    status.Suppliers,
		"last_sync":    nullableTime(status.LastSync),
		"last_attempt": nullableTime(status.LastAttempt),
		"status":       status.Status,
	})
}

// GET /api/sml/parties/last-sync
func (h *SMLPartyHandler) LastSync(c *gin.Context) {
	if h.cache == nil {
		c.JSON(http.StatusOK, gin.H{
			"customers":    0,
			"suppliers":    0,
			"last_sync":    nil,
			"last_attempt": nil,
			"status":       "not_configured",
		})
		return
	}
	status := h.cache.Status()
	out := gin.H{
		"customers":    status.Customers,
		"suppliers":    status.Suppliers,
		"last_sync":    nullableTime(status.LastSync),
		"last_attempt": nullableTime(status.LastAttempt),
		"status":       status.Status,
	}
	if status.Error != "" {
		out["error"] = sml.HumanizeConnectionError(status.Error)
	}
	c.JSON(http.StatusOK, out)
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// DocFormatItem mirrors the erp_doc_format row returned by sml-api-bybos.
type DocFormatItem struct {
	Code       string `json:"code"`
	Name1      string `json:"name_1"`
	Name2      string `json:"name_2"`
	Format     string `json:"format"`
	ScreenCode string `json:"screen_code"`
}

type SMLMasterItem struct {
	Code       string `json:"code"`
	Name1      string `json:"name_1"`
	BankCode   string `json:"bank_code,omitempty"`
	BankBranch string `json:"bank_branch,omitempty"`
	BookNumber string `json:"book_number,omitempty"`
}

type smlProxyErrorBody struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details"`
}

type smlMasterPageMeta struct {
	Total int `json:"total"`
	Page  int `json:"page"`
	Size  int `json:"size"`
}

// GET /api/sml/doc-formats?screen_code=PO|SI|SR
// Proxies to sml-api-bybos GET /api/v1/ic/doc-formats?screen_code=<code>.
// screen_code: PO=ใบสั่งซื้อ, SI=ขายสินค้าและบริการ, SR=ใบสั่งขาย
func (h *SMLPartyHandler) DocFormats(c *gin.Context) {
	if h.smlBaseURL == "" || h.smlGUID == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SML REST URL ยังไม่ได้ตั้งค่า"})
		return
	}
	screenCode := strings.ToUpper(strings.TrimSpace(c.Query("screen_code")))

	targetURL := h.smlBaseURL + "/api/v1/ic/doc-formats"
	if screenCode != "" {
		targetURL += "?screen_code=" + screenCode
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	req.Header.Set("x-api-key", h.smlGUID)
	if h.smlTenant != "" {
		req.Header.Set("x-tenant", h.smlTenant)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("เรียก SML ไม่สำเร็จ: %v", err)})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool            `json:"success"`
		Data    []DocFormatItem `json:"data"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "parse SML response failed"})
		return
	}
	if !result.Success {
		c.JSON(http.StatusBadGateway, gin.H{"error": result.Message})
		return
	}
	if result.Data == nil {
		result.Data = []DocFormatItem{}
	}
	c.JSON(http.StatusOK, gin.H{"data": result.Data})
}

// GET /api/sml/branches?search=&limit=
func (h *SMLPartyHandler) Branches(c *gin.Context) {
	h.proxyERPMaster(c, "/api/v1/erp/branches")
}

// GET /api/sml/sales?search=&limit=
func (h *SMLPartyHandler) Sales(c *gin.Context) {
	h.proxyERPMaster(c, "/api/v1/erp/users")
}

// GET /api/sml/expenses?search=&limit=
func (h *SMLPartyHandler) Expenses(c *gin.Context) {
	h.proxyERPMaster(c, "/api/v1/erp/expenses")
}

// GET /api/sml/incomes?search=&limit=
func (h *SMLPartyHandler) Incomes(c *gin.Context) {
	h.proxyERPMaster(c, "/api/v1/erp/incomes")
}

// GET /api/sml/passbooks?search=&limit=
func (h *SMLPartyHandler) Passbooks(c *gin.Context) {
	h.proxyERPMaster(c, "/api/v1/erp/passbooks")
}

func (h *SMLPartyHandler) proxyERPMaster(c *gin.Context, upstreamPath string) {
	if h.smlBaseURL == "" || h.smlGUID == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SML REST URL ยังไม่ได้ตั้งค่า"})
		return
	}

	q := url.Values{}
	q.Set("page", "1")
	q.Set("size", strconv.Itoa(queryLimit(c, 20, 100)))
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		q.Set("search", search)
	}
	targetURL := h.smlBaseURL + upstreamPath + "?" + q.Encode()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	req.Header.Set("x-api-key", h.smlGUID)
	if h.smlTenant != "" {
		req.Header.Set("x-tenant", h.smlTenant)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "เรียก SML ไม่สำเร็จ: " + sml.HumanizeConnectionError(err.Error())})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool               `json:"success"`
		Data    []SMLMasterItem    `json:"data"`
		Meta    smlMasterPageMeta  `json:"meta"`
		Error   *smlProxyErrorBody `json:"error"`
		Message string             `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "อ่านข้อมูล master จาก SML ไม่สำเร็จ"})
		return
	}
	if resp.StatusCode >= 400 || !result.Success {
		msg := result.Message
		if result.Error != nil && result.Error.Message != "" {
			msg = strings.TrimSpace(result.Error.Code + " " + result.Error.Message + " " + fmt.Sprint(result.Error.Details))
		}
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "ดึงข้อมูล master จาก SML ไม่สำเร็จ: " + sml.HumanizeConnectionError(msg)})
		return
	}
	if result.Data == nil {
		result.Data = []SMLMasterItem{}
	}
	c.JSON(http.StatusOK, gin.H{
		"data":  result.Data,
		"total": result.Meta.Total,
		"page":  result.Meta.Page,
		"size":  result.Meta.Size,
	})
}
