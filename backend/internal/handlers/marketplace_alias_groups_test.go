package handlers

import "testing"

func TestMarketplaceGroupCursorRoundTrip(t *testing.T) {
	want := marketplaceGroupCursor{Source: "shopee", AccountKey: "shop:42", ParentKey: "100/blue"}
	encoded := encodeMarketplaceCursor(want)
	var got marketplaceGroupCursor
	if err := decodeMarketplaceCursor(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cursor=%#v want=%#v", got, want)
	}
}

func TestMarketplacePageLimitCapsParentAndChildPages(t *testing.T) {
	if got := marketplacePageLimit("999", 30, 50); got != 50 {
		t.Fatalf("parent limit=%d", got)
	}
	if got := marketplacePageLimit("0", 50, 100); got != 50 {
		t.Fatalf("child fallback=%d", got)
	}
}
