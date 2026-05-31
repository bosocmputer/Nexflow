package catalog

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"

	"nexflow/internal/repository"
)

func TestRefreshOneUpsertsProductMissingFromLocalCatalog(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ic/products/SHIP_POL" {
			t.Fatalf("path = %s, want /api/v1/ic/products/SHIP_POL", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": {
				"code": "SHIP_POL",
				"name": "ค่าขนส่งสินค้า",
				"unit_standard": "ครั้ง",
				"group_code": "SERVICE",
				"balance_qty": 0
			}
		}`))
	}))
	defer upstream.Close()

	expectCatalogGetOne(mock, "SHIP_POL").WillReturnRows(sqlmock.NewRows(catalogGetOneColumns))
	mock.ExpectExec("INSERT INTO sml_catalog").
		WithArgs(
			"SHIP_POL", "ค่าขนส่งสินค้า", "", "ครั้ง", "", "",
			nil, "SERVICE", sqlmock.AnyArg(), 0, nil, "", nil, true,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	now := time.Now()
	expectCatalogGetOne(mock, "SHIP_POL").WillReturnRows(
		sqlmock.NewRows(catalogGetOneColumns).
			AddRow("SHIP_POL", "ค่าขนส่งสินค้า", "", "ครั้ง", "", "", nil, "SERVICE", float64(0), "pending", nil, 0, nil, "", nil, nil, now, now),
	)

	svc := NewSMLCatalogService(repository.NewSMLCatalogRepo(db), upstream.URL, nil, zap.NewNop())
	item, notFound, err := svc.RefreshOne("SHIP_POL")
	if err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if notFound {
		t.Fatal("notFound = true, want false")
	}
	if item == nil || item.ItemCode != "SHIP_POL" || item.ItemName != "ค่าขนส่งสินค้า" || item.UnitCode != "ครั้ง" {
		t.Fatalf("item = %+v", item)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshOneMapsSMLNotFound(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()

	svc := NewSMLCatalogService(repository.NewSMLCatalogRepo(db), upstream.URL, nil, zap.NewNop())
	item, notFound, err := svc.RefreshOne("NO_SUCH")
	if err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if !notFound {
		t.Fatal("notFound = false, want true")
	}
	if item != nil {
		t.Fatalf("item = %+v, want nil", item)
	}
}

var catalogGetOneColumns = []string{
	"item_code",
	"item_name",
	"item_name2",
	"unit_code",
	"wh_code",
	"shelf_code",
	"price",
	"group_code",
	"balance_qty",
	"embedding_status",
	"embedded_at",
	"image_count",
	"primary_image_roworder",
	"primary_image_guid",
	"primary_image_bytes",
	"image_synced_at",
	"synced_at",
	"created_at",
}

func expectCatalogGetOne(mock sqlmock.Sqlmock, code string) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery("SELECT item_code, item_name, item_name2, unit_code, wh_code, shelf_code,").
		WithArgs(code)
}
