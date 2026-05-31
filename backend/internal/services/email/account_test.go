package emailservice

import (
	"errors"
	"testing"

	"nexflow/internal/models"
)

func TestShouldDrainBacklog(t *testing.T) {
	tests := []struct {
		name string
		res  PollResult
		want bool
	}{
		{
			name: "limited result continues in background",
			res:  PollResult{Limited: true, Backlog: 100},
			want: true,
		},
		{
			name: "backlog result continues in background",
			res:  PollResult{Backlog: 1},
			want: true,
		},
		{
			name: "clean result stops",
			res:  PollResult{},
			want: false,
		},
		{
			name: "hard error stops",
			res:  PollResult{Err: errors.New("imap failed"), Limited: true, Backlog: 100},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldDrainBacklog(tt.res); got != tt.want {
				t.Fatalf("shouldDrainBacklog() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIMAPPollJobProgressAggregatesAcrossCycles(t *testing.T) {
	var progress imapPollJobProgress

	firstBase := progress.snapshotBase()
	first := PollResult{
		MessagesFound: 10,
		Backlog:       5,
		Limited:       true,
		Summary: models.IMAPPollSummary{
			Scanned:          5,
			Created:          2,
			AlreadyProcessed: 1,
			SkippedUser:      2,
			Failed:           1,
		},
		Details: []models.IMAPPollDetail{
			{Status: "processed", ReasonCode: "accepted"},
			{Status: "skipped", ReasonCode: "duplicate"},
			{Status: "skipped", ReasonCode: "fetch_body_failed"},
		},
	}
	progress.applyFinal(firstBase, first)

	secondBase := progress.snapshotBase()
	second := PollResult{
		MessagesFound: 5,
		Summary: models.IMAPPollSummary{
			Scanned:          5,
			Created:          3,
			AlreadyProcessed: 1,
			SkippedUser:      1,
			Failed:           0,
		},
		Details: []models.IMAPPollDetail{
			{Status: "processed", ReasonCode: "accepted"},
			{Status: "skipped", ReasonCode: "duplicate_or_empty"},
		},
	}
	progress.applyFinal(secondBase, second)

	if progress.TotalCount != 10 {
		t.Fatalf("TotalCount = %d, want 10", progress.TotalCount)
	}
	if progress.ScannedCount != 10 {
		t.Fatalf("ScannedCount = %d, want 10", progress.ScannedCount)
	}
	if progress.CreatedCount != 5 {
		t.Fatalf("CreatedCount = %d, want 5", progress.CreatedCount)
	}
	if progress.SkippedCount != 4 {
		t.Fatalf("SkippedCount = %d, want 4", progress.SkippedCount)
	}
	if progress.FailedCount != 1 {
		t.Fatalf("FailedCount = %d, want 1", progress.FailedCount)
	}
	if progress.BacklogCount != 0 {
		t.Fatalf("BacklogCount = %d, want 0", progress.BacklogCount)
	}
	if progress.ReasonCounts["accepted"] != 2 {
		t.Fatalf("accepted reason count = %d, want 2", progress.ReasonCounts["accepted"])
	}
	if progress.ReasonCounts["fetch_body_failed"] != 1 {
		t.Fatalf("fetch_body_failed reason count = %d, want 1", progress.ReasonCounts["fetch_body_failed"])
	}
}

func TestPollJobSkippedCountDoesNotDoubleCountDuplicates(t *testing.T) {
	summary := models.IMAPPollSummary{
		Scanned:          50,
		Created:          3,
		AlreadyProcessed: 47,
		SkippedUser:      47,
		Failed:           0,
	}

	if got := pollJobSkippedCount(summary); got != 47 {
		t.Fatalf("pollJobSkippedCount() = %d, want 47", got)
	}
}
