package handlers

import (
	"strings"
	"testing"
)

func TestParseTikTokCSVGroupsOrdersAndSkipsNonShippedStatuses(t *testing.T) {
	csvData := "\ufeffOrder ID,Order Status,Order Substatus,Cancelation/Return Type,Normal or Pre-order,SKU ID,Seller SKU,Product Name,Variation,Quantity,SKU Subtotal After Discount,SKU Unit Original Price,Shipping Fee After Discount,Order Amount,Created Time,Paid Time,Tracking ID,Payment Method,Buyer Username,Recipient\n" +
		"583870900000000001\t,จัดส่งแล้ว,จัดส่งสำเร็จ,,Normal,SKU-001\t,,Serum A,30ml,2,200,120,38,238,10/05/2026 22:05:43\t,10/05/2026 22:06:01\t,TH123,COD,buyer1,คุณเอ\n" +
		"583870900000000001\t,จัดส่งแล้ว,จัดส่งสำเร็จ,,Normal,SKU-002\t,,Serum B,50ml,1,58,70,38,238,10/05/2026 22:05:43\t,10/05/2026 22:06:01\t,TH123,COD,buyer1,คุณเอ\n" +
		"583870900000000002\t,ยกเลิกแล้ว,ยกเลิกแล้ว,Cancel,Normal,SKU-003\t,,Canceled,Default,1,29,29,38,67,10/05/2026 12:00:00\t,,TH999,COD,buyer2,คุณบี\n" +
		"583870900000000003\t,ที่จะจัดส่ง,รอเข้ารับ,,Normal,SKU-004\t,,Pending,Default,1,29,29,38,67,10/05/2026 13:00:00\t,,TH888,COD,buyer3,คุณซี\n"

	orders, warnings, skipped, err := parseTikTokCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("parseTikTokCSV() error = %v", err)
	}
	if skipped != 2 {
		t.Fatalf("skipped = %d, want 2", skipped)
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "กรอง 2 แถว") {
		t.Fatalf("warnings = %#v, want skip warning", warnings)
	}
	if len(orders) != 1 {
		t.Fatalf("orders = %d, want 1", len(orders))
	}
	o := orders[0]
	if o.OrderID != "583870900000000001" {
		t.Fatalf("OrderID = %q", o.OrderID)
	}
	if o.DocDate != "2026-05-10" {
		t.Fatalf("DocDate = %q, want 2026-05-10", o.DocDate)
	}
	if o.ItemCount != 2 || o.TotalQty != 3 {
		t.Fatalf("ItemCount/TotalQty = %d/%v, want 2/3", o.ItemCount, o.TotalQty)
	}
	if o.PaidAmount != 238 {
		t.Fatalf("PaidAmount = %v, want 238 (order amount must not double count multi-row orders)", o.PaidAmount)
	}
	if o.ShippingAmount != 38 {
		t.Fatalf("ShippingAmount = %v, want 38", o.ShippingAmount)
	}
	if o.Items[0].SKU != "SKU-001" || o.Items[0].TikTokSKU != "SKU-001" {
		t.Fatalf("first item SKU/TikTokSKU = %q/%q", o.Items[0].SKU, o.Items[0].TikTokSKU)
	}
	if o.Items[0].Price != 100 {
		t.Fatalf("first item Price = %v, want subtotal/qty = 100", o.Items[0].Price)
	}
}
