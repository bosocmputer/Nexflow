package linemyshop

import (
	"encoding/json"
	"strings"
)

type OrderListResponse struct {
	CurrentPage int            `json:"currentPage"`
	Data        []OrderSummary `json:"data"`
	PerPage     int            `json:"perPage"`
	TotalPage   int            `json:"totalPage"`
	TotalRow    int            `json:"totalRow"`
}

type OrderSummary struct {
	OrderNumber    string `json:"orderNumber"`
	OrderStatus    string `json:"orderStatus"`
	PaymentStatus  string `json:"paymentStatus"`
	ShipmentStatus string `json:"shipmentStatus"`
	LastUpdatedAt  string `json:"lastUpdatedAt"`
}

func DecodeOrderList(body []byte) (OrderListResponse, error) {
	var out OrderListResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	for i := range out.Data {
		out.Data[i].OrderNumber = strings.TrimSpace(out.Data[i].OrderNumber)
		out.Data[i].OrderStatus = strings.ToUpper(strings.TrimSpace(out.Data[i].OrderStatus))
		out.Data[i].PaymentStatus = strings.ToUpper(strings.TrimSpace(out.Data[i].PaymentStatus))
		out.Data[i].ShipmentStatus = strings.ToUpper(strings.TrimSpace(out.Data[i].ShipmentStatus))
	}
	return out, nil
}
