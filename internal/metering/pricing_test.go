package metering

import (
	"math"
	"testing"
)

func TestParseKRWPriceMicrosPer1KPreservesExplicitZeroAndExactDecimals(t *testing.T) {
	for input, want := range map[string]int64{
		"0":         0,
		"0.000001":  1,
		"12.345678": 12_345_678,
	} {
		got, err := ParseKRWPriceMicrosPer1K(input)
		if err != nil {
			t.Fatalf("ParseKRWPriceMicrosPer1K(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseKRWPriceMicrosPer1K(%q) = %d, want %d", input, got, want)
		}
	}
	for _, input := range []string{"", "-1", "0.0000001", "nan", "9223372036855"} {
		if _, err := ParseKRWPriceMicrosPer1K(input); err == nil {
			t.Fatalf("ParseKRWPriceMicrosPer1K(%q) accepted invalid or inexact price", input)
		}
	}
}

func TestTokenCostMicrosUsesCheckedExactHalfAwayRounding(t *testing.T) {
	for _, tc := range []struct {
		quantity, rate, want int64
	}{
		{1, 1, 0},
		{500, 1, 1},
		{1_000, 1, 1},
		{3, 500, 2},
		{1_000, 0, 0},
	} {
		got, err := TokenCostMicros(tc.quantity, tc.rate)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("TokenCostMicros(%d, %d) = %d, want %d", tc.quantity, tc.rate, got, tc.want)
		}
	}
	if _, err := TokenCostMicros(-1, 1); err == nil {
		t.Fatal("negative non-adjustment quantity was accepted")
	}
	if _, err := TokenCostMicros(math.MaxInt64, math.MaxInt64); err == nil {
		t.Fatal("overflowing cost was accepted")
	}
}

func TestResolveKRWPriceMicrosPer1KDistinguishesConfiguredFreeFromLegacy(t *testing.T) {
	for _, tc := range []struct {
		exact      int64
		configured bool
		legacy     float64
		want       int64
		wantSet    bool
	}{
		{exact: 0, configured: true, legacy: 99, want: 0, wantSet: true},
		{exact: 1_250_000, configured: true, want: 1_250_000, wantSet: true},
		{legacy: 2.5, want: 2_500_000, wantSet: true},
		{legacy: 0, wantSet: false},
	} {
		got, set, err := ResolveKRWPriceMicrosPer1K(tc.exact, tc.configured, tc.legacy)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want || set != tc.wantSet {
			t.Fatalf("ResolveKRWPriceMicrosPer1K(%d,%v,%v) = (%d,%v), want (%d,%v)", tc.exact, tc.configured, tc.legacy, got, set, tc.want, tc.wantSet)
		}
	}
}
