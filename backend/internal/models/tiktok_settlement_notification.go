package models

// TikTokSettlementLineNotification is immutable operational evidence for a
// settlement terminal outcome. It intentionally excludes buyer, address,
// phone, bank-account, and token data.
type TikTokSettlementLineNotification struct {
	RunID        string
	ShopID       string
	ShopName     string
	StatementID  string
	PaymentID    string
	Currency     string
	TotalAmount  float64
	OrderCount   int
	RCDocNo      string
	Outcome      string // sent, failed, unknown_result
	ErrorMessage string
}
