package handlers

import (
	"fmt"
	"testing"

	"github.com/lib/pq"

	"nexflow/internal/models"
)

func TestValidateLineMyShopConnectionUpsert(t *testing.T) {
	channelID := int64(12345)
	zeroChannelID := int64(0)

	tests := []struct {
		name          string
		in            models.LineMyShopConnectionUpsert
		requireAPIKey bool
		wantError     bool
	}{
		{
			name:          "valid create",
			in:            models.LineMyShopConnectionUpsert{Name: "OA Plus", APIKey: "apikey123", ChannelID: &channelID},
			requireAPIKey: true,
		},
		{
			name:          "create requires api key",
			in:            models.LineMyShopConnectionUpsert{Name: "OA Plus"},
			requireAPIKey: true,
			wantError:     true,
		},
		{
			name:          "edit allows blank api key",
			in:            models.LineMyShopConnectionUpsert{Name: "OA Plus"},
			requireAPIKey: false,
		},
		{
			name:          "reject api key whitespace",
			in:            models.LineMyShopConnectionUpsert{Name: "OA Plus", APIKey: "abc def"},
			requireAPIKey: true,
			wantError:     true,
		},
		{
			name:          "reject webhook secret newline",
			in:            models.LineMyShopConnectionUpsert{Name: "OA Plus", APIKey: "apikey123", WebhookSecret: "secret\n"},
			requireAPIKey: true,
			wantError:     true,
		},
		{
			name:          "reject non-positive channel id",
			in:            models.LineMyShopConnectionUpsert{Name: "OA Plus", APIKey: "apikey123", ChannelID: &zeroChannelID},
			requireAPIKey: true,
			wantError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateLineMyShopConnectionUpsert(tt.in, tt.requireAPIKey)
			if tt.wantError && got == "" {
				t.Fatal("validateLineMyShopConnectionUpsert() = empty, want error")
			}
			if !tt.wantError && got != "" {
				t.Fatalf("validateLineMyShopConnectionUpsert() = %q, want no error", got)
			}
		})
	}
}

func TestIsLineMyShopChannelIDConflict(t *testing.T) {
	err := fmt.Errorf("update line myshop connection: %w", &pq.Error{
		Code:       "23505",
		Constraint: "line_myshop_connections_channel_id_unique",
	})
	if !isLineMyShopChannelIDConflict(err) {
		t.Fatal("isLineMyShopChannelIDConflict() = false, want true")
	}

	other := fmt.Errorf("update line myshop connection: %w", &pq.Error{
		Code:       "23505",
		Constraint: "other_unique_constraint",
	})
	if isLineMyShopChannelIDConflict(other) {
		t.Fatal("isLineMyShopChannelIDConflict(other) = true, want false")
	}
}
