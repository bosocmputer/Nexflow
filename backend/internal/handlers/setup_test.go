package handlers

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSetupEmailReadyTreatsNoNewMailAsReady(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT COUNT\\(\\*\\),").
		WillReturnRows(sqlmock.NewRows([]string{"total", "ready", "no_new_mail", "errors", "never_poll"}).
			AddRow(1, 1, 1, 0, 0))

	ready, detail := (&SetupHandler{db: db}).emailReady()
	if !ready {
		t.Fatalf("emailReady ready = false, detail=%q", detail)
	}
	if detail != "ทดสอบสำเร็จ แต่ไม่มีอีเมลใหม่" {
		t.Fatalf("emailReady detail = %q", detail)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSetupEmailReadyDistinguishesNeverPolledAndError(t *testing.T) {
	tests := []struct {
		name       string
		total      int
		ready      int
		noNewMail  int
		errors     int
		neverPoll  int
		wantReady  bool
		wantDetail string
	}{
		{
			name:       "never polled",
			total:      1,
			neverPoll:  1,
			wantReady:  false,
			wantDetail: "เพิ่ม inbox แล้ว แต่ยังไม่เคยทดสอบหรือดึงอีเมล",
		},
		{
			name:       "last poll error",
			total:      1,
			errors:     1,
			wantReady:  false,
			wantDetail: "เพิ่ม inbox แล้ว แต่รอบล่าสุดดึงอีเมลไม่สำเร็จ",
		},
		{
			name:       "partial ready",
			total:      2,
			ready:      1,
			errors:     1,
			wantReady:  true,
			wantDetail: "พร้อมใช้งานบางกล่อง (1/2) · มีกล่องที่ต้องตรวจ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()

			mock.ExpectQuery("SELECT COUNT\\(\\*\\),").
				WillReturnRows(sqlmock.NewRows([]string{"total", "ready", "no_new_mail", "errors", "never_poll"}).
					AddRow(tt.total, tt.ready, tt.noNewMail, tt.errors, tt.neverPoll))

			gotReady, gotDetail := (&SetupHandler{db: db}).emailReady()
			if gotReady != tt.wantReady || gotDetail != tt.wantDetail {
				t.Fatalf("emailReady() = (%v, %q), want (%v, %q)", gotReady, gotDetail, tt.wantReady, tt.wantDetail)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("expectations: %v", err)
			}
		})
	}
}
