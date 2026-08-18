package metering

import (
	"fmt"
	"strings"
)

type Metric string

const (
	TokensIn     Metric = "tokens_in"
	TokensOut    Metric = "tokens_out"
	CacheRead    Metric = "cache_read"
	CacheWrite   Metric = "cache_write"
	MediaTokens  Metric = "media_tokens"
	GPUSeconds   Metric = "gpu_seconds"
	StorageBytes Metric = "storage_bytes"
	ToolCall     Metric = "tool_call"
	Reservation  Metric = "reservation"
	FlatFee      Metric = "flat_fee"
	Refund       Metric = "refund"
)

const (
	UnitTokens   = "tokens"
	UnitSeconds  = "seconds"
	UnitBytes    = "bytes"
	UnitCount    = "count"
	UnitUSDMicro = "usd_micro"
	UnitKRW      = "krw"
	UnitUnknown  = "unknown"
)

var expectedUnits = map[Metric]string{
	TokensIn:     UnitTokens,
	TokensOut:    UnitTokens,
	CacheRead:    UnitTokens,
	CacheWrite:   UnitTokens,
	MediaTokens:  UnitTokens,
	GPUSeconds:   UnitSeconds,
	StorageBytes: UnitBytes,
	ToolCall:     UnitCount,
	Reservation:  UnitCount,
	FlatFee:      UnitCount,
	Refund:       UnitCount,
}

func ExpectedUnit(metric string) (string, bool) {
	unit, ok := expectedUnits[Metric(metric)]
	return unit, ok
}

func KnownMetrics() map[string]string {
	out := make(map[string]string, len(expectedUnits))
	for metric, unit := range expectedUnits {
		out[string(metric)] = unit
	}
	return out
}

func NormalizeUnit(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "token", "tokens":
		return UnitTokens
	case "second", "seconds", "sec", "s":
		return UnitSeconds
	case "byte", "bytes", "b":
		return UnitBytes
	case "count", "request", "requests", "call", "calls":
		return UnitCount
	case "usd_micro", "micro_usd":
		return UnitUSDMicro
	default:
		return strings.ToLower(strings.TrimSpace(unit))
	}
}

func Validate(metric, unit string) (string, error) {
	expected, ok := ExpectedUnit(metric)
	if !ok {
		return "", fmt.Errorf("metering: unknown metric %q", metric)
	}
	normalized := NormalizeUnit(unit)
	if normalized == "" {
		return "", fmt.Errorf("metering: metric %s requires explicit unit %s", metric, expected)
	}
	if normalized != expected {
		return "", fmt.Errorf("metering: metric %s requires unit %s, got %s", metric, expected, normalized)
	}
	return normalized, nil
}
