package repository

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"

	"nexflow/internal/models"
)

var (
	shopeeOrderDatePattern = regexp.MustCompile(`วันที่สั่งซื้อ\s*[:：]\s*([^\r\n<]+)`)
	shopeeSellerPattern    = regexp.MustCompile(`ผู้ขาย\s*[:：]\s*([^\r\n<]+)`)
	htmlTagPattern         = regexp.MustCompile(`<[^>]+>`)
	spacePattern           = regexp.MustCompile(`[ \t]+`)
)

func enrichShopeeBillRawData(b *models.Bill, itemCount int, stripBody bool) {
	if b == nil || b.RawData == nil || b.Source != "shopee_shipped" {
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(b.RawData, &raw); err != nil {
		return
	}

	orderID := strings.TrimSpace(stringField(raw, "order_id"))
	if orderID == "" {
		orderID = strings.TrimSpace(stringField(raw, "shopee_order_id"))
	}
	bodyText := stringField(raw, "body_text")
	bodyHTML := stringField(raw, "body_html")
	body := shopeeSummaryBody(bodyText, bodyHTML)
	block := shopeeOrderBlock(body, orderID)
	if block == "" {
		block = body
	}

	setIfEmpty(raw, "order_datetime", firstSubmatch(shopeeOrderDatePattern, block))
	setIfEmpty(raw, "seller_name", firstSubmatch(shopeeSellerPattern, block))
	setShopeeMoneyIfMissing(raw, "goods_total_amount", "ยอดรวมค่าสินค้า", bodyText, bodyHTML, orderID)
	setShopeeMoneyIfMissing(raw, "shipping_amount", "ค่าจัดส่งสินค้า", bodyText, bodyHTML, orderID)
	setShopeeMoneyIfMissing(raw, "paid_total_amount", "ยอดที่ต้องชำระทั้งหมด", bodyText, bodyHTML, orderID)
	summary := ExtractShopeeDiscountSummary(bodyText, bodyHTML, orderID)
	if summary.HasAny() {
		raw["discount_summary"] = summary
	}
	if _, exists := raw["shopee_coin_amount"]; !exists {
		if coinAmount, ok := ExtractShopeeCoinAmount(bodyText, bodyHTML, orderID, summary.TotalDiscountAmount); ok && coinAmount > 0 {
			raw["shopee_coin_amount"] = coinAmount
		}
	}
	if summary := ExtractShopeePaymentSummary(bodyText, bodyHTML, orderID); summary.HasAny() {
		raw["payment_summary"] = summary
	}
	if itemCount > 0 {
		raw["item_count"] = itemCount
	}

	if stripBody {
		delete(raw, "body_text")
		delete(raw, "body_html")
	}

	if out, err := json.Marshal(raw); err == nil {
		b.RawData = out
	}
}

// ExtractShopeeShippingAmount returns the "ค่าจัดส่งสินค้า" amount for one
// Shopee purchase order email block. It shares the same parsing path as list
// enrichment so persisted shipping-line behavior matches what users see.
func ExtractShopeeShippingAmount(bodyText, bodyHTML, orderID string) (float64, bool) {
	return ExtractShopeeMoneyLabel(bodyText, bodyHTML, orderID, "ค่าจัดส่งสินค้า")
}

// ShopeeDiscountSummary is persisted in bills.raw_data.discount_summary. The
// amounts are tolerant: missing labels stay zero, and coupon-code text is kept
// separate from money amounts.
type ShopeeDiscountSummary struct {
	ShopeeDiscountAmount float64  `json:"shopee_discount_amount"`
	ShopDiscountAmount   float64  `json:"shop_discount_amount"`
	TotalDiscountAmount  float64  `json:"total_discount_amount"`
	ShopeeDiscountCodes  []string `json:"shopee_discount_codes,omitempty"`
	ShopDiscountCodes    []string `json:"shop_discount_codes,omitempty"`
	AllocationMethod     string   `json:"allocation_method,omitempty"`
}

func (s ShopeeDiscountSummary) HasAny() bool {
	return s.TotalDiscountAmount > 0 || len(s.ShopeeDiscountCodes) > 0 || len(s.ShopDiscountCodes) > 0
}

// ExtractShopeeDiscountSummary returns Shopee/shop coupon amounts and codes for
// a single order block. It never treats coupon-code text as money because money
// extraction requires the Thai baht symbol.
func ExtractShopeeDiscountSummary(bodyText, bodyHTML, orderID string) ShopeeDiscountSummary {
	for _, body := range shopeeSummaryBodiesHTMLFirst(bodyText, bodyHTML) {
		block := shopeeOrderBlock(body, strings.TrimSpace(orderID))
		if block == "" {
			block = body
		}
		if summary := extractShopeeDiscountSummaryFromBlock(block); summary.HasAny() {
			return summary
		}
	}
	return ShopeeDiscountSummary{}
}

// ShopeePaymentSummary is persisted in bills.raw_data.payment_summary. It is
// intentionally tolerant because some Shopee emails omit payment details or use
// small whitespace variants around the payment method.
type ShopeePaymentSummary struct {
	PaymentMethod     string  `json:"payment_method,omitempty"`
	PaymentPaidAt     string  `json:"payment_paid_at,omitempty"`
	PaymentPaidAmount float64 `json:"payment_paid_amount,omitempty"`
	IsCreditDebitCard bool    `json:"is_credit_debit_card"`
	DocRefAmount      string  `json:"doc_ref_amount,omitempty"`
}

func (s ShopeePaymentSummary) HasAny() bool {
	return s.PaymentMethod != "" || s.PaymentPaidAt != "" || s.PaymentPaidAmount > 0
}

// ExtractShopeePaymentSummary returns payment details for a single Shopee order
// block. A missing or malformed block returns zero values and must not block
// bill creation.
func ExtractShopeePaymentSummary(bodyText, bodyHTML, orderID string) ShopeePaymentSummary {
	body := shopeeSummaryBody(bodyText, bodyHTML)
	block := shopeeOrderBlock(body, strings.TrimSpace(orderID))
	if block == "" {
		block = body
	}
	summary := extractShopeePaymentSummaryFromBlock(block)
	if summary.HasAny() || block == body {
		return summary
	}
	// Shopee payment details can be a single email-level section after all
	// order blocks. It is safe to fall back for payment/card reference because
	// Nexflow stores the total card charge in doc_ref, not a per-line amount.
	return extractShopeePaymentSummaryFromBlock(body)
}

func extractShopeeDiscountSummaryFromBlock(block string) ShopeeDiscountSummary {
	var summary ShopeeDiscountSummary
	summary.ShopeeDiscountAmount, summary.ShopeeDiscountCodes = extractShopeeDiscountParts(block, "โค้ดส่วนลดของ Shopee")
	summary.ShopDiscountAmount, summary.ShopDiscountCodes = extractShopeeDiscountParts(block, "โค้ดส่วนลดร้านค้า")
	summary.ShopeeDiscountAmount = roundMoney(summary.ShopeeDiscountAmount)
	summary.ShopDiscountAmount = roundMoney(summary.ShopDiscountAmount)
	summary.TotalDiscountAmount = roundMoney(summary.ShopeeDiscountAmount + summary.ShopDiscountAmount)
	if summary.HasAny() {
		summary.AllocationMethod = "proportional_by_gross_excluding_shipping"
	}
	return summary
}

var bahtAmountPattern = regexp.MustCompile(`฿\s*([\d,]+(?:\.\d+)?)`)

func extractShopeePaymentSummaryFromBlock(block string) ShopeePaymentSummary {
	var summary ShopeePaymentSummary
	summary.PaymentMethod = extractShopeeTextLabel("วิธีการชำระเงิน", block)
	summary.PaymentPaidAt = extractShopeeTextLabel("วันที่ชำระเงิน", block)
	if amount, ok := extractMoneyLabel("จำนวนเงินที่จ่าย", block); ok {
		summary.PaymentPaidAmount = roundMoney(amount)
	}
	summary.IsCreditDebitCard = isShopeeCreditDebitMethod(summary.PaymentMethod)
	if summary.IsCreditDebitCard && summary.PaymentPaidAmount > 0 {
		summary.DocRefAmount = formatShopeePaidAmount(summary.PaymentPaidAmount)
	}
	return summary
}

func extractShopeeTextLabel(label, block string) string {
	values := extractShopeeLabelValues(block, label)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func isShopeeCreditDebitMethod(method string) bool {
	normalized := strings.TrimSpace(method)
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "\t", "")
	return strings.Contains(normalized, "บัตรเครดิต") && strings.Contains(normalized, "บัตรเดบิต")
}

func formatShopeePaidAmount(amount float64) string {
	amount = roundMoney(amount)
	if amount == math.Trunc(amount) {
		return strconv.FormatInt(int64(amount), 10)
	}
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(amount, 'f', 2, 64), "0"), ".")
}

func extractShopeeDiscountParts(block, label string) (float64, []string) {
	var total float64
	codes := []string{}
	seenCodes := map[string]bool{}
	for _, value := range extractShopeeLabelValues(block, label) {
		if value == "" {
			continue
		}
		if money := bahtAmountPattern.FindStringSubmatch(value); len(money) >= 2 {
			clean := strings.ReplaceAll(money[1], ",", "")
			if v, err := strconv.ParseFloat(clean, 64); err == nil {
				total += v
			}
			continue
		}
		code := strings.Trim(value, " \t:-：")
		if code != "" && !seenCodes[code] {
			codes = append(codes, code)
			seenCodes[code] = true
		}
	}
	return total, codes
}

// ExtractShopeeMoneyLabel returns one Shopee money label scoped to orderID. It
// checks the HTML MIME part before text because Shopee sometimes omits summary
// rows from text/plain while keeping them in the HTML table.
func ExtractShopeeMoneyLabel(bodyText, bodyHTML, orderID, label string) (float64, bool) {
	for _, body := range shopeeSummaryBodiesHTMLFirst(bodyText, bodyHTML) {
		block := shopeeOrderBlock(body, strings.TrimSpace(orderID))
		if block == "" {
			block = body
		}
		if amount, ok := extractMoneyLabel(label, block); ok {
			return roundMoney(amount), true
		}
	}
	return 0, false
}

func CalcShopeeCoinAmount(goodsTotal, couponDiscount, paidTotal, shippingAmount float64) float64 {
	coinAmount := roundMoney(goodsTotal - couponDiscount - (paidTotal - shippingAmount))
	if coinAmount <= 0 {
		return 0
	}
	return coinAmount
}

func ExtractShopeeCoinAmount(bodyText, bodyHTML, orderID string, couponDiscount float64) (float64, bool) {
	goodsTotal, okGoods := ExtractShopeeMoneyLabel(bodyText, bodyHTML, orderID, "ยอดรวมค่าสินค้า")
	paidTotal, okPaid := ExtractShopeeMoneyLabel(bodyText, bodyHTML, orderID, "ยอดที่ต้องชำระทั้งหมด")
	shippingAmount, okShipping := ExtractShopeeMoneyLabel(bodyText, bodyHTML, orderID, "ค่าจัดส่งสินค้า")
	if !okGoods || !okPaid || !okShipping {
		return 0, false
	}
	return CalcShopeeCoinAmount(goodsTotal, couponDiscount, paidTotal, shippingAmount), true
}

// AllocateShopeeDiscountsByLine splits total discount proportionally by gross
// item value, excluding Shopee shipping rows, while capping each line at gross.
func AllocateShopeeDiscountsByLine(items []models.BillItem, totalDiscount float64) []float64 {
	out := make([]float64, len(items))
	target := roundMoney(totalDiscount)
	if target <= 0 {
		return out
	}

	active := discountableItemIndexes(items, out)
	grossTotal := 0.0
	for _, idx := range active {
		grossTotal = roundMoney(grossTotal + itemGross(items[idx]))
	}
	if grossTotal <= 0 {
		return out
	}
	if target > grossTotal {
		target = grossTotal
	}
	for _, idx := range active {
		gross := itemGross(items[idx])
		portion := roundMoney(target * gross / grossTotal)
		if portion > gross {
			portion = gross
		}
		out[idx] = portion
	}
	rebalanceShopeeDiscountResidual(items, active, out, target)
	return out
}

func ApplyShopeeDiscountsToItems(items []models.BillItem, totalDiscount float64) {
	discounts := AllocateShopeeDiscountsByLine(items, totalDiscount)
	for i := range items {
		items[i].DiscountAmount = discounts[i]
	}
}

func discountableItemIndexes(items []models.BillItem, allocated []float64) []int {
	out := []int{}
	for i, item := range items {
		if item.SourceSKU == models.ShopeeShippingSourceSKU {
			continue
		}
		if roundMoney(itemGross(item)-allocated[i]) > 0 {
			out = append(out, i)
		}
	}
	return out
}

func itemGross(item models.BillItem) float64 {
	if item.Price == nil || item.Qty <= 0 {
		return 0
	}
	return roundMoney(item.Qty * *item.Price)
}

func rebalanceShopeeDiscountResidual(items []models.BillItem, indexes []int, allocated []float64, target float64) {
	for attempts := 0; attempts < len(indexes)+2; attempts++ {
		diff := roundMoney(target - sumMoney(allocated))
		if math.Abs(diff) < 0.005 {
			return
		}
		if diff > 0 {
			idx := largestGrossWithCapacity(items, indexes, allocated)
			if idx < 0 {
				return
			}
			capacity := roundMoney(itemGross(items[idx]) - allocated[idx])
			allocated[idx] = roundMoney(allocated[idx] + math.Min(diff, capacity))
			continue
		}
		idx := largestAllocatedIndex(indexes, allocated)
		if idx < 0 {
			return
		}
		allocated[idx] = roundMoney(allocated[idx] - math.Min(-diff, allocated[idx]))
	}
}

func largestGrossWithCapacity(items []models.BillItem, indexes []int, allocated []float64) int {
	best := -1
	bestGross := -1.0
	for _, idx := range indexes {
		if roundMoney(itemGross(items[idx])-allocated[idx]) <= 0 {
			continue
		}
		gross := itemGross(items[idx])
		if gross > bestGross {
			best = idx
			bestGross = gross
		}
	}
	return best
}

func largestAllocatedIndex(indexes []int, allocated []float64) int {
	best := -1
	bestAmount := -1.0
	for _, idx := range indexes {
		if allocated[idx] > bestAmount {
			best = idx
			bestAmount = allocated[idx]
		}
	}
	return best
}

func sumMoney(values []float64) float64 {
	sum := 0.0
	for _, value := range values {
		sum = roundMoney(sum + value)
	}
	return sum
}

func roundMoney(v float64) float64 {
	return math.Round(v*100) / 100
}

func shopeeSummaryBody(bodyText, bodyHTML string) string {
	bodyText = strings.TrimSpace(bodyText)
	if bodyText != "" {
		if looksLikeHTML(bodyText) {
			if text := strings.TrimSpace(htmlToSummaryText(bodyText)); text != "" {
				return text
			}
		}
		return bodyText
	}
	return htmlToSummaryText(bodyHTML)
}

func shopeeSummaryBodiesHTMLFirst(bodyText, bodyHTML string) []string {
	bodies := []string{}
	add := func(body string) {
		body = strings.TrimSpace(body)
		if body == "" {
			return
		}
		for _, existing := range bodies {
			if existing == body {
				return
			}
		}
		bodies = append(bodies, body)
	}
	add(htmlToSummaryText(bodyHTML))
	if looksLikeHTML(bodyText) {
		add(htmlToSummaryText(bodyText))
	} else {
		add(bodyText)
	}
	return bodies
}

func looksLikeHTML(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "<html") ||
		strings.Contains(lower, "<body") ||
		strings.Contains(lower, "<table") ||
		strings.Contains(lower, "<tr") ||
		strings.Contains(lower, "<td") ||
		strings.Contains(lower, "<div") ||
		strings.Contains(lower, "<br")
}

func shopeeOrderBlock(body, orderID string) string {
	if body == "" || orderID == "" {
		return ""
	}
	idx := -1
	searchFrom := 0
	for {
		found := strings.Index(body[searchFrom:], orderID)
		if found < 0 {
			break
		}
		candidate := searchFrom + found
		if strings.LastIndex(body[:candidate], "หมายเลขคำสั่งซื้อ") >= 0 {
			idx = candidate
			break
		}
		searchFrom = candidate + len(orderID)
	}
	if idx < 0 {
		idx = strings.Index(body, orderID)
	}
	if idx < 0 {
		return ""
	}
	start := strings.LastIndex(body[:idx], "หมายเลขคำสั่งซื้อ")
	if start < 0 {
		start = idx
	}
	after := body[idx+len(orderID):]
	endRel := strings.Index(after, "หมายเลขคำสั่งซื้อ")
	if endRel < 0 {
		return body[start:]
	}
	return body[start : idx+len(orderID)+endRel]
}

func stringField(raw map[string]interface{}, key string) string {
	v, ok := raw[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func setIfEmpty(raw map[string]interface{}, key, value string) {
	if value == "" || stringField(raw, key) != "" {
		return
	}
	raw[key] = strings.TrimSpace(value)
}

func firstSubmatch(re *regexp.Regexp, text string) string {
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func setShopeeMoneyIfMissing(raw map[string]interface{}, key, label, bodyText, bodyHTML, orderID string) {
	if _, ok := raw[key]; ok {
		return
	}
	if v, ok := ExtractShopeeMoneyLabel(bodyText, bodyHTML, orderID, label); ok {
		raw[key] = v
	}
}

func extractMoneyLabel(label, text string) (float64, bool) {
	for _, value := range extractShopeeLabelValues(text, label) {
		m := bahtAmountPattern.FindStringSubmatch(value)
		if len(m) < 2 {
			continue
		}
		clean := strings.ReplaceAll(m[1], ",", "")
		if v, err := strconv.ParseFloat(clean, 64); err == nil {
			return v, true
		}
	}
	return 0, false
}

func extractShopeeLabelValues(block, label string) []string {
	if block == "" || label == "" {
		return nil
	}
	lines := strings.Split(block, "\n")
	values := []string{}
	for i, line := range lines {
		idx := strings.Index(line, label)
		if idx < 0 {
			continue
		}
		value := trimShopeeLabelValue(line[idx+len(label):])
		if value == "" {
			value = nextShopeeLabelValue(lines, i+1, label)
		}
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func nextShopeeLabelValue(lines []string, start int, currentLabel string) string {
	for i := start; i < len(lines); i++ {
		value := strings.TrimSpace(lines[i])
		if value == "" {
			continue
		}
		if strings.Contains(value, currentLabel) || strings.HasSuffix(value, ":") || strings.HasSuffix(value, "：") {
			return ""
		}
		return trimShopeeLabelValue(value)
	}
	return ""
}

func trimShopeeLabelValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, " \t:-：")
	return strings.TrimSpace(value)
}

func htmlToSummaryText(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = strings.ReplaceAll(s, "</tr>", "\n")
	s = strings.ReplaceAll(s, "</td>", " ")
	s = strings.ReplaceAll(s, "</div>", "\n")
	s = htmlTagPattern.ReplaceAllString(s, "")
	replacer := strings.NewReplacer(
		"&nbsp;", " ",
		"&#160;", " ",
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
	)
	s = replacer.Replace(s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(spacePattern.ReplaceAllString(line, " "))
	}
	return strings.Join(lines, "\n")
}
