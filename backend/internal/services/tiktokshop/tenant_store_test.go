package tiktokshop

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTenantConnectionStoreSyncsGatewayMetadataAtomically(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO tiktok_shop_connections").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO tiktok_shop_connections").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	connections := []GatewayConnection{
		validGatewayConnection("11111111-1111-4111-8111-111111111111", "7000714532876273420"),
		validGatewayConnection("22222222-2222-4222-8222-222222222222", "7000714532876273421"),
	}
	if err := NewTenantConnectionStore(database).Sync(t.Context(), connections); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTenantConnectionStoreRejectsInvalidMetadataBeforeDatabase(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	connection := validGatewayConnection("11111111-1111-4111-8111-111111111111", "7000714532876273420")
	connection.AccessExpiresAt = "not-a-time"
	if err := NewTenantConnectionStore(database).Sync(t.Context(), []GatewayConnection{connection}); !errors.Is(err, ErrInvalidGatewayConnection) {
		t.Fatalf("Sync() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func validGatewayConnection(connectionID, shopID string) GatewayConnection {
	return GatewayConnection{
		GatewayConnectionID: connectionID, ShopID: shopID, ShopName: "AOY", ShopRegion: "TH", SellerType: "LOCAL", ShopCode: "THAOY1",
		GrantedScopes:   []string{"seller.authorization.info", "seller.order.info"},
		AccessExpiresAt: "2026-09-11T10:00:00Z", RefreshExpiresAt: "2026-10-10T10:00:00Z",
		ConnectedAt: "2026-09-10T10:00:00Z", UpdatedAt: "2026-09-10T10:00:00Z",
	}
}
