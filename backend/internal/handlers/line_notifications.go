package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	lineservice "nexflow/internal/services/line"
	linenotify "nexflow/internal/services/line_notifications"
)

type LineNotificationHandler struct {
	lineOARepo *repository.LineOAAccountRepo
	repo       *repository.LineNotificationRepo
	registry   *lineservice.Registry
	auditRepo  *repository.AuditLogRepo
	cfg        *config.Config
	logger     *zap.Logger
}

func NewLineNotificationHandler(
	lineOARepo *repository.LineOAAccountRepo,
	repo *repository.LineNotificationRepo,
	registry *lineservice.Registry,
	auditRepo *repository.AuditLogRepo,
	cfg *config.Config,
	logger *zap.Logger,
) *LineNotificationHandler {
	return &LineNotificationHandler{lineOARepo: lineOARepo, repo: repo, registry: registry, auditRepo: auditRepo, cfg: cfg, logger: logger}
}

func (h *LineNotificationHandler) Overview(c *gin.Context) {
	senders, err := h.lineOARepo.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด LINE OA ไม่สำเร็จ"})
		return
	}
	recipients, err := h.repo.ListRecipients(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดผู้รับแจ้งเตือนไม่สำเร็จ"})
		return
	}
	deliveries, err := h.repo.RecentDeliveries(c.Request.Context(), 12)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดประวัติการส่ง LINE ไม่สำเร็จ"})
		return
	}
	maskedSenders := make([]*models.LineOAAccount, 0, len(senders))
	enabledSenders := 0
	for _, sender := range senders {
		if sender.Enabled {
			enabledSenders++
		}
		maskedSenders = append(maskedSenders, maskAccount(sender))
	}
	enabledRecipients := 0
	for _, r := range recipients {
		if r.Enabled {
			enabledRecipients++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"senders":     maskedSenders,
		"recipients":  recipients,
		"deliveries":  deliveries,
		"sample_text": h.sampleMessage(),
		"readiness": gin.H{
			"sender_count":             len(senders),
			"enabled_sender_count":     enabledSenders,
			"recipient_count":          len(recipients),
			"enabled_recipient_count":  enabledRecipients,
			"shopee_realtime_enabled":  h.cfg != nil && h.cfg.ShopeeRealtimeOpsEnabled,
			"delivery_worker_interval": "15s",
		},
	})
}

func (h *LineNotificationHandler) CreateSender(c *gin.Context) {
	var in models.LineOAAccountUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(in.ChannelSecret) == "" || strings.TrimSpace(in.ChannelAccessToken) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณากรอก Channel secret และ Channel access token"})
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	a := &models.LineOAAccount{
		Name:               strings.TrimSpace(in.Name),
		ChannelSecret:      strings.TrimSpace(in.ChannelSecret),
		ChannelAccessToken: strings.TrimSpace(in.ChannelAccessToken),
		Enabled:            enabled,
	}
	if err := h.lineOARepo.Create(a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เพิ่ม LINE OA ไม่สำเร็จ"})
		return
	}
	h.tryFillBotUserID(a)
	h.reloadRegistry()
	h.audit(c, "line_notification_sender_created", a.ID, gin.H{"name": a.Name})
	c.JSON(http.StatusCreated, maskAccount(a))
}

func (h *LineNotificationHandler) UpdateSender(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	var in models.LineOAAccountUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, err := h.lineOARepo.Update(id, in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "แก้ไข LINE OA ไม่สำเร็จ: " + err.Error()})
		return
	}
	if strings.TrimSpace(in.ChannelAccessToken) != "" {
		h.tryFillBotUserID(updated)
	}
	h.reloadRegistry()
	h.audit(c, "line_notification_sender_updated", id, gin.H{"name": updated.Name})
	c.JSON(http.StatusOK, maskAccount(updated))
}

func (h *LineNotificationHandler) TestSender(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	a, err := h.lineOARepo.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด LINE OA ไม่สำเร็จ"})
		return
	}
	if a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบ LINE OA"})
		return
	}
	svc, err := lineservice.New(a.ChannelSecret, a.ChannelAccessToken, "")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "สร้าง LINE service ไม่สำเร็จ"})
		return
	}
	info, err := svc.GetBotInfo()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "ทดสอบ LINE OA ไม่สำเร็จ: " + err.Error()})
		return
	}
	if info != nil && info.UserID != "" {
		_ = h.lineOARepo.SetBotUserID(id, info.UserID)
		h.reloadRegistry()
	}
	h.audit(c, "line_notification_sender_tested", id, gin.H{"display_name": info.DisplayName, "basic_id": info.BasicID})
	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"bot_user_id":  info.UserID,
		"display_name": info.DisplayName,
		"basic_id":     info.BasicID,
		"premium_id":   info.PremiumID,
	})
}

func (h *LineNotificationHandler) CreateRecipient(c *gin.Context) {
	var in models.LineNotificationRecipientUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	row, err := h.repo.CreateRecipient(c.Request.Context(), in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "เพิ่มผู้รับแจ้งเตือนไม่สำเร็จ: " + err.Error()})
		return
	}
	h.audit(c, "line_notification_recipient_created", row.ID, gin.H{"name": row.Name, "destination_type": row.DestinationType})
	c.JSON(http.StatusCreated, row)
}

func (h *LineNotificationHandler) UpdateRecipient(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	var in models.LineNotificationRecipientUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	row, err := h.repo.UpdateRecipient(c.Request.Context(), id, in)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบผู้รับแจ้งเตือน"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "แก้ไขผู้รับแจ้งเตือนไม่สำเร็จ: " + err.Error()})
		return
	}
	h.audit(c, "line_notification_recipient_updated", row.ID, gin.H{"name": row.Name, "enabled": row.Enabled})
	c.JSON(http.StatusOK, row)
}

func (h *LineNotificationHandler) DeleteRecipient(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if err := h.repo.DeleteRecipient(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ลบผู้รับแจ้งเตือนไม่สำเร็จ: " + err.Error()})
		return
	}
	h.audit(c, "line_notification_recipient_deleted", id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *LineNotificationHandler) TestRecipient(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	recipient, err := h.repo.GetRecipient(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดผู้รับแจ้งเตือนไม่สำเร็จ"})
		return
	}
	if recipient == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบผู้รับแจ้งเตือน"})
		return
	}
	a, err := h.lineOARepo.Get(recipient.LineOAID)
	if err != nil || a == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่พบ LINE OA sender"})
		return
	}
	svc, err := lineservice.New(a.ChannelSecret, a.ChannelAccessToken, "")
	if err == nil {
		message := h.sampleMessage()
		altText, contents := linenotify.BuildShopeeNewOrderRichLineFlexWithPayment(sampleShopeeOrderSnapshot(), sampleShopeeOrderPayment(), h.publicBaseURL())
		if contents != nil {
			err = svc.PushFlex(recipient.DestinationID, altText, contents)
		}
		if err != nil || contents == nil {
			err = svc.PushText(recipient.DestinationID, message)
		}
	}
	if err != nil {
		_ = h.repo.MarkRecipientTest(c.Request.Context(), id, "failed", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": "ส่งข้อความทดสอบไม่สำเร็จ: " + err.Error()})
		return
	}
	_ = h.repo.MarkRecipientTest(c.Request.Context(), id, "sent", "")
	h.audit(c, "line_notification_recipient_tested", id, gin.H{"name": recipient.Name})
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "ส่งข้อความทดสอบแล้ว"})
}

func (h *LineNotificationHandler) sampleMessage() string {
	return linenotify.BuildShopeeNewOrderLineTextWithPayment(sampleShopeeOrderSnapshot(), sampleShopeeOrderPayment(), h.publicBaseURL())
}

func sampleShopeeOrderSnapshot() *models.ShopeeOrderSnapshot {
	return &models.ShopeeOrderSnapshot{
		ShopID:          264993963,
		ShopLabel:       "Henna.milkford",
		OrderSN:         "260621NDVGSKMA",
		PaymentMethod:   "Credit Card/Debit Card",
		TotalAmount:     245,
		ItemCount:       1,
		PackageNumber:   "OFG235736492235190",
		LogisticsStatus: "LOGISTICS_READY",
		ShippingCarrier: "EMS - Thailand Post",
		RawDetail: []byte(`{
		  "order_sn":"260621NDVGSKMA",
		  "payment_method":"Credit Card/Debit Card",
		  "cod":false,
		  "total_amount":245,
		  "create_time":1782037292,
		  "pay_time":1782037297,
		  "estimated_shipping_fee":35,
		  "package_list":[
		    {
		      "package_number":"OFG235736492235190",
		      "logistics_status":"LOGISTICS_READY",
		      "shipping_carrier":"EMS - Thailand Post"
		    }
		  ],
		  "item_list":[
		    {
		      "item_name":"ชุดใหญ่ 10 กรัม สีเพ้นคิ้วเฮนน่า",
		      "model_name":"B.น้ำตาลเข้ม",
		      "model_quantity_purchased":1,
		      "model_original_price":300,
		      "model_discounted_price":245
		    }
		  ]
		}`),
	}
}

func sampleShopeeOrderPayment() *models.ShopeeOrderPaymentSnapshot {
	return &models.ShopeeOrderPaymentSnapshot{
		ShopID:                 264993963,
		OrderSN:                "260621NDVGSKMA",
		Status:                 "ready",
		BuyerTotalAmount:       245,
		EscrowAmount:           263,
		DeductionAmount:        -18,
		CommissionFee:          42,
		SellerTransactionFee:   10,
		VoucherFromShopee:      60,
		BuyerPaidShippingFee:   15,
		SellerShippingDiscount: 20,
	}
}

func (h *LineNotificationHandler) publicBaseURL() string {
	if h == nil || h.cfg == nil {
		return ""
	}
	return h.cfg.PublicBaseURL
}

func (h *LineNotificationHandler) tryFillBotUserID(a *models.LineOAAccount) {
	if a == nil {
		return
	}
	svc, err := lineservice.New(a.ChannelSecret, a.ChannelAccessToken, "")
	if err != nil {
		return
	}
	info, err := svc.GetBotInfo()
	if err != nil || info == nil || info.UserID == "" {
		return
	}
	_ = h.lineOARepo.SetBotUserID(a.ID, info.UserID)
	a.BotUserID = info.UserID
}

func (h *LineNotificationHandler) reloadRegistry() {
	if h != nil && h.registry != nil {
		if err := h.registry.Reload(); err != nil && h.logger != nil {
			h.logger.Warn("line notification registry reload failed", zap.Error(err))
		}
	}
}

func (h *LineNotificationHandler) audit(c *gin.Context, action, targetID string, detail map[string]interface{}) {
	if h.auditRepo == nil {
		return
	}
	var userID *string
	if uid := c.GetString("user_id"); uid != "" {
		userID = &uid
	}
	id := strings.TrimSpace(targetID)
	_ = h.auditRepo.Log(models.AuditEntry{
		Action:   action,
		TargetID: &id,
		UserID:   userID,
		Source:   "line_notification",
		Level:    "info",
		TraceID:  c.GetString("trace_id"),
		Detail:   detail,
	})
}
