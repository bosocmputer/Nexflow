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

type DocNoClient struct {
	cfg        PartyConfig
	httpClient *http.Client
	log        *zap.Logger
}

type NextDocNoRequest struct {
	Route   string
	Prefix  string
	Format  string
	DocDate string
}

type NextDocNoResponse struct {
	Route     string `json:"route"`
	TransFlag int    `json:"trans_flag"`
	Prefix    string `json:"prefix"`
	Format    string `json:"format"`
	DocDate   string `json:"doc_date"`
	LastDocNo string `json:"last_doc_no"`
	LastSeq   int    `json:"last_seq"`
	NextDocNo string `json:"next_doc_no"`
	NextSeq   int    `json:"next_seq"`
}

type nextDocNoEnvelope struct {
	Success bool              `json:"success"`
	Data    NextDocNoResponse `json:"data"`
	Message string            `json:"message"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func NewDocNoClient(cfg PartyConfig, log *zap.Logger) *DocNoClient {
	return &DocNoClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 12 * time.Second},
		log:        log,
	}
}

func (c *DocNoClient) IsConfigured() bool {
	return c != nil && c.cfg.BaseURL != "" && c.cfg.GUID != "" && c.cfg.Database != ""
}

func (c *DocNoClient) Next(ctx context.Context, req NextDocNoRequest) (*NextDocNoResponse, error) {
	if !c.IsConfigured() {
		return nil, fmt.Errorf("SML doc_no client not configured")
	}
	u, err := url.Parse(c.cfg.BaseURL + "/api/v1/ic/doc-no/next")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("route", req.Route)
	q.Set("prefix", req.Prefix)
	q.Set("format", req.Format)
	if req.DocDate != "" {
		q.Set("doc_date", req.DocDate)
	}
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-Api-Key", c.cfg.GUID)
	httpReq.Header.Set("guid", c.cfg.GUID)
	httpReq.Header.Set("databaseName", c.cfg.Database)
	httpReq.Header.Set("X-Tenant", c.cfg.Database)
	httpReq.Header.Set("Accept", "application/json")
	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sml doc_no next: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var env nextDocNoEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("sml doc_no next HTTP %d decode failed: %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || !env.Success {
		msg := env.Message
		if env.Error != nil && env.Error.Message != "" {
			msg = env.Error.Message
		}
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("sml doc_no next HTTP %d: %s", resp.StatusCode, msg)
	}
	if env.Data.NextDocNo == "" || env.Data.NextSeq < 1 {
		return nil, fmt.Errorf("sml doc_no next returned incomplete data")
	}
	if c.log != nil {
		c.log.Info("sml_doc_no_next",
			zap.String("route", env.Data.Route),
			zap.String("prefix", env.Data.Prefix),
			zap.String("format", env.Data.Format),
			zap.String("next_doc_no", env.Data.NextDocNo),
			zap.Int("next_seq", env.Data.NextSeq),
			zap.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	}
	return &env.Data, nil
}
