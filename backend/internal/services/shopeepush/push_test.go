package shopeepush

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestParseOrderPush(t *testing.T) {
	body := []byte(`{"shop_id":264993963,"code":3,"timestamp":1783560000,"data":{"ordersn":"260707TEST","status":"READY_TO_SHIP","update_time":1783560001}}`)
	event, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if event.ShopID != 264993963 || event.OrderSN != "260707TEST" || event.PushName != "order_status_push" || event.DedupeKey == "" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestVerifyTokenAndSignature(t *testing.T) {
	secret := "push-secret"
	body := []byte(`{"shop_id":1}`)
	callbackURL := "https://shopee-gateway.nextstep-soft.com/webhook/shopee"
	if err := Verify(secret, secret, "", nil, body); err != nil {
		t.Fatalf("verify token: %v", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(callbackURL + "|" + string(body)))
	signature := hex.EncodeToString(mac.Sum(nil))
	if err := Verify(secret, "", signature, []string{callbackURL}, body); err != nil {
		t.Fatalf("verify signature: %v", err)
	}
	if err := Verify(secret, "wrong", "wrong", []string{callbackURL}, body); err == nil {
		t.Fatal("expected invalid signature")
	}
}
