package smlprofile

import (
	"testing"
	"time"
)

func TestMetricsUseOnlyBoundedLabelsAndCalculatePercentiles(t *testing.T) {
	metrics := NewMetrics()
	for i := 1; i <= 100; i++ {
		metrics.ObserveRequest("aoy", "SaleInvoice", Version, "complete", time.Duration(i)*time.Millisecond, false)
	}
	metrics.ObserveRequest("order-PII@example.com", "SaleInvoice", Version, "terminal_failure", time.Second, true)
	snapshot := metrics.Snapshot()
	if len(snapshot) != 2 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if snapshot[0].Tenant != "aoy" || snapshot[0].P95MS != 95 || snapshot[0].P99MS != 99 {
		t.Fatalf("aoy snapshot=%+v", snapshot[0])
	}
	if snapshot[1].Tenant != "invalid" || snapshot[1].ErrorCount != 1 {
		t.Fatalf("unsafe label was not bounded: %+v", snapshot[1])
	}
}
