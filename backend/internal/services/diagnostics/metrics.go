package diagnostics

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type evidenceMetricKey struct {
	Route  string
	Status string
}

type EvidenceMetricSnapshot struct {
	Route      string `json:"route"`
	Status     string `json:"status"`
	Count      int64  `json:"count"`
	DurationMS int64  `json:"duration_ms"`
}

type EvidenceMetrics struct {
	mu     sync.Mutex
	series map[evidenceMetricKey]EvidenceMetricSnapshot
}

var DefaultEvidenceMetrics = NewEvidenceMetrics()

func NewEvidenceMetrics() *EvidenceMetrics {
	return &EvidenceMetrics{series: make(map[evidenceMetricKey]EvidenceMetricSnapshot)}
}

func boundedMetricValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 64 {
		return "unknown"
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return "invalid"
		}
	}
	return value
}

func (m *EvidenceMetrics) Observe(route, status string, duration time.Duration) {
	if m == nil {
		return
	}
	key := evidenceMetricKey{Route: boundedMetricValue(route), Status: boundedMetricValue(status)}
	m.mu.Lock()
	defer m.mu.Unlock()
	value := m.series[key]
	value.Route = key.Route
	value.Status = key.Status
	value.Count++
	value.DurationMS += max(0, duration.Milliseconds())
	m.series[key] = value
}

func (m *EvidenceMetrics) Snapshot() []EvidenceMetricSnapshot {
	if m == nil {
		return []EvidenceMetricSnapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]EvidenceMetricSnapshot, 0, len(m.series))
	for _, value := range m.series {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Route+result[i].Status < result[j].Route+result[j].Status
	})
	return result
}
