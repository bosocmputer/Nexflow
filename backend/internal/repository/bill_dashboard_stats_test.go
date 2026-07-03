package repository

import (
	"errors"
	"math"
	"testing"
	"time"
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

func TestApplyPlatformSalesDashboardStatsZeroFillsAndShares(t *testing.T) {
	stats := map[string]interface{}{}
	window := platformSalesWindow{
		timezone:  platformSalesTimezone,
		fromDate:  "2026-07-01",
		toDate:    "2026-07-03",
		todayDate: "2026-07-03",
	}

	applyPlatformSalesDashboardStats(
		stats,
		[]platformSalesSummary{
			{Platform: "shopee", TotalAmount: 1200, TodayAmount: 250, OrderCount: 4, SentCount: 2, PendingCount: 1, NeedsReviewCount: 1},
			{Platform: "tiktok", TotalAmount: 300, TodayAmount: 0, OrderCount: 1, FailedCount: 1},
			{Platform: "manual", TotalAmount: 99999, TodayAmount: 99999, OrderCount: 99},
		},
		[]platformSalesTrendRow{
			{Date: "2026-07-01", Platform: "shopee", Amount: 800},
			{Date: "2026-07-03", Platform: "shopee", Amount: 250},
			{Date: "2026-07-03", Platform: "tiktok", Amount: 0},
		},
		window,
	)

	assertFloatStat(t, stats, "sales_mtd_total", 1500)
	assertFloatStat(t, stats, "sales_today_total", 250)
	assertIntStat(t, stats, "sales_mtd_order_count", 5)

	summaries, ok := stats["platform_sales"].([]platformSalesSummary)
	if !ok {
		t.Fatalf("platform_sales = %#v (%T), want []platformSalesSummary", stats["platform_sales"], stats["platform_sales"])
	}
	if len(summaries) != 3 {
		t.Fatalf("platform_sales len = %d, want 3", len(summaries))
	}
	if summaries[0].Platform != "shopee" || summaries[0].Label != "Shopee" || summaries[0].SharePct != 80 {
		t.Fatalf("shopee summary = %#v, want label Shopee and 80%% share", summaries[0])
	}
	if summaries[1].Platform != "lazada" || summaries[1].TotalAmount != 0 || summaries[1].SharePct != 0 {
		t.Fatalf("lazada zero-fill summary = %#v", summaries[1])
	}
	if summaries[2].Platform != "tiktok" || summaries[2].SharePct != 20 || summaries[2].FailedCount != 1 {
		t.Fatalf("tiktok summary = %#v, want 20%% share and failed count", summaries[2])
	}

	points, ok := stats["platform_sales_trend"].([]platformSalesTrendPoint)
	if !ok {
		t.Fatalf("platform_sales_trend = %#v (%T), want []platformSalesTrendPoint", stats["platform_sales_trend"], stats["platform_sales_trend"])
	}
	if len(points) != 3 {
		t.Fatalf("trend len = %d, want 3", len(points))
	}
	if points[1].Date != "2026-07-02" || points[1].ShopeeAmount != 0 || points[1].LazadaAmount != 0 || points[1].TiktokAmount != 0 {
		t.Fatalf("missing-day trend point = %#v, want zero-filled 2026-07-02", points[1])
	}

	meta, ok := stats["platform_sales_meta"].(platformSalesMeta)
	if !ok {
		t.Fatalf("platform_sales_meta = %#v (%T), want platformSalesMeta", stats["platform_sales_meta"], stats["platform_sales_meta"])
	}
	if meta.Timezone != "Asia/Bangkok" || meta.FromDate != "2026-07-01" || meta.ToDate != "2026-07-03" {
		t.Fatalf("meta = %#v", meta)
	}
}

func TestBuildPlatformSalesWindowUsesBangkokMonthAndToday(t *testing.T) {
	now := time.Date(2026, 6, 30, 18, 30, 0, 0, time.UTC) // 2026-07-01 01:30 in Bangkok.
	window := buildPlatformSalesWindow(now)

	if window.fromDate != "2026-07-01" || window.toDate != "2026-07-01" || window.todayDate != "2026-07-01" {
		t.Fatalf("window dates = from %s to %s today %s", window.fromDate, window.toDate, window.todayDate)
	}
	if window.fromTime.Location().String() != "Asia/Bangkok" {
		t.Fatalf("fromTime location = %s, want Asia/Bangkok", window.fromTime.Location())
	}
	if got := window.toExclusive.Sub(window.fromTime); got != 24*time.Hour {
		t.Fatalf("window duration = %s, want 24h for first day", got)
	}
}

func TestBuildPlatformSalesWindowForDateRangeUsesSelectedEndDate(t *testing.T) {
	now := time.Date(2026, 6, 30, 18, 30, 0, 0, time.UTC)
	window, err := buildPlatformSalesWindowForDateRange(now, "2026-06-15", "2026-06-20")
	if err != nil {
		t.Fatalf("buildPlatformSalesWindowForDateRange: %v", err)
	}

	if window.fromDate != "2026-06-15" || window.toDate != "2026-06-20" || window.todayDate != "2026-06-20" {
		t.Fatalf("window dates = from %s to %s today %s", window.fromDate, window.toDate, window.todayDate)
	}
	if got := window.toExclusive.Sub(window.fromTime); got != 6*24*time.Hour {
		t.Fatalf("window duration = %s, want inclusive 6-day range", got)
	}
}

func TestBuildPlatformSalesWindowForDateRangeRejectsInvalidRange(t *testing.T) {
	_, err := buildPlatformSalesWindowForDateRange(time.Now(), "2026-07-10", "2026-07-01")
	if !errors.Is(err, ErrInvalidDashboardDateRange) {
		t.Fatalf("error = %v, want ErrInvalidDashboardDateRange", err)
	}
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
