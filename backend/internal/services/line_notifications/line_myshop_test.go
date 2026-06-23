package linenotify

import (
	"strings"
	"testing"

	"nexflow/internal/models"
)

func TestBuildLineMyShopOrderLineTextRedactsPII(t *testing.T) {
	billID := "bill-1001"
	snap := &models.LineMyShopOrderSnapshot{
		ConnectionID:   "conn-1",
		ConnectionName: "Main MyShop",
		OrderNo:        "LMS-1001",
		OrderStatus:    "FINALIZED",
		PaymentStatus:  "PAID",
		ShipmentStatus: "SHIPMENT_READY",
		ItemCount:      2,
		TotalAmount:    1234.50,
		BillID:         &billID,
		RawWebhook:     []byte(`{"shippingAddress":{"recipientName":"Jane Buyer","phoneNumber":"0899999999","address":"secret address"}}`),
	}

	got := BuildLineMyShopOrderLineText(snap, "https://example.com")
	for _, forbidden := range []string{"Jane Buyer", "0899999999", "secret address", "recipientName", "phoneNumber"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("notification leaked %q: %s", forbidden, got)
		}
	}
	for _, want := range []string{"LINE MyShop", "LMS-1001", "Main MyShop", "PAID", "SHIPMENT_READY", "https://example.com/bills/bill-1001"} {
		if !strings.Contains(got, want) {
			t.Fatalf("notification missing %q: %s", want, got)
		}
	}
}

func TestLineMyShopOrderActionURLFallsBackToSaleQueueSearch(t *testing.T) {
	got := LineMyShopOrderActionURL("https://example.com", "LMS-1001", "")
	want := "https://example.com/sale-invoices?source=line_myshop&search=LMS-1001"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}
