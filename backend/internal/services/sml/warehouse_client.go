package sml

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

// WarehouseClient reads SML v4 warehouse and shelf master data. It reuses the
// same REST base URL + headers as product/customer/supplier clients.
type WarehouseClient struct {
	cfg        PartyConfig
	httpClient *http.Client
	log        *zap.Logger
}

type Warehouse struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	Name1      string  `json:"name_1,omitempty"`
	Name2      string  `json:"name_2,omitempty"`
	Status     int     `json:"status"`
	BranchCode string  `json:"branch_code,omitempty"`
	Shelves    []Shelf `json:"shelf,omitempty"`
}

type Shelf struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	Name1         string `json:"name_1,omitempty"`
	Name2         string `json:"name_2,omitempty"`
	WarehouseCode string `json:"whcode"`
	Status        int    `json:"status"`
	Remark        string `json:"remark,omitempty"`
}

func NewWarehouseClient(cfg PartyConfig, log *zap.Logger) *WarehouseClient {
	return &WarehouseClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		log:        log,
	}
}

func (c *WarehouseClient) headers() map[string]string {
	return map[string]string{
		"guid":           c.cfg.GUID,
		"provider":       c.cfg.Provider,
		"configFileName": c.cfg.ConfigFile,
		"databaseName":   c.cfg.Database,
		"Accept":         "application/json",
	}
}

type warehousePageInfo struct {
	PageNo      int `json:"pageNo"`
	PageSize    int `json:"pageSize"`
	RecordCount int `json:"recordCount"`
	PageCount   int `json:"pageCount"`
	Size        int `json:"size"`
	Page        int `json:"page"`
	TotalRecord int `json:"total_record"`
	MaxPage     int `json:"max_page"`
}

type warehouseListResponse struct {
	Success bool              `json:"success"`
	Data    []warehouseRecord `json:"data"`
	Meta    struct {
		Total int `json:"total"`
		Page  int `json:"page"`
		Size  int `json:"size"`
	} `json:"meta"`
	Pages warehousePageInfo `json:"pages"`
}

type warehouseOneResponse struct {
	Success bool            `json:"success"`
	Data    warehouseRecord `json:"data"`
}

type warehouseRecord struct {
	Code       string        `json:"code"`
	Name       string        `json:"name"`
	Name1      string        `json:"name_1"`
	Name2      string        `json:"name_2"`
	Status     int           `json:"status"`
	BranchCode string        `json:"branch_code"`
	Shelves    []shelfRecord `json:"shelf"`
	ShelvesV1  []shelfRecord `json:"shelves"`
}

type shelfRecord struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	Name1         string `json:"name_1"`
	Name2         string `json:"name_2"`
	WarehouseCode string `json:"whcode"`
	Status        int    `json:"status"`
	Remark        string `json:"remark"`
}

func normalizeWarehouse(w warehouseRecord) Warehouse {
	name := w.Name1
	if name == "" {
		name = w.Name2
	}
	if name == "" {
		name = w.Name
	}
	shelfRows := w.Shelves
	if len(shelfRows) == 0 {
		shelfRows = w.ShelvesV1
	}
	shelves := make([]Shelf, 0, len(shelfRows))
	for _, s := range shelfRows {
		shelves = append(shelves, normalizeShelf(s, w.Code))
	}
	return Warehouse{
		Code:       w.Code,
		Name:       name,
		Name1:      w.Name1,
		Name2:      w.Name2,
		Status:     w.Status,
		BranchCode: w.BranchCode,
		Shelves:    shelves,
	}
}

func normalizeShelf(s shelfRecord, fallbackWH string) Shelf {
	name := s.Name1
	if name == "" {
		name = s.Name2
	}
	if name == "" {
		name = s.Name
	}
	wh := s.WarehouseCode
	if wh == "" {
		wh = fallbackWH
	}
	return Shelf{
		Code:          s.Code,
		Name:          name,
		Name1:         s.Name1,
		Name2:         s.Name2,
		WarehouseCode: wh,
		Status:        s.Status,
		Remark:        s.Remark,
	}
}

func (c *WarehouseClient) fetchPage(ctx context.Context, page, size int) (*warehouseListResponse, error) {
	u := fmt.Sprintf("%s/api/v1/ic/warehouses?page=%d&size=%d", c.cfg.BaseURL, page, size)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range c.headers() {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sml warehouse fetch page %d: %w", page, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sml warehouse read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sml warehouse page %d HTTP %d: %s", page, resp.StatusCode, string(body))
	}
	var wr warehouseListResponse
	if err := json.Unmarshal(body, &wr); err != nil {
		return nil, fmt.Errorf("sml warehouse decode page %d: %w", page, err)
	}
	if !wr.Success {
		return nil, fmt.Errorf("sml warehouse page %d: success=false", page)
	}
	return &wr, nil
}

func (c *WarehouseClient) FetchAll(ctx context.Context) ([]Warehouse, error) {
	const pageSize = 200
	var out []Warehouse
	for page := 1; ; page++ {
		wr, err := c.fetchPage(ctx, page, pageSize)
		if err != nil {
			return nil, err
		}
		for _, row := range wr.Data {
			out = append(out, normalizeWarehouse(row))
		}
		total := wr.Pages.RecordCount
		if total == 0 {
			total = wr.Pages.TotalRecord
		}
		if total == 0 {
			total = wr.Meta.Total
		}
		maxPage := wr.Pages.PageCount
		if maxPage == 0 {
			maxPage = wr.Pages.MaxPage
		}
		if maxPage == 0 && wr.Meta.Size > 0 {
			maxPage = (wr.Meta.Total + wr.Meta.Size - 1) / wr.Meta.Size
		}
		if len(wr.Data) == 0 {
			break
		}
		if total > 0 && len(out) >= total {
			break
		}
		if maxPage > 0 && page >= maxPage {
			break
		}
	}
	for i := range out {
		if len(out[i].Shelves) > 0 {
			continue
		}
		detail, err := c.Get(ctx, out[i].Code)
		if err != nil {
			c.log.Warn("sml warehouse detail skipped",
				zap.String("warehouse", out[i].Code),
				zap.Error(err),
			)
			continue
		}
		if detail != nil {
			out[i] = *detail
		}
	}
	return out, nil
}

func (c *WarehouseClient) Get(ctx context.Context, code string) (*Warehouse, error) {
	u := fmt.Sprintf("%s/api/v1/ic/warehouses/%s", c.cfg.BaseURL, url.PathEscape(code))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range c.headers() {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sml warehouse/%s: %w", code, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sml warehouse/%s HTTP %d: %s", code, resp.StatusCode, string(body))
	}
	var wr warehouseOneResponse
	if err := json.Unmarshal(body, &wr); err != nil {
		return nil, err
	}
	if !wr.Success || wr.Data.Code == "" {
		return nil, nil
	}
	w := normalizeWarehouse(wr.Data)
	return &w, nil
}

func (c *WarehouseClient) IsConfigured() bool {
	return c.cfg.BaseURL != "" && c.cfg.GUID != "" &&
		c.cfg.Provider != "" && c.cfg.ConfigFile != "" && c.cfg.Database != ""
}
