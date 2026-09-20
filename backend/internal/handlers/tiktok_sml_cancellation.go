package handlers

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
	"nexflow/internal/services/smlprofile"
	"nexflow/internal/services/tiktokshop"
)

var (
	errTikTokCancellationEvidenceBlocked = errors.New("TikTok cancellation sale evidence is not eligible")
	errTikTokCancellationRouteBlocked    = errors.New("TikTok cancellation route is not ready")
	errTikTokCancellationFeatureDisabled = errors.New("TikTok cancellation document creation is disabled")
	errTikTokCancellationReviewChanged   = errors.New("TikTok cancellation review evidence changed")
	errTikTokCancellationBusy            = errors.New("TikTok cancellation creation is already running")
	errTikTokCancellationReconciliation  = errors.New("TikTok cancellation requires reconciliation")
)

type tikTokCancellationStore interface {
	Evidence(context.Context, string, string) (repository.TikTokCancellationEvidenceRow, error)
	Latest(context.Context, string, string, string) (*models.TikTokSMLCancellation, error)
	UpsertPreview(context.Context, repository.TikTokCancellationAttemptInput, json.RawMessage) (*models.TikTokSMLCancellation, error)
	StartCreate(context.Context, repository.TikTokCancellationAttemptInput) (*models.TikTokSMLCancellation, string, error)
	Complete(context.Context, string, string, string, json.RawMessage, string, string) (*models.TikTokSMLCancellation, error)
}

type tikTokCancellationRouteStore interface {
	Get(string, string) (*models.ChannelDefault, error)
}

type tikTokCancellationClient interface {
	IsConfigured() bool
	Preview(context.Context, string, sml.SaleInvoiceCancelRequest) (int, *sml.SaleInvoiceCancelResponse, error)
	CreateBytes(context.Context, string, sml.SaleInvoiceCancelKind, []byte, string) (int, *sml.SaleInvoiceCancelResponse, error)
}

type TikTokCancellationCoordinator struct {
	store         tikTokCancellationStore
	routes        tikTokCancellationRouteStore
	client        tikTokCancellationClient
	createEnabled bool
	profileMode   string
	allocateDocNo func(context.Context, *models.ChannelDefault, bool) (string, error)
	readiness     func(context.Context) error
	billH         *BillHandler
	logger        *zap.Logger
}

type TikTokCancellationPreview struct {
	Status             string                    `json:"status"`
	Message            string                    `json:"message"`
	CreateEnabled      bool                      `json:"create_enabled"`
	ReviewDigest       string                    `json:"review_digest"`
	ShopID             string                    `json:"shop_id"`
	OrderID            string                    `json:"order_id"`
	BillID             string                    `json:"bill_id"`
	SaleSMLDocNo       string                    `json:"sale_sml_doc_no"`
	PreviewCancelDocNo string                    `json:"preview_cancel_sml_doc_no"`
	Destination        string                    `json:"destination"`
	DocFormatCode      string                    `json:"doc_format_code"`
	RouteConfigVersion int64                     `json:"route_config_version"`
	Existing           *TikTokCancellationResult `json:"existing,omitempty"`
}

type TikTokCancellationResult struct {
	Status            string `json:"status"`
	SaleSMLDocNo      string `json:"sale_sml_doc_no"`
	CancelSMLDocNo    string `json:"cancel_sml_doc_no,omitempty"`
	StockRecalcStatus string `json:"stock_recalc_status"`
	StockRecalcError  string `json:"stock_recalc_error,omitempty"`
}

func tikTokCancellationResult(record *models.TikTokSMLCancellation) *TikTokCancellationResult {
	if record == nil {
		return nil
	}
	return &TikTokCancellationResult{
		Status: record.Status, SaleSMLDocNo: record.SaleSMLDocNo, CancelSMLDocNo: record.CancelSMLDocNo,
		StockRecalcStatus: record.StockRecalcStatus, StockRecalcError: record.StockRecalcError,
	}
}

type TikTokCancellationCreateInput struct {
	ShopID, OrderID, ReviewDigest, ActorID, TraceID string
}

func NewTikTokCancellationCoordinator(
	cfg *config.Config,
	store *repository.TikTokCancellationRepo,
	routes *repository.ChannelDefaultRepo,
	client *sml.SaleInvoiceCancelClient,
	billH *BillHandler,
	logger *zap.Logger,
) *TikTokCancellationCoordinator {
	if logger == nil {
		logger = zap.NewNop()
	}
	mode := smlprofile.ModeOff
	if cfg != nil {
		if value, ok := cfg.SMLDocumentProfileRouteModes["saleinvoicecancel"]; ok && strings.TrimSpace(value) != "" {
			mode = strings.TrimSpace(value)
		}
	}
	coordinator := &TikTokCancellationCoordinator{
		store: store, routes: routes, client: client, billH: billH, logger: logger, profileMode: mode,
		createEnabled: cfg != nil && cfg.TikTokShopSMLCancelDocumentsEnabled && cfg.TikTokShopCancelWebhookEnabled,
	}
	coordinator.allocateDocNo = coordinator.allocateCancellationDocNo
	coordinator.readiness = func(ctx context.Context) error {
		if billH == nil || billH.smlReadiness == nil {
			return nil
		}
		status := billH.smlReadiness.Check(ctx, false)
		if !status.Ready {
			return errors.New(status.Message)
		}
		return nil
	}
	return coordinator
}

func (c *TikTokCancellationCoordinator) Preview(ctx context.Context, shopID, orderID, actorID string) (*TikTokCancellationPreview, error) {
	evidence, route, routeMeta, review, err := c.reviewEvidence(ctx, shopID, orderID)
	if err != nil {
		return nil, err
	}
	existing, err := c.store.Latest(ctx, evidence.ShopID, evidence.OrderID, evidence.SMLAttemptID)
	if err != nil {
		return nil, err
	}
	if existing != nil && (existing.Status == "created" || existing.Status == "already_exists") {
		return &TikTokCancellationPreview{
			Status: "already_exists", Message: "มีเอกสารยกเลิก SML สำหรับใบขายนี้แล้ว", CreateEnabled: false,
			ReviewDigest: existing.ReviewDigest, ShopID: evidence.ShopID, OrderID: evidence.OrderID,
			BillID: evidence.BillID, SaleSMLDocNo: evidence.BillSMLDocNo, Destination: routeMeta.Destination,
			DocFormatCode: route.DocFormatCode, RouteConfigVersion: route.ConfigVersion, Existing: tikTokCancellationResult(existing),
		}, nil
	}
	if existing != nil && (existing.Status == "creating" || existing.Status == "unknown") {
		return nil, errTikTokCancellationReconciliation
	}
	if c.client == nil || !c.client.IsConfigured() || c.allocateDocNo == nil {
		return nil, fmt.Errorf("%w: SML cancellation client is not configured", errTikTokCancellationRouteBlocked)
	}
	if c.readiness != nil {
		if err := c.readiness(ctx); err != nil {
			return nil, fmt.Errorf("SML is not ready: %w", err)
		}
	}
	previewDocNo, err := c.allocateDocNo(ctx, route, false)
	if err != nil {
		return nil, err
	}
	request, err := c.cancellationRequest(route, routeMeta, evidence.OrderID, previewDocNo, time.Now())
	if err != nil {
		return nil, err
	}
	statusCode, response, err := c.client.Preview(ctx, evidence.BillSMLDocNo, request)
	if err != nil || response == nil || statusCode >= 300 || !response.IsSuccess() {
		return nil, fmt.Errorf("SML cancellation preview failed")
	}
	digest := tikTokCancellationReviewDigest(review)
	record, err := c.store.UpsertPreview(ctx, repository.TikTokCancellationAttemptInput{
		ShopID: evidence.ShopID, OrderID: evidence.OrderID, BillID: evidence.BillID, SMLAttemptID: evidence.SMLAttemptID,
		SaleSMLDocNo: evidence.BillSMLDocNo, SourceHash: evidence.SourceHash, ReviewDigest: digest,
		RouteEndpoint: route.Endpoint, RouteConfigVersion: route.ConfigVersion,
		RouteSignature: review.RouteSignature, CreatedBy: strings.TrimSpace(actorID),
	}, response.Raw())
	if err != nil {
		return nil, err
	}
	return &TikTokCancellationPreview{
		Status: "previewed", Message: "ตรวจตัวอย่างเอกสารยกเลิก SML แล้ว", CreateEnabled: c.createEnabled,
		ReviewDigest: digest, ShopID: evidence.ShopID, OrderID: evidence.OrderID, BillID: evidence.BillID,
		SaleSMLDocNo: evidence.BillSMLDocNo, PreviewCancelDocNo: response.CancelDocNo(),
		Destination: routeMeta.Destination, DocFormatCode: route.DocFormatCode,
		RouteConfigVersion: route.ConfigVersion, Existing: tikTokCancellationResult(record),
	}, nil
}

func (c *TikTokCancellationCoordinator) Create(ctx context.Context, input TikTokCancellationCreateInput) (*models.TikTokSMLCancellation, error) {
	if c == nil || !c.createEnabled {
		return nil, errTikTokCancellationFeatureDisabled
	}
	evidence, route, routeMeta, review, err := c.reviewEvidence(ctx, input.ShopID, input.OrderID)
	if err != nil {
		return nil, err
	}
	digest := tikTokCancellationReviewDigest(review)
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(input.ReviewDigest)), []byte(digest)) != 1 {
		return nil, errTikTokCancellationReviewChanged
	}
	if c.client == nil || !c.client.IsConfigured() || c.allocateDocNo == nil {
		return nil, fmt.Errorf("%w: SML cancellation client is not configured", errTikTokCancellationRouteBlocked)
	}
	if c.readiness != nil {
		if err := c.readiness(ctx); err != nil {
			return nil, fmt.Errorf("SML is not ready: %w", err)
		}
	}
	existing, err := c.store.Latest(ctx, evidence.ShopID, evidence.OrderID, evidence.SMLAttemptID)
	if err != nil {
		return nil, err
	}
	if existing == nil || existing.ReviewDigest != digest {
		return nil, errTikTokCancellationReviewChanged
	}
	if existing.Status == "created" || existing.Status == "already_exists" {
		return existing, nil
	}
	if existing.Status == "creating" || existing.Status == "unknown" {
		if existing.Status == "unknown" {
			return nil, errTikTokCancellationReconciliation
		}
		return nil, errTikTokCancellationBusy
	}
	cancelDocNo := strings.TrimSpace(existing.CancelSMLDocNo)
	payload := append(json.RawMessage(nil), existing.RequestPayload...)
	if cancelDocNo == "" || len(payload) == 0 || string(payload) == "{}" {
		cancelDocNo, err = c.allocateDocNo(ctx, route, true)
		if err != nil {
			return nil, err
		}
		request, buildErr := c.cancellationRequest(route, routeMeta, evidence.OrderID, cancelDocNo, time.Now())
		if buildErr != nil {
			return nil, buildErr
		}
		payload, err = json.Marshal(request)
		if err != nil {
			return nil, err
		}
	}
	record, state, err := c.store.StartCreate(ctx, repository.TikTokCancellationAttemptInput{
		ShopID: evidence.ShopID, OrderID: evidence.OrderID, BillID: evidence.BillID, SMLAttemptID: evidence.SMLAttemptID,
		SaleSMLDocNo: evidence.BillSMLDocNo, CancelSMLDocNo: cancelDocNo, SourceHash: evidence.SourceHash,
		ReviewDigest: digest, RouteEndpoint: route.Endpoint, RouteConfigVersion: route.ConfigVersion,
		RouteSignature: review.RouteSignature, RequestPayload: payload, CreatedBy: strings.TrimSpace(input.ActorID),
	})
	if err != nil {
		return nil, err
	}
	switch state {
	case repository.TikTokCancellationStartDone:
		return record, nil
	case repository.TikTokCancellationStartBusy:
		return nil, errTikTokCancellationBusy
	case repository.TikTokCancellationStartStale:
		return nil, errTikTokCancellationReviewChanged
	case repository.TikTokCancellationStartReconciliation:
		return nil, errTikTokCancellationReconciliation
	case repository.TikTokCancellationStartStarted:
	default:
		return nil, fmt.Errorf("unexpected TikTok cancellation start state %q", state)
	}
	statusCode, response, createErr := c.client.CreateBytes(ctx, evidence.BillSMLDocNo, sml.SaleInvoiceCancelKindVoid, record.RequestPayload, strings.TrimSpace(input.TraceID))
	if createErr != nil || response == nil || statusCode >= 500 {
		message := "SML cancellation result is unknown"
		if createErr != nil {
			message = createErr.Error()
		}
		completed, completeErr := c.store.Complete(ctx, record.ID, "unknown", record.CancelSMLDocNo, responseRaw(response), "unknown_result", message)
		if completeErr != nil {
			c.logger.Error("tiktok_sml_cancel_unknown_persist_failed", zap.String("attempt_id", record.ID), zap.Error(completeErr))
		}
		_ = completed
		return nil, errTikTokCancellationReconciliation
	}
	if statusCode >= 300 || !response.IsSuccess() {
		message := response.GetMessage()
		if message == "" {
			message = "SML rejected the cancellation document"
		}
		_, _ = c.store.Complete(ctx, record.ID, "failed", record.CancelSMLDocNo, response.Raw(), response.GetCode(), message)
		return nil, fmt.Errorf("SML cancellation failed")
	}
	finalStatus := "created"
	if response.IsAlreadyExists() {
		finalStatus = "already_exists"
	}
	responseDocNo := strings.TrimSpace(response.CancelDocNo())
	if responseDocNo == "" {
		responseDocNo = record.CancelSMLDocNo
	}
	completed, err := c.store.Complete(ctx, record.ID, finalStatus, responseDocNo, response.Raw(), "", "")
	if err != nil {
		// The SML write already succeeded. Persist an explicit unknown state when
		// possible so no retry can allocate or submit another document number.
		recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = c.store.Complete(recordCtx, record.ID, "unknown", responseDocNo, response.Raw(), "completion_persist_failed", err.Error())
		cancel()
		return nil, err
	}
	if completed == nil {
		record.Status, record.CancelSMLDocNo, record.StockRecalcStatus = finalStatus, responseDocNo, "pending"
		completed = record
	}
	return completed, nil
}

func (c *TikTokCancellationCoordinator) reviewEvidence(ctx context.Context, shopID, orderID string) (repository.TikTokCancellationEvidenceRow, *models.ChannelDefault, shopeeSMLCancellationRoute, tiktokshop.TikTokCancellationReviewEvidence, error) {
	var empty repository.TikTokCancellationEvidenceRow
	if c == nil || c.store == nil || c.routes == nil {
		return empty, nil, shopeeSMLCancellationRoute{}, tiktokshop.TikTokCancellationReviewEvidence{}, errTikTokCancellationRouteBlocked
	}
	evidence, err := c.store.Evidence(ctx, strings.TrimSpace(shopID), strings.TrimSpace(orderID))
	if err != nil {
		return empty, nil, shopeeSMLCancellationRoute{}, tiktokshop.TikTokCancellationReviewEvidence{}, err
	}
	eligibility := tiktokshop.EvaluateTikTokCancellationEvidence(tiktokshop.TikTokCancellationEvidence{
		ShopID: evidence.ShopID, OrderID: evidence.OrderID, OrderStatus: tiktokshop.OrderStatus(evidence.OrderStatus),
		BillID: evidence.BillID, BillSource: evidence.BillSource, BillSourceAccountKey: evidence.BillSourceAccountKey,
		BillSourceFlow: evidence.BillSourceFlow, BillStatus: evidence.BillStatus, BillDocumentRoute: evidence.BillDocumentRoute,
		BillSMLDocNo: evidence.BillSMLDocNo, SMLAttemptID: evidence.SMLAttemptID, SMLAttemptState: evidence.SMLAttemptState,
		SMLAttemptRoute: evidence.SMLAttemptRoute, SMLAttemptDocNo: evidence.SMLAttemptDocNo,
	})
	if !eligibility.Eligible {
		return empty, nil, shopeeSMLCancellationRoute{}, tiktokshop.TikTokCancellationReviewEvidence{}, fmt.Errorf("%w: %s", errTikTokCancellationEvidenceBlocked, eligibility.Code)
	}
	route, err := c.routes.Get("tiktok_shop_cancel", "sale")
	if err != nil {
		return empty, nil, shopeeSMLCancellationRoute{}, tiktokshop.TikTokCancellationReviewEvidence{}, err
	}
	if route == nil || route.ConfigVersion < 1 || strings.TrimSpace(route.Endpoint) != "/api/v1/ic/sale-invoices/:doc_no/void" ||
		strings.TrimSpace(route.DocFormatCode) == "" || strings.TrimSpace(route.DocPrefix) == "" || !strings.Contains(route.DocRunningFormat, "#") {
		return empty, route, shopeeSMLCancellationRoute{}, tiktokshop.TikTokCancellationReviewEvidence{}, errTikTokCancellationRouteBlocked
	}
	routeMeta, err := resolveShopeeSMLCancellationRoute(route.Endpoint)
	if err != nil || routeMeta.Kind != sml.SaleInvoiceCancelKindVoid || routeMeta.DocNoRoute != "saleinvoicecancel" {
		return empty, route, shopeeSMLCancellationRoute{}, tiktokshop.TikTokCancellationReviewEvidence{}, errTikTokCancellationRouteBlocked
	}
	review := tikTokCancellationReviewEvidence(evidence, route, c.profileMode)
	return evidence, route, routeMeta, review, nil
}

func tikTokCancellationReviewEvidence(evidence repository.TikTokCancellationEvidenceRow, route *models.ChannelDefault, profileMode ...string) tiktokshop.TikTokCancellationReviewEvidence {
	return tiktokshop.TikTokCancellationReviewEvidence{
		ShopID: evidence.ShopID, OrderID: evidence.OrderID, SourceHash: evidence.SourceHash,
		SnapshotSyncedAt: evidence.LastSyncedAt.UTC().Format(time.RFC3339Nano),
		BillID:           evidence.BillID, SMLAttemptID: evidence.SMLAttemptID, SaleSMLDocNo: evidence.BillSMLDocNo,
		RouteConfigVersion: route.ConfigVersion, RouteSignature: tikTokCancellationRouteSignature(route, profileMode...),
	}
}

func tikTokCancellationReviewDigest(evidence tiktokshop.TikTokCancellationReviewEvidence) string {
	return tiktokshop.TikTokCancellationReviewDigest(evidence)
}

func tikTokCancellationRouteSignature(route *models.ChannelDefault, profileMode ...string) string {
	return shopeeSMLCancellationRouteSignature(route, profileMode...)
}

func (c *TikTokCancellationCoordinator) cancellationRequest(route *models.ChannelDefault, routeMeta shopeeSMLCancellationRoute, orderID, docNo string, now time.Time) (sml.SaleInvoiceCancelRequest, error) {
	remark := strings.TrimSpace(route.Remark)
	if remark == "" {
		remark = "TikTok Shop order cancelled: " + strings.TrimSpace(orderID)
	}
	if err := smlprofile.ValidateFreeText("remark", remark); err != nil {
		return sml.SaleInvoiceCancelRequest{}, err
	}
	request := sml.SaleInvoiceCancelRequest{
		Kind: routeMeta.Kind, DocDate: now.In(shopeeAutoSMLBangkokTimeZone).Format("2006-01-02"),
		DocTime: now.In(shopeeAutoSMLBangkokTimeZone).Format("15:04"), DocFormatCode: route.DocFormatCode,
		DocNo: strings.TrimSpace(docNo), Remark: remark, UserRequest: "NEXFLOW",
	}
	if c.profileMode == smlprofile.ModeActive {
		if err := smlprofile.ValidateFreeText("remark_2", route.Remark2); err != nil {
			return sml.SaleInvoiceCancelRequest{}, err
		}
		request.DocumentProfileVersion = sml.InvoiceDocumentProfileVersion
		request.Remark2 = strings.TrimSpace(route.Remark2)
		request.Remark5 = "NEXFLOW|tiktok_shop|" + strings.TrimSpace(orderID)
		request.CreatorCode = "BILLFLOW"
		request.CashierCode = "BILLFLOW"
	}
	return request, nil
}

func (c *TikTokCancellationCoordinator) allocateCancellationDocNo(ctx context.Context, route *models.ChannelDefault, reserve bool) (string, error) {
	if c == nil || c.billH == nil || c.billH.docNoClient == nil || !c.billH.docNoClient.IsConfigured() || route == nil {
		return "", fmt.Errorf("SML doc_no API is not configured")
	}
	prefix, format := resolveDocCounterPattern(route, "SIC")
	now := time.Now().In(shopeeAutoSMLBangkokTimeZone)
	next, err := c.billH.docNoClient.Next(ctx, sml.NextDocNoRequest{Route: "saleinvoicecancel", Prefix: prefix, Format: format, DocDate: now.Format("2006-01-02")})
	if err != nil {
		return "", err
	}
	if !reserve {
		return strings.TrimSpace(next.NextDocNo), nil
	}
	if c.billH.docCounters == nil {
		return "", fmt.Errorf("local doc_no counter not configured")
	}
	docDate, err := time.Parse("2006-01-02", next.DocDate)
	if err != nil || docDate.IsZero() {
		docDate = now
	}
	concrete, ok := c.store.(*repository.TikTokCancellationRepo)
	if !ok {
		return "", fmt.Errorf("TikTok cancellation repository is not configured")
	}
	for index := 0; index < 100; index++ {
		docNo, err := c.billH.docCounters.GenerateDocNoAtLeast(prefix, format, docDate, next.NextSeq)
		if err != nil {
			return "", err
		}
		billExists, err := c.billH.localDocNoExists(docNo, "")
		if err != nil {
			return "", err
		}
		cancelExists, err := concrete.CancelDocNoExists(ctx, docNo)
		if err != nil {
			return "", err
		}
		if !billExists && !cancelExists {
			return docNo, nil
		}
	}
	return "", fmt.Errorf("cannot allocate unique TikTok cancellation doc_no")
}

func (c *TikTokCancellationCoordinator) Start(ctx context.Context) {
	if c == nil || !c.createEnabled || c.billH == nil {
		return
	}
	repo, ok := c.store.(*repository.TikTokCancellationRepo)
	if !ok {
		return
	}
	if recovered, err := repo.RecoverStaleCreates(ctx, 5*time.Minute); err != nil {
		c.logger.Warn("tiktok_sml_cancel_create_recover_failed", zap.Error(err))
	} else if recovered > 0 {
		c.logger.Warn("tiktok_sml_cancel_create_recovered_as_unknown", zap.Int64("jobs", recovered))
	}
	if recovered, err := repo.RecoverStaleStockRecalculations(ctx); err != nil {
		c.logger.Warn("tiktok_sml_cancel_stock_recalc_recover_failed", zap.Error(err))
	} else if recovered > 0 {
		c.logger.Warn("tiktok_sml_cancel_stock_recalc_recovered", zap.Int64("jobs", recovered))
	}
	go func() {
		c.processStockRecalculation(ctx, repo)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.processStockRecalculation(ctx, repo)
			}
		}
	}()
}

func (c *TikTokCancellationCoordinator) processStockRecalculation(ctx context.Context, repo *repository.TikTokCancellationRepo) {
	job, err := repo.ClaimStockRecalculation(ctx, 2*time.Minute)
	if err != nil || job == nil {
		if err != nil && ctx.Err() == nil {
			c.logger.Warn("tiktok_sml_cancel_stock_recalc_claim_failed", zap.Error(err))
		}
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	fail := func(cause error) {
		message := "คำนวณสต๊อก SML หลังยกเลิก TikTok Shop ไม่สำเร็จ"
		if cause != nil {
			message += ": " + cause.Error()
		}
		recordCtx, recordCancel := context.WithTimeout(context.Background(), 5*time.Second)
		terminal, markErr := repo.FailStockRecalculation(recordCtx, job.ID, message, 10)
		recordCancel()
		if markErr != nil {
			c.logger.Error("tiktok_sml_cancel_stock_recalc_record_failed", zap.String("attempt_id", job.ID), zap.Error(markErr))
			return
		}
		if terminal {
			c.auditStockRecalculation(job, "tiktok_shop_sml_cancel_stock_recalc_failed", "error", message, 0)
		}
	}
	if c.billH.billRepo == nil {
		fail(errors.New("Bill repository is not configured"))
		return
	}
	bill, err := c.billH.billRepo.FindByID(job.BillID)
	if err != nil || bill == nil {
		fail(errors.New("load original Bill failed"))
		return
	}
	itemCodes := smlCancellationItemCodes(bill)
	if len(itemCodes) == 0 {
		fail(errors.New("original Bill has no SML item code"))
		return
	}
	stockConfig, err := c.billH.resolveStockRecalcConfig()
	if err != nil || strings.TrimSpace(stockConfig.StockRequestURL) == "" || strings.TrimSpace(stockConfig.Provider) == "" || strings.TrimSpace(stockConfig.Database) == "" {
		fail(errors.New("SML processstockrequest runtime is not ready"))
		return
	}
	client := sml.NewStockRequestClient(stockConfig.StockRequestURL, stockConfig.Provider, stockConfig.Database, c.logger)
	if err := client.ProcessStockRequest(runCtx, itemCodes); err != nil {
		fail(err)
		return
	}
	if err := repo.CompleteStockRecalculation(runCtx, job.ID); err != nil {
		fail(err)
		return
	}
	c.auditStockRecalculation(job, "tiktok_shop_sml_cancel_stock_recalc_ok", "info", "", len(itemCodes))
	c.logger.Info("tiktok_sml_cancel_stock_recalc_succeeded", zap.String("attempt_id", job.ID), zap.String("cancel_doc_no", job.CancelSMLDocNo), zap.Int("item_count", len(itemCodes)))
}

func (c *TikTokCancellationCoordinator) auditStockRecalculation(job *models.TikTokSMLCancellation, action, level, message string, itemCount int) {
	if c == nil || c.billH == nil || c.billH.auditRepo == nil || job == nil {
		return
	}
	billID := job.BillID
	detail := map[string]any{
		"attempt_id": job.ID, "shop_id": job.ShopID, "order_id": job.OrderID,
		"sale_sml_doc_no": job.SaleSMLDocNo, "cancel_sml_doc_no": job.CancelSMLDocNo,
		"item_count": itemCount,
	}
	if message != "" {
		detail["error"] = message
	}
	_ = c.billH.auditRepo.Log(models.AuditEntry{Action: action, TargetID: &billID, Source: "sml", Level: level, Detail: detail})
}
