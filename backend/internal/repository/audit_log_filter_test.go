package repository

import (
	"strings"
	"testing"
)

func TestAuditSourceFilterSeparatesTikTokShopAPIFromTikTokExcel(t *testing.T) {
	where, args, next := appendAuditSourceFilter("WHERE 1=1", nil, 1, "tiktok_shop")
	if !strings.Contains(where, "a.action LIKE") || !strings.Contains(where, "a.detail->>'flow'") {
		t.Fatalf("TikTok Shop where = %q", where)
	}
	if len(args) != 3 || args[0] != "tiktok_shop" || args[1] != "tiktok_shop_%" || args[2] != "tiktok_shop_api_reviewed" || next != 4 {
		t.Fatalf("TikTok Shop args=%#v next=%d", args, next)
	}

	where, args, next = appendAuditSourceFilter("WHERE 1=1", nil, 1, "tiktok")
	if !strings.Contains(where, "a.source =") || !strings.Contains(where, "NOT (") || !strings.Contains(where, "a.detail->>'flow'") {
		t.Fatalf("TikTok Excel where = %q", where)
	}
	if len(args) != 3 || args[0] != "tiktok" || args[1] != "tiktok_shop_%" || args[2] != "tiktok_shop_api_reviewed" || next != 4 {
		t.Fatalf("TikTok Excel args=%#v next=%d", args, next)
	}
}

func TestAuditSourceFilterKeepsOrdinarySourcesSimple(t *testing.T) {
	where, args, next := appendAuditSourceFilter("WHERE 1=1", nil, 1, "sml")
	if where != "WHERE 1=1 AND a.source = $1" || len(args) != 1 || args[0] != "sml" || next != 2 {
		t.Fatalf("ordinary source filter where=%q args=%#v next=%d", where, args, next)
	}
}
