package handlers

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
)

type InstanceSettingsHandler struct {
	repo  *repository.AppSettingsRepo
	audit *repository.AuditLogRepo
	cfg   *config.Config
	log   *zap.Logger
}

func NewInstanceSettingsHandler(repo *repository.AppSettingsRepo, audit *repository.AuditLogRepo, cfg *config.Config, log *zap.Logger) *InstanceSettingsHandler {
	return &InstanceSettingsHandler{repo: repo, audit: audit, cfg: cfg, log: log}
}

type settingDef struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Group        string `json:"group"`
	Type         string `json:"type"`
	DefaultValue string `json:"default_value,omitempty"`
	Required     bool   `json:"required,omitempty"`
	Description  string `json:"description,omitempty"`
}

const (
	instanceConnectionTimeout = 4 * time.Second
	instanceResponseBodyLimit = 768
)

var instanceSettingDefs = []settingDef{
	{Key: "instance.name", Label: "ชื่อร้าน", Group: "instance", Type: "text", DefaultValue: "Nexflow", Required: true, Description: "ชื่อที่ใช้ระบุร้านนี้ใน Nexflow"},
	{Key: "instance.support_contact", Label: "ผู้ดูแลระบบ / ช่องทางติดต่อ", Group: "instance", Type: "text", DefaultValue: "", Description: "ชื่อ เบอร์โทร หรือช่องทางติดต่อผู้ดูแลของร้าน"},
}

func (h *InstanceSettingsHandler) Get(c *gin.Context) {
	dbSettings, err := h.repo.All()
	if err != nil {
		h.log.Error("instance settings list", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	settings := make([]gin.H, 0, len(instanceSettingDefs))
	for _, def := range instanceSettingDefs {
		dbVal, fromDB := dbSettings[def.Key]
		dbValue := ""
		if fromDB {
			dbValue = strings.TrimSpace(dbVal.Value)
		}
		value := dbValue
		source := "unset"
		if value != "" {
			source = "database"
		} else if def.DefaultValue != "" {
			value = def.DefaultValue
			source = "default"
		}

		missing := def.Required && value == ""

		settings = append(settings, gin.H{
			"key":              def.Key,
			"label":            def.Label,
			"group":            def.Group,
			"type":             def.Type,
			"value":            value,
			"source":           source,
			"secret":           false,
			"has_secret":       false,
			"required":         def.Required,
			"locked":           false,
			"missing":          missing,
			"restart_required": false,
			"description":      def.Description,
			"overridden":       fromDB,
			"active":           true,
			"pending_restart":  false,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"settings":                 settings,
		"restart_required":         false,
		"pending_restart":          false,
		"pending_restart_settings": []string{},
		"missing_required":         []string{},
		"setup_complete":           true,
	})
}

func (h *InstanceSettingsHandler) Update(c *gin.Context) {
	var body struct {
		Settings map[string]string `json:"settings"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	allowed := map[string]settingDef{}
	for _, def := range instanceSettingDefs {
		allowed[def.Key] = def
	}

	values := map[string]string{}
	for key, value := range body.Settings {
		_, ok := allowed[key]
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":  "setting_not_editable",
				"error": "ค่านี้จัดการโดยผู้ดูแลระบบและไม่สามารถแก้จากหน้านี้ได้",
				"key":   key,
			})
			return
		}
		normalized, err := normalizeInstanceProfileValue(key, value)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_setting", "error": err.Error(), "key": key})
			return
		}
		values[key] = normalized
	}

	if len(values) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "updated": 0, "restart_required": false})
		return
	}
	current, err := h.repo.All()
	if err != nil {
		h.log.Error("instance profile current values", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	changed := map[string]string{}
	for key, value := range values {
		currentValue := strings.TrimSpace(current[key].Value)
		if key == "instance.name" && currentValue == "" {
			currentValue = "Nexflow"
		}
		if value != currentValue {
			changed[key] = value
		}
	}
	if len(changed) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "updated": 0, "restart_required": false})
		return
	}

	userID := c.GetString("user_id")
	if err := h.repo.UpsertMany(changed, map[string]bool{}, userID); err != nil {
		h.log.Error("instance settings update", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	h.auditProfileUpdate(c, changed)

	c.JSON(http.StatusOK, gin.H{
		"ok":               true,
		"updated":          len(changed),
		"restart_required": false,
	})
}

func (h *InstanceSettingsHandler) Restart(c *gin.Context) {
	h.log.Warn("deprecated instance restart endpoint called",
		zap.String("user_id", c.GetString("user_id")),
		zap.String("user_email", c.GetString("user_email")),
	)
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"deprecated": true,
		"restarted":  false,
		"message":    "การ restart จัดการผ่านระบบ deploy แล้ว",
	})
}

// TestConnection is retained for compatibility and only checks the active
// server-side runtime configuration. Client-supplied connection targets are ignored.
func (h *InstanceSettingsHandler) TestConnection(c *gin.Context) {
	runtimeSettings, err := h.repo.SMLRuntimeSettings(h.cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด config ไม่ได้"})
		return
	}

	httpClient := &http.Client{Timeout: instanceConnectionTimeout}

	// ── SML ──────────────────────────────────────────────────────────────────
	baseURL := runtimeSettings.RestBaseURL
	guid := h.cfg.ShopeeSMLGUID // ค่าตายตัวจาก .env ใช้ร่วมกันทุก instance
	database := runtimeSettings.Database
	stockURL := runtimeSettings.StockRequestURL
	var smlProxyResult, smlTenantResult, smlStockResult checkResult

	lineResult := checkResult{OK: true, Skipped: true, Layer: "line", Detail: "จัดการ LINE OA และผู้รับแจ้งเตือนในหน้า LINE แจ้งเตือน"}

	var wg sync.WaitGroup
	run := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}

	run(func() {
		smlProxyResult = checkSMLProxyReachable(httpClient, baseURL)
	})
	run(func() {
		smlTenantResult = checkSMLTenantLookup(httpClient, baseURL, guid, database)
	})
	run(func() {
		smlStockResult = checkSMLStockURL(httpClient, stockURL)
	})
	wg.Wait()

	smlResult := combineSMLDiagnostics(smlProxyResult, smlTenantResult, smlStockResult)
	logFailedInstanceCheck(h.log, "sml_proxy", smlProxyResult)
	logFailedInstanceCheck(h.log, "sml_tenant", smlTenantResult)
	logFailedInstanceCheck(h.log, "sml_stock_request", smlStockResult)

	allOK := checkPassed(smlResult) && checkPassed(lineResult)
	c.JSON(http.StatusOK, gin.H{
		"ok":                allOK,
		"checked_at":        time.Now().Format(time.RFC3339),
		"sml":               smlResult,
		"sml_proxy":         smlProxyResult,
		"sml_tenant":        smlTenantResult,
		"sml_stock_request": smlStockResult,
		"line":              lineResult,
		"ai_enabled":        false,
	})
}

func normalizeInstanceProfileValue(key, value string) (string, error) {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	switch key {
	case "instance.name":
		if normalized == "" {
			return "", fmt.Errorf("กรุณากรอกชื่อร้าน")
		}
		if utf8.RuneCountInString(normalized) > 120 {
			return "", fmt.Errorf("ชื่อร้านต้องไม่เกิน 120 ตัวอักษร")
		}
	case "instance.support_contact":
		if utf8.RuneCountInString(normalized) > 200 {
			return "", fmt.Errorf("ข้อมูลผู้ดูแลระบบต้องไม่เกิน 200 ตัวอักษร")
		}
	default:
		return "", fmt.Errorf("ค่านี้ไม่สามารถแก้จากหน้านี้ได้")
	}
	return normalized, nil
}

func (h *InstanceSettingsHandler) auditProfileUpdate(c *gin.Context, changed map[string]string) {
	if h.audit == nil || len(changed) == 0 {
		return
	}
	keys := make([]string, 0, len(changed))
	for key := range changed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var userID *string
	if id := strings.TrimSpace(c.GetString("user_id")); id != "" {
		userID = &id
	}
	targetID := "instance_profile"
	if err := h.audit.Log(models.AuditEntry{
		Action:   "instance_profile_updated",
		TargetID: &targetID,
		UserID:   userID,
		Source:   "instance_settings",
		Level:    "info",
		TraceID:  c.GetString("trace_id"),
		Detail:   gin.H{"changed_keys": keys},
	}); err != nil && h.log != nil {
		h.log.Warn("instance profile audit failed", zap.Error(err))
	}
}

type checkResult struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Layer      string `json:"layer,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	LatencyMS  int64  `json:"latency_ms,omitempty"`
}

func checkPassed(r checkResult) bool {
	return r.OK || r.Skipped
}

func doInstanceGET(client *http.Client, rawURL string, headers map[string]string) (int, []byte, int64, error) {
	start := time.Now()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	latencyMS := time.Since(start).Milliseconds()
	if err != nil {
		return 0, nil, latencyMS, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, instanceResponseBodyLimit))
	return resp.StatusCode, body, latencyMS, nil
}

func checkSMLProxyReachable(client *http.Client, baseURL string) checkResult {
	result := checkResult{Layer: "sml_proxy"}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		result.Error = "ยังไม่ได้ตั้งค่า sml-api-byboss URL"
		return result
	}

	code, body, latencyMS, err := doInstanceGET(client, baseURL+"/health", nil)
	result.HTTPStatus = code
	result.LatencyMS = latencyMS
	if err != nil {
		result.Error = "ติดต่อ sml-api-byboss ไม่ได้ภายในเวลาที่กำหนด ตรวจ container/port 8200 และ network ระหว่าง Nexflow กับ sml-api-byboss"
		result.Detail = summarizeConnectionError(err)
		return result
	}
	if code >= 500 {
		result.Error = fmt.Sprintf("sml-api-byboss ตอบ HTTP %d ระหว่างตรวจ proxy", code)
		result.Detail = summarizeBody(body)
		return result
	}
	result.OK = true
	if code == http.StatusOK {
		result.Detail = "sml-api-byboss ตอบ /health ได้"
	} else {
		result.Detail = fmt.Sprintf("sml-api-byboss ตอบ HTTP %d แปลว่า proxy reachable แต่ endpoint /health อาจไม่มีใน service นี้", code)
	}
	return result
}

func checkSMLTenantLookup(client *http.Client, baseURL, guid, database string) checkResult {
	result := checkResult{Layer: "sml_tenant"}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	database = strings.TrimSpace(database)
	if baseURL == "" || guid == "" || database == "" {
		result.Error = "ยังไม่ได้ตั้งค่า sml-api-byboss URL, guid หรือ database tenant"
		return result
	}

	smlURL := baseURL + "/api/v1/ic/products?page=1"
	code, body, latencyMS, err := doInstanceGET(client, smlURL, map[string]string{
		"guid":     guid,
		"X-Tenant": database,
	})
	result.HTTPStatus = code
	result.LatencyMS = latencyMS
	if err != nil {
		result.Error = fmt.Sprintf("sml-api-byboss ไม่ตอบ หรือ downstream SML domain ของ tenant '%s' มีปัญหา", database)
		result.Detail = summarizeConnectionError(err)
		return result
	}
	switch code {
	case http.StatusOK:
		result.OK = true
		result.Detail = fmt.Sprintf("product lookup ผ่าน tenant %s", database)
	case http.StatusUnauthorized:
		result.Error = "guid (API key) ไม่ถูกต้อง"
	case http.StatusForbidden:
		result.Error = fmt.Sprintf("database tenant '%s' ไม่ถูกต้องหรือไม่มีสิทธิ์เข้าถึงใน sml-api-byboss", database)
	default:
		result.Error = fmt.Sprintf("sml-api-byboss ตอบ HTTP %d ระหว่าง product lookup tenant %s", code, database)
		result.Detail = summarizeBody(body)
	}
	return result
}

func checkSMLStockURL(client *http.Client, stockURL string) checkResult {
	result := checkResult{Layer: "sml_stock_request"}
	stockURL = strings.TrimRight(strings.TrimSpace(stockURL), "/")
	if stockURL == "" {
		result.OK = true
		result.Skipped = true
		result.Detail = "ไม่ได้ตั้งค่า Stock Request URL ระบบจะข้ามการคำนวณต้นทุนสต๊อกหลังส่ง SML"
		return result
	}

	code, body, latencyMS, err := doInstanceGET(client, stockURL, nil)
	result.HTTPStatus = code
	result.LatencyMS = latencyMS
	if err != nil {
		result.Error = "endpoint คำนวณต้นทุนสต๊อกติดต่อไม่ได้ ตรวจ Stock Request URL หรือ network ไป SML Java server"
		result.Detail = summarizeConnectionError(err)
		return result
	}
	if code >= 500 {
		result.Error = fmt.Sprintf("Stock Request URL ตอบ HTTP %d ระหว่าง read-only reachability check", code)
		result.Detail = summarizeBody(body)
		return result
	}
	result.OK = true
	result.Detail = fmt.Sprintf("Stock Request URL reachable (HTTP %d); ยังไม่ได้ POST processstockrequest", code)
	return result
}

func combineSMLDiagnostics(proxy, tenant, stock checkResult) checkResult {
	result := checkResult{Layer: "sml"}
	if !checkPassed(proxy) {
		result.Error = proxy.Error
		result.Detail = proxy.Detail
		return result
	}
	if !checkPassed(tenant) {
		result.Error = tenant.Error
		result.Detail = tenant.Detail
		return result
	}
	if !checkPassed(stock) {
		result.Error = stock.Error
		result.Detail = stock.Detail
		return result
	}
	result.OK = true
	if stock.Skipped {
		result.Detail = "SML product lookup ผ่าน; Stock Request URL ยังไม่ได้ตั้งค่า"
	} else {
		result.Detail = "SML product lookup และ Stock Request URL ผ่าน"
	}
	return result
}

func summarizeConnectionError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if len(msg) > instanceResponseBodyLimit {
		return msg[:instanceResponseBodyLimit] + "..."
	}
	return msg
}

func summarizeBody(body []byte) string {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return ""
	}
	if len(msg) > instanceResponseBodyLimit {
		return msg[:instanceResponseBodyLimit] + "..."
	}
	return msg
}

func logFailedInstanceCheck(log *zap.Logger, layer string, result checkResult) {
	if log == nil || checkPassed(result) {
		return
	}
	log.Warn("instance_connection_check_failed",
		zap.String("layer", layer),
		zap.Int("http_status", result.HTTPStatus),
		zap.Int64("latency_ms", result.LatencyMS),
		zap.String("error", result.Error),
	)
}
