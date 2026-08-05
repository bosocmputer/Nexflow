package marketplace

import "testing"

func TestNormalizeKeyOnlyRemovesBOMAndCollapsesWhitespace(t *testing.T) {
	got := NormalizeKey("\ufeff  Official SKU-A / สีแดง  ", "")
	want := "Official SKU-A / สีแดง"
	if got != want {
		t.Fatalf("NormalizeKey = %q, want %q", got, want)
	}
}

func TestNormalizeKeyPreservesCaseAndPunctuation(t *testing.T) {
	upper := NormalizeKey("SKU-A/No.1", "")
	lower := NormalizeKey("sku-a/no.1", "")
	if upper == lower {
		t.Fatalf("case was changed: upper=%q lower=%q", upper, lower)
	}
}
