package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
	"nexflow/internal/services/smlprofile"
)

const shopeeSMLRouteBundlePreviewTTL = 10 * time.Minute

type shopeeSMLRouteSpec struct {
	Name     string
	Endpoint string
	Main     bool
}

var shopeeSMLRouteSpecs = map[string]shopeeSMLRouteSpec{
	"saleinvoice":       {Name: "saleinvoice", Endpoint: "/api/v1/ic/sale-invoices", Main: true},
	"saleorder":         {Name: "saleorder", Endpoint: "/api/v1/ic/sale-orders", Main: true},
	"saleinvoicecancel": {Name: "saleinvoicecancel", Endpoint: "/api/v1/ic/sale-invoices/:doc_no/void"},
	"creditnote":        {Name: "creditnote", Endpoint: "/api/v1/ic/sale-invoices/:doc_no/cancel"},
	"saleordercancel":   {Name: "saleordercancel", Endpoint: "/api/v1/ic/sale-orders/:doc_no/void"},
}

type shopeeSMLRouteBundleRequest struct {
	MainRoute                   string                `json:"main_route"`
	CancellationRoute           string                `json:"cancellation_route"`
	Main                        models.ChannelDefault `json:"main"`
	Cancellation                models.ChannelDefault `json:"cancellation"`
	ExpectedMainConfigVersion   int64                 `json:"expected_main_config_version"`
	ExpectedCancelConfigVersion int64                 `json:"expected_cancel_config_version"`
	PreviewToken                string                `json:"preview_token"`
}

type normalizedShopeeSMLRouteBundle struct {
	MainRoute         string
	CancellationRoute string
	Main              *models.ChannelDefault
	Cancellation      *models.ChannelDefault
	MainVersion       int64
	CancelVersion     int64
}

type shopeeSMLRouteBundlePreviewClaims struct {
	Tenant              string `json:"tenant"`
	PayloadHash         string `json:"payload_hash"`
	MainConfigVersion   int64  `json:"main_config_version"`
	CancelConfigVersion int64  `json:"cancel_config_version"`
	CapabilityRevision  string `json:"capability_revision"`
	MainMode            string `json:"main_mode"`
	CancellationMode    string `json:"cancellation_mode"`
	ExpiresAt           int64  `json:"expires_at"`
}

func (h *ChannelDefaultsHandler) GetShopeeSMLRouteBundle(c *gin.Context) {
	main, err := h.repo.Get("shopee_realtime", "sale")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดเส้นทางเอกสารหลักไม่สำเร็จ"})
		return
	}
	cancelRoute, err := h.repo.Get("shopee_realtime_cancel", "sale")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดเส้นทางเอกสารยกเลิกไม่สำเร็จ"})
		return
	}
	mainName := routeNameFromEndpoint(main, true)
	cancelName := routeNameFromEndpoint(cancelRoute, false)
	capability, capabilityErr := h.fetchBundleCapability(c)
	capabilityStatus := h.bundleCapabilityStatus(capability, capabilityErr, mainName, cancelName)
	c.JSON(http.StatusOK, gin.H{
		"main_route": mainName, "cancellation_route": cancelName,
		"main": main, "cancellation": cancelRoute,
		"main_config_version":   channelDefaultVersion(main),
		"cancel_config_version": channelDefaultVersion(cancelRoute),
		"route_modes":           h.bundleRouteModes(mainName, cancelName),
		"capability":            capabilityStatus,
		"readiness":             h.bundleReadiness(main, cancelRoute, mainName, cancelName, capability, capabilityErr),
	})
}

func (h *ChannelDefaultsHandler) PreviewShopeeSMLRouteBundle(c *gin.Context) {
	request, normalized, ok := h.bindAndNormalizeShopeeSMLRouteBundle(c)
	if !ok {
		return
	}
	capability, err := h.fetchBundleCapability(c)
	if err != nil {
		h.bundleCapabilityConflict(c, err)
		return
	}
	if err := sml.ValidateGatewayProfileCapability(capability, h.profileRouteModes,
		[]string{normalized.MainRoute, normalized.CancellationRoute}, false); err != nil {
		h.bundleCapabilityConflict(c, err)
		return
	}
	payloadHash, err := normalized.bundleHash()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "สร้างข้อมูล Preview ไม่สำเร็จ"})
		return
	}
	expiresAt := time.Now().Add(shopeeSMLRouteBundlePreviewTTL)
	claims := h.bundlePreviewClaims(normalized, payloadHash, capability.ContractRevision, expiresAt)
	token, err := signShopeeSMLRouteBundlePreview(claims, h.previewSigningKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ระบบลงนาม Preview ยังไม่พร้อม"})
		return
	}
	_ = request
	c.JSON(http.StatusOK, gin.H{
		"preview_token": token, "expires_at": expiresAt,
		"main_route": normalized.MainRoute, "cancellation_route": normalized.CancellationRoute,
		"main": normalized.Main, "cancellation": normalized.Cancellation,
		"main_config_version": normalized.MainVersion, "cancel_config_version": normalized.CancelVersion,
		"capability_revision": capability.ContractRevision,
		"route_modes":         h.bundleRouteModes(normalized.MainRoute, normalized.CancellationRoute),
		"readiness":           h.bundleReadiness(normalized.Main, normalized.Cancellation, normalized.MainRoute, normalized.CancellationRoute, capability, nil),
		"warnings": []string{
			"การเปลี่ยนแปลงมีผลเฉพาะเอกสารใหม่",
			"เมื่อบันทึก ระบบจะหยุด Auto SML ทุก Shopee shop ที่เปิดอยู่เพื่อให้ตรวจสอบและยืนยันใหม่",
		},
	})
}

func (h *ChannelDefaultsHandler) UpdateShopeeSMLRouteBundle(c *gin.Context) {
	request, normalized, ok := h.bindAndNormalizeShopeeSMLRouteBundle(c)
	if !ok {
		return
	}
	if strings.TrimSpace(request.PreviewToken) == "" {
		c.JSON(http.StatusConflict, gin.H{"code": "preview_required", "error": "กรุณาตรวจสอบค่าก่อนบันทึก"})
		return
	}
	capability, err := h.fetchBundleCapability(c)
	if err != nil {
		h.bundleCapabilityConflict(c, err)
		return
	}
	if err := sml.ValidateGatewayProfileCapability(capability, h.profileRouteModes,
		[]string{normalized.MainRoute, normalized.CancellationRoute}, false); err != nil {
		h.bundleCapabilityConflict(c, err)
		return
	}
	payloadHash, err := normalized.bundleHash()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ตรวจสอบข้อมูล Preview ไม่สำเร็จ"})
		return
	}
	expectedClaims := h.bundlePreviewClaims(normalized, payloadHash, capability.ContractRevision, time.Time{})
	if err := validateShopeeSMLRouteBundlePreview(request.PreviewToken, h.previewSigningKey, expectedClaims, time.Now()); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": "preview_stale", "error": "ค่าหรือความพร้อมของระบบเปลี่ยนหลัง Preview กรุณาตรวจสอบใหม่"})
		return
	}

	beforeMain, err := h.repo.Get("shopee_realtime", "sale")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ตรวจรุ่นการตั้งค่าเอกสารหลักไม่สำเร็จ"})
		return
	}
	beforeCancel, err := h.repo.Get("shopee_realtime_cancel", "sale")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ตรวจรุ่นการตั้งค่าเอกสารยกเลิกไม่สำเร็จ"})
		return
	}
	if channelDefaultVersion(beforeMain) != normalized.MainVersion || channelDefaultVersion(beforeCancel) != normalized.CancelVersion {
		c.JSON(http.StatusConflict, gin.H{"code": "preview_stale", "error": "การตั้งค่าถูกแก้ไขแล้ว กรุณาโหลดและ Preview ใหม่"})
		return
	}
	normalized.Main.ConfigVersion = normalized.MainVersion + 1
	normalized.Cancellation.ConfigVersion = normalized.CancelVersion + 1
	auditDetail := map[string]interface{}{
		"before": map[string]interface{}{
			"main":         safeChannelDefaultAudit(beforeMain, h.modeForRoute(routeNameFromEndpoint(beforeMain, true))),
			"cancellation": safeChannelDefaultAudit(beforeCancel, h.modeForRoute(routeNameFromEndpoint(beforeCancel, false))),
		},
		"after": map[string]interface{}{
			"main":         safeChannelDefaultAudit(normalized.Main, h.modeForRoute(normalized.MainRoute)),
			"cancellation": safeChannelDefaultAudit(normalized.Cancellation, h.modeForRoute(normalized.CancellationRoute)),
		},
		"main_route": normalized.MainRoute, "cancellation_route": normalized.CancellationRoute,
		"capability_revision":                capability.ContractRevision,
		"auto_sml_paused_for_reconfirmation": true,
	}
	result, err := h.repo.UpdateShopeeSMLRouteBundle(c.Request.Context(), repository.ShopeeSMLRouteBundleUpdate{
		Main: normalized.Main, Cancellation: normalized.Cancellation,
		ExpectedMainVersion: normalized.MainVersion, ExpectedCancelVersion: normalized.CancelVersion,
		UpdatedBy: c.GetString("user_id"), TraceID: c.GetString("trace_id"), AuditDetail: auditDetail,
	})
	if errors.Is(err, repository.ErrConfigVersionConflict) {
		c.JSON(http.StatusConflict, gin.H{"code": "preview_stale", "error": "การตั้งค่าถูกแก้ไขแล้ว กรุณาโหลดและ Preview ใหม่"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกชุดเส้นทาง Shopee ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"main": result.Main, "cancellation": result.Cancellation,
		"main_route": normalized.MainRoute, "cancellation_route": normalized.CancellationRoute,
		"paused_shops": result.PausedShops,
		"message":      "บันทึกเส้นทางแล้ว กรุณาตรวจสอบก่อนเปิด Auto SML อีกครั้ง",
	})
}

func (h *ChannelDefaultsHandler) bindAndNormalizeShopeeSMLRouteBundle(c *gin.Context) (*shopeeSMLRouteBundleRequest, *normalizedShopeeSMLRouteBundle, bool) {
	var request shopeeSMLRouteBundleRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, channelDefaultRequestLimit)
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลชุดเส้นทางไม่ถูกต้อง"})
		return nil, nil, false
	}
	normalized, err := normalizeShopeeSMLRouteBundle(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return nil, nil, false
	}
	return &request, normalized, true
}

func normalizeShopeeSMLRouteBundle(request shopeeSMLRouteBundleRequest) (*normalizedShopeeSMLRouteBundle, error) {
	mainRoute := strings.ToLower(strings.TrimSpace(request.MainRoute))
	cancelRoute := strings.ToLower(strings.TrimSpace(request.CancellationRoute))
	mainSpec, mainOK := shopeeSMLRouteSpecs[mainRoute]
	cancelSpec, cancelOK := shopeeSMLRouteSpecs[cancelRoute]
	if !mainOK || !mainSpec.Main {
		return nil, fmt.Errorf("เอกสารหลักต้องเป็นใบสั่งขายหรือขายสินค้าและบริการ")
	}
	if !cancelOK || cancelSpec.Main {
		return nil, fmt.Errorf("เอกสารเมื่อยกเลิกไม่อยู่ในรายการที่ระบบรองรับ")
	}
	if (mainRoute == "saleorder" && cancelRoute != "saleordercancel") ||
		(mainRoute == "saleinvoice" && cancelRoute != "saleinvoicecancel" && cancelRoute != "creditnote") {
		return nil, fmt.Errorf("เอกสารหลักและเอกสารเมื่อยกเลิกใช้ร่วมกันไม่ได้")
	}
	if request.ExpectedMainConfigVersion < 0 || request.ExpectedCancelConfigVersion < 0 {
		return nil, fmt.Errorf("config version ต้องเป็นศูนย์หรือมากกว่า")
	}
	if endpoint := strings.TrimSpace(request.Main.Endpoint); endpoint != "" && endpoint != mainSpec.Endpoint {
		return nil, fmt.Errorf("ปลายทางเอกสารหลักไม่ถูกต้อง กรุณาเลือกจากรายการที่ระบบรองรับ")
	}
	if endpoint := strings.TrimSpace(request.Cancellation.Endpoint); endpoint != "" && endpoint != cancelSpec.Endpoint {
		return nil, fmt.Errorf("ปลายทางเอกสารเมื่อยกเลิกไม่ถูกต้อง กรุณาเลือกจากรายการที่ระบบรองรับ")
	}
	main := request.Main
	main.Channel, main.BillType, main.Endpoint = "shopee_realtime", "sale", mainSpec.Endpoint
	cancel := request.Cancellation
	cancel.Channel, cancel.BillType, cancel.Endpoint = "shopee_realtime_cancel", "sale", cancelSpec.Endpoint
	// Cancellation authority comes from the immutable source SML document. Only
	// destination and document numbering are configurable, so stale UI fields
	// can never override customer, branch, VAT, warehouse, or remarks.
	cancel.PartyCode, cancel.PartyName, cancel.PartyPhone, cancel.PartyAddress, cancel.PartyTaxID = "", "", "", "", ""
	cancel.BranchCode, cancel.SaleCode, cancel.UnitCode, cancel.DocTime = "", "", "", ""
	cancel.WHCode, cancel.ShelfCode, cancel.VATType, cancel.VATRate, cancel.InquiryType = "", "", -1, -1, -1
	cancel.Remark, cancel.Remark2 = "", ""
	cancel.ShippingItemEnabled, cancel.ShippingItemCode, cancel.ShippingItemUnitCode = false, "", ""
	trimBundleChannelDefault(&main)
	trimBundleChannelDefault(&cancel)
	mainUpsert := channelDefaultModelToUpsert(main, request.ExpectedMainConfigVersion)
	if err := normalizeAndValidateChannelDefault(&mainUpsert); err != nil {
		return nil, err
	}
	cancelUpsert := channelDefaultModelToUpsert(cancel, request.ExpectedCancelConfigVersion)
	if err := normalizeAndValidateChannelDefault(&cancelUpsert); err != nil {
		return nil, err
	}
	main = *channelDefaultFromUpsert(mainUpsert)
	cancel = *channelDefaultFromUpsert(cancelUpsert)
	return &normalizedShopeeSMLRouteBundle{
		MainRoute: mainRoute, CancellationRoute: cancelRoute,
		Main: &main, Cancellation: &cancel,
		MainVersion: request.ExpectedMainConfigVersion, CancelVersion: request.ExpectedCancelConfigVersion,
	}, nil
}

func channelDefaultModelToUpsert(d models.ChannelDefault, expected int64) models.ChannelDefaultUpsert {
	return models.ChannelDefaultUpsert{
		Channel: d.Channel, BillType: d.BillType, PartyCode: d.PartyCode, PartyName: d.PartyName,
		PartyPhone: d.PartyPhone, PartyAddress: d.PartyAddress, PartyTaxID: d.PartyTaxID,
		DocFormatCode: d.DocFormatCode, Endpoint: d.Endpoint, DocPrefix: d.DocPrefix,
		DocRunningFormat: d.DocRunningFormat, BranchCode: d.BranchCode, SaleCode: d.SaleCode,
		UnitCode: d.UnitCode, DocTime: d.DocTime, ShippingItemEnabled: d.ShippingItemEnabled,
		ShippingItemCode: d.ShippingItemCode, ShippingItemUnitCode: d.ShippingItemUnitCode,
		PassbookCode: d.PassbookCode, PassbookName: d.PassbookName, BankCode: d.BankCode,
		BankBranch: d.BankBranch, ExpenseCode: d.ExpenseCode, ExpenseName: d.ExpenseName,
		WHCode: d.WHCode, ShelfCode: d.ShelfCode, VATType: d.VATType, VATRate: d.VATRate,
		InquiryType: d.InquiryType, Remark: d.Remark, Remark2: d.Remark2,
		ExpectedConfigVersion: &expected,
	}
}

func trimBundleChannelDefault(d *models.ChannelDefault) {
	if d == nil {
		return
	}
	fields := []*string{
		&d.PartyCode, &d.PartyName, &d.PartyPhone, &d.PartyAddress, &d.PartyTaxID,
		&d.DocFormatCode, &d.DocPrefix, &d.DocRunningFormat, &d.BranchCode, &d.SaleCode,
		&d.UnitCode, &d.DocTime, &d.ShippingItemCode, &d.ShippingItemUnitCode,
		&d.PassbookCode, &d.PassbookName, &d.BankCode, &d.BankBranch, &d.ExpenseCode,
		&d.ExpenseName, &d.WHCode, &d.ShelfCode, &d.Remark, &d.Remark2,
	}
	for _, field := range fields {
		*field = strings.TrimSpace(*field)
	}
}

func (n normalizedShopeeSMLRouteBundle) bundleHash() (string, error) {
	main, cancel := *n.Main, *n.Cancellation
	main.ConfigVersion, cancel.ConfigVersion = 0, 0
	main.UpdatedAt, cancel.UpdatedAt = time.Time{}, time.Time{}
	main.UpdatedBy, cancel.UpdatedBy = nil, nil
	body, err := json.Marshal(struct {
		MainRoute         string                `json:"main_route"`
		CancellationRoute string                `json:"cancellation_route"`
		Main              models.ChannelDefault `json:"main"`
		Cancellation      models.ChannelDefault `json:"cancellation"`
	}{n.MainRoute, n.CancellationRoute, main, cancel})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func (h *ChannelDefaultsHandler) bundlePreviewClaims(n *normalizedShopeeSMLRouteBundle, payloadHash, revision string, expiresAt time.Time) shopeeSMLRouteBundlePreviewClaims {
	claims := shopeeSMLRouteBundlePreviewClaims{
		Tenant: h.tenantKey, PayloadHash: payloadHash,
		MainConfigVersion: n.MainVersion, CancelConfigVersion: n.CancelVersion,
		CapabilityRevision: revision, MainMode: h.modeForRoute(n.MainRoute),
		CancellationMode: h.modeForRoute(n.CancellationRoute),
	}
	if !expiresAt.IsZero() {
		claims.ExpiresAt = expiresAt.Unix()
	}
	return claims
}

func signShopeeSMLRouteBundlePreview(claims shopeeSMLRouteBundlePreviewClaims, secret string) (string, error) {
	if len(secret) < 32 || strings.TrimSpace(claims.Tenant) == "" {
		return "", errors.New("route bundle preview signing is not configured")
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func validateShopeeSMLRouteBundlePreview(token, secret string, expected shopeeSMLRouteBundlePreviewClaims, now time.Time) error {
	if len(secret) < 32 {
		return errors.New("preview signing key is not configured")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return errors.New("preview token is invalid")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("preview token is invalid")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return errors.New("preview token signature is invalid")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return errors.New("preview token is invalid")
	}
	var actual shopeeSMLRouteBundlePreviewClaims
	if err := json.Unmarshal(body, &actual); err != nil || actual.ExpiresAt <= now.Unix() {
		return errors.New("preview token expired or invalid")
	}
	expiresAt := actual.ExpiresAt
	actual.ExpiresAt, expected.ExpiresAt = 0, 0
	if actual != expected || expiresAt <= now.Unix() {
		return errors.New("settings changed after preview")
	}
	return nil
}

func routeNameFromEndpoint(d *models.ChannelDefault, main bool) string {
	if d == nil {
		return ""
	}
	endpoint := strings.TrimSpace(d.Endpoint)
	for name, spec := range shopeeSMLRouteSpecs {
		if spec.Main == main && endpoint == spec.Endpoint {
			return name
		}
	}
	return ""
}

func channelDefaultVersion(d *models.ChannelDefault) int64 {
	if d == nil {
		return 0
	}
	return d.ConfigVersion
}

func (h *ChannelDefaultsHandler) modeForRoute(route string) string {
	if mode, ok := h.profileRouteModes[route]; ok {
		return mode
	}
	return smlprofile.ModeOff
}

func (h *ChannelDefaultsHandler) bundleRouteModes(mainRoute, cancelRoute string) map[string]string {
	out := make(map[string]string, len(shopeeSMLRouteSpecs))
	for route := range shopeeSMLRouteSpecs {
		out[route] = h.modeForRoute(route)
	}
	return out
}

func (h *ChannelDefaultsHandler) fetchBundleCapability(c *gin.Context) (*sml.GatewayCapabilities, error) {
	if h.capabilityClient == nil {
		return nil, errors.New("SML Gateway capability client is not configured")
	}
	return h.capabilityClient.Fetch(c.Request.Context())
}

func (h *ChannelDefaultsHandler) bundleCapabilityStatus(capability *sml.GatewayCapabilities, fetchErr error, mainRoute, cancelRoute string) gin.H {
	status := gin.H{"compatible": false, "revision": "", "message": "ยังตรวจสอบความพร้อมของ SML Gateway ไม่ได้"}
	if fetchErr != nil {
		return status
	}
	status["revision"] = capability.ContractRevision
	if mainRoute == "" || cancelRoute == "" {
		status["message"] = "กรุณาตั้งค่าเอกสารหลักและเอกสารเมื่อยกเลิกให้ครบ"
		return status
	}
	if err := sml.ValidateGatewayProfileCapability(capability, h.profileRouteModes, []string{mainRoute, cancelRoute}, false); err != nil {
		status["message"] = "SML Gateway รุ่นนี้ยังไม่รองรับชุดเส้นทางที่เลือก"
		return status
	}
	status["compatible"] = true
	status["message"] = "SML Gateway รองรับชุดเส้นทางนี้"
	return status
}

func (h *ChannelDefaultsHandler) bundleReadiness(main, cancelRoute *models.ChannelDefault, mainName, cancelName string, capability *sml.GatewayCapabilities, capabilityErr error) gin.H {
	pairCompatible := (mainName == "saleorder" && cancelName == "saleordercancel") ||
		(mainName == "saleinvoice" && (cancelName == "saleinvoicecancel" || cancelName == "creditnote"))
	configured := main != nil && cancelRoute != nil && len(channelDefaultMissingPrerequisites(main)) == 0 &&
		strings.TrimSpace(cancelRoute.DocFormatCode) != "" && strings.TrimSpace(cancelRoute.DocPrefix) != "" &&
		strings.Contains(cancelRoute.DocRunningFormat, "#")
	capabilityCompatible := false
	if capabilityErr == nil && pairCompatible {
		capabilityCompatible = sml.ValidateGatewayProfileCapability(capability, h.profileRouteModes, []string{mainName, cancelName}, false) == nil
	}
	automationReady := configured && capabilityCompatible &&
		h.modeForRoute(mainName) == smlprofile.ModeActive && h.modeForRoute(cancelName) == smlprofile.ModeActive
	return gin.H{
		"configured": configured, "pair_compatible": pairCompatible,
		"capability_compatible": capabilityCompatible, "automation_ready": automationReady,
	}
}

func (h *ChannelDefaultsHandler) bundleCapabilityConflict(c *gin.Context, err error) {
	if h.logger != nil {
		h.logger.Warn("Shopee SML route bundle capability mismatch")
	}
	c.JSON(http.StatusConflict, gin.H{
		"code":  "capability_mismatch",
		"error": "SML Gateway ยังไม่พร้อมสำหรับชุดเส้นทางนี้ กรุณาติดต่อผู้ดูแลระบบ",
	})
}
