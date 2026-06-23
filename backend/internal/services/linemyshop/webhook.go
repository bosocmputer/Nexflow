package linemyshop

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	EventReadyToShip            = "ORDER.READY_TO_SHIP"
	EventPendingPayment         = "ORDER.PENDING_PAYMENT"
	EventCanceled               = "ORDER.CANCELED"
	EventCompleted              = "ORDER.COMPLETED"
	EventShippingAddressUpdated = "ORDER_DETAIL.SHIPPING_ADDRESS_UPDATED"
	OrderStatusFinalized        = "FINALIZED"
	OrderStatusCanceled         = "CANCELED"
	PaymentStatusPaid           = "PAID"
	PaymentStatusPending        = "PENDING"
	PaymentMethodCOD            = "COD"
	ShipmentStatusReadyToShip   = "SHIPMENT_READY"
	ShipmentStatusShippedAll    = "SHIPPED_ALL"
)

type WebhookPayload struct {
	Event           WebhookEvent       `json:"event"`
	OrderNumber     string             `json:"orderNumber"`
	OrderStatus     string             `json:"orderStatus"`
	PaymentMethod   string             `json:"paymentMethod"`
	PaymentStatus   string             `json:"paymentStatus"`
	ShipmentStatus  string             `json:"shipmentStatus"`
	OrderItems      []WebhookOrderItem `json:"orderItems"`
	ShipmentDetail  ShipmentDetail     `json:"shipmentDetail"`
	ShipmentPrice   float64            `json:"shipmentPrice"`
	SubtotalPrice   float64            `json:"subtotalPrice"`
	DiscountAmount  float64            `json:"discountAmount"`
	TotalPrice      float64            `json:"totalPrice"`
	Shop            WebhookShop        `json:"shop"`
	Shipping        ShippingAddress    `json:"shippingAddress"`
	RemarkBuyer     string             `json:"remarkBuyer"`
	RemarkRecipient string             `json:"remarkRecipient"`
}

type WebhookEvent struct {
	Name      string `json:"name"`
	Timestamp string `json:"timestamp"`
}

type WebhookOrderItem struct {
	Barcode         string    `json:"barcode"`
	DiscountedPrice *float64  `json:"discountedPrice"`
	ImageURL        string    `json:"imageURL"`
	Name            string    `json:"name"`
	Price           float64   `json:"price"`
	ProductID       int64     `json:"productId"`
	Quantity        float64   `json:"quantity"`
	SKU             string    `json:"sku"`
	VariantID       int64     `json:"variantId"`
	Variants        []Variant `json:"variants"`
	Weight          float64   `json:"weight"`
}

type Variant struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ShipmentDetail struct {
	Description           string `json:"description"`
	IsAutoTracking        bool   `json:"isAutoTracking"`
	IsCOD                 bool   `json:"isCod"`
	Name                  string `json:"name"`
	ShipmentCompanyID     int64  `json:"shipmentCompanyId"`
	ShipmentCompanyNameEn string `json:"shipmentCompanyNameEn"`
	ShipmentCompanyNameTh string `json:"shipmentCompanyNameTh"`
	TrackingNumber        string `json:"trackingNumber"`
	TrackingURL           string `json:"trackingUrl"`
	Type                  string `json:"type"`
}

type ShippingAddress struct {
	Address       string `json:"address"`
	Country       string `json:"country"`
	District      string `json:"district"`
	Email         string `json:"email"`
	PhoneNumber   string `json:"phoneNumber"`
	PostalCode    string `json:"postalCode"`
	Province      string `json:"province"`
	RecipientName string `json:"recipientName"`
	SubDistrict   string `json:"subDistrict"`
}

type WebhookShop struct {
	ChannelID int64  `json:"channelId"`
	PremiumID string `json:"premiumId"`
	RandomID  string `json:"randomId"`
}

func DecodeWebhookPayload(body []byte) (WebhookPayload, error) {
	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return payload, err
	}
	payload.OrderNumber = strings.TrimSpace(payload.OrderNumber)
	payload.Event.Name = strings.TrimSpace(payload.Event.Name)
	payload.OrderStatus = strings.ToUpper(strings.TrimSpace(payload.OrderStatus))
	payload.PaymentStatus = strings.ToUpper(strings.TrimSpace(payload.PaymentStatus))
	payload.PaymentMethod = strings.ToUpper(strings.TrimSpace(payload.PaymentMethod))
	payload.ShipmentStatus = strings.ToUpper(strings.TrimSpace(payload.ShipmentStatus))
	return payload, nil
}

func (p WebhookPayload) EventTime() *time.Time {
	raw := strings.TrimSpace(p.Event.Timestamp)
	if raw == "" {
		return nil
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return &t
		}
	}
	if sec, err := strconv.ParseInt(raw, 10, 64); err == nil && sec > 0 {
		t := time.Unix(sec, 0).UTC()
		return &t
	}
	return nil
}

func (p WebhookPayload) EligibleForBill() bool {
	if p.OrderNumber == "" || strings.EqualFold(p.OrderStatus, OrderStatusCanceled) || p.Event.Name == EventCanceled {
		return false
	}
	if strings.EqualFold(p.PaymentStatus, PaymentStatusPaid) {
		return true
	}
	if strings.EqualFold(p.ShipmentStatus, ShipmentStatusReadyToShip) || strings.EqualFold(p.Event.Name, EventReadyToShip) {
		return true
	}
	return false
}

func (p WebhookPayload) ItemCount() int {
	if len(p.OrderItems) > 0 {
		return len(p.OrderItems)
	}
	return 0
}

func DedupeKey(connectionID, requestID string, payload WebhookPayload, rawBody []byte) string {
	connectionID = strings.TrimSpace(connectionID)
	requestID = strings.TrimSpace(requestID)
	orderNo := strings.TrimSpace(payload.OrderNumber)
	eventName := strings.TrimSpace(payload.Event.Name)
	eventTime := strings.TrimSpace(payload.Event.Timestamp)
	if requestID != "" {
		return fmt.Sprintf("line_myshop:webhook:%s:%s", connectionID, requestID)
	}
	h := sha1.Sum(rawBody)
	return fmt.Sprintf("line_myshop:webhook:%s:%s:%s:%s:%s", connectionID, orderNo, eventName, eventTime, hex.EncodeToString(h[:8]))
}

func VariantSuffix(vars []Variant) string {
	parts := []string{}
	for _, v := range vars {
		name := strings.TrimSpace(v.Name)
		value := strings.TrimSpace(v.Value)
		if name == "" && value == "" {
			continue
		}
		if name != "" && value != "" {
			parts = append(parts, name+": "+value)
		} else if value != "" {
			parts = append(parts, value)
		} else {
			parts = append(parts, name)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " / ")
}

func RawItemName(item WebhookOrderItem) string {
	name := strings.TrimSpace(item.Name)
	if suffix := VariantSuffix(item.Variants); suffix != "" {
		if name == "" {
			return suffix
		}
		return name + " (" + suffix + ")"
	}
	return name
}
