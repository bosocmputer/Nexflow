package main

import (
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
)

func TestResolveBootstrapAdminCredentialsProductionRequiresPassword(t *testing.T) {
	_, err := resolveBootstrapAdminCredentials("production", func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "BOOTSTRAP_ADMIN_PASSWORD") {
		t.Fatalf("expected missing password error, got %v", err)
	}
}

func TestResolveBootstrapAdminCredentialsProductionRejectsShortPassword(t *testing.T) {
	values := map[string]string{"BOOTSTRAP_ADMIN_PASSWORD": "too-short"}
	_, err := resolveBootstrapAdminCredentials("production", func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "at least 16 characters") {
		t.Fatalf("expected short password error, got %v", err)
	}
}

func TestResolveBootstrapAdminCredentialsProductionUsesSuppliedValues(t *testing.T) {
	values := map[string]string{
		"BOOTSTRAP_ADMIN_EMAIL":    "  owner@ploy.example  ",
		"BOOTSTRAP_ADMIN_NAME":     "  Ploy Owner  ",
		"BOOTSTRAP_ADMIN_PASSWORD": "a-strong-random-password-2026",
	}

	got, err := resolveBootstrapAdminCredentials("production", func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("resolve credentials: %v", err)
	}
	if got.Email != "owner@ploy.example" {
		t.Fatalf("email = %q", got.Email)
	}
	if got.Name != "Ploy Owner" {
		t.Fatalf("name = %q", got.Name)
	}
	if got.Password != values["BOOTSTRAP_ADMIN_PASSWORD"] {
		t.Fatal("password was not preserved")
	}
}

func TestResolveBootstrapAdminCredentialsDevelopmentKeepsLocalFallback(t *testing.T) {
	got, err := resolveBootstrapAdminCredentials("development", func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolve credentials: %v", err)
	}
	if got.Email != "admin@nexflow.local" || got.Name != "Admin" || got.Password != "admin1234" {
		t.Fatalf("unexpected local fallback: %#v", got)
	}
}

func TestSeedAdminUserProductionFailsClosedWithoutPassword(t *testing.T) {
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM users").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	err = seedAdminUser(db, "production", zap.NewNop())
	if err == nil || !strings.Contains(err.Error(), "BOOTSTRAP_ADMIN_PASSWORD") {
		t.Fatalf("expected fail-closed bootstrap error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestSeedAdminUserExistingTenantDoesNotRequireBootstrapPassword(t *testing.T) {
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM users").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	if err := seedAdminUser(db, "production", zap.NewNop()); err != nil {
		t.Fatalf("existing tenant seed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}
