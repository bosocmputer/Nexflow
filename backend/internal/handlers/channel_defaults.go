package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/smlprofile"
)

const channelDefaultRequestLimit = 2 << 20

// ChannelDefaultsHandler exposes route/document defaults for channel_defaults.
type ChannelDefaultsHandler struct {
	repo                *repository.ChannelDefaultRepo
	auditRepo           *repository.AuditLogRepo
	logger              *zap.Logger
	purchaseFlowEnabled bool
	profileMode         string
}

func NewChannelDefaultsHandler(
	repo *repository.ChannelDefaultRepo,
	auditRepo *repository.AuditLogRepo,
	purchaseFlowEnabled bool,
	profileMode string,
	logger *zap.Logger,
) *ChannelDefaultsHandler {
	return &ChannelDefaultsHandler{
		repo:                repo,
		auditRepo:           auditRepo,
		purchaseFlowEnabled: purchaseFlowEnabled,
		profileMode:         profileMode,
		logger:              logger,
	}
}

// GET /api/settings/channel-defaults
func (h *ChannelDefaultsHandler) List(c *gin.Context) {
	rows, err := h.repo.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !h.purchaseFlowEnabled {
		filtered := rows[:0]
		for _, row := range rows {
			if row.BillType != "purchase" {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// PUT /api/settings/channel-defaults — upsert by (channel, bill_type)
func (h *ChannelDefaultsHandler) Upsert(c *gin.Context) {
	var in models.ChannelDefaultUpsert
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, channelDefaultRequestLimit)
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.purchaseFlowEnabled && in.BillType == "purchase" {
		featureGone(c, "ฝั่งซื้อถูกปิดใช้งานแล้ว")
		return
	}
	if !validChannelBillTypeCombo(in.Channel, in.BillType) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid channel/bill_type combo (e.g. shopee_shipped must be purchase)",
		})
		return
	}
	if err := normalizeAndValidateChannelDefault(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if in.ExpectedConfigVersion == nil || *in.ExpectedConfigVersion < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected_config_version must be zero or greater"})
		return
	}

	before, err := h.repo.Get(in.Channel, in.BillType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if (before == nil && *in.ExpectedConfigVersion != 0) ||
		(before != nil && before.ConfigVersion != *in.ExpectedConfigVersion) {
		c.JSON(http.StatusConflict, gin.H{
			"code":  "config_version_conflict",
			"error": "การตั้งค่าถูกแก้ไขแล้ว กรุณาโหลดข้อมูลล่าสุดและตรวจสอบอีกครั้ง",
		})
		return
	}

	d := channelDefaultFromUpsert(in)
	userID := c.GetString("user_id")
	pauseAutoSML := in.Channel == "shopee_realtime" && in.BillType == "sale"
	var updated *models.ChannelDefault
	if before == nil {
		updated, err = h.repo.CreateExpected(d, userID, pauseAutoSML)
	} else {
		updated, err = h.repo.UpdateExpected(d, userID, *in.ExpectedConfigVersion, pauseAutoSML)
	}
	if errors.Is(err, repository.ErrConfigVersionConflict) {
		c.JSON(http.StatusConflict, gin.H{
			"code":  "config_version_conflict",
			"error": "การตั้งค่าถูกแก้ไขแล้ว กรุณาโหลดข้อมูลล่าสุดและตรวจสอบอีกครั้ง",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.audit(c, "channel_default_updated", map[string]interface{}{
		"channel": in.Channel, "bill_type": in.BillType,
		"before":                             safeChannelDefaultAudit(before, h.profileMode),
		"after":                              safeChannelDefaultAudit(updated, h.profileMode),
		"auto_sml_paused_for_reconfirmation": pauseAutoSML,
	})
	c.JSON(http.StatusOK, updated)
}

type channelDefaultPreviewRequest struct {
	models.ChannelDefaultUpsert
	PreviewContext struct {
		Channel  string `json:"channel"`
		OrderRef string `json:"order_ref"`
		BillNo   string `json:"bill_no"`
	} `json:"preview_context"`
}

// Preview validates and resolves a proposed configuration without reading or
// mutating channel_defaults. Master-data refreshes are deliberately separate.
func (h *ChannelDefaultsHandler) Preview(c *gin.Context) {
	var in channelDefaultPreviewRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, channelDefaultRequestLimit)
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.purchaseFlowEnabled && in.BillType == "purchase" {
		featureGone(c, "ฝั่งซื้อถูกปิดใช้งานแล้ว")
		return
	}
	if !validChannelBillTypeCombo(in.Channel, in.BillType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel/bill_type combo"})
		return
	}
	if err := normalizeAndValidateChannelDefault(&in.ChannelDefaultUpsert); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	d := channelDefaultFromUpsert(in.ChannelDefaultUpsert)
	if in.ExpectedConfigVersion != nil {
		d.ConfigVersion = *in.ExpectedConfigVersion + 1
	}
	missing := channelDefaultMissingPrerequisites(d)
	profileVersion := ""
	if h.profileMode != smlprofile.ModeOff {
		profileVersion = smlprofile.Version
	}
	wireProfileVersion := ""
	if h.profileMode == smlprofile.ModeActive {
		wireProfileVersion = smlprofile.Version
	}
	c.JSON(http.StatusOK, gin.H{
		"profile_mode":    h.profileMode,
		"profile_version": profileVersion,
		"route_signature": smlprofile.RouteSignature(*d, h.profileMode),
		"resolved":        gin.H{"remark": d.Remark, "remark_2": d.Remark2},
		"system_fields": gin.H{
			"creator_code": "BILLFLOW", "cashier_code": "BILLFLOW", "user_request": "NEXFLOW",
			"remark_5":      "NEXFLOW|" + d.Channel + "|" + firstNonEmpty(in.PreviewContext.OrderRef, in.PreviewContext.BillNo),
			"currency_code": "THB", "exchange_rate": "1", "timezone": "Asia/Bangkok",
		},
		"payload": gin.H{
			"endpoint": d.Endpoint, "doc_format_code": d.DocFormatCode, "warehouse": d.WHCode,
			"location": d.ShelfCode, "vat_type": d.VATType, "vat_rate": d.VATRate,
			"party_code": d.PartyCode, "branch_code": d.BranchCode,
			"remark": d.Remark, "remark_2": d.Remark2, "document_profile_version": wireProfileVersion,
		},
		"missing_prerequisites": missing,
		"warnings":              previewWarnings(d, missing),
	})
}

func normalizeAndValidateChannelDefault(in *models.ChannelDefaultUpsert) error {
	if err := smlprofile.ValidateFreeText("remark", in.Remark); err != nil {
		return err
	}
	if err := smlprofile.ValidateFreeText("remark_2", in.Remark2); err != nil {
		return err
	}
	in.Remark = strings.TrimSpace(in.Remark)
	in.Remark2 = strings.TrimSpace(in.Remark2)
	in.ShippingItemCode = strings.TrimSpace(in.ShippingItemCode)
	in.ShippingItemUnitCode = strings.TrimSpace(in.ShippingItemUnitCode)
	in.PassbookCode = strings.TrimSpace(in.PassbookCode)
	in.PassbookName = strings.TrimSpace(in.PassbookName)
	in.BankCode = strings.TrimSpace(in.BankCode)
	in.BankBranch = strings.TrimSpace(in.BankBranch)
	in.ExpenseCode = strings.TrimSpace(in.ExpenseCode)
	in.ExpenseName = strings.TrimSpace(in.ExpenseName)
	if !supportsConfiguredShippingItem(in.Channel, in.BillType) {
		in.ShippingItemEnabled = false
		in.ShippingItemCode = ""
		in.ShippingItemUnitCode = ""
	}
	if in.Channel != "shopee_settlement" || in.BillType != "ar_receipt" {
		in.PassbookCode = ""
		in.PassbookName = ""
		in.BankCode = ""
		in.BankBranch = ""
		in.ExpenseCode = ""
		in.ExpenseName = ""
	} else {
		in.Endpoint = "/api/v1/ar/receipts"
		in.PartyCode = ""
		in.PartyName = ""
		in.PartyPhone = ""
		in.PartyAddress = ""
		in.PartyTaxID = ""
		in.BranchCode = ""
		in.SaleCode = ""
		in.UnitCode = ""
		in.DocTime = ""
		in.WHCode = ""
		in.ShelfCode = ""
		in.VATType = -1
		in.VATRate = -1
		in.InquiryType = -1
		in.ShippingItemEnabled = false
		in.ShippingItemCode = ""
		in.ShippingItemUnitCode = ""
		if strings.TrimSpace(in.DocFormatCode) == "" {
			return fmt.Errorf("กรุณาเลือกรูปแบบเอกสารรับชำระ (screen_code=EE)")
		}
		if strings.TrimSpace(in.PassbookCode) == "" {
			return fmt.Errorf("กรุณาเลือกบัญชีรับเงินสำหรับรับชำระ Shopee")
		}
		if strings.TrimSpace(in.DocPrefix) == "" {
			in.DocPrefix = strings.TrimSpace(in.DocFormatCode)
		}
		if strings.TrimSpace(in.DocRunningFormat) == "" {
			in.DocRunningFormat = "@YYMM####"
		}
	}
	if in.ShippingItemEnabled && in.ShippingItemCode == "" {
		return fmt.Errorf("กรุณาเลือกสินค้า SML สำหรับค่าจัดส่งก่อนเปิดใช้งาน")
	}
	if in.ShippingItemEnabled && in.ShippingItemUnitCode == "" {
		return fmt.Errorf("กรุณาเลือกหน่วย SML สำหรับค่าจัดส่งก่อนเปิดใช้งาน")
	}
	if err := validateShopeeRealtimeAutoDefaults(*in); err != nil {
		return err
	}
	if err := validateShopeeRealtimeCancelDefaults(*in); err != nil {
		return err
	}
	return nil
}

func channelDefaultFromUpsert(in models.ChannelDefaultUpsert) *models.ChannelDefault {
	return &models.ChannelDefault{
		Channel:          in.Channel,
		BillType:         in.BillType,
		PartyCode:        in.PartyCode,
		PartyName:        in.PartyName,
		PartyPhone:       in.PartyPhone,
		PartyAddress:     in.PartyAddress,
		PartyTaxID:       in.PartyTaxID,
		DocFormatCode:    in.DocFormatCode,
		Endpoint:         in.Endpoint,
		DocPrefix:        in.DocPrefix,
		DocRunningFormat: in.DocRunningFormat,
		BranchCode:       in.BranchCode,
		SaleCode:         in.SaleCode,
		UnitCode:         "",
		// Auto SML persists doc_time immediately before its first SML write.
		DocTime:              "",
		ShippingItemEnabled:  in.ShippingItemEnabled,
		ShippingItemCode:     in.ShippingItemCode,
		ShippingItemUnitCode: in.ShippingItemUnitCode,
		PassbookCode:         in.PassbookCode,
		PassbookName:         in.PassbookName,
		BankCode:             in.BankCode,
		BankBranch:           in.BankBranch,
		ExpenseCode:          in.ExpenseCode,
		ExpenseName:          in.ExpenseName,
		WHCode:               in.WHCode,
		ShelfCode:            in.ShelfCode,
		VATType:              in.VATType,
		VATRate:              in.VATRate,
		InquiryType:          in.InquiryType,
		Remark:               in.Remark,
		Remark2:              in.Remark2,
	}
}

func channelDefaultMissingPrerequisites(d *models.ChannelDefault) []string {
	if d.Channel != "shopee_realtime" || d.BillType != "sale" {
		return []string{}
	}
	missing := make([]string, 0)
	for _, field := range []struct{ value, label string }{
		{d.Endpoint, "ปลายทาง SML"}, {d.DocFormatCode, "รูปแบบเอกสาร"},
		{d.DocPrefix, "คำนำหน้าเลขเอกสาร"}, {d.DocRunningFormat, "รูปแบบเลขรัน"},
		{d.PartyCode, "ลูกค้า SML"}, {d.WHCode, "คลัง"}, {d.ShelfCode, "พื้นที่เก็บ"},
	} {
		if strings.TrimSpace(field.value) == "" {
			missing = append(missing, field.label)
		}
	}
	if d.VATType < 0 {
		missing = append(missing, "ประเภทภาษี")
	}
	if d.VATRate < 0 {
		missing = append(missing, "อัตราภาษี")
	}
	if d.ShippingItemEnabled && strings.TrimSpace(d.ShippingItemCode) == "" {
		missing = append(missing, "สินค้าค่าจัดส่ง")
	}
	if d.ShippingItemEnabled && strings.TrimSpace(d.ShippingItemUnitCode) == "" {
		missing = append(missing, "หน่วยค่าจัดส่ง")
	}
	return missing
}

func previewWarnings(d *models.ChannelDefault, missing []string) []string {
	warnings := []string{"การเปลี่ยนแปลงมีผลเฉพาะเอกสารใหม่"}
	if len(missing) > 0 {
		warnings = append(warnings, "ยังเปิด Auto SML ไม่ได้จนกว่าข้อมูลที่จำเป็นจะครบ")
	}
	if d.Channel == "shopee_realtime" {
		warnings = append(warnings, "หากเปลี่ยนเส้นทาง ระบบจะหยุด Auto SML และให้ตรวจสอบค่าก่อนเปิดใช้งานอีกครั้ง")
	}
	return warnings
}

func safeChannelDefaultAudit(d *models.ChannelDefault, mode string) map[string]interface{} {
	if d == nil {
		return nil
	}
	return map[string]interface{}{
		"config_version": d.ConfigVersion, "route_signature": smlprofile.RouteSignature(*d, mode),
		"endpoint": d.Endpoint, "doc_format_code": d.DocFormatCode, "doc_prefix": d.DocPrefix,
		"doc_running_format": d.DocRunningFormat, "wh_code": d.WHCode, "shelf_code": d.ShelfCode,
		"vat_type": d.VATType, "vat_rate": d.VATRate, "shipping_item_enabled": d.ShippingItemEnabled,
		"shipping_item_code": d.ShippingItemCode, "shipping_item_unit_code": d.ShippingItemUnitCode,
		"remark_configured": d.Remark != "", "remark_2_configured": d.Remark2 != "", "profile_mode": mode,
	}
}

func validateShopeeRealtimeAutoDefaults(in models.ChannelDefaultUpsert) error {
	if in.Channel != "shopee_realtime" || in.BillType != "sale" {
		return nil
	}
	required := []struct {
		value string
		label string
	}{
		{in.Endpoint, "ปลายทาง SML"},
		{in.DocFormatCode, "รูปแบบเอกสาร"},
		{in.DocPrefix, "คำนำหน้าเลขเอกสาร"},
		{in.DocRunningFormat, "รูปแบบเลขรัน"},
		{in.PartyCode, "ลูกค้า SML"},
		{in.WHCode, "คลัง"},
		{in.ShelfCode, "พื้นที่เก็บ"},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("กรุณาตั้งค่า%sสำหรับ Auto SML", field.label)
		}
	}
	if in.VATType < 0 {
		return fmt.Errorf("กรุณาตั้งค่าประเภทภาษีสำหรับ Auto SML")
	}
	if in.VATRate < 0 {
		return fmt.Errorf("กรุณาตั้งค่าอัตราภาษีสำหรับ Auto SML")
	}
	return nil
}

func validateShopeeRealtimeCancelDefaults(in models.ChannelDefaultUpsert) error {
	if in.Channel != "shopee_realtime_cancel" || in.BillType != "sale" {
		return nil
	}
	endpoint := strings.TrimSpace(in.Endpoint)
	switch endpoint {
	case "/api/v1/ic/sale-invoices/:doc_no/void",
		"/api/v1/ic/sale-invoices/:doc_no/cancel":
	default:
		return fmt.Errorf("กรุณาเลือกปลายทางยกเลิก SML ที่ระบบรองรับ")
	}
	required := []struct {
		value string
		label string
	}{
		{in.DocFormatCode, "รูปแบบเอกสารจาก SML"},
		{in.DocPrefix, "คำนำหน้าเลขเอกสาร"},
		{in.DocRunningFormat, "รูปแบบเลขรัน"},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("กรุณาเลือก%sสำหรับเส้นทาง Shopee ยกเลิกหลังส่ง SML", field.label)
		}
	}
	if !strings.Contains(in.DocRunningFormat, "#") {
		return fmt.Errorf("รูปแบบเลขรันของเอกสารยกเลิกต้องมี # อย่างน้อย 1 ตัว")
	}
	return nil
}

// supportsConfiguredShippingItem is intentionally narrow. Marketplace sale
// flows use this item only for the positive shipping amount paid by the buyer.
// Shopee uses buyer_paid_shipping_fee from escrow, not actual/estimated carrier
// cost. shopee_shipped remains the legacy purchase-email flow.
func supportsConfiguredShippingItem(channel, billType string) bool {
	if billType == "sale" && (channel == "shopee" || channel == "shopee_realtime" || channel == "lazada" || channel == "tiktok") {
		return true
	}
	return channel == "shopee_shipped" && billType == "purchase"
}

// validChannelBillTypeCombo enforces UI-level rules so admins can't save
// nonsensical pairs (shopee_shipped is purchase-only, etc.).
func validChannelBillTypeCombo(channel, billType string) bool {
	switch channel {
	case "shopee_settlement":
		return billType == "ar_receipt"
	case "shopee_shipped":
		return billType == "purchase"
	case "email":
		return billType == "sale" || billType == "purchase"
	case "shopee", "shopee_realtime", "shopee_realtime_cancel", "shopee_email", "line", "manual", "line_myshop":
		return billType == "sale"
	case "lazada":
		return billType == "sale" || billType == "purchase"
	case "tiktok":
		return billType == "sale"
	}
	return false
}

func (h *ChannelDefaultsHandler) audit(c *gin.Context, action string, detail map[string]interface{}) {
	if h.auditRepo == nil {
		return
	}
	var userID *string
	if uid := c.GetString("user_id"); uid != "" {
		userID = &uid
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action:  action,
		UserID:  userID,
		Source:  "channel_defaults",
		Level:   "info",
		TraceID: c.GetString("trace_id"),
		Detail:  detail,
	})
}
