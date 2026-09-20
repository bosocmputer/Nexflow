package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
)

type tikTokCancellationStoreFake struct {
	evidence  repository.TikTokCancellationEvidenceRow
	record    *models.TikTokSMLCancellation
	state     string
	upserts   int
	starts    int
	completes int
}

func (f *tikTokCancellationStoreFake) Evidence(context.Context, string, string) (repository.TikTokCancellationEvidenceRow, error) {
	return f.evidence, nil
}
func (f *tikTokCancellationStoreFake) Latest(context.Context, string, string, string) (*models.TikTokSMLCancellation, error) {
	return f.record, nil
}
func (f *tikTokCancellationStoreFake) UpsertPreview(context.Context, repository.TikTokCancellationAttemptInput, json.RawMessage) (*models.TikTokSMLCancellation, error) {
	f.upserts++
	return f.record, nil
}
func (f *tikTokCancellationStoreFake) StartCreate(context.Context, repository.TikTokCancellationAttemptInput) (*models.TikTokSMLCancellation, string, error) {
	f.starts++
	return f.record, f.state, nil
}
func (f *tikTokCancellationStoreFake) Complete(context.Context, string, string, string, json.RawMessage, string, string) (*models.TikTokSMLCancellation, error) {
	f.completes++
	return f.record, nil
}

type tikTokCancellationRouteStoreFake struct{ route *models.ChannelDefault }

func (f tikTokCancellationRouteStoreFake) Get(string, string) (*models.ChannelDefault, error) {
	return f.route, nil
}

type tikTokCancellationClientFake struct {
	previewCalls int
	createCalls  int
}

func (f *tikTokCancellationClientFake) IsConfigured() bool { return true }
func (f *tikTokCancellationClientFake) Preview(context.Context, string, sml.SaleInvoiceCancelRequest) (int, *sml.SaleInvoiceCancelResponse, error) {
	f.previewCalls++
	return 200, tikTokCancellationSuccessResponse("SIC26090001"), nil
}
func (f *tikTokCancellationClientFake) CreateBytes(context.Context, string, sml.SaleInvoiceCancelKind, []byte, string) (int, *sml.SaleInvoiceCancelResponse, error) {
	f.createCalls++
	return 201, tikTokCancellationSuccessResponse("SIC26090001"), nil
}

func TestTikTokReviewedCancellationPreviewRequiresOwnedSaleEvidence(t *testing.T) {
	store := &tikTokCancellationStoreFake{evidence: reviewedTikTokCancellationEvidence()}
	store.evidence.SMLAttemptState = "unknown"
	client := &tikTokCancellationClientFake{}
	coordinator := newTikTokCancellationCoordinatorForTest(store, client, true)

	_, err := coordinator.Preview(t.Context(), "7494619203789490654", "586030483469993439", "user-1")
	if !errors.Is(err, errTikTokCancellationEvidenceBlocked) {
		t.Fatalf("err=%v", err)
	}
	if client.previewCalls != 0 || store.upserts != 0 {
		t.Fatalf("preview calls=%d upserts=%d", client.previewCalls, store.upserts)
	}
}

func TestTikTokReviewedCancellationCreateStopsBeforeAllocationWhenFeatureOff(t *testing.T) {
	store := &tikTokCancellationStoreFake{evidence: reviewedTikTokCancellationEvidence()}
	client := &tikTokCancellationClientFake{}
	coordinator := newTikTokCancellationCoordinatorForTest(store, client, false)
	allocations := 0
	coordinator.allocateDocNo = func(context.Context, *models.ChannelDefault, bool) (string, error) {
		allocations++
		return "SIC26090001", nil
	}

	_, err := coordinator.Create(t.Context(), TikTokCancellationCreateInput{
		ShopID: "7494619203789490654", OrderID: "586030483469993439", ReviewDigest: strings.Repeat("d", 64),
		ActorID: "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef", TraceID: "trace-1",
	})
	if !errors.Is(err, errTikTokCancellationFeatureDisabled) {
		t.Fatalf("err=%v", err)
	}
	if allocations != 0 || client.createCalls != 0 || store.starts != 0 {
		t.Fatalf("allocations=%d create=%d starts=%d", allocations, client.createCalls, store.starts)
	}
}

func TestTikTokReviewedCancellationCreateRejectsChangedEvidenceBeforeAllocation(t *testing.T) {
	store := &tikTokCancellationStoreFake{evidence: reviewedTikTokCancellationEvidence()}
	client := &tikTokCancellationClientFake{}
	coordinator := newTikTokCancellationCoordinatorForTest(store, client, true)
	allocations := 0
	coordinator.allocateDocNo = func(context.Context, *models.ChannelDefault, bool) (string, error) {
		allocations++
		return "SIC26090001", nil
	}

	_, err := coordinator.Create(t.Context(), TikTokCancellationCreateInput{
		ShopID: store.evidence.ShopID, OrderID: store.evidence.OrderID, ReviewDigest: strings.Repeat("d", 64),
		ActorID: "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef", TraceID: "trace-1",
	})
	if !errors.Is(err, errTikTokCancellationReviewChanged) {
		t.Fatalf("err=%v", err)
	}
	if allocations != 0 || client.createCalls != 0 || store.starts != 0 {
		t.Fatalf("allocations=%d create=%d starts=%d", allocations, client.createCalls, store.starts)
	}
}

func TestTikTokCancellationResultExposesOnlyOperatorSafeFields(t *testing.T) {
	createdBy := "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef"
	encoded, err := json.Marshal(tikTokCancellationResult(&models.TikTokSMLCancellation{
		ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", BillID: "03ee1216-acb4-4a88-842c-7edc6eb44292",
		SMLAttemptID: "11111111-1111-1111-1111-111111111111", SourceHash: strings.Repeat("s", 64),
		ReviewDigest: strings.Repeat("d", 64), CreatedBy: &createdBy, Status: "created",
		SaleSMLDocNo: "BF-INV26090001", CancelSMLDocNo: "SIC26090001", StockRecalcStatus: "pending",
	}))
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, forbidden := range []string{"aaaaaaaa-aaaa", "03ee1216", "11111111", strings.Repeat("s", 64), strings.Repeat("d", 64), createdBy} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked internal value %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"cancel_sml_doc_no":"SIC26090001"`) || !strings.Contains(body, `"stock_recalc_status":"pending"`) {
		t.Fatalf("response omitted operator fields: %s", body)
	}
}

func TestTikTokReviewedCancellationCreateUsesPersistedReviewedAttempt(t *testing.T) {
	evidence := reviewedTikTokCancellationEvidence()
	review := tikTokCancellationReviewEvidence(evidence, reviewedTikTokCancellationRoute())
	digest := tikTokCancellationReviewDigest(review)
	record := &models.TikTokSMLCancellation{
		ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ShopID: evidence.ShopID, OrderID: evidence.OrderID,
		BillID: evidence.BillID, SMLAttemptID: evidence.SMLAttemptID, SaleSMLDocNo: evidence.BillSMLDocNo,
		CancelSMLDocNo: "SIC26090001", Status: "previewed", SourceHash: evidence.SourceHash,
		ReviewDigest: digest, RouteEndpoint: reviewedTikTokCancellationRoute().Endpoint,
		RouteConfigVersion: reviewedTikTokCancellationRoute().ConfigVersion,
		RouteSignature:     tikTokCancellationRouteSignature(reviewedTikTokCancellationRoute()),
		RequestPayload:     json.RawMessage(`{"kind":"reviewed"}`), StockRecalcStatus: "not_required",
	}
	store := &tikTokCancellationStoreFake{evidence: evidence, record: record, state: repository.TikTokCancellationStartStarted}
	client := &tikTokCancellationClientFake{}
	coordinator := newTikTokCancellationCoordinatorForTest(store, client, true)

	result, err := coordinator.Create(t.Context(), TikTokCancellationCreateInput{
		ShopID: evidence.ShopID, OrderID: evidence.OrderID, ReviewDigest: digest,
		ActorID: "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef", TraceID: "trace-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.createCalls != 1 || store.starts != 1 || store.completes != 1 || result.CancelSMLDocNo != "SIC26090001" {
		t.Fatalf("result=%+v create=%d starts=%d completes=%d", result, client.createCalls, store.starts, store.completes)
	}
}

func newTikTokCancellationCoordinatorForTest(store *tikTokCancellationStoreFake, client *tikTokCancellationClientFake, enabled bool) *TikTokCancellationCoordinator {
	return &TikTokCancellationCoordinator{
		store: store, routes: tikTokCancellationRouteStoreFake{route: reviewedTikTokCancellationRoute()}, client: client,
		createEnabled: enabled, profileMode: "off",
		allocateDocNo: func(context.Context, *models.ChannelDefault, bool) (string, error) { return "SIC26090001", nil },
	}
}

func reviewedTikTokCancellationEvidence() repository.TikTokCancellationEvidenceRow {
	return repository.TikTokCancellationEvidenceRow{
		ShopID: "7494619203789490654", OrderID: "586030483469993439", OrderStatus: "CANCELLED", SourceHash: strings.Repeat("s", 64),
		LastSyncedAt: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC),
		BillID:       "03ee1216-acb4-4a88-842c-7edc6eb44292", BillSource: "tiktok", BillSourceAccountKey: "shop:7494619203789490654",
		BillSourceFlow: "tiktok_shop_api_reviewed", BillStatus: "sent", BillDocumentRoute: "saleinvoice", BillSMLDocNo: "BF-INV26090001",
		SMLAttemptID: "11111111-1111-1111-1111-111111111111", SMLAttemptState: "sent", SMLAttemptRoute: "saleinvoice", SMLAttemptDocNo: "BF-INV26090001",
	}
}

func reviewedTikTokCancellationRoute() *models.ChannelDefault {
	return &models.ChannelDefault{
		Channel: "tiktok_shop_cancel", BillType: "sale", Endpoint: "/api/v1/ic/sale-invoices/:doc_no/void",
		DocFormatCode: "SIC", DocPrefix: "SIC", DocRunningFormat: "YYMM####", ConfigVersion: 3,
	}
}

func tikTokCancellationSuccessResponse(docNo string) *sml.SaleInvoiceCancelResponse {
	raw := json.RawMessage(`{"success":true,"data":{"doc_no":"` + docNo + `"}}`)
	var response sml.SaleInvoiceCancelResponse
	_ = json.Unmarshal(raw, &response)
	return &response
}
