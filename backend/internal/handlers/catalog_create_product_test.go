package handlers

import "testing"

func TestBuildQuickCreateProductRequestOmitsPriceFormulas(t *testing.T) {
	request := buildQuickCreateProductRequest(createProductRequest{
		Code: "SKU-1", Name: "สินค้า", UnitCode: "แพ็ค",
	})
	if len(request.PriceFormulas) != 0 {
		t.Fatalf("price formulas = %#v, want omitted", request.PriceFormulas)
	}
	if len(request.Units) != 1 || request.Units[0].StandValue != 1 || request.Units[0].DivideValue != 1 {
		t.Fatalf("units = %#v, want one 1/1 unit", request.Units)
	}
}
