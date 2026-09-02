package smlprofile

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const maxMetricDurationsPerSeries = 512

type metricKey struct {
	Tenant  string
	Route   string
	Profile string
	Status  string
}

type metricSeries struct {
	Count       int64
	ErrorCount  int64
	DurationsMS []int64
}

type RequestMetricSnapshot struct {
	Tenant     string `json:"tenant"`
	Route      string `json:"route"`
	Profile    string `json:"profile"`
	Status     string `json:"status"`
	Count      int64  `json:"count"`
	ErrorCount int64  `json:"error_count"`
	P50MS      int64  `json:"p50_ms"`
	P95MS      int64  `json:"p95_ms"`
	P99MS      int64  `json:"p99_ms"`
}

type Metrics struct {
	mu     sync.Mutex
	series map[metricKey]*metricSeries
}

var DefaultMetrics = NewMetrics()

func NewMetrics() *Metrics {
	return &Metrics{series: make(map[metricKey]*metricSeries)}
}

// ObserveRequest intentionally accepts only bounded dimensions. Order SN,
// document number, correlation ID, and other unbounded identifiers are never
// metric labels.
func (m *Metrics) ObserveRequest(tenant, route, profile, status string, duration time.Duration, failed bool) {
	if m == nil {
		return
	}
	key := metricKey{
		Tenant: boundedMetricLabel(tenant, "unknown"), Route: boundedMetricLabel(route, "unknown"),
		Profile: boundedMetricLabel(profile, "legacy"), Status: boundedMetricLabel(status, "unknown"),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	series := m.series[key]
	if series == nil {
		series = &metricSeries{}
		m.series[key] = series
	}
	series.Count++
	if failed {
		series.ErrorCount++
	}
	ms := duration.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	if len(series.DurationsMS) == maxMetricDurationsPerSeries {
		copy(series.DurationsMS, series.DurationsMS[1:])
		series.DurationsMS[len(series.DurationsMS)-1] = ms
	} else {
		series.DurationsMS = append(series.DurationsMS, ms)
	}
}

func (m *Metrics) Snapshot() []RequestMetricSnapshot {
	if m == nil {
		return []RequestMetricSnapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]RequestMetricSnapshot, 0, len(m.series))
	for key, series := range m.series {
		durations := append([]int64(nil), series.DurationsMS...)
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		out = append(out, RequestMetricSnapshot{
			Tenant: key.Tenant, Route: key.Route, Profile: key.Profile, Status: key.Status,
			Count: series.Count, ErrorCount: series.ErrorCount,
			P50MS: percentile(durations, 0.50), P95MS: percentile(durations, 0.95), P99MS: percentile(durations, 0.99),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i].Tenant + out[i].Route + out[i].Profile + out[i].Status
		right := out[j].Tenant + out[j].Route + out[j].Profile + out[j].Status
		return left < right
	})
	return out
}

func percentile(values []int64, quantile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1)*quantile + 0.5)
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func boundedMetricLabel(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	if len(value) > 64 {
		return "invalid"
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return "invalid"
		}
	}
	return value
}
