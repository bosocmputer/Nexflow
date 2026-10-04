package linenotify

import (
	"encoding/json"
	"testing"

	"nexflow/internal/models"
)

func TestBuildLineNotificationSampleCoversPublishedCatalog(t *testing.T) {
	for _, event := range models.LineNotificationEventCatalog() {
		t.Run(event.Key, func(t *testing.T) {
			sample, ok := BuildLineNotificationSample(event.Key, "https://nexflow-aoy.nextstep-soft.com")
			if !ok {
				t.Fatalf("missing sample for %s", event.Key)
			}
			if sample.EventKey != event.Key || sample.AltText == "" || sample.MessageText == "" {
				t.Fatalf("incomplete sample: %#v", sample)
			}
			if !json.Valid(sample.FlexPayload) || string(sample.FlexPayload) == "{}" {
				t.Fatalf("invalid flex payload for %s: %s", event.Key, sample.FlexPayload)
			}
		})
	}
}
