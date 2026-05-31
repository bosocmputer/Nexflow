package sml

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

// PartyConfig holds the auth headers shared by GET /v3/api/customer and supplier.
// All fields reuse the SHOPEE_SML_* config (party master lives on the same SML 248
// instance as saleinvoice/purchaseorder).
type PartyConfig struct {
	BaseURL    string
	GUID       string
	Provider   string
	ConfigFile string
	Database   string
}

// Party is the subset of customer/supplier fields Nexflow needs for the
// per-channel default picker. Customer responses lack address/telephone in
// the list view (they appear only in /customer/{code}) so those columns may
// be empty; supplier responses do include them.
type Party struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Name1      string `json:"name_1,omitempty"`
	Name2      string `json:"name_2,omitempty"`
	NameEng1   string `json:"name_eng_1,omitempty"`
	FirstName  string `json:"first_name,omitempty"`
	LastName   string `json:"last_name,omitempty"`
	Firstname  string `json:"firstname,omitempty"`
	Lastname   string `json:"lastname,omitempty"`
	TaxID      string `json:"tax_id,omitempty"`
	CardID     string `json:"card_id,omitempty"`
	BranchType int    `json:"branch_type,omitempty"`
	BranchCode string `json:"branch_code,omitempty"`
	ARStatus   int    `json:"ar_status,omitempty"`
	APStatus   int    `json:"ap_status,omitempty"`
	Telephone  string `json:"telephone,omitempty"`
	Address    string `json:"address,omitempty"`
	Remark     string `json:"remark,omitempty"`
}

// PartyClient is a paginated GET-only client for SML 248 party master.
//
// Quick-create (POST) is intentionally NOT supported — the legacy /restapi/
// schema requires ~25 fields with non-obvious naming and the v3 endpoint
// returns NullPointerException without documenting the required shape.
// Admin must create parties in SML manually then click "refresh" in Nexflow.
type PartyClient struct {
	cfg        PartyConfig
	httpClient *http.Client
	log        *zap.Logger
}

func NewPartyClient(cfg PartyConfig, log *zap.Logger) *PartyClient {
	return &PartyClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		log:        log,
	}
}

func (c *PartyClient) headers() map[string]string {
	return map[string]string{
		"guid":           c.cfg.GUID,
		"provider":       c.cfg.Provider,
		"configFileName": c.cfg.ConfigFile,
		"databaseName":   c.cfg.Database,
		"Accept":         "application/json",
	}
}

type partyListResponse struct {
	Success bool    `json:"success"`
	Message string  `json:"message"`
	Code    string  `json:"code"`
	Data    []Party `json:"data"`
	Meta    struct {
		Total int `json:"total"`
		Page  int `json:"page"`
		Size  int `json:"size"`
	} `json:"meta"`
	Pages struct {
		Size        int `json:"size"`
		Page        int `json:"page"`
		TotalRecord int `json:"total_record"`
		MaxPage     int `json:"max_page"`
	} `json:"pages"`
}

// partyPath maps "customer"/"supplier" to the v1 REST path.
func partyPath(endpoint string) string {
	if endpoint == "supplier" {
		return "ap/suppliers"
	}
	return "ar/customers"
}

// fetchPage returns one page of {endpoint} (e.g. "customer" or "supplier").
func (c *PartyClient) fetchPage(ctx context.Context, endpoint string, page, size int) (*partyListResponse, error) {
	u := fmt.Sprintf("%s/api/v1/%s?page=%d&size=%d",
		c.cfg.BaseURL, partyPath(endpoint), page, size)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range c.headers() {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sml %s fetch page %d: %w", endpoint, page, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sml %s read body: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sml %s page %d HTTP %d: %s",
			endpoint, page, resp.StatusCode, string(body))
	}
	var pr partyListResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("sml %s decode page %d: %w", endpoint, page, err)
	}
	if !pr.Success {
		detail := pr.Message
		if detail == "" {
			detail = string(body)
		}
		return nil, fmt.Errorf("sml %s page %d: success=false code=%s message=%s", endpoint, page, pr.Code, detail)
	}
	for i := range pr.Data {
		normalizeParty(&pr.Data[i])
	}
	return &pr, nil
}

// fetchAll loops pages until total_record reached.
func (c *PartyClient) fetchAll(ctx context.Context, endpoint string) ([]Party, error) {
	const pageSize = 200
	var out []Party
	for page := 1; ; page++ {
		pr, err := c.fetchPage(ctx, endpoint, page, pageSize)
		if err != nil {
			return nil, err
		}
		out = append(out, pr.Data...)
		total := pr.Pages.TotalRecord
		if total == 0 {
			total = pr.Meta.Total
		}
		maxPage := pr.Pages.MaxPage
		if maxPage == 0 && pr.Meta.Size > 0 {
			maxPage = (pr.Meta.Total + pr.Meta.Size - 1) / pr.Meta.Size
		}
		if len(pr.Data) == 0 {
			break
		}
		if total > 0 && len(out) >= total {
			break
		}
		if maxPage > 0 && page >= maxPage {
			break
		}
	}
	return out, nil
}

// FetchAllCustomers returns the full customer list (all pages). 1004 records
// at the time of writing — ~5 pages × 200 each.
func (c *PartyClient) FetchAllCustomers(ctx context.Context) ([]Party, error) {
	return c.fetchAll(ctx, "customer")
}

// FetchAllSuppliers returns the full supplier list. 500 records at writing.
func (c *PartyClient) FetchAllSuppliers(ctx context.Context) ([]Party, error) {
	return c.fetchAll(ctx, "supplier")
}

type partyDetailResponse struct {
	Success bool  `json:"success"`
	Data    Party `json:"data"`
}

type createPartyResponse struct {
	Success bool  `json:"success"`
	Data    Party `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Message string `json:"message"`
}

type CustomerCreateInput struct {
	Code       string `json:"code"`
	ARStatus   *int   `json:"ar_status,omitempty"`
	FirstName  string `json:"first_name,omitempty"`
	LastName   string `json:"last_name,omitempty"`
	Name1      string `json:"name_1,omitempty"`
	NameEng1   string `json:"name_eng_1,omitempty"`
	Address    string `json:"address,omitempty"`
	Remark     string `json:"remark,omitempty"`
	TaxID      string `json:"tax_id,omitempty"`
	BranchType *int   `json:"branch_type,omitempty"`
	BranchCode string `json:"branch_code,omitempty"`
	CardID     string `json:"card_id,omitempty"`
}

type SupplierCreateInput struct {
	Code       string `json:"code"`
	APStatus   *int   `json:"ap_status,omitempty"`
	Firstname  string `json:"firstname,omitempty"`
	Lastname   string `json:"lastname,omitempty"`
	Name1      string `json:"name_1,omitempty"`
	NameEng1   string `json:"name_eng_1,omitempty"`
	Address    string `json:"address,omitempty"`
	Remark     string `json:"remark,omitempty"`
	TaxID      string `json:"tax_id,omitempty"`
	BranchType *int   `json:"branch_type,omitempty"`
	BranchCode string `json:"branch_code,omitempty"`
	CardID     string `json:"card_id,omitempty"`
}

// GetCustomer fetches a single customer by code. Returns nil (no error) when
// SML responds with 404 / data:null.
func (c *PartyClient) GetCustomer(ctx context.Context, code string) (*Party, error) {
	return c.getOne(ctx, "customer", code)
}

// GetSupplier fetches a single supplier by code.
func (c *PartyClient) GetSupplier(ctx context.Context, code string) (*Party, error) {
	return c.getOne(ctx, "supplier", code)
}

func (c *PartyClient) CreateCustomer(ctx context.Context, input CustomerCreateInput) (int, *Party, error) {
	input.Code = strings.TrimSpace(input.Code)
	input.FirstName = strings.TrimSpace(input.FirstName)
	input.LastName = strings.TrimSpace(input.LastName)
	input.Name1 = strings.TrimSpace(input.Name1)
	input.NameEng1 = strings.TrimSpace(input.NameEng1)
	input.Address = strings.TrimSpace(input.Address)
	input.Remark = strings.TrimSpace(input.Remark)
	input.TaxID = strings.TrimSpace(input.TaxID)
	input.BranchCode = strings.TrimSpace(input.BranchCode)
	input.CardID = strings.TrimSpace(input.CardID)
	return c.createOne(ctx, "customer", input)
}

func (c *PartyClient) CreateSupplier(ctx context.Context, input SupplierCreateInput) (int, *Party, error) {
	input.Code = strings.TrimSpace(input.Code)
	input.Firstname = strings.TrimSpace(input.Firstname)
	input.Lastname = strings.TrimSpace(input.Lastname)
	input.Name1 = strings.TrimSpace(input.Name1)
	input.NameEng1 = strings.TrimSpace(input.NameEng1)
	input.Address = strings.TrimSpace(input.Address)
	input.Remark = strings.TrimSpace(input.Remark)
	input.TaxID = strings.TrimSpace(input.TaxID)
	input.BranchCode = strings.TrimSpace(input.BranchCode)
	input.CardID = strings.TrimSpace(input.CardID)
	return c.createOne(ctx, "supplier", input)
}

func (c *PartyClient) createOne(ctx context.Context, endpoint string, payload any) (int, *Party, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	u := fmt.Sprintf("%s/api/v1/%s", c.cfg.BaseURL, partyPath(endpoint))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	for k, v := range c.headers() {
		req.Header.Set(k, v)
	}
	req.Header.Set("X-Api-Key", c.cfg.GUID)
	req.Header.Set("X-Tenant", c.cfg.Database)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("sml %s create: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var pr createPartyResponse
	if err := json.Unmarshal(respBody, &pr); err != nil {
		return resp.StatusCode, nil, fmt.Errorf("sml %s create HTTP %d decode failed: %w", endpoint, resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusCreated || !pr.Success {
		msg := pr.Message
		if pr.Error != nil && pr.Error.Message != "" {
			msg = pr.Error.Message
		}
		if msg == "" {
			msg = string(respBody)
		}
		return resp.StatusCode, nil, errors.New(msg)
	}
	normalizeParty(&pr.Data)
	if pr.Data.Code == "" {
		return resp.StatusCode, nil, fmt.Errorf("sml %s create returned empty code", endpoint)
	}
	return resp.StatusCode, &pr.Data, nil
}

func (c *PartyClient) getOne(ctx context.Context, endpoint, code string) (*Party, error) {
	u := fmt.Sprintf("%s/api/v1/%s/%s",
		c.cfg.BaseURL, partyPath(endpoint), url.PathEscape(code))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range c.headers() {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sml %s/%s: %w", endpoint, code, err)
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
		return nil, fmt.Errorf("sml %s/%s HTTP %d: %s",
			endpoint, code, resp.StatusCode, string(body))
	}
	var pr partyDetailResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, err
	}
	if !pr.Success || pr.Data.Code == "" {
		return nil, nil
	}
	normalizeParty(&pr.Data)
	return &pr.Data, nil
}

func normalizeParty(p *Party) {
	firstName := strings.TrimSpace(p.FirstName)
	if firstName == "" {
		firstName = strings.TrimSpace(p.Firstname)
	}
	lastName := strings.TrimSpace(p.LastName)
	if lastName == "" {
		lastName = strings.TrimSpace(p.Lastname)
	}
	if p.Name == "" {
		p.Name = p.Name1
	}
	if p.Name == "" {
		p.Name = strings.TrimSpace(strings.TrimSpace(firstName + " " + lastName))
	}
}

// IsConfigured reports whether the client has the SML 248 base URL + headers
// needed to fetch parties. Used at boot to decide whether to start the cache.
func (c *PartyClient) IsConfigured() bool {
	return c.cfg.BaseURL != "" && c.cfg.GUID != "" &&
		c.cfg.Provider != "" && c.cfg.ConfigFile != "" && c.cfg.Database != ""
}
