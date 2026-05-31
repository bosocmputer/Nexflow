package emailservice

import (
	"context"
	"testing"

	"nexflow/internal/models"

	"github.com/emersion/go-imap/v2"
)

func TestClassifyDispatchWarning(t *testing.T) {
	tests := []struct {
		name     string
		warning  string
		wantCode string
		wantSkip bool
	}{
		{
			name:     "duplicate is user skipped",
			warning:  "เมลนี้เคยสร้างบิลแล้ว",
			wantCode: "duplicate",
			wantSkip: true,
		},
		{
			name:     "duplicate or empty is user skipped",
			warning:  "duplicate_or_empty: ไม่มีบิลใหม่จากเมลนี้ อาจซ้ำหรือไม่มีรายการสินค้าที่ใช้ได้",
			wantCode: "duplicate_or_empty",
			wantSkip: true,
		},
		{
			name:     "duplicate or empty thai label is user skipped",
			warning:  "ไม่มีบิลใหม่จากเมลนี้ อาจซ้ำหรือไม่มีรายการสินค้าที่ใช้ได้",
			wantCode: "duplicate_or_empty",
			wantSkip: true,
		},
		{
			name:     "empty items remains warning",
			warning:  "AI extract shopee email: empty items",
			wantCode: "empty_items",
			wantSkip: false,
		},
		{
			name:     "no attachment is user skipped",
			warning:  "no supported attachment",
			wantCode: "no_supported_attachment",
			wantSkip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, label, userSkipped := classifyDispatchWarning(tt.warning)
			if code != tt.wantCode {
				t.Fatalf("code = %q, want %q", code, tt.wantCode)
			}
			if label == "" {
				t.Fatal("label should be user-readable")
			}
			if userSkipped != tt.wantSkip {
				t.Fatalf("userSkipped = %v, want %v", userSkipped, tt.wantSkip)
			}
		})
	}
}

func TestPollResultStatus(t *testing.T) {
	tests := []struct {
		name string
		res  PollResult
		want string
	}{
		{
			name: "duplicate only is not error",
			res: PollResult{
				MessagesFound: 3,
				Skipped:       3,
				Summary:       modelsSummary(3, 0, 3, 3, 0),
			},
			want: "no_new_mail",
		},
		{
			name: "created bill is ok",
			res: PollResult{
				MessagesFound: 1,
				Processed:     1,
				Summary:       modelsSummary(1, 1, 0, 0, 0),
			},
			want: "ok",
		},
		{
			name: "processing warning",
			res: PollResult{
				MessagesFound:   1,
				Skipped:         1,
				ProcessWarnings: []string{"empty orders"},
			},
			want: "warning",
		},
		{
			name: "shutdown cancel is interrupted",
			res: PollResult{
				Err: context.Canceled,
			},
			want: "interrupted",
		},
		{
			name: "shutdown cancel after progress is partial",
			res: PollResult{
				Err:         context.Canceled,
				Skipped:     10,
				LastSeenUID: 42,
			},
			want: "partial",
		},
		{
			name: "large mailbox is backlog",
			res: PollResult{
				MessagesFound: 200,
				Skipped:       150,
				Backlog:       50,
				Limited:       true,
				Summary:       modelsSummary(150, 0, 150, 150, 0),
			},
			want: "backlog",
		},
		{
			name: "created bills with backlog stays backlog when skips are user-level",
			res: PollResult{
				MessagesFound: 150,
				Processed:     57,
				Skipped:       93,
				Backlog:       520,
				Limited:       true,
				Summary:       modelsSummary(150, 57, 2, 91, 0),
			},
			want: "backlog",
		},
		{
			name: "created bills with true processing failure still needs attention",
			res: PollResult{
				MessagesFound:   150,
				Processed:       57,
				Skipped:         93,
				Backlog:         520,
				Limited:         true,
				Summary:         modelsSummary(150, 57, 2, 90, 1),
				ProcessWarnings: []string{"empty orders"},
			},
			want: "warning",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.res.Status(); got != tt.want {
				t.Fatalf("Status() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCandidateUIDsUsesProgressAndLimit(t *testing.T) {
	got := candidateUIDs([]imap.UID{1, 2, 3, 4}, 2, 1)
	if got.Total != 2 {
		t.Fatalf("Total = %d, want 2", got.Total)
	}
	if got.Backlog != 1 {
		t.Fatalf("Backlog = %d, want 1", got.Backlog)
	}
	if !got.Limited {
		t.Fatal("Limited = false, want true")
	}
	if len(got.Selected) != 1 || got.Selected[0] != 3 {
		t.Fatalf("Selected = %#v, want [3]", got.Selected)
	}
}

func modelsSummary(scanned, created, alreadyProcessed, skippedUser, failed int) models.IMAPPollSummary {
	return models.IMAPPollSummary{
		Scanned:          scanned,
		Created:          created,
		AlreadyProcessed: alreadyProcessed,
		SkippedUser:      skippedUser,
		Failed:           failed,
	}
}
