package marketplace

import (
	"fmt"
	"math/big"
	"strings"
)

const MaxQuantityMultiplier int64 = 1_000_000

type QuantityConversionInput struct {
	MarketplaceQty string
	Multiplier     int64
	StandValue     string
	DivideValue    string
}

type QuantityConversion struct {
	SMLQty    *big.Rat
	BaseQty   *big.Rat
	BaseFloor *big.Int
}

func CalculateQuantityConversion(input QuantityConversionInput) (QuantityConversion, error) {
	if input.Multiplier < 1 || input.Multiplier > MaxQuantityMultiplier {
		return QuantityConversion{}, fmt.Errorf("quantity multiplier must be between 1 and %d", MaxQuantityMultiplier)
	}
	qty, err := positiveOrZeroDecimal("marketplace quantity", input.MarketplaceQty)
	if err != nil {
		return QuantityConversion{}, err
	}
	stand, err := positiveDecimal("stand value", input.StandValue)
	if err != nil {
		return QuantityConversion{}, err
	}
	divide, err := positiveDecimal("divide value", input.DivideValue)
	if err != nil {
		return QuantityConversion{}, err
	}

	multiplier := new(big.Rat).SetInt64(input.Multiplier)
	smlQty := new(big.Rat).Mul(qty, multiplier)
	unitFactor := new(big.Rat).Quo(stand, divide)
	baseQty := new(big.Rat).Mul(smlQty, unitFactor)
	baseFloor := new(big.Int).Quo(new(big.Int).Set(baseQty.Num()), baseQty.Denom())
	return QuantityConversion{SMLQty: smlQty, BaseQty: baseQty, BaseFloor: baseFloor}, nil
}

func positiveDecimal(name, raw string) (*big.Rat, error) {
	value, err := positiveOrZeroDecimal(name, raw)
	if err != nil {
		return nil, err
	}
	if value.Sign() <= 0 {
		return nil, fmt.Errorf("%s must be greater than zero", name)
	}
	return value, nil
}

func positiveOrZeroDecimal(name, raw string) (*big.Rat, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	value, ok := new(big.Rat).SetString(raw)
	if !ok {
		return nil, fmt.Errorf("%s is not a valid decimal", name)
	}
	if value.Sign() < 0 {
		return nil, fmt.Errorf("%s must not be negative", name)
	}
	return value, nil
}

// RatFiniteDecimal renders a rational only when it has a finite base-10
// representation. SML quantity is marketplace decimal multiplied by an
// integer, so a non-terminating result indicates corrupted input.
func RatFiniteDecimal(value *big.Rat) (string, error) {
	if value == nil {
		return "", fmt.Errorf("rational value is required")
	}
	denominator := new(big.Int).Set(value.Denom())
	twos, fives := 0, 0
	two := big.NewInt(2)
	five := big.NewInt(5)
	zero := big.NewInt(0)
	for new(big.Int).Mod(denominator, two).Cmp(zero) == 0 {
		denominator.Quo(denominator, two)
		twos++
	}
	for new(big.Int).Mod(denominator, five).Cmp(zero) == 0 {
		denominator.Quo(denominator, five)
		fives++
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return "", fmt.Errorf("rational %s has no finite decimal representation", value.RatString())
	}
	scale := twos
	if fives > scale {
		scale = fives
	}
	scaled := new(big.Int).Mul(value.Num(), pow10(scale))
	scaled.Quo(scaled, value.Denom())
	return formatScaledInteger(scaled, scale), nil
}

// RatCeilDecimal rounds a non-negative demand upward at the requested scale.
// Over-reserving a tiny fraction is safe; rounding demand down could advertise
// stock that has not been proven available.
func RatCeilDecimal(value *big.Rat, scale int) string {
	if value == nil || scale < 0 {
		return ""
	}
	scaledNumerator := new(big.Int).Mul(value.Num(), pow10(scale))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(scaledNumerator, value.Denom(), remainder)
	if value.Sign() > 0 && remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return formatScaledInteger(quotient, scale)
}

func pow10(scale int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
}

func formatScaledInteger(value *big.Int, scale int) string {
	negative := value.Sign() < 0
	digits := new(big.Int).Abs(new(big.Int).Set(value)).String()
	if scale == 0 {
		if negative {
			return "-" + digits
		}
		return digits
	}
	for len(digits) <= scale {
		digits = "0" + digits
	}
	whole := digits[:len(digits)-scale]
	fraction := strings.TrimRight(digits[len(digits)-scale:], "0")
	result := whole
	if fraction != "" {
		result += "." + fraction
	}
	if negative && result != "0" {
		result = "-" + result
	}
	return result
}
