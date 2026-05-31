package repository

import (
	"testing"
	"time"
)

func TestTimeIDCursorRoundTrip(t *testing.T) {
	ts := time.Date(2026, 5, 18, 10, 11, 12, 123456789, time.FixedZone("ICT", 7*3600))
	id := "11111111-2222-3333-4444-555555555555"
	cursor := encodeTimeIDCursor(ts, id)
	gotTime, gotID, err := decodeTimeIDCursor(cursor)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if gotID != id {
		t.Fatalf("id = %q, want %q", gotID, id)
	}
	if !gotTime.Equal(ts.UTC()) {
		t.Fatalf("time = %s, want %s", gotTime, ts.UTC())
	}
}

func TestDecodeTimeIDCursorRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"not-base64", encodeTimeIDCursor(time.Now(), ""), "eHl6"} {
		if _, _, err := decodeTimeIDCursor(input); err == nil {
			t.Fatalf("decodeTimeIDCursor(%q) returned nil error", input)
		}
	}
}
