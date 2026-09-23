package handlers

// Parser for the TikTok Seller Center "Income" Excel export.  The export is
// useful financial evidence, but its withdrawal rows do not identify the
// included orders.  Keep that distinction in the type system so callers
// cannot accidentally turn an equal amount into an accounting assertion.

import (
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	tikTokIncomeOrdersSheet      = "รายละเอียดคำสั่งซื้อ"
	tikTokIncomeReportSheet      = "รายงาน"
	tikTokIncomeWithdrawalsSheet = "บันทึกการถอน"
	maxTikTokIncomeExportRows    = 10_000
	// This is deliberately a fixed confirmation value.  The upload cannot
	// prove withdrawal membership, so a caller must make an explicit
	// accounting assertion before a future RC candidate is persisted.
	tikTokIncomeBankReceiptConfirmation = "CONFIRM_TIKTOK_BANK_RECEIPT"
)

type tikTokIncomeExport struct {
	Currency                string
	Orders                  []tikTokIncomeOrder
	Withdrawals             []tikTokIncomeWithdrawal
	OrderPaymentTotalCents  int64
	ReportPaymentTotalCents int64
}

type tikTokIncomeOrder struct {
	OrderID         string
	TransactionType string
	PaymentTime     string
	Currency        string
	PaymentCents    int64
	IncomeCents     int64
	RefundCents     int64
	FeeCents        int64
	ShippingCents   int64
}

type tikTokIncomeWithdrawal struct {
	ID          string `json:"withdrawal_id"`
	Type        string `json:"type"`
	RequestedAt string `json:"requested_at"`
	AmountCents int64  `json:"amount_cents"`
	Status      string `json:"status"`
	CompletedAt string `json:"completed_at"`
	Currency    string `json:"currency"`
}

type tikTokIncomeWithdrawalSelection struct {
	Withdrawal                  tikTokIncomeWithdrawal
	CanCreateReceipt            bool
	RequiresOperatorAttestation bool
	BlockReason                 string
}

func parseTikTokIncomeExport(src io.Reader) (*tikTokIncomeExport, error) {
	f, err := excelize.OpenReader(src)
	if err != nil {
		return nil, fmt.Errorf("เปิดไฟล์รายได้ TikTok ไม่ได้: %w", err)
	}
	defer f.Close()

	for _, name := range []string{tikTokIncomeOrdersSheet, tikTokIncomeReportSheet, tikTokIncomeWithdrawalsSheet} {
		index, indexErr := f.GetSheetIndex(name)
		if indexErr != nil || index == -1 {
			return nil, fmt.Errorf("ไม่พบ sheet %q ในไฟล์รายได้ TikTok", name)
		}
	}

	orders, currency, total, err := parseTikTokIncomeOrders(f)
	if err != nil {
		return nil, err
	}
	reportTotal, err := parseTikTokIncomeReportTotal(f)
	if err != nil {
		return nil, err
	}
	withdrawals, err := parseTikTokIncomeWithdrawals(f, currency)
	if err != nil {
		return nil, err
	}
	return &tikTokIncomeExport{
		Currency: currency, Orders: orders, Withdrawals: withdrawals,
		OrderPaymentTotalCents: total, ReportPaymentTotalCents: reportTotal,
	}, nil
}

func (e tikTokIncomeExport) SelectWithdrawal(id string) (tikTokIncomeWithdrawalSelection, error) {
	id = strings.TrimSpace(id)
	for _, withdrawal := range e.Withdrawals {
		if withdrawal.ID != id {
			continue
		}
		selection := tikTokIncomeWithdrawalSelection{
			Withdrawal: withdrawal,
			// TikTok's export does not prove that a withdrawal owns these rows.
			// A human must separately confirm the bank receipt and report scope.
			RequiresOperatorAttestation: true,
		}
		switch {
		case !strings.EqualFold(withdrawal.Status, "Transferred"):
			selection.BlockReason = "รอบถอนนี้ยังไม่สำเร็จ"
		case strings.ToUpper(strings.TrimSpace(withdrawal.Currency)) != "THB" || strings.ToUpper(strings.TrimSpace(e.Currency)) != "THB":
			selection.BlockReason = "ไฟล์รายได้หรือรอบถอนไม่ใช่สกุล THB"
		case len(e.Orders) == 0:
			selection.BlockReason = "ไฟล์ไม่มีรายละเอียดคำสั่งซื้อ"
		case e.ReportPaymentTotalCents != e.OrderPaymentTotalCents:
			selection.BlockReason = "ยอดรายงานไม่ตรงกับรายละเอียดคำสั่งซื้อ"
		case withdrawal.AmountCents != e.OrderPaymentTotalCents:
			selection.BlockReason = "ยอดรอบถอนไม่ตรงกับยอดรายละเอียดคำสั่งซื้อ"
		default:
			// An exact amount is only a validation after the operator attests to
			// the export's scope. It is never treated as a TikTok membership key.
			selection.CanCreateReceipt = false
		}
		return selection, nil
	}
	return tikTokIncomeWithdrawalSelection{}, fmt.Errorf("ไม่พบรอบถอนเงินที่เลือกในไฟล์")
}

func validateTikTokIncomeReceiptAttestation(export tikTokIncomeExport, withdrawalID, confirmation string) error {
	selection, err := export.SelectWithdrawal(withdrawalID)
	if err != nil {
		return err
	}
	if selection.BlockReason != "" {
		return fmt.Errorf("ยังสร้าง RC ไม่ได้: %s", selection.BlockReason)
	}
	if strings.TrimSpace(confirmation) != tikTokIncomeBankReceiptConfirmation {
		return fmt.Errorf("กรุณายืนยันว่าเงินเข้าบัญชีจริงและไฟล์นี้เป็นรายละเอียดของรอบถอนที่เลือก")
	}
	seenOrders := make(map[string]struct{}, len(export.Orders))
	for _, order := range export.Orders {
		orderID := strings.TrimSpace(order.OrderID)
		if orderID == "" {
			return fmt.Errorf("ไฟล์มีคำสั่งซื้อที่ไม่มีเลขอ้างอิง")
		}
		if _, exists := seenOrders[orderID]; exists {
			return fmt.Errorf("ไฟล์มีคำสั่งซื้อ %s ซ้ำ จึงต้องตรวจไฟล์ก่อนสร้าง RC", orderID)
		}
		seenOrders[orderID] = struct{}{}
		if !tikTokIncomeIsOrderTransaction(order.TransactionType) {
			return fmt.Errorf("ไฟล์มีรายการ %q ที่ไม่ใช่คำสั่งซื้อ เช่น การปรับยอดหรือคืนเงิน", strings.TrimSpace(order.TransactionType))
		}
		if order.RefundCents != 0 {
			return fmt.Errorf("ไฟล์มีเงินคืน จึงต้องตรวจเอกสารลดหนี้ก่อนสร้าง RC")
		}
	}
	return nil
}

func tikTokIncomeIsOrderTransaction(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "คำสั่งซื้อ" || value == "order"
}

func parseTikTokIncomeOrders(f *excelize.File) ([]tikTokIncomeOrder, string, int64, error) {
	rows, err := tikTokIncomeRows(f, tikTokIncomeOrdersSheet)
	if err != nil {
		return nil, "", 0, err
	}
	if len(rows) < 2 {
		return nil, "", 0, fmt.Errorf("sheet รายละเอียดคำสั่งซื้อไม่มีข้อมูล")
	}
	headers := tikTokIncomeHeaders(rows[0])
	required := []string{"หมายเลขคำสั่งซื้อ/การปรับ", "ประเภทธุรกรรม", "เวลาที่ชำระคำสั่งซื้อ", "สกุลเงิน", "ยอดการชำระเงินทั้งหมด", "รายได้ทั้งหมด", "ยอดรวมเงินคืนหลังหักส่วนลดจากผู้ขาย", "ค่าธรรมเนียมทั้งหมด", "ยอดรวมค่าจัดส่งที่ร้านค้าจ่ายจริง"}
	for _, label := range required {
		if _, ok := headers[label]; !ok {
			return nil, "", 0, fmt.Errorf("sheet รายละเอียดคำสั่งซื้อไม่มีคอลัมน์ %q", label)
		}
	}

	orders := make([]tikTokIncomeOrder, 0, len(rows)-1)
	currency := ""
	var total int64
	for index, row := range rows[1:] {
		orderID := tikTokIncomeCell(row, headers, "หมายเลขคำสั่งซื้อ/การปรับ")
		if orderID == "" {
			continue
		}
		payment, err := tikTokIncomeCents(row, headers, "ยอดการชำระเงินทั้งหมด")
		if err != nil {
			return nil, "", 0, fmt.Errorf("รายละเอียดคำสั่งซื้อแถว %d: %w", index+2, err)
		}
		income, err := tikTokIncomeCents(row, headers, "รายได้ทั้งหมด")
		if err != nil {
			return nil, "", 0, fmt.Errorf("รายละเอียดคำสั่งซื้อแถว %d: %w", index+2, err)
		}
		refund, err := tikTokIncomeCents(row, headers, "ยอดรวมเงินคืนหลังหักส่วนลดจากผู้ขาย")
		if err != nil {
			return nil, "", 0, fmt.Errorf("รายละเอียดคำสั่งซื้อแถว %d: %w", index+2, err)
		}
		fee, err := tikTokIncomeCents(row, headers, "ค่าธรรมเนียมทั้งหมด")
		if err != nil {
			return nil, "", 0, fmt.Errorf("รายละเอียดคำสั่งซื้อแถว %d: %w", index+2, err)
		}
		shipping, err := tikTokIncomeCents(row, headers, "ยอดรวมค่าจัดส่งที่ร้านค้าจ่ายจริง")
		if err != nil {
			return nil, "", 0, fmt.Errorf("รายละเอียดคำสั่งซื้อแถว %d: %w", index+2, err)
		}
		rowCurrency := strings.ToUpper(tikTokIncomeCell(row, headers, "สกุลเงิน"))
		if rowCurrency == "" {
			return nil, "", 0, fmt.Errorf("รายละเอียดคำสั่งซื้อแถว %d: ไม่มีสกุลเงิน", index+2)
		}
		if currency == "" {
			currency = rowCurrency
		} else if currency != rowCurrency {
			return nil, "", 0, fmt.Errorf("รายละเอียดคำสั่งซื้อมีมากกว่าหนึ่งสกุลเงิน")
		}
		orders = append(orders, tikTokIncomeOrder{
			OrderID: orderID, TransactionType: tikTokIncomeCell(row, headers, "ประเภทธุรกรรม"),
			PaymentTime: tikTokIncomeCell(row, headers, "เวลาที่ชำระคำสั่งซื้อ"), Currency: rowCurrency,
			PaymentCents: payment, IncomeCents: income, RefundCents: refund, FeeCents: fee, ShippingCents: shipping,
		})
		total += payment
	}
	if len(orders) == 0 {
		return nil, "", 0, fmt.Errorf("sheet รายละเอียดคำสั่งซื้อไม่มีรายการที่นำมาใช้ได้")
	}
	return orders, currency, total, nil
}

func parseTikTokIncomeReportTotal(f *excelize.File) (int64, error) {
	rows, err := tikTokIncomeRows(f, tikTokIncomeReportSheet)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		for col, value := range row {
			if strings.TrimSpace(value) != "ยอดการชำระเงินทั้งหมด" {
				continue
			}
			for i := len(row) - 1; i > col; i-- {
				if strings.TrimSpace(row[i]) == "" {
					continue
				}
				amount, err := parseDecimalCents(strings.TrimSpace(row[i]))
				if err != nil {
					return 0, fmt.Errorf("ยอดการชำระเงินทั้งหมดใน sheet รายงานไม่ถูกต้อง: %w", err)
				}
				return amount, nil
			}
		}
	}
	return 0, fmt.Errorf("sheet รายงานไม่มีข้อมูลยอดการชำระเงินทั้งหมด")
}

func parseTikTokIncomeWithdrawals(f *excelize.File, currency string) ([]tikTokIncomeWithdrawal, error) {
	rows, err := tikTokIncomeRows(f, tikTokIncomeWithdrawalsSheet)
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("sheet บันทึกการถอนไม่มีข้อมูล")
	}
	headers := tikTokIncomeHeaders(rows[0])
	required := []string{"ประเภทธุรกรรม", "ID อ้างอิง", "เวลาส่งคำขอ", "จำนวน", "สถานะ", "เวลาที่สำเร็จ"}
	for _, label := range required {
		if _, ok := headers[label]; !ok {
			return nil, fmt.Errorf("sheet บันทึกการถอนไม่มีคอลัมน์ %q", label)
		}
	}
	withdrawals := make([]tikTokIncomeWithdrawal, 0, len(rows)-1)
	for index, row := range rows[1:] {
		id := tikTokIncomeCell(row, headers, "ID อ้างอิง")
		if id == "" {
			continue
		}
		amount, err := tikTokIncomeCents(row, headers, "จำนวน")
		if err != nil {
			return nil, fmt.Errorf("บันทึกการถอนแถว %d: %w", index+2, err)
		}
		withdrawals = append(withdrawals, tikTokIncomeWithdrawal{
			ID: id, Type: tikTokIncomeCell(row, headers, "ประเภทธุรกรรม"), RequestedAt: tikTokIncomeCell(row, headers, "เวลาส่งคำขอ"),
			AmountCents: amount, Status: tikTokIncomeCell(row, headers, "สถานะ"), CompletedAt: tikTokIncomeCell(row, headers, "เวลาที่สำเร็จ"), Currency: currency,
		})
	}
	if len(withdrawals) == 0 {
		return nil, fmt.Errorf("sheet บันทึกการถอนไม่มีรายการถอนเงิน")
	}
	return withdrawals, nil
}

func tikTokIncomeRows(f *excelize.File, sheet string) ([][]string, error) {
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("อ่าน sheet %s ไม่ได้: %w", sheet, err)
	}
	if len(rows) > maxTikTokIncomeExportRows+1 {
		return nil, fmt.Errorf("sheet %s มีเกิน %d แถว กรุณาแบ่งไฟล์แล้วนำเข้าใหม่", sheet, maxTikTokIncomeExportRows)
	}
	return rows, nil
}

func tikTokIncomeHeaders(row []string) map[string]int {
	result := make(map[string]int, len(row))
	for i, value := range row {
		value = strings.TrimSpace(value)
		if value != "" {
			result[value] = i
		}
	}
	return result
}

func tikTokIncomeCell(row []string, headers map[string]int, label string) string {
	i, ok := headers[label]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func tikTokIncomeCents(row []string, headers map[string]int, label string) (int64, error) {
	value := tikTokIncomeCell(row, headers, label)
	if value == "" {
		return 0, fmt.Errorf("ไม่มีค่า %s", label)
	}
	amount, err := parseDecimalCents(value)
	if err != nil {
		return 0, fmt.Errorf("ค่า %s ไม่ถูกต้อง", label)
	}
	return amount, nil
}
