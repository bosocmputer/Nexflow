package repository

import (
	"testing"

	"nexflow/internal/models"
)

func TestExtractShopeeShippingAmountScopesToOrderBlock(t *testing.T) {
	body := `
หมายเลขคำสั่งซื้อ #2601AAA
ค่าจัดส่งสินค้า: ฿38.00
หมายเลขคำสั่งซื้อ #2601BBB
ค่าจัดส่งสินค้า: ฿1,250.50
`

	got, ok := ExtractShopeeShippingAmount(body, "", "#2601BBB")
	if !ok {
		t.Fatal("expected shipping amount")
	}
	if got != 1250.50 {
		t.Fatalf("shipping amount = %v, want 1250.50", got)
	}
}

func TestExtractShopeeShippingAmountMissingReturnsFalse(t *testing.T) {
	if got, ok := ExtractShopeeShippingAmount("หมายเลขคำสั่งซื้อ #2601AAA", "", "#2601AAA"); ok || got != 0 {
		t.Fatalf("shipping amount = %v ok=%v, want zero false", got, ok)
	}
}

func TestExtractShopeeShippingAmountZeroReturnsTrue(t *testing.T) {
	body := `
หมายเลขคำสั่งซื้อ #2601AAA
ค่าจัดส่งสินค้า: ฿0
`

	got, ok := ExtractShopeeShippingAmount(body, "", "#2601AAA")
	if !ok {
		t.Fatal("expected shipping amount label to be detected")
	}
	if got != 0 {
		t.Fatalf("shipping amount = %v, want 0", got)
	}
}

func TestExtractShopeeDiscountSummaryBothDiscountsAndCodes(t *testing.T) {
	body := `
หมายเลขคำสั่งซื้อ: #2605211KR3XK1G
โค้ดส่วนลดของ Shopee: ฿322
โค้ดส่วนลดของ Shopee: DDX20HPPDL21MAY
โค้ดส่วนลดร้านค้า: ฿8
โค้ดส่วนลดร้านค้า: ALOV14998
หมายเลขคำสั่งซื้อ: #OTHER
โค้ดส่วนลดของ Shopee: ฿999
`

	got := ExtractShopeeDiscountSummary(body, "", "#2605211KR3XK1G")
	if got.ShopeeDiscountAmount != 322 || got.ShopDiscountAmount != 8 || got.TotalDiscountAmount != 330 {
		t.Fatalf("summary amounts = %+v, want 322/8/330", got)
	}
	if len(got.ShopeeDiscountCodes) != 1 || got.ShopeeDiscountCodes[0] != "DDX20HPPDL21MAY" {
		t.Fatalf("shopee codes = %#v", got.ShopeeDiscountCodes)
	}
	if len(got.ShopDiscountCodes) != 1 || got.ShopDiscountCodes[0] != "ALOV14998" {
		t.Fatalf("shop codes = %#v", got.ShopDiscountCodes)
	}
}

func TestExtractShopeeDiscountSummaryFromShopeeHTMLTableCells(t *testing.T) {
	bodyHTML := `
<table>
  <tr><td>หมายเลขคำสั่งซื้อ: </td><td>#2605236MY1Q8EH</td></tr>
  <tr><td>ยอดรวมค่าสินค้า: </td><td>฿1,338</td></tr>
  <tr><td>โค้ดส่วนลดของ Shopee: </td><td>฿19</td></tr>
  <tr><td>โค้ดส่วนลดของ Shopee: </td><td>17M20023A</td></tr>
  <tr><td>ค่าจัดส่งสินค้า: </td><td>฿121</td></tr>
  <tr><td>ยอดที่ต้องชำระทั้งหมด: </td><td>฿1,440</td></tr>
</table>
<table>
  <tr><td>หมายเลขคำสั่งซื้อ: </td><td>#OTHER</td></tr>
  <tr><td>โค้ดส่วนลดของ Shopee: </td><td>฿999</td></tr>
</table>`

	got := ExtractShopeeDiscountSummary("", bodyHTML, "#2605236MY1Q8EH")
	if got.ShopeeDiscountAmount != 19 || got.TotalDiscountAmount != 19 {
		t.Fatalf("summary amounts = %+v, want 19", got)
	}
	if len(got.ShopeeDiscountCodes) != 1 || got.ShopeeDiscountCodes[0] != "17M20023A" {
		t.Fatalf("shopee codes = %#v", got.ShopeeDiscountCodes)
	}
}

func TestExtractShopeeDiscountSummaryConvertsHTMLStoredInBodyText(t *testing.T) {
	bodyText := `
<html><body><table>
  <tr><td>หมายเลขคำสั่งซื้อ: </td><td>#2605236MY1Q8EH</td></tr>
  <tr><td>โค้ดส่วนลดของ Shopee: </td><td>฿19</td></tr>
  <tr><td>โค้ดส่วนลดของ Shopee: </td><td>17M20023A</td></tr>
  <tr><td>ค่าจัดส่งสินค้า: </td><td>฿121</td></tr>
</table></body></html>`

	got := ExtractShopeeDiscountSummary(bodyText, "", "#2605236MY1Q8EH")
	if got.ShopeeDiscountAmount != 19 || got.TotalDiscountAmount != 19 {
		t.Fatalf("summary amounts = %+v, want 19", got)
	}
	if len(got.ShopeeDiscountCodes) != 1 || got.ShopeeDiscountCodes[0] != "17M20023A" {
		t.Fatalf("shopee codes = %#v", got.ShopeeDiscountCodes)
	}
}

func TestExtractShopeeDiscountSummaryFromSplitHTMLTableCells(t *testing.T) {
	bodyText := `
<!doctype html>
<html>
<body>
  <table>
    <tr>
      <td width="49%">โค้ดส่วนลดร้านค้า:</td>
      <td width="49%">฿10</td>
    </tr>
    <tr>
      <td width="49%">โค้ดส่วนลดร้านค้า:</td>
      <td width="49%">SHOP10</td>
    </tr>
    <tr>
      <td width="49%">โค้ดส่วนลดของ Shopee:</td>
      <td width="49%">฿45</td>
    </tr>
    <tr>
      <td width="49%">โค้ดส่วนลดของ Shopee:</td>
      <td width="49%">17M20023A</td>
    </tr>
  </table>
</body>
</html>`

	got := ExtractShopeeDiscountSummary(bodyText, "", "")
	if got.ShopeeDiscountAmount != 45 || got.ShopDiscountAmount != 10 || got.TotalDiscountAmount != 55 {
		t.Fatalf("summary amounts = %+v, want 45/10/55", got)
	}
	if len(got.ShopeeDiscountCodes) != 1 || got.ShopeeDiscountCodes[0] != "17M20023A" {
		t.Fatalf("shopee codes = %#v", got.ShopeeDiscountCodes)
	}
	if len(got.ShopDiscountCodes) != 1 || got.ShopDiscountCodes[0] != "SHOP10" {
		t.Fatalf("shop codes = %#v", got.ShopDiscountCodes)
	}
}

func TestExtractShopeeShippingAmountFromSplitHTMLTableCells(t *testing.T) {
	bodyText := `
<html>
<body>
  <table>
    <tr>
      <td width="49%">ค่าจัดส่งสินค้า:</td>
      <td width="49%">฿121</td>
    </tr>
  </table>
</body>
</html>`

	got, ok := ExtractShopeeShippingAmount(bodyText, "", "")
	if !ok || got != 121 {
		t.Fatalf("shipping amount = %v ok=%v, want 121 true", got, ok)
	}
}

func TestExtractShopeeDiscountSummaryToleratesMissingDiscountKinds(t *testing.T) {
	shopeeOnly := ExtractShopeeDiscountSummary("หมายเลขคำสั่งซื้อ #A\nโค้ดส่วนลดของ Shopee: ฿20", "", "#A")
	if shopeeOnly.TotalDiscountAmount != 20 || shopeeOnly.ShopDiscountAmount != 0 {
		t.Fatalf("shopeeOnly = %+v", shopeeOnly)
	}

	shopOnly := ExtractShopeeDiscountSummary("หมายเลขคำสั่งซื้อ #A\nโค้ดส่วนลดร้านค้า: ฿7", "", "#A")
	if shopOnly.TotalDiscountAmount != 7 || shopOnly.ShopeeDiscountAmount != 0 {
		t.Fatalf("shopOnly = %+v", shopOnly)
	}

	none := ExtractShopeeDiscountSummary("หมายเลขคำสั่งซื้อ #A\nโค้ดส่วนลดร้านค้า: ALOV14998", "", "#A")
	if none.TotalDiscountAmount != 0 || len(none.ShopDiscountCodes) != 1 {
		t.Fatalf("none = %+v, want code but zero amount", none)
	}
}

func TestExtractShopeePaymentSummaryCreditDebit(t *testing.T) {
	body := `
หมายเลขคำสั่งซื้อ: #2605211KR3XK1G
รายละเอียดการชำระเงิน
วิธีการชำระเงิน:	บัตรเครดิต/บัตรเดบิต
วันที่ชำระเงิน:	21 พ.ค. 2026 16:40:04
จำนวนเงินที่จ่าย:	฿7,275
หมายเลขคำสั่งซื้อ: #OTHER
จำนวนเงินที่จ่าย: ฿999
`

	got := ExtractShopeePaymentSummary(body, "", "#2605211KR3XK1G")
	if got.PaymentMethod != "บัตรเครดิต/บัตรเดบิต" || got.PaymentPaidAt == "" {
		t.Fatalf("payment text = %+v", got)
	}
	if !got.IsCreditDebitCard || got.PaymentPaidAmount != 7275 || got.DocRefAmount != "7275" {
		t.Fatalf("payment summary = %+v, want card amount/doc_ref 7275", got)
	}
}

func TestExtractShopeePaymentSummaryFallsBackToEmailLevelSection(t *testing.T) {
	body := `
หมายเลขคำสั่งซื้อ: #ORDER-A
รายการสินค้า
หมายเลขคำสั่งซื้อ: #ORDER-B
รายการสินค้า
รายละเอียดการชำระเงิน
วิธีการชำระเงิน: บัตรเครดิต/บัตรเดบิต
วันที่ชำระเงิน: 23 พ.ค. 2026 16:45:11
จำนวนเงินที่จ่าย: ฿15,800
`

	got := ExtractShopeePaymentSummary(body, "", "#ORDER-A")
	if !got.IsCreditDebitCard || got.PaymentPaidAmount != 15800 || got.DocRefAmount != "15800" {
		t.Fatalf("payment summary = %+v, want email-level card amount/doc_ref 15800", got)
	}
}

func TestExtractShopeePaymentSummaryNonCardDoesNotSetDocRef(t *testing.T) {
	body := `
หมายเลขคำสั่งซื้อ: #A
วิธีการชำระเงิน: ShopeePay
จำนวนเงินที่จ่าย: ฿1,234.50
`

	got := ExtractShopeePaymentSummary(body, "", "#A")
	if got.IsCreditDebitCard || got.DocRefAmount != "" {
		t.Fatalf("payment summary = %+v, want non-card without doc_ref", got)
	}
	if got.PaymentPaidAmount != 1234.50 {
		t.Fatalf("amount = %v, want 1234.50", got.PaymentPaidAmount)
	}
}

func TestExtractShopeePaymentSummaryMissingBlockIsTolerant(t *testing.T) {
	got := ExtractShopeePaymentSummary("หมายเลขคำสั่งซื้อ: #A\nยอดรวมค่าสินค้า: ฿100", "", "#A")
	if got.HasAny() {
		t.Fatalf("summary = %+v, want empty", got)
	}
}

func TestAllocateShopeeDiscountsByLineEqualExcludesShippingAndRounds(t *testing.T) {
	p100 := 100.0
	p200 := 200.0
	p38 := 38.0
	items := []models.BillItem{
		{Qty: 1, Price: &p100},
		{Qty: 1, Price: &p200},
		{SourceSKU: models.ShopeeShippingSourceSKU, Qty: 1, Price: &p38},
	}

	got := AllocateShopeeDiscountsByLine(items, 10.01)
	want := []float64{5.01, 5, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("discount[%d] = %v, want %v (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestAllocateShopeeDiscountsByLineCapsAndRedistributes(t *testing.T) {
	p2 := 2.0
	p100 := 100.0
	items := []models.BillItem{
		{Qty: 1, Price: &p2},
		{Qty: 1, Price: &p100},
	}

	got := AllocateShopeeDiscountsByLine(items, 20)
	if got[0] != 2 || got[1] != 18 {
		t.Fatalf("discounts = %v, want [2 18]", got)
	}
}
