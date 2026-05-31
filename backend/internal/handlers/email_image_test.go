package handlers

import "testing"

func TestExtractShopeeImageURLsPrefersProductImage(t *testing.T) {
	html := `
		<img src="https://tracking.mail.shopee.co.th/tracking/1/open/abc">
		<img src="https://cf.shopee.sg/file/0cd023d64f04491f3dc8076d6932dfdc">
		<img src="https://cf.shopee.co.th/file/th-11134207-81zth-mimxd9980lc477">
	`

	got := extractShopeeImageURLs(html)
	if len(got) == 0 {
		t.Fatal("extractShopeeImageURLs() returned no URLs")
	}
	if got[0] != "https://cf.shopee.co.th/file/th-11134207-81zth-mimxd9980lc477" {
		t.Fatalf("first URL = %q, want product image", got[0])
	}
	for _, u := range got {
		if u == "https://tracking.mail.shopee.co.th/tracking/1/open/abc" {
			t.Fatal("tracking pixel URL should be excluded")
		}
	}
}
