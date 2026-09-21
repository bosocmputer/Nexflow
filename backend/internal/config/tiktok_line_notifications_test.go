package config

import (
	"testing"
	"time"
)

func TestParseTikTokShopLineNotificationCutoffRequiresExplicitRFC3339WhenEnabled(t *testing.T) {
	if _, err := parseTikTokShopLineNotificationCutoff(true, ""); err == nil {
		t.Fatal("enabled notifications must require an explicit cutoff")
	}
	if _, err := parseTikTokShopLineNotificationCutoff(true, "21/09/2026 10:00"); err == nil {
		t.Fatal("enabled notifications must reject non-RFC3339 cutoff")
	}
	want := time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC)
	got, err := parseTikTokShopLineNotificationCutoff(true, "2026-09-21T10:00:00+07:00")
	if err != nil || !got.Equal(want) {
		t.Fatalf("got=%s err=%v want=%s", got, err, want)
	}
	if got, err := parseTikTokShopLineNotificationCutoff(false, "not-used"); err != nil || !got.IsZero() {
		t.Fatalf("disabled cutoff got=%s err=%v", got, err)
	}
}
