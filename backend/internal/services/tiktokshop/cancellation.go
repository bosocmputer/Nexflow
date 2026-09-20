package tiktokshop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

type TikTokCancellationEvidence struct {
	ShopID               string
	OrderID              string
	OrderStatus          OrderStatus
	BillID               string
	BillSource           string
	BillSourceAccountKey string
	BillSourceFlow       string
	BillStatus           string
	BillDocumentRoute    string
	BillSMLDocNo         string
	SMLAttemptID         string
	SMLAttemptState      string
	SMLAttemptRoute      string
	SMLAttemptDocNo      string
}

type TikTokCancellationEligibilityCode string

const (
	TikTokCancellationEligible             TikTokCancellationEligibilityCode = "eligible"
	TikTokCancellationOrderNotFinal        TikTokCancellationEligibilityCode = "order_not_final"
	TikTokCancellationNotRequired          TikTokCancellationEligibilityCode = "not_required"
	TikTokCancellationOwnershipMismatch    TikTokCancellationEligibilityCode = "ownership_mismatch"
	TikTokCancellationEvidenceInconsistent TikTokCancellationEligibilityCode = "evidence_inconsistent"
	TikTokCancellationUnsupportedSaleRoute TikTokCancellationEligibilityCode = "unsupported_sale_route"
)

type TikTokCancellationEligibility struct {
	Eligible bool
	Code     TikTokCancellationEligibilityCode
}

func EvaluateTikTokCancellationEvidence(evidence TikTokCancellationEvidence) TikTokCancellationEligibility {
	if evidence.OrderStatus != OrderStatusCancelled {
		return TikTokCancellationEligibility{Code: TikTokCancellationOrderNotFinal}
	}
	if strings.TrimSpace(evidence.BillID) == "" {
		return TikTokCancellationEligibility{Code: TikTokCancellationNotRequired}
	}
	if strings.TrimSpace(evidence.BillStatus) != "sent" && strings.TrimSpace(evidence.BillSMLDocNo) == "" {
		return TikTokCancellationEligibility{Code: TikTokCancellationNotRequired}
	}
	if strings.TrimSpace(evidence.BillSource) != "tiktok" ||
		strings.TrimSpace(evidence.BillSourceAccountKey) != "shop:"+strings.TrimSpace(evidence.ShopID) ||
		strings.TrimSpace(evidence.BillSourceFlow) != TikTokReviewedBillFlow {
		return TikTokCancellationEligibility{Code: TikTokCancellationOwnershipMismatch}
	}
	if strings.TrimSpace(evidence.BillDocumentRoute) != "saleinvoice" || strings.TrimSpace(evidence.SMLAttemptRoute) != "saleinvoice" {
		return TikTokCancellationEligibility{Code: TikTokCancellationUnsupportedSaleRoute}
	}
	if strings.TrimSpace(evidence.BillStatus) != "sent" || strings.TrimSpace(evidence.BillSMLDocNo) == "" ||
		strings.TrimSpace(evidence.SMLAttemptID) == "" || strings.TrimSpace(evidence.SMLAttemptState) != "sent" ||
		strings.TrimSpace(evidence.SMLAttemptDocNo) == "" ||
		strings.TrimSpace(evidence.SMLAttemptDocNo) != strings.TrimSpace(evidence.BillSMLDocNo) {
		return TikTokCancellationEligibility{Code: TikTokCancellationEvidenceInconsistent}
	}
	return TikTokCancellationEligibility{Eligible: true, Code: TikTokCancellationEligible}
}

type TikTokCancellationReviewEvidence struct {
	ShopID             string `json:"shop_id"`
	OrderID            string `json:"order_id"`
	SourceHash         string `json:"source_hash"`
	BillID             string `json:"bill_id"`
	SMLAttemptID       string `json:"sml_attempt_id"`
	SaleSMLDocNo       string `json:"sale_sml_doc_no"`
	RouteConfigVersion int64  `json:"route_config_version"`
	RouteSignature     string `json:"route_signature"`
}

func TikTokCancellationReviewDigest(evidence TikTokCancellationReviewEvidence) string {
	body, _ := json.Marshal(evidence)
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}
