package marketplace

import "testing"

func TestCalculateQuantityConversionUsesExactRationalArithmetic(t *testing.T) {
	result, err := CalculateQuantityConversion(QuantityConversionInput{
		MarketplaceQty: "2", Multiplier: 2, StandValue: "12", DivideValue: "1",
	})
	if err != nil {
		t.Fatalf("CalculateQuantityConversion: %v", err)
	}
	if result.SMLQty.RatString() != "4" || result.BaseQty.RatString() != "48" {
		t.Fatalf("sml=%s base=%s", result.SMLQty.RatString(), result.BaseQty.RatString())
	}
}

func TestCalculateQuantityConversionKeepsNonTerminatingRatioExact(t *testing.T) {
	result, err := CalculateQuantityConversion(QuantityConversionInput{
		MarketplaceQty: "1", Multiplier: 1, StandValue: "1", DivideValue: "3",
	})
	if err != nil {
		t.Fatalf("CalculateQuantityConversion: %v", err)
	}
	if result.BaseQty.RatString() != "1/3" || result.BaseFloor.String() != "0" {
		t.Fatalf("base=%s floor=%s", result.BaseQty.RatString(), result.BaseFloor.String())
	}
}

func TestCalculateQuantityConversionRejectsAmbiguousOrUnsafeInput(t *testing.T) {
	cases := []QuantityConversionInput{
		{MarketplaceQty: "1", Multiplier: 0, StandValue: "1", DivideValue: "1"},
		{MarketplaceQty: "1", Multiplier: 1, StandValue: "", DivideValue: "1"},
		{MarketplaceQty: "1", Multiplier: 1, StandValue: "1", DivideValue: "0"},
		{MarketplaceQty: "-1", Multiplier: 1, StandValue: "1", DivideValue: "1"},
	}
	for _, input := range cases {
		if _, err := CalculateQuantityConversion(input); err == nil {
			t.Fatalf("input %#v should fail closed", input)
		}
	}
}

func TestRatDecimalFormattingIsExactOrConservative(t *testing.T) {
	result, err := CalculateQuantityConversion(QuantityConversionInput{
		MarketplaceQty: "1.25", Multiplier: 2, StandValue: "1", DivideValue: "3",
	})
	if err != nil {
		t.Fatal(err)
	}
	sml, err := RatFiniteDecimal(result.SMLQty)
	if err != nil || sml != "2.5" {
		t.Fatalf("finite SML decimal = %q, err=%v", sml, err)
	}
	baseDemand := RatCeilDecimal(result.BaseQty, 6)
	if baseDemand != "0.833334" {
		t.Fatalf("conservative base demand = %q", baseDemand)
	}
}
