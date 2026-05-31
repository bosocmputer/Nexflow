package anomaly

import (
	"nexflow/internal/models"
)

// DuplicateChecker is implemented by the bill repository to check for same-day duplicates.
type DuplicateChecker interface {
	ExistsDuplicateToday(source, customerName string, itemCodes []string) (bool, error)
}

// CustomerLookup is implemented by the bill repository to check whether a customer
// has been seen before in any prior bill.
type CustomerLookup interface {
	HasSeenCustomer(customerName string) (bool, error)
}

type Service struct {
	dupChecker  DuplicateChecker
	custLookup  CustomerLookup
}

func New(dc DuplicateChecker) *Service { return &Service{dupChecker: dc} }

// WithCustomerLookup adds the optional new-customer detection lookup.
func (s *Service) WithCustomerLookup(cl CustomerLookup) *Service {
	s.custLookup = cl
	return s
}

type CheckInput struct {
	Items        []models.BillItem
	CustomerName string
	Source       string
	// Lookup data from DB (passed from caller to avoid circular deps)
	AvgPrices  map[string]float64 // item_code -> avg_price
	MaxQtys    map[string]float64 // item_code -> max_ever_qty
	KnownItems map[string]bool
}

func (s *Service) Check(input CheckInput) []models.Anomaly {
	var anomalies []models.Anomaly

	hasBlock := 0
	hasWarn := 0

	// block: duplicate bill — same source + customer + item codes today
	if s.dupChecker != nil && input.CustomerName != "" {
		var codes []string
		for _, item := range input.Items {
			if item.ItemCode != nil {
				codes = append(codes, *item.ItemCode)
			}
		}
		if dup, err := s.dupChecker.ExistsDuplicateToday(input.Source, input.CustomerName, codes); err == nil && dup {
			anomalies = append(anomalies, models.Anomaly{
				Code:     "duplicate_bill",
				Severity: "block",
				Message:  "บิลซ้ำ — พบบิลลูกค้าเดียวกันสินค้าเดียวกันในวันนี้แล้ว",
			})
			hasBlock++
		}
	}

	// warn: new customer — ลูกค้าที่ไม่เคยมีบิลใน DB มาก่อน
	if s.custLookup != nil && input.CustomerName != "" {
		if seen, err := s.custLookup.HasSeenCustomer(input.CustomerName); err == nil && !seen {
			anomalies = append(anomalies, models.Anomaly{
				Code:     "new_customer",
				Severity: "warn",
				Message:  "ลูกค้าใหม่ — ยังไม่เคยมีบิลในระบบ",
			})
			hasWarn++
		}
	}

	for _, item := range input.Items {
		// block: qty = 0
		if item.Qty == 0 {
			anomalies = append(anomalies, models.Anomaly{
				Code:     "qty_zero",
				Severity: "block",
				Message:  "จำนวนสินค้าเป็น 0",
			})
			hasBlock++
		}

		// block: price = 0
		if item.Price != nil && *item.Price == 0 {
			anomalies = append(anomalies, models.Anomaly{
				Code:     "price_zero",
				Severity: "block",
				Message:  "ราคาสินค้าเป็น 0",
			})
			hasBlock++
		}

		if item.ItemCode == nil {
			continue
		}
		code := *item.ItemCode

		// warn: new item not in mapping
		if len(input.KnownItems) > 0 && !input.KnownItems[code] {
			anomalies = append(anomalies, models.Anomaly{
				Code:     "new_item",
				Severity: "warn",
				Message:  "สินค้าใหม่ที่ยังไม่เคยบันทึก",
			})
			hasWarn++
		}

		// warn: price anomaly
		if item.Price != nil && input.AvgPrices[code] > 0 {
			avg := input.AvgPrices[code]
			if *item.Price > avg*1.5 {
				anomalies = append(anomalies, models.Anomaly{
					Code:     "price_too_high",
					Severity: "warn",
					Message:  "ราคาสูงกว่าค่าเฉลี่ยเกิน 50%",
				})
				hasWarn++
			} else if *item.Price < avg*0.5 {
				anomalies = append(anomalies, models.Anomaly{
					Code:     "price_too_low",
					Severity: "warn",
					Message:  "ราคาต่ำกว่าค่าเฉลี่ยเกิน 50%",
				})
				hasWarn++
			}
		}

		// warn: qty suspicious
		if input.MaxQtys[code] > 0 && item.Qty > input.MaxQtys[code]*2 {
			anomalies = append(anomalies, models.Anomaly{
				Code:     "qty_suspicious",
				Severity: "warn",
				Message:  "จำนวนสูงผิดปกติ",
			})
			hasWarn++
		}
	}

	_ = hasBlock
	_ = hasWarn
	return anomalies
}

// CanAutoConfirm returns true if bill can be auto-confirmed
func CanAutoConfirm(anomalies []models.Anomaly, confidence, threshold float64) bool {
	blocks := 0
	warns := 0
	for _, a := range anomalies {
		if a.Severity == "block" {
			blocks++
		} else {
			warns++
		}
	}
	return confidence >= threshold && blocks == 0 && warns <= 1
}
