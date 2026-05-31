package models

import "time"

type AIUsageLog struct {
	ID               string                 `json:"id"`
	Provider         string                 `json:"provider"`
	Model            string                 `json:"model"`
	Feature          string                 `json:"feature"`
	Operation        string                 `json:"operation"`
	BillID           *string                `json:"bill_id,omitempty"`
	InputTokens      int                    `json:"input_tokens"`
	OutputTokens     int                    `json:"output_tokens"`
	TotalTokens      int                    `json:"total_tokens"`
	EstimatedCostUSD float64                `json:"estimated_cost_usd"`
	DurationMs       *int                   `json:"duration_ms,omitempty"`
	Status           string                 `json:"status"`
	Error            string                 `json:"error,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
}

type AIUsageEntry struct {
	Provider         string
	Model            string
	Feature          string
	Operation        string
	BillID           *string
	InputTokens      int
	OutputTokens     int
	TotalTokens      int
	EstimatedCostUSD float64
	DurationMs       *int
	Status           string
	Error            string
	Metadata         map[string]interface{}
}

type AIUsageFilter struct {
	DateFrom string `form:"date_from"`
	DateTo   string `form:"date_to"`
	Model    string `form:"model"`
	Feature  string `form:"feature"`
	Status   string `form:"status"`
	Page     int    `form:"page,default=1"`
	PageSize int    `form:"page_size,default=50"`
}

type AIUsageBucket struct {
	Key              string  `json:"key"`
	Label            string  `json:"label"`
	Requests         int     `json:"requests"`
	Success          int     `json:"success"`
	Errors           int     `json:"errors"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

type AIUsageSummary struct {
	Total            AIUsageBucket   `json:"total"`
	Today            AIUsageBucket   `json:"today"`
	SevenDays        AIUsageBucket   `json:"seven_days"`
	Month            AIUsageBucket   `json:"month"`
	ByModel          []AIUsageBucket `json:"by_model"`
	ByFeature        []AIUsageBucket `json:"by_feature"`
	TopExpensive     []AIUsageLog    `json:"top_expensive"`
	Daily            []AIUsageBucket `json:"daily"`
	EstimatedTHBRate float64         `json:"estimated_thb_rate"`
}
