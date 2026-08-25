package repository

import (
	"errors"
	"testing"
)

func TestMarketplaceSharedPoolPolicyAction(t *testing.T) {
	tests := []struct {
		name             string
		policy           string
		conversionStatus string
		wantPromote      bool
		wantErr          error
	}{
		{name: "managed member stays managed", policy: "managed", conversionStatus: "ready"},
		{name: "ready blocked member is promoted", policy: "blocked", conversionStatus: "ready", wantPromote: true},
		{name: "blocked member must have proven conversion", policy: "blocked", conversionStatus: "needs_review", wantErr: ErrMarketplaceUnitNotReady},
		{name: "manual member requires an explicit separate action", policy: "manual_unmanaged", conversionStatus: "ready", wantErr: ErrMarketplaceSharedPoolPolicyLocked},
		{name: "zeroing member cannot be interrupted", policy: "zeroing", conversionStatus: "ready", wantErr: ErrMarketplaceSharedPoolPolicyLocked},
		{name: "disabled member cannot be silently re-enabled", policy: "disabled_zero", conversionStatus: "ready", wantErr: ErrMarketplaceSharedPoolPolicyLocked},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			promote, err := marketplaceSharedPoolPolicyAction(test.policy, test.conversionStatus)
			if promote != test.wantPromote || !errors.Is(err, test.wantErr) {
				t.Fatalf("promote=%v err=%v, want promote=%v err=%v", promote, err, test.wantPromote, test.wantErr)
			}
		})
	}
}
