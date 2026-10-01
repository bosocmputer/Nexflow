package config

import "testing"

func TestLineMyShopRetirementDefaultsOffButPreservesExplicitOverride(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-only-32-character-jwt-secret-0000")
	t.Setenv("DATABASE_URL", "postgres://test.invalid/nexflow")
	t.Setenv("ENABLE_LINE_MYSHOP", "")
	if Load().LineMyShopEnabled {
		t.Fatal("retired MyShop ingestion must default off")
	}
	t.Setenv("ENABLE_LINE_MYSHOP", "true")
	if !Load().LineMyShopEnabled {
		t.Fatal("an explicit legacy tenant override must remain effective")
	}
}
