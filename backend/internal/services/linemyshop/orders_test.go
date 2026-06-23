package linemyshop

import "testing"

func TestDecodeOrderListNormalizesStatuses(t *testing.T) {
	body := []byte(`{
		"currentPage":1,
		"perPage":50,
		"totalPage":1,
		"totalRow":1,
		"data":[{"orderNumber":" LMS-1001 ","orderStatus":"finalized","paymentStatus":" paid ","shipmentStatus":"shipment_ready","lastUpdatedAt":"2026-06-22T00:00:00Z"}]
	}`)
	got, err := DecodeOrderList(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalRow != 1 || len(got.Data) != 1 {
		t.Fatalf("unexpected list shape: %#v", got)
	}
	order := got.Data[0]
	if order.OrderNumber != "LMS-1001" || order.OrderStatus != "FINALIZED" || order.PaymentStatus != "PAID" || order.ShipmentStatus != "SHIPMENT_READY" {
		t.Fatalf("order not normalized: %#v", order)
	}
}
