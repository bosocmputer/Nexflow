package repository

import (
	"math"
	"testing"
)

func TestApplyPilotDashboardStats(t *testing.T) {
	stats := map[string]interface{}{}

	applyPilotDashboardStats(stats, 20, 3, 4, 10, 2)

	assertIntStat(t, stats, "pilot_30d_total", 20)
	assertIntStat(t, stats, "pilot_30d_needs_review", 3)
	assertIntStat(t, stats, "pilot_30d_pending", 4)
	assertIntStat(t, stats, "pilot_30d_sent", 10)
	assertIntStat(t, stats, "pilot_30d_failed", 2)
	assertIntStat(t, stats, "pilot_30d_remaining", 9)
	assertIntStat(t, stats, "pilot_30d_estimated_minutes_saved", 40)
	assertFloatStat(t, stats, "pilot_30d_success_rate", 50)
	assertFloatStat(t, stats, "pilot_30d_estimated_hours_saved", float64(40)/60)
}

func TestApplyPilotDashboardStatsWithNoBills(t *testing.T) {
	stats := map[string]interface{}{}

	applyPilotDashboardStats(stats, 0, 0, 0, 0, 0)

	assertIntStat(t, stats, "pilot_30d_total", 0)
	assertIntStat(t, stats, "pilot_30d_remaining", 0)
	assertIntStat(t, stats, "pilot_30d_estimated_minutes_saved", 0)
	assertFloatStat(t, stats, "pilot_30d_success_rate", 0)
	assertFloatStat(t, stats, "pilot_30d_estimated_hours_saved", 0)
}

func assertIntStat(t *testing.T, stats map[string]interface{}, key string, want int) {
	t.Helper()

	got, ok := stats[key].(int)
	if !ok {
		t.Fatalf("%s = %#v (%T), want int", key, stats[key], stats[key])
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", key, got, want)
	}
}

func assertFloatStat(t *testing.T, stats map[string]interface{}, key string, want float64) {
	t.Helper()

	got, ok := stats[key].(float64)
	if !ok {
		t.Fatalf("%s = %#v (%T), want float64", key, stats[key], stats[key])
	}
	if math.Abs(got-want) > 0.000001 {
		t.Fatalf("%s = %f, want %f", key, got, want)
	}
}
