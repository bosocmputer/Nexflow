package handlers

import (
	"context"
	"testing"

	"nexflow/internal/config"
	"nexflow/internal/models"
)

type tikTokAutoSMLLineNotifierFake struct {
	kind         string
	notification models.TikTokAutoSMLNotification
	dedupeKey    string
}

func (f *tikTokAutoSMLLineNotifierFake) EnqueueTikTokShopAutoSMLSuccess(_ context.Context, in models.TikTokAutoSMLNotification, dedupeKey string) (int, error) {
	f.kind, f.notification, f.dedupeKey = "success", in, dedupeKey
	return 1, nil
}

func (f *tikTokAutoSMLLineNotifierFake) EnqueueTikTokShopAutoSMLReview(_ context.Context, in models.TikTokAutoSMLNotification, dedupeKey string) (int, error) {
	f.kind, f.notification, f.dedupeKey = "review", in, dedupeKey
	return 1, nil
}

func (f *tikTokAutoSMLLineNotifierFake) EnqueueTikTokShopAutoSMLFailure(_ context.Context, in models.TikTokAutoSMLNotification, dedupeKey string) (int, error) {
	f.kind, f.notification, f.dedupeKey = "failure", in, dedupeKey
	return 1, nil
}

type tikTokAutoSMLShopLabelFake struct{ label string }

func (f tikTokAutoSMLShopLabelFake) ActiveShopLabel(context.Context, string) (string, error) {
	return f.label, nil
}

func TestTikTokAutoSMLLineSuccessUsesSafeDocumentEvidence(t *testing.T) {
	notifier := &tikTokAutoSMLLineNotifierFake{}
	controller := NewTikTokAutoSMLController(&config.Config{TikTokShopLineEnabled: true}, nil, nil, nil, nil, nil, nil)
	controller.SetLineNotifier(notifier, tikTokAutoSMLShopLabelFake{label: "henna_milkford"})
	controller.enqueueLine(t.Context(), "success", models.TikTokAutoSMLJob{
		ShopID: "7494619203789490654", OrderID: "586180035911386153", SMLDocNo: "BF-INV26090001",
	}, &models.Bill{
		ID: "bill-123", TotalAmount: floatPtr(307.49), SMLDocNo: stringPtr("BF-INV26090001"),
		Items: []models.BillItem{
			{RawName: "สีเพ้นท์คิ้วมิวฟอร์ด", Qty: 2},
			{RawName: "ค่าจัดส่ง TikTok Shop", SourceSKU: models.TikTokShippingSourceSKU, Qty: 1},
		},
	}, "", "")

	if notifier.kind != "success" {
		t.Fatalf("kind=%q, want success", notifier.kind)
	}
	if notifier.notification.ShopName != "henna_milkford" || notifier.notification.SMLDocNo != "BF-INV26090001" {
		t.Fatalf("unexpected notification: %#v", notifier.notification)
	}
	if len(notifier.notification.Items) != 1 || notifier.notification.Items[0].ProductName != "สีเพ้นท์คิ้วมิวฟอร์ด" {
		t.Fatalf("shipping or invalid item leaked into notification: %#v", notifier.notification.Items)
	}
	if notifier.dedupeKey != "tiktok_shop:auto_sml:success:7494619203789490654:586180035911386153:BF-INV26090001" {
		t.Fatalf("dedupe=%q", notifier.dedupeKey)
	}
}

func floatPtr(v float64) *float64 { return &v }
func stringPtr(v string) *string  { return &v }
