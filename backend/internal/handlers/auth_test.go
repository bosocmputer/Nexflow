package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

type stubSalesOrderRouteReader struct {
	configured bool
	err        error
}

func (s stubSalesOrderRouteReader) HasConfiguredSalesOrderRoute(context.Context) (bool, error) {
	return s.configured, s.err
}

func TestAuthNavigationCapabilitiesReflectTenantRouteAndFailClosed(t *testing.T) {
	handler := NewAuthHandler(nil, 24, zap.NewNop()).WithNavigationCapabilities(stubSalesOrderRouteReader{configured: true})
	user := &models.User{}
	handler.attachNavigationCapabilities(context.Background(), user)
	if !user.NavigationCapabilities.SalesOrdersConfigured {
		t.Fatal("sales order capability = false, want true")
	}

	user.NavigationCapabilities.SalesOrdersConfigured = false
	handler = NewAuthHandler(nil, 24, zap.NewNop()).WithNavigationCapabilities(stubSalesOrderRouteReader{configured: true, err: errors.New("database unavailable")})
	handler.attachNavigationCapabilities(context.Background(), user)
	if user.NavigationCapabilities.SalesOrdersConfigured {
		t.Fatal("sales order capability = true after lookup error, want fail-closed false")
	}
}

func TestAuthMeDoesNotReportDatabaseFailureAsUnauthorized(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT id, email, name, role, password_hash, created_at").
		WithArgs("user-1").
		WillReturnError(errors.New("database unavailable"))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("user_id", "user-1")

	handler := NewAuthHandler(repository.NewUserRepo(db), 24, zap.NewNop())
	handler.Me(ctx)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestAuthMeReturnsUnauthorizedWhenUserNoLongerExists(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT id, email, name, role, password_hash, created_at").
		WithArgs("missing-user").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "name", "role", "password_hash", "created_at"}))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("user_id", "missing-user")

	handler := NewAuthHandler(repository.NewUserRepo(db), 24, zap.NewNop())
	handler.Me(ctx)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
