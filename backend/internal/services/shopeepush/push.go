package shopeepush

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidSignature = errors.New("Shopee push signature is invalid")

type Event struct {
	ShopID     int64     `json:"shop_id"`
	OrderSN    string    `json:"order_sn,omitempty"`
	Code       int       `json:"code"`
	PushName   string    `json:"push_name"`
	Status     string    `json:"status,omitempty"`
	UpdateTime time.Time `json:"update_time,omitempty"`
	Timestamp  time.Time `json:"timestamp,omitempty"`
	DedupeKey  string    `json:"dedupe_key"`
}

type Meta struct {
	Name            string
	RequiresOrderSN bool
	ShopLevel       bool
}

var CodeMeta = map[int]Meta{
	1:  {Name: "shop_authorization_push", ShopLevel: true},
	2:  {Name: "shop_authorization_canceled_push", ShopLevel: true},
	3:  {Name: "order_status_push", RequiresOrderSN: true},
	4:  {Name: "order_trackingno_push", RequiresOrderSN: true},
	12: {Name: "open_api_authorization_expiry", ShopLevel: true},
	15: {Name: "shipping_document_status_push", RequiresOrderSN: true},
	23: {Name: "booking_status_push", RequiresOrderSN: true},
	24: {Name: "booking_trackingno_push", RequiresOrderSN: true},
	25: {Name: "booking_shipping_document_status_push", RequiresOrderSN: true},
	30: {Name: "package_fulfillment_status_push", RequiresOrderSN: true},
	47: {Name: "package_info_push", RequiresOrderSN: true},
}

func Parse(body []byte) (Event, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Event{}, errors.New("payload is not JSON")
	}
	data, _ := raw["data"].(map[string]interface{})
	shopID := int64FromAny(raw["shop_id"])
	if shopID <= 0 {
		shopID = int64FromAny(data["shop_id"])
	}
	orderSN := firstString(data["ordersn"], data["order_sn"], raw["ordersn"], raw["order_sn"])
	code := int(int64FromAny(raw["code"]))
	meta := CodeMeta[code]
	status := firstString(data["status"], data["order_status"], raw["status"], raw["order_status"])
	updateTime := unixTime(data["update_time"])
	timestamp := unixTime(raw["timestamp"])
	if shopID <= 0 {
		return Event{}, errors.New("payload does not contain shop_id")
	}
	if orderSN == "" && meta.RequiresOrderSN {
		return Event{}, errors.New("payload does not contain order_sn")
	}
	sum := sha256.Sum256(body)
	dedupeKey := fmt.Sprintf("%d:%s:%d:%s:%d:%d:%s", shopID, orderSN, code, status, updateTime.Unix(), timestamp.Unix(), hex.EncodeToString(sum[:]))
	return Event{
		ShopID:     shopID,
		OrderSN:    orderSN,
		Code:       code,
		PushName:   Name(code),
		Status:     strings.ToUpper(status),
		UpdateTime: updateTime,
		Timestamp:  timestamp,
		DedupeKey:  dedupeKey,
	}, nil
}

// Verify accepts either the configured callback token or Shopee's HMAC form.
// Callback URLs must include the exact request URI used by Shopee.
func Verify(secret, token, signature string, callbackURLs []string, body []byte) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ErrInvalidSignature
	}
	if token = strings.TrimSpace(token); token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(secret)) == 1 {
		return nil
	}
	signature = NormalizeSignature(signature)
	if signature == "" {
		return ErrInvalidSignature
	}
	for _, callbackURL := range callbackURLs {
		callbackURL = strings.TrimSpace(callbackURL)
		if callbackURL == "" {
			continue
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(callbackURL + "|" + string(body)))
		expected := hex.EncodeToString(mac.Sum(nil))
		if subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) == 1 {
			return nil
		}
	}
	return ErrInvalidSignature
}

func NormalizeSignature(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	for _, prefix := range []string{"sha256=", "hmac-sha256 ", "bearer "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(value[len(prefix):])
		}
	}
	return value
}

func Name(code int) string {
	if meta, ok := CodeMeta[code]; ok {
		return meta.Name
	}
	return "unknown"
}

func IsOrder(code int) bool     { return CodeMeta[code].RequiresOrderSN }
func IsShopLevel(code int) bool { return CodeMeta[code].ShopLevel }

func int64FromAny(value interface{}) int64 {
	switch number := value.(type) {
	case float64:
		return int64(number)
	case int64:
		return number
	case int:
		return int64(number)
	case string:
		out, _ := strconv.ParseInt(strings.TrimSpace(number), 10, 64)
		return out
	default:
		return 0
	}
}

func unixTime(value interface{}) time.Time {
	seconds := int64FromAny(value)
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0)
}

func firstString(values ...interface{}) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}
