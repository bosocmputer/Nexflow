package handlers

import "testing"

func TestNormalizeInstanceURL(t *testing.T) {
	got, msg := normalizeInstanceURL(" http://192.168.2.109:8200/ ")
	if msg != "" {
		t.Fatalf("normalizeInstanceURL() error = %q, want none", msg)
	}
	if got != "http://192.168.2.109:8200" {
		t.Fatalf("url = %q, want normalized base URL", got)
	}
}

func TestNormalizeInstanceURLRejectsInvalidScheme(t *testing.T) {
	if _, msg := normalizeInstanceURL("ftp://192.168.2.109:8200"); msg == "" {
		t.Fatal("normalizeInstanceURL() returned empty error, want invalid scheme error")
	}
}

func TestNormalizeInstanceSettingDatabaseName(t *testing.T) {
	def := settingDef{Key: "sml.database"}
	got, msg := normalizeInstanceSetting(def, " SML1_2026 ")
	if msg != "" {
		t.Fatalf("normalizeInstanceSetting() error = %q, want none", msg)
	}
	if got != "SML1_2026" {
		t.Fatalf("database = %q, want trimmed name", got)
	}
}

func TestNormalizeInstanceSettingRejectsUnsafeDatabaseName(t *testing.T) {
	def := settingDef{Key: "sml.database"}
	if _, msg := normalizeInstanceSetting(def, "SML1_2026;DROP"); msg == "" {
		t.Fatal("normalizeInstanceSetting() returned empty error, want database validation error")
	}
}

func TestNormalizeInstanceSettingAutoConfirmThreshold(t *testing.T) {
	def := settingDef{Key: "automation.auto_confirm_threshold"}
	got, msg := normalizeInstanceSetting(def, "0.85")
	if msg != "" {
		t.Fatalf("normalizeInstanceSetting() error = %q, want none", msg)
	}
	if got != "0.85" {
		t.Fatalf("threshold = %q, want 0.85", got)
	}
	if _, msg := normalizeInstanceSetting(def, "1.5"); msg == "" {
		t.Fatal("normalizeInstanceSetting() accepted threshold > 1")
	}
}
