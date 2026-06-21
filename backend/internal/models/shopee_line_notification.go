package models

import "encoding/json"

type ShopeeSettlementLineRun struct {
	ID                    string
	ShopID                int64
	ShopLabel             string
	Status                string
	ReleaseDateFrom       string
	ReleaseDateTo         string
	TotalCount            int
	ReadyCount            int
	BlockedCount          int
	SentCount             int
	BuyerTotalAmountTotal float64
	PayoutAmountTotal     float64
	DeductionAmountTotal  float64
	Items                 []ShopeeSettlementLineItem
}

type ShopeeSettlementLineItem struct {
	OrderSN           string
	EscrowReleaseTime string
	PayoutAmount      float64
	EscrowAmount      float64
	BuyerTotalAmount  float64
	DeductionAmount   float64
	InvoiceAmount     float64
	DifferenceAmount  float64
	Status            string
	BlockReason       string
	RawEscrow         json.RawMessage
}
