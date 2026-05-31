package anomaly

import (
	"testing"

	"nexflow/internal/models"
)

func ptr[T any](v T) *T { return &v }

// ── CanAutoConfirm ────────────────────────────────────────────────────────────

func TestCanAutoConfirm_NoAnomalies(t *testing.T) {
	if !CanAutoConfirm(nil, 0.90, 0.85) {
		t.Error("expected auto-confirm when no anomalies and confidence >= threshold")
	}
}

func TestCanAutoConfirm_LowConfidence(t *testing.T) {
	if CanAutoConfirm(nil, 0.60, 0.85) {
		t.Error("expected no auto-confirm when confidence < threshold")
	}
}

func TestCanAutoConfirm_BlockAnomaly(t *testing.T) {
	anomalies := []models.Anomaly{{Code: "price_zero", Severity: "block"}}
	if CanAutoConfirm(anomalies, 0.99, 0.85) {
		t.Error("expected no auto-confirm when block anomaly present")
	}
}

func TestCanAutoConfirm_OneWarnAllowed(t *testing.T) {
	anomalies := []models.Anomaly{{Code: "new_item", Severity: "warn"}}
	if !CanAutoConfirm(anomalies, 0.90, 0.85) {
		t.Error("expected auto-confirm with exactly 1 warn and high confidence")
	}
}

func TestCanAutoConfirm_TwoWarnsBlocked(t *testing.T) {
	anomalies := []models.Anomaly{
		{Code: "new_item", Severity: "warn"},
		{Code: "price_too_high", Severity: "warn"},
	}
	if CanAutoConfirm(anomalies, 0.95, 0.85) {
		t.Error("expected no auto-confirm when 2 warn anomalies")
	}
}

// ── Check ─────────────────────────────────────────────────────────────────────

func TestCheck_QtyZero(t *testing.T) {
	svc := New(nil)
	out := svc.Check(CheckInput{
		Items: []models.BillItem{{RawName: "ปูน", Qty: 0}},
	})
	found := containsCode(out, "qty_zero")
	if !found {
		t.Error("expected qty_zero anomaly")
	}
	if out[0].Severity != "block" {
		t.Errorf("expected block severity, got %s", out[0].Severity)
	}
}

func TestCheck_PriceZero(t *testing.T) {
	svc := New(nil)
	code := "CEM001"
	out := svc.Check(CheckInput{
		Items: []models.BillItem{{RawName: "ปูน", Qty: 1, ItemCode: &code, Price: ptr(0.0)}},
	})
	if !containsCode(out, "price_zero") {
		t.Error("expected price_zero anomaly")
	}
}

func TestCheck_PriceTooHigh(t *testing.T) {
	svc := New(nil)
	code := "CEM001"
	out := svc.Check(CheckInput{
		Items:     []models.BillItem{{RawName: "ปูน", Qty: 1, ItemCode: &code, Price: ptr(1000.0)}},
		AvgPrices: map[string]float64{"CEM001": 300.0}, // 1000 > 300*1.5
	})
	if !containsCode(out, "price_too_high") {
		t.Error("expected price_too_high anomaly")
	}
}

func TestCheck_PriceTooLow(t *testing.T) {
	svc := New(nil)
	code := "CEM001"
	out := svc.Check(CheckInput{
		Items:     []models.BillItem{{RawName: "ปูน", Qty: 1, ItemCode: &code, Price: ptr(50.0)}},
		AvgPrices: map[string]float64{"CEM001": 300.0}, // 50 < 300*0.5
	})
	if !containsCode(out, "price_too_low") {
		t.Error("expected price_too_low anomaly")
	}
}

func TestCheck_NormalPriceNoAnomaly(t *testing.T) {
	svc := New(nil)
	code := "CEM001"
	out := svc.Check(CheckInput{
		Items:     []models.BillItem{{RawName: "ปูน", Qty: 2, ItemCode: &code, Price: ptr(300.0)}},
		AvgPrices: map[string]float64{"CEM001": 300.0},
	})
	if len(out) != 0 {
		t.Errorf("expected no anomalies for normal price, got %v", out)
	}
}

func TestCheck_NewItem(t *testing.T) {
	svc := New(nil)
	code := "NEWITEM"
	out := svc.Check(CheckInput{
		Items:      []models.BillItem{{RawName: "สินค้าใหม่", Qty: 1, ItemCode: &code, Price: ptr(100.0)}},
		KnownItems: map[string]bool{"CEM001": true},
	})
	if !containsCode(out, "new_item") {
		t.Error("expected new_item anomaly")
	}
}

func TestCheck_QtySuspicious(t *testing.T) {
	svc := New(nil)
	code := "CEM001"
	out := svc.Check(CheckInput{
		Items:   []models.BillItem{{RawName: "ปูน", Qty: 1000, ItemCode: &code, Price: ptr(300.0)}},
		MaxQtys: map[string]float64{"CEM001": 100.0}, // 1000 > 100*2
	})
	if !containsCode(out, "qty_suspicious") {
		t.Error("expected qty_suspicious anomaly")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func containsCode(anomalies []models.Anomaly, code string) bool {
	for _, a := range anomalies {
		if a.Code == code {
			return true
		}
	}
	return false
}
