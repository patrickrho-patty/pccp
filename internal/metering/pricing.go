package metering

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const microsPerKRW = int64(1_000_000)

// ParseKRWPriceMicrosPer1K converts an exact decimal KRW-per-1K-token
// catalog price into the integer representation persisted with metering.
func ParseKRWPriceMicrosPer1K(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("metering: price is required")
	}
	price := new(big.Rat)
	if _, ok := price.SetString(value); !ok || price.Sign() < 0 {
		return 0, fmt.Errorf("metering: invalid non-negative price %q", value)
	}
	micros := new(big.Rat).Mul(price, new(big.Rat).SetInt64(microsPerKRW))
	if !micros.IsInt() || !micros.Num().IsInt64() {
		return 0, fmt.Errorf("metering: price %q is not representable in integer micros", value)
	}
	return micros.Num().Int64(), nil
}

// ResolveKRWPriceMicrosPer1K selects the exact configured value for new rows
// and provides a compatibility bridge for positive legacy float prices.
func ResolveKRWPriceMicrosPer1K(exact int64, configured bool, legacy float64) (int64, bool, error) {
	if configured {
		if exact < 0 {
			return 0, false, fmt.Errorf("metering: configured price must be non-negative")
		}
		return exact, true, nil
	}
	if legacy <= 0 {
		return 0, false, nil
	}
	rate, err := ParseKRWPriceMicrosPer1K(strconv.FormatFloat(legacy, 'f', -1, 64))
	if err != nil {
		return 0, false, err
	}
	return rate, true, nil
}

// TokenCostMicros applies an exact micros-per-1K-token price, rounding a
// fractional micro half away from zero and rejecting overflow.
func TokenCostMicros(quantity, rateMicrosPer1K int64) (int64, error) {
	if quantity < 0 || rateMicrosPer1K < 0 {
		return 0, fmt.Errorf("metering: token quantity and rate must be non-negative")
	}
	numerator := new(big.Int).Mul(big.NewInt(quantity), big.NewInt(rateMicrosPer1K))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, big.NewInt(1_000), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(big.NewInt(1_000)) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, fmt.Errorf("metering: token cost overflows int64 micros")
	}
	return quotient.Int64(), nil
}
