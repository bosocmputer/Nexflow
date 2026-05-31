package sml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestDocNoClientNext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ic/doc-no/next" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant"); got != "sml1_2026" {
			t.Fatalf("X-Tenant = %q", got)
		}
		if got := r.URL.Query().Get("route"); got != "purchaseorder" {
			t.Fatalf("route = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"route":"purchaseorder","trans_flag":6,"prefix":"NX-PO","format":"YYMM####","doc_date":"2026-05-25","last_doc_no":"NX-PO26050009","last_seq":9,"next_doc_no":"NX-PO26050010","next_seq":10}}`))
	}))
	defer srv.Close()

	client := NewDocNoClient(PartyConfig{BaseURL: srv.URL, GUID: "smlx", Database: "sml1_2026"}, zap.NewNop())
	got, err := client.Next(context.Background(), NextDocNoRequest{
		Route:   "purchaseorder",
		Prefix:  "NX-PO",
		Format:  "YYMM####",
		DocDate: "2026-05-25",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.NextDocNo != "NX-PO26050010" || got.NextSeq != 10 {
		t.Fatalf("next = %s/%d", got.NextDocNo, got.NextSeq)
	}
}

func TestDocNoClientNextRejectsIncompleteResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"next_seq":0}}`))
	}))
	defer srv.Close()

	client := NewDocNoClient(PartyConfig{BaseURL: srv.URL, GUID: "smlx", Database: "sml1_2026"}, zap.NewNop())
	if _, err := client.Next(context.Background(), NextDocNoRequest{Route: "saleorder"}); err == nil {
		t.Fatal("expected incomplete response error")
	}
}
