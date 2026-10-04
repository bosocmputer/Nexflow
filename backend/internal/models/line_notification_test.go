package models

import "testing"

func TestNormalizeLineNotificationEventKeysRejectsUnknownAndDuplicates(t *testing.T) {
	got, err := NormalizeLineNotificationEventKeys([]string{
		LineNotificationEventShopeeOrderNew,
		" " + LineNotificationEventTikTokOrderCancelled + " ",
		LineNotificationEventShopeeOrderNew,
	})
	if err != nil {
		t.Fatalf("NormalizeLineNotificationEventKeys: %v", err)
	}
	if len(got) != 2 || got[0] != LineNotificationEventShopeeOrderNew || got[1] != LineNotificationEventTikTokOrderCancelled {
		t.Fatalf("normalized keys = %#v", got)
	}

	if _, err := NormalizeLineNotificationEventKeys([]string{"unknown.event"}); err == nil {
		t.Fatal("expected unknown event key to fail closed")
	}
}

func TestDefaultLineNotificationEventKeysFocusOnOperationalWork(t *testing.T) {
	defaults := DefaultLineNotificationEventKeys()
	want := map[string]bool{
		LineNotificationEventShopeeOrderNew:       true,
		LineNotificationEventTikTokOrderNew:       true,
		LineNotificationEventNextStepOrderNew:     true,
		LineNotificationEventShopeeOrderCancelled: true,
		LineNotificationEventTikTokOrderCancelled: true,
		LineNotificationEventShopeeSMLReview:      true,
		LineNotificationEventTikTokSMLReview:      true,
		LineNotificationEventShopeeSMLFailed:      true,
		LineNotificationEventTikTokSMLFailed:      true,
	}
	if len(defaults) != len(want) {
		t.Fatalf("default keys = %#v, want %d", defaults, len(want))
	}
	for _, key := range defaults {
		if !want[key] {
			t.Fatalf("unexpected default event key %q", key)
		}
	}
}
