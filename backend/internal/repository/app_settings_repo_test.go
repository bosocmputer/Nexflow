package repository

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/config"
)

func TestAppSettingsApplyToConfigUsesInstanceSMLSettings(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT key, value, is_secret, updated_at").
		WillReturnRows(sqlmock.NewRows([]string{"key", "value", "is_secret", "updated_at"}).
			AddRow("sml.rest_base_url", "http://172.24.0.1:8200", false, "2026-06-04").
			AddRow("sml.provider", "DATA", false, "2026-06-04").
			AddRow("sml.config_file", "SMLConfigDATA.xml", false, "2026-06-04").
			AddRow("sml.database", "aoy", false, "2026-06-04"))

	cfg := &config.Config{
		ShopeeSMLURL:        "http://env-sml",
		ShopeeSMLProvider:   "SMLGOH",
		ShopeeSMLConfigFile: "SMLConfigSMLGOH.xml",
		ShopeeSMLDatabase:   "SML1_2026",
	}

	if err := NewAppSettingsRepo(db).ApplyToConfig(cfg); err != nil {
		t.Fatalf("ApplyToConfig: %v", err)
	}

	if cfg.ShopeeSMLURL != "http://172.24.0.1:8200" ||
		cfg.ShopeeSMLProvider != "DATA" ||
		cfg.ShopeeSMLConfigFile != "SMLConfigDATA.xml" ||
		cfg.ShopeeSMLDatabase != "aoy" {
		t.Fatalf("config not applied from instance settings: %#v", cfg)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestRuntimeSettingValuesIncludesSMLProviderAndConfigFile(t *testing.T) {
	cfg := &config.Config{
		ShopeeSMLURL:        "http://172.24.0.1:8200",
		ShopeeSMLProvider:   "DATA",
		ShopeeSMLConfigFile: "SMLConfigDATA.xml",
		ShopeeSMLDatabase:   "aoy",
	}

	values := RuntimeSettingValues(cfg)
	if values["sml.provider"] != "DATA" {
		t.Fatalf("sml.provider = %q", values["sml.provider"])
	}
	if values["sml.config_file"] != "SMLConfigDATA.xml" {
		t.Fatalf("sml.config_file = %q", values["sml.config_file"])
	}
}

func TestSMLRuntimeSettingsUsesDBValuesAndStockURL(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT key, value, is_secret, updated_at").
		WillReturnRows(sqlmock.NewRows([]string{"key", "value", "is_secret", "updated_at"}).
			AddRow("sml.rest_base_url", "http://172.24.0.1:8200", false, "2026-06-04").
			AddRow("sml.provider", "DATA", false, "2026-06-04").
			AddRow("sml.config_file", "SMLConfigDATA.xml", false, "2026-06-04").
			AddRow("sml.database", "aoy", false, "2026-06-04").
			AddRow("sml.stock_request_url", "http://demserver.3bbddns.com:47308", false, "2026-06-04"))

	cfg := &config.Config{
		ShopeeSMLURL:        "http://env-sml",
		ShopeeSMLProvider:   "SMLGOH",
		ShopeeSMLConfigFile: "SMLConfigSMLGOH.xml",
		ShopeeSMLDatabase:   "SML1_2026",
	}

	settings, err := NewAppSettingsRepo(db).SMLRuntimeSettings(cfg)
	if err != nil {
		t.Fatalf("SMLRuntimeSettings: %v", err)
	}
	if settings.Provider != "DATA" || settings.ConfigFile != "SMLConfigDATA.xml" || settings.Database != "aoy" {
		t.Fatalf("unexpected SML runtime settings: %#v", settings)
	}
	if settings.StockRequestURL != "http://demserver.3bbddns.com:47308" {
		t.Fatalf("stock url = %q", settings.StockRequestURL)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestSMLRuntimeSettingsFallsBackToEnvWhenKeysMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT key, value, is_secret, updated_at").
		WillReturnRows(sqlmock.NewRows([]string{"key", "value", "is_secret", "updated_at"}).
			AddRow("sml.stock_request_url", "http://stock.local", false, "2026-06-04"))

	cfg := &config.Config{
		ShopeeSMLURL:        "http://env-sml",
		ShopeeSMLProvider:   "SMLGOH",
		ShopeeSMLConfigFile: "SMLConfigSMLGOH.xml",
		ShopeeSMLDatabase:   "SML1_2026",
	}

	settings, err := NewAppSettingsRepo(db).SMLRuntimeSettings(cfg)
	if err != nil {
		t.Fatalf("SMLRuntimeSettings: %v", err)
	}
	if settings.Provider != "SMLGOH" || settings.ConfigFile != "SMLConfigSMLGOH.xml" || settings.Database != "SML1_2026" {
		t.Fatalf("expected env fallback, got %#v", settings)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}
