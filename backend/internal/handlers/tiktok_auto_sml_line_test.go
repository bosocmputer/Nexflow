package handlers

import (
	"context"
	"strings"
	"testing"
	"time"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/tiktokshop"
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

type tikTokAutoSMLAuditFake struct{ entries []models.AuditEntry }

func (f *tikTokAutoSMLAuditFake) Log(entry models.AuditEntry) error {
	f.entries = append(f.entries, entry)
	return nil
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

type tikTokAutoSMLWorkStoreFake struct {
	setting            *models.TikTokAutoSMLSetting
	linkedBillID       string
	markedBillID       string
	markedBillJobID    string
	markedReviewDigest string
	transientJobID     string
	transientCode      string
	transientMessage   string
}

func (f *tikTokAutoSMLWorkStoreFake) ListSettings(context.Context) ([]models.TikTokAutoSMLSetting, error) {
	if f.setting == nil {
		return nil, nil
	}
	return []models.TikTokAutoSMLSetting{*f.setting}, nil
}
func (f *tikTokAutoSMLWorkStoreFake) GetSetting(context.Context, string) (*models.TikTokAutoSMLSetting, error) {
	return f.setting, nil
}
func (f *tikTokAutoSMLWorkStoreFake) UpdateSetting(context.Context, repository.TikTokAutoSMLSettingUpdate) (*models.TikTokAutoSMLSetting, error) {
	return f.setting, nil
}
func (f *tikTokAutoSMLWorkStoreFake) RetryJob(context.Context, string, string, string, string) error {
	return nil
}
func (f *tikTokAutoSMLWorkStoreFake) Enqueue(context.Context, repository.TikTokAutoSMLEnqueueInput) (bool, error) {
	return false, nil
}
func (f *tikTokAutoSMLWorkStoreFake) ListBillBacklogCandidates(context.Context, int) ([]repository.TikTokAutoSMLBacklogCandidate, error) {
	return nil, nil
}
func (f *tikTokAutoSMLWorkStoreFake) RecoverStaleJobs(context.Context) (int64, error) { return 0, nil }
func (f *tikTokAutoSMLWorkStoreFake) LeaseJobs(context.Context, int, time.Duration) ([]models.TikTokAutoSMLJob, error) {
	return nil, nil
}
func (f *tikTokAutoSMLWorkStoreFake) LinkBill(_ context.Context, _, billID, _ string) error {
	f.linkedBillID = billID
	return nil
}
func (f *tikTokAutoSMLWorkStoreFake) MarkBillCreated(_ context.Context, jobID, billID, reviewDigest string) error {
	f.markedBillJobID, f.markedBillID, f.markedReviewDigest = jobID, billID, reviewDigest
	return nil
}
func (f *tikTokAutoSMLWorkStoreFake) GetOrSetDocumentTime(context.Context, string, string) (string, error) {
	return "", nil
}
func (f *tikTokAutoSMLWorkStoreFake) MarkNeedsReview(context.Context, string, string, string, string) error {
	return nil
}
func (f *tikTokAutoSMLWorkStoreFake) MarkCancelled(context.Context, string, string, string) error {
	return nil
}
func (f *tikTokAutoSMLWorkStoreFake) MarkSucceeded(context.Context, string, string, string) error {
	return nil
}

func (f *tikTokAutoSMLWorkStoreFake) MarkTransientFailure(_ context.Context, jobID, code, message string, _ int) error {
	f.transientJobID, f.transientCode, f.transientMessage = jobID, code, message
	return nil
}
func (f *tikTokAutoSMLWorkStoreFake) PauseForRouteChange(context.Context, string) error { return nil }

func TestTikTokAutoBillDoesNotPromoteQueuedJobAfterSMLSettingChanges(t *testing.T) {
	actorID := "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef"
	preview := &tiktokshop.TikTokBillShadowPreview{
		ShopID: "7494619203789490654", OrderID: "586180035911386153", Currency: "THB",
		OrderStatus: tiktokshop.OrderStatusAwaitingCollection, ReadyForReviewedBill: true,
		ReviewDigest: strings.Repeat("d", 64),
		Route: tiktokshop.TikTokBillShadowRoute{
			Ready: true, ShippingReady: true, SemanticRoute: "sale_invoice", DocFormatCode: "SI",
			ConfigVersion: 2, ShippingItemCode: "AH-0061", ShippingItemUnitCode: "ชิ้น",
		},
		Amounts: tiktokshop.TikTokBillShadowAmounts{ProductSubtotal: "100.00", Shipping: "15.00", ProposedDocumentTotal: "115.00"},
		Items: []tiktokshop.TikTokBillShadowItem{{
			ProductID: "product-1", SKUID: "sku-1", Quantity: 1, UnitSalePrice: "100.00", LineTotal: "100.00",
			Mapping: tiktokshop.TikTokBillShadowItemMapping{Status: tiktokshop.TikTokBillShadowMappingReady, ItemCode: "AH-0001", UnitCode: "ชิ้น", SMLQuantity: "1", MappingRevision: 2},
		}},
	}
	fingerprint, err := tikTokAutoSMLBillFingerprint(preview)
	if err != nil {
		t.Fatal(err)
	}
	routeSignature := tikTokAutoSMLRouteSignature(preview.Route)
	store := &tikTokAutoSMLWorkStoreFake{setting: &models.TikTokAutoSMLSetting{
		ShopID: preview.ShopID, AutoBillEnabled: true, SMLEnabled: true, EnabledBy: &actorID,
		ConfigVersion: 3, RouteSignature: routeSignature,
	}}
	creator := &tenantTikTokReviewedBillCreatorFake{result: &tiktokshop.TikTokReviewedBillResult{BillID: "80834efe-8bc3-4109-b270-a139d418f747"}}
	controller := NewTikTokAutoSMLController(&config.Config{TikTokShopAutoSMLEnabled: true}, store, &tenantTikTokBillShadowPreviewerFake{result: preview}, creator, nil, nil, nil)
	controller.processJob(t.Context(), models.TikTokAutoSMLJob{
		ID: "65b124d5-570d-48d4-9741-d22f1f46f1ef", ShopID: preview.ShopID, OrderID: preview.OrderID,
		Status: models.TikTokAutoSMLRunning, Attempts: 1, TriggerConfigVersion: 2,
		BillFingerprint: fingerprint, RouteSignature: routeSignature,
	})

	if creator.calls != 1 || store.linkedBillID == "" || store.markedBillID != store.linkedBillID {
		t.Fatalf("creator=%d linked=%q marked=%q", creator.calls, store.linkedBillID, store.markedBillID)
	}
	if store.markedBillJobID != "65b124d5-570d-48d4-9741-d22f1f46f1ef" || store.markedReviewDigest != preview.ReviewDigest {
		t.Fatalf("unexpected completed job evidence: %#v", store)
	}
}

func TestTikTokAutoBillLinksAnExistingTikTokExcelBillWithoutSendingIt(t *testing.T) {
	actorID := "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef"
	preview := &tiktokshop.TikTokBillShadowPreview{
		ShopID: "7494619203789490654", OrderID: "586180035911386153", Currency: "THB",
		OrderStatus: tiktokshop.OrderStatusAwaitingCollection, ReadyForReviewedBill: true,
		ReviewDigest: strings.Repeat("e", 64),
		Route:        tiktokshop.TikTokBillShadowRoute{Ready: true, ShippingReady: true, SemanticRoute: "sale_invoice", DocFormatCode: "SI", ConfigVersion: 2, ShippingItemCode: "AH-0061", ShippingItemUnitCode: "ชิ้น"},
		Amounts:      tiktokshop.TikTokBillShadowAmounts{ProductSubtotal: "100.00", Shipping: "15.00", ProposedDocumentTotal: "115.00"},
		Items:        []tiktokshop.TikTokBillShadowItem{{ProductID: "product-1", SKUID: "sku-1", Quantity: 1, UnitSalePrice: "100.00", LineTotal: "100.00", Mapping: tiktokshop.TikTokBillShadowItemMapping{Status: tiktokshop.TikTokBillShadowMappingReady, ItemCode: "AH-0001", UnitCode: "ชิ้น", SMLQuantity: "1", MappingRevision: 2}}},
		ExistingBill: &tiktokshop.TikTokBillShadowExistingBill{ID: "0b5fa961-d42e-45ef-8b96-3ffdb5dc28a8", SourceAccountKey: "default", SourceFlow: "tiktok_excel"},
	}
	fingerprint, err := tikTokAutoSMLBillFingerprint(preview)
	if err != nil {
		t.Fatal(err)
	}
	routeSignature := tikTokAutoSMLRouteSignature(preview.Route)
	store := &tikTokAutoSMLWorkStoreFake{setting: &models.TikTokAutoSMLSetting{ShopID: preview.ShopID, AutoBillEnabled: true, SMLEnabled: true, EnabledBy: &actorID, ConfigVersion: 3, RouteSignature: routeSignature}}
	creator := &tenantTikTokReviewedBillCreatorFake{}
	controller := NewTikTokAutoSMLController(&config.Config{TikTokShopAutoSMLEnabled: true}, store, &tenantTikTokBillShadowPreviewerFake{result: preview}, creator, nil, nil, nil)
	job := models.TikTokAutoSMLJob{ID: "65b124d5-570d-48d4-9741-d22f1f46f1ef", ShopID: preview.ShopID, OrderID: preview.OrderID, Status: models.TikTokAutoSMLRunning, Attempts: 1, TriggerConfigVersion: 2, BillFingerprint: fingerprint, RouteSignature: routeSignature}

	controller.processJob(t.Context(), job)

	if creator.calls != 0 {
		t.Fatalf("creator calls=%d, want 0", creator.calls)
	}
	if store.markedBillJobID != job.ID || store.markedBillID != preview.ExistingBill.ID {
		t.Fatalf("existing Bill must terminate the job: %#v", store)
	}
}

func TestTikTokAutoSMLContinuesOwnedAPIBillWhenOrderBecomesEligible(t *testing.T) {
	actorID := "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef"
	billID := "013f0caa-1282-4a00-9e70-3326ec50a4bb"
	preview := &tiktokshop.TikTokBillShadowPreview{
		ShopID: "7494619203789490654", OrderID: "586313800340309351", Currency: "THB",
		OrderStatus: tiktokshop.OrderStatusAwaitingCollection, ReadyForReviewedBill: true,
		ReviewDigest: strings.Repeat("f", 64),
		Route:        tiktokshop.TikTokBillShadowRoute{Ready: true, ShippingReady: true, SemanticRoute: "sale_invoice", DocFormatCode: "BF-INV", ConfigVersion: 3, ShippingItemCode: "AH-0061", ShippingItemUnitCode: "ชิ้น"},
		Amounts:      tiktokshop.TikTokBillShadowAmounts{ProductSubtotal: "68.00", ProposedDocumentTotal: "68.00"},
		Items:        []tiktokshop.TikTokBillShadowItem{{ProductID: "product-1", SKUID: "sku-1", Quantity: 1, UnitSalePrice: "68.00", LineTotal: "68.00", Mapping: tiktokshop.TikTokBillShadowItemMapping{Status: tiktokshop.TikTokBillShadowMappingReady, ItemCode: "AH-0001", UnitCode: "ชิ้น", SMLQuantity: "1", MappingRevision: 3}}},
		ExistingBill: &tiktokshop.TikTokBillShadowExistingBill{ID: billID, Status: "pending", SourceAccountKey: "shop:7494619203789490654", SourceFlow: tiktokshop.TikTokReviewedBillFlow, DocumentRoute: "saleinvoice"},
	}
	fingerprint, err := tikTokAutoSMLBillFingerprint(preview)
	if err != nil {
		t.Fatal(err)
	}
	routeSignature := tikTokAutoSMLRouteSignature(preview.Route)
	store := &tikTokAutoSMLWorkStoreFake{setting: &models.TikTokAutoSMLSetting{
		ShopID: preview.ShopID, AutoBillEnabled: true, SMLEnabled: true, EnabledBy: &actorID,
		ConfigVersion: 3, RouteSignature: routeSignature,
	}}
	controller := NewTikTokAutoSMLController(&config.Config{TikTokShopAutoSMLEnabled: true, TikTokShopSMLSendEnabled: true}, store, &tenantTikTokBillShadowPreviewerFake{result: preview}, &tenantTikTokReviewedBillCreatorFake{}, nil, nil, nil)
	job := models.TikTokAutoSMLJob{
		ID: "604f36a2-c56a-41e9-9cc4-9cfcd2e95b76", ShopID: preview.ShopID, OrderID: preview.OrderID,
		Status: models.TikTokAutoSMLRunning, Attempts: 1, TriggerConfigVersion: 3,
		BillFingerprint: fingerprint, RouteSignature: routeSignature, BillID: &billID,
	}

	controller.processJob(t.Context(), job)

	if store.markedBillJobID != "" {
		t.Fatalf("owned API Bill must continue toward SML instead of returning to manual state: marked job=%q", store.markedBillJobID)
	}
	if store.transientJobID != job.ID || store.transientCode != "bill_sender_unavailable" {
		t.Fatalf("owned API Bill did not continue to the SML sender guard: job=%q code=%q message=%q", store.transientJobID, store.transientCode, store.transientMessage)
	}
}

func TestTikTokAutoSMLQueueAuditDoesNotUseNumericOrderIDAsUUIDTarget(t *testing.T) {
	audit := &tikTokAutoSMLAuditFake{}
	controller := NewTikTokAutoSMLController(&config.Config{}, nil, nil, nil, nil, audit, nil)
	controller.auditEvent("tiktok_auto_sml_queued", "info", models.TikTokAutoSMLJob{
		ShopID: "7494619203789490654", OrderID: "586228966518261658", TriggerConfigVersion: 3,
	}, nil)
	if len(audit.entries) != 1 || audit.entries[0].TargetID != nil {
		t.Fatalf("queue audit target must be nil until a Bill exists: %#v", audit.entries)
	}
	detail, ok := audit.entries[0].Detail.(map[string]interface{})
	if !ok || detail["order_id"] != "586228966518261658" || detail["shop_id"] != "7494619203789490654" {
		t.Fatalf("queue audit must preserve shop/order evidence in detail: %#v", audit.entries[0].Detail)
	}
}
