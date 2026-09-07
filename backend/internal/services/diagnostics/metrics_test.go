package diagnostics

import (
	"testing"
	"time"
)

func TestEvidenceMetricsUseOnlyBoundedRouteAndStatus(t *testing.T) {
	metrics := NewEvidenceMetrics()
	metrics.Observe("SaleInvoice", "started", 2*time.Millisecond)
	metrics.Observe("buyer@example.test", "finish_failed", 3*time.Millisecond)

	got := metrics.Snapshot()
	if len(got) != 2 {
		t.Fatalf("snapshot = %#v", got)
	}
	if got[0].Route != "invalid" || got[1].Route != "saleinvoice" {
		t.Fatalf("unbounded label escaped: %#v", got)
	}
}
