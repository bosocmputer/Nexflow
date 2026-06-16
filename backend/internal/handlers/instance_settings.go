package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/repository"
)

type InstanceSettingsHandler struct {
	repo *repository.AppSettingsRepo
	cfg  *config.Config
	log  *zap.Logger
}

func NewInstanceSettingsHandler(repo *repository.AppSettingsRepo, cfg *config.Config, log *zap.Logger) *InstanceSettingsHandler {
	return &InstanceSettingsHandler{repo: repo, cfg: cfg, log: log}
}

type settingDef struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Group        string `json:"group"`
	Type         string `json:"type"`
	EnvKey       string `json:"env_key,omitempty"`
	DefaultValue string `json:"default_value,omitempty"`
	Secret       bool   `json:"secret,omitempty"`
	Restart      bool   `json:"restart_required,omitempty"`
	Required     bool   `json:"required,omitempty"`
	Locked       bool   `json:"locked,omitempty"` // ค่าตายตัว ห้ามแก้ผ่าน UI
	Description  string `json:"description,omitempty"`
}

const (
	instanceConnectionTimeout = 4 * time.Second
	instanceResponseBodyLimit = 768
)

var instanceSettingDefs = []settingDef{
	{Key: "instance.name", Label: "ชื่อร้าน", Group: "instance", Type: "text", DefaultValue: "Nexflow", Description: "ไม่บังคับ ใช้ให้ทีมดูแลรู้ว่า Nexflow ชุดนี้เป็นของร้านไหน"},
	{Key: "instance.slug", Label: "รหัสร้าน", Group: "instance", Type: "text", DefaultValue: "default", Description: "ไม่บังคับ ใช้เป็นชื่อสั้นสำหรับแยกเอกสาร backup และ deploy"},
	{Key: "instance.support_contact", Label: "ผู้ดูแลระบบ", Group: "instance", Type: "text", DefaultValue: "", Description: "ไม่บังคับ เบอร์หรือชื่อคนที่ดูแลระบบชุดนี้"},

	{Key: "sml.rest_base_url", Label: "sml-api-byboss URL", Group: "sml", Type: "url", Restart: true, Required: true, Description: "URL internal proxy จาก Nexflow backend ไป sml-api-byboss เช่น http://172.24.0.1:8200 ไม่ใช่ SML domain ปลายทางของ tenant"},
	{Key: "sml.provider", Label: "Provider", Group: "sml", Type: "text", Restart: true, Required: true, Description: "รหัส provider ของ SML instance นี้ เช่น DATA ใช้กับ SML REST และ stock process"},
	{Key: "sml.config_file", Label: "Config file", Group: "sml", Type: "text", Restart: true, Required: true, Description: "ชื่อไฟล์ config ของ SML instance นี้ เช่น SMLConfigDATA.xml"},
	{Key: "sml.database", Label: "Database (tenant)", Group: "sml", Type: "text", Restart: true, Required: true, Description: "ชื่อ database SML ของร้านนี้ ต้องเป็น lowercase เช่น sml1_2026 (sml-api-byboss แปลงเป็น lowercase เสมอ ห้ามใช้ตัวพิมพ์ใหญ่)"},
	{Key: "sml.stock_request_url", Label: "Stock Request URL", Group: "sml", Type: "url", Restart: false, Required: false, Description: "URL ของ SML server คำนวณต้นทุนสต๊อก (ไม่ใช่ sml-api-byboss) — path /SMLJavaWebService/rest/v1/processstockrequest จะถูกเติมอัตโนมัติ เช่น http://192.168.2.248:8080 (ว่าง = ข้ามการคำนวณ)"},

	{Key: "line.notify_channel_secret", Label: "LINE Channel secret", Group: "line", Type: "password", Secret: true, Restart: true, Description: "ใช้กับ LINE OA ที่ส่งแจ้งเตือนระบบ"},
	{Key: "line.notify_channel_access_token", Label: "LINE Channel access token", Group: "line", Type: "password", Secret: true, Restart: true, Description: "ใช้ส่ง Push แจ้งเตือน error และสถานะระบบไปยังแอดมิน"},
	{Key: "line.notify_admin_user_id", Label: "LINE admin user ID", Group: "line", Type: "text", Restart: true, Description: "userId ของผู้รับแจ้งเตือนระบบ เช่น SML error, email error, disk/tunnel warning"},

	{Key: "ai.openrouter_api_key", Label: "OpenRouter API key", Group: "ai", Type: "password", Secret: true, Restart: true, Required: true},
	{Key: "ai.openrouter_model", Label: "Model หลัก", Group: "ai", Type: "text", Restart: true, Required: true},
	{Key: "ai.openrouter_fallback_model", Label: "Model สำรอง", Group: "ai", Type: "text", Restart: true},
	{Key: "ai.openrouter_audio_model", Label: "Audio model", Group: "ai", Type: "text", Restart: true},
	{Key: "automation.auto_confirm_threshold", Label: "เกณฑ์ confidence", Group: "automation", Type: "number", Restart: true},
}

var smlDatabaseNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
var smlProviderPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
var smlConfigFilePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func (h *InstanceSettingsHandler) Get(c *gin.Context) {
	dbSettings, err := h.repo.All()
	if err != nil {
		h.log.Error("instance settings list", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	runtimeValues := repository.RuntimeSettingValues(h.cfg)
	settings := make([]gin.H, 0, len(instanceSettingDefs))
	pendingKeys := []string{}
	missingRequired := []string{}
	for _, def := range instanceSettingDefs {
		dbVal, fromDB := dbSettings[def.Key]
		dbValue := ""
		if fromDB {
			dbValue = strings.TrimSpace(dbVal.Value)
		}
		runtimeValue := strings.TrimSpace(runtimeValues[def.Key])
		value := dbValue
		source := "unset"
		if value != "" {
			source = "database"
		} else if runtimeValue != "" {
			value = runtimeValue
			source = "env"
		} else if def.DefaultValue != "" {
			value = def.DefaultValue
			source = "default"
		}

		missing := def.Required && value == ""

		active := true
		pendingRestart := false
		if def.Restart && !def.Locked && dbValue != "" && runtimeValue != "" && dbValue != runtimeValue {
			active = false
			pendingRestart = true
			pendingKeys = append(pendingKeys, def.Key)
		}
		if missing {
			missingRequired = append(missingRequired, def.Key)
		}

		displayValue := value
		displayRuntimeValue := runtimeValue
		hasSecret := false
		if def.Secret && value != "" {
			hasSecret = true
			displayValue = maskSecret(value)
		}
		if def.Secret && runtimeValue != "" {
			displayRuntimeValue = maskSecret(runtimeValue)
		}

		settings = append(settings, gin.H{
			"key":              def.Key,
			"label":            def.Label,
			"group":            def.Group,
			"type":             def.Type,
			"value":            displayValue,
			"source":           source,
			"secret":           def.Secret,
			"has_secret":       hasSecret,
			"required":         def.Required,
			"locked":           def.Locked,
			"missing":          missing,
			"restart_required": def.Restart,
			"description":      def.Description,
			"overridden":       fromDB,
			"runtime_value":    displayRuntimeValue,
			"active":           active,
			"pending_restart":  pendingRestart,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"settings":                 settings,
		"restart_required":         len(pendingKeys) > 0,
		"pending_restart":          len(pendingKeys) > 0,
		"pending_restart_settings": pendingKeys,
		"missing_required":         missingRequired,
		"setup_complete":           len(missingRequired) == 0,
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
	secretKeys := map[string]bool{}
	for _, def := range instanceSettingDefs {
		allowed[def.Key] = def
		if def.Secret {
			secretKeys[def.Key] = true
		}
	}

	// optional fields that may be explicitly cleared to empty string
	clearableKeys := map[string]bool{
		"sml.stock_request_url": true,
	}

	values := map[string]string{}
	for key, value := range body.Settings {
		def, ok := allowed[key]
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown setting: " + key})
			return
		}
		if def.Locked {
			continue // ค่าตายตัว ไม่อนุญาตให้แก้ผ่าน API
		}
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			if clearableKeys[key] {
				values[key] = "" // explicit clear allowed for optional fields
			}
			continue // skip blank for non-clearable fields
		}
		if def.Secret && strings.Contains(trimmed, "••••••••") {
			continue // skip masked placeholder — user didn't change the secret
		}
		if normalized, msg := normalizeInstanceSetting(def, trimmed); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg, "key": key})
			return
		} else {
			trimmed = normalized
		}
		values[key] = trimmed
	}

	if len(values) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "updated": 0})
		return
	}

	userID := c.GetString("user_id")
	if err := h.repo.UpsertMany(values, secretKeys, userID); err != nil {
		h.log.Error("instance settings update", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":               true,
		"updated":          len(values),
		"restart_required": true,
	})
}

func (h *InstanceSettingsHandler) Restart(c *gin.Context) {
	h.log.Warn("admin requested backend restart",
		zap.String("user_id", c.GetString("user_id")),
		zap.String("user_email", c.GetString("user_email")),
	)
	c.JSON(http.StatusAccepted, gin.H{
		"ok":      true,
		"message": "backend restart scheduled",
	})

	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
}

// effectiveValue is kept only for optional fields that have a built-in default
// (instance.name / instance.slug). All SML/AI/LINE values must be set via UI.
func (h *InstanceSettingsHandler) effectiveValue(def settingDef, dbSettings map[string]repository.AppSetting) (string, string) {
	if s, ok := dbSettings[def.Key]; ok && strings.TrimSpace(s.Value) != "" {
		return s.Value, "database"
	}
	if def.DefaultValue != "" {
		return def.DefaultValue, "default"
	}
	return "", "unset"
}

// TestConnections tests SML, LINE, and OpenRouter connectivity using saved DB values.
// Each check is independent — partial success is returned so the UI can show per-service status.
func (h *InstanceSettingsHandler) TestConnection(c *gin.Context) {
	dbSettings, err := h.repo.All()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด config ไม่ได้"})
		return
	}

	allowed := map[string]settingDef{}
	for _, def := range instanceSettingDefs {
		allowed[def.Key] = def
	}
	var body struct {
		Settings map[string]string `json:"settings"`
	}
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	overrides := map[string]string{}
	for key, value := range body.Settings {
		def, ok := allowed[key]
		if !ok || def.Locked {
			continue
		}
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || (def.Secret && strings.Contains(trimmed, "••••••••")) {
			continue
		}
		if normalized, msg := normalizeInstanceSetting(def, trimmed); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg, "key": key})
			return
		} else {
			overrides[key] = normalized
		}
	}

	cfgFallback := repository.RuntimeSettingValues(h.cfg)
	get := func(key string) string {
		if v := strings.TrimSpace(overrides[key]); v != "" {
			return v
		}
		if v := strings.TrimSpace(dbSettings[key].Value); v != "" {
			return v
		}
		return strings.TrimSpace(cfgFallback[key])
	}

	httpClient := &http.Client{Timeout: instanceConnectionTimeout}

	// ── SML ──────────────────────────────────────────────────────────────────
	baseURL := get("sml.rest_base_url")
	guid := h.cfg.ShopeeSMLGUID // ค่าตายตัวจาก .env ใช้ร่วมกันทุก instance
	database := get("sml.database")
	stockURL := get("sml.stock_request_url")
	var smlProxyResult, smlTenantResult, smlStockResult checkResult

	// ── LINE ─────────────────────────────────────────────────────────────────
	lineResult := checkResult{}
	lineToken := get("line.notify_channel_access_token")
	if lineToken == "" {
		lineResult.Error = "ยังไม่ได้ตั้งค่า LINE Channel access token"
	} else {
		code, body, latencyMS, err := doInstanceGET(httpClient, "https://api.line.me/v2/bot/info",
			map[string]string{"Authorization": "Bearer " + lineToken})
		lineResult.HTTPStatus = code
		lineResult.LatencyMS = latencyMS
		if err != nil {
			lineResult.Error = fmt.Sprintf("เชื่อมต่อ LINE API ไม่ได้: %v", err)
		} else if code == http.StatusOK {
			lineResult.OK = true
			// extract displayName from JSON cheaply
			s := string(body)
			if i := strings.Index(s, `"displayName":"`); i >= 0 {
				rest := s[i+15:]
				if j := strings.Index(rest, `"`); j >= 0 {
					lineResult.Detail = rest[:j]
				}
			}
		} else {
			lineResult.Error = "access token ไม่ถูกต้องหรือหมดอายุแล้ว"
		}
	}

	// ── OpenRouter ───────────────────────────────────────────────────────────
	orResult := checkResult{}
	orKey := get("ai.openrouter_api_key")
	if orKey == "" {
		orResult.Error = "ยังไม่ได้ตั้งค่า OpenRouter API key"
	} else {
		code, body, latencyMS, err := doInstanceGET(httpClient, "https://openrouter.ai/api/v1/auth/key",
			map[string]string{"Authorization": "Bearer " + orKey})
		orResult.HTTPStatus = code
		orResult.LatencyMS = latencyMS
		if err != nil {
			orResult.Error = fmt.Sprintf("เชื่อมต่อ OpenRouter ไม่ได้: %v", err)
		} else if code == http.StatusOK {
			orResult.OK = true
			// extract limit_remaining from JSON cheaply
			s := string(body)
			if i := strings.Index(s, `"limit_remaining":`); i >= 0 {
				rest := strings.TrimSpace(s[i+18:])
				end := strings.IndexAny(rest, ",}")
				if end > 0 {
					orResult.Detail = "credit คงเหลือ: " + strings.TrimSpace(rest[:end])
				}
			}
		} else {
			orResult.Error = "API key ไม่ถูกต้อง"
		}
	}

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

	allOK := checkPassed(smlResult) && checkPassed(lineResult) && checkPassed(orResult)
	c.JSON(http.StatusOK, gin.H{
		"ok":                allOK,
		"sml":               smlResult,
		"sml_proxy":         smlProxyResult,
		"sml_tenant":        smlTenantResult,
		"sml_stock_request": smlStockResult,
		"line":              lineResult,
		"openrouter":        orResult,
	})
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

func maskSecret(v string) string {
	if len(v) <= 8 {
		return "••••••••"
	}
	return v[:4] + "••••••••" + v[len(v)-4:]
}

func normalizeInstanceSetting(def settingDef, value string) (string, string) {
	value = strings.TrimSpace(value)
	switch def.Key {
	case "sml.rest_base_url":
		return normalizeInstanceURL(value)
	case "sml.stock_request_url":
		if value == "" {
			return "", "" // allow clear
		}
		return normalizeInstanceURL(value)
	case "sml.provider":
		if !smlProviderPattern.MatchString(value) {
			return "", "Provider ใช้ได้เฉพาะตัวอักษร ตัวเลข และ _ เท่านั้น"
		}
	case "sml.config_file":
		if !smlConfigFilePattern.MatchString(value) {
			return "", "Config file ใช้ได้เฉพาะตัวอักษร ตัวเลข จุด ขีดกลาง และ _ เท่านั้น"
		}
	case "sml.database":
		if !smlDatabaseNamePattern.MatchString(value) {
			return "", "Database (tenant) ใช้ได้เฉพาะตัวอักษร ตัวเลข และ _ เท่านั้น"
		}
	case "automation.auto_confirm_threshold":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || f < 0 || f > 1 {
			return "", "เกณฑ์ confidence ต้องเป็นตัวเลขระหว่าง 0 ถึง 1"
		}
		return floatString(f), ""
	}
	return value, ""
}

func normalizeInstanceURL(value string) (string, string) {
	value = strings.TrimSpace(value)
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "SML REST URL ต้องเป็น URL เต็ม เช่น http://192.168.2.109:8200"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "SML REST URL ต้องขึ้นต้นด้วย http:// หรือ https://"
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), ""
}

func floatString(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
