package api

const (
	UnitTokens   = "tokens"
	UnitSeconds  = "seconds"
	UnitBytes    = "bytes"
	UnitCount    = "count"
	UnitUSDMicro = "usd_micro"
	UnitKRW      = "krw"
	UnitUnknown  = "unknown"
)

type MeterState string

const (
	MeterStateRecorded    MeterState = "recorded"
	MeterStateZero        MeterState = "zero"
	MeterStateUnavailable MeterState = "unavailable"
	MeterStateDelayed     MeterState = "delayed"
	MeterStateError       MeterState = "error"
)

// Usage is one unit-safe aggregate. A quantity without its unit, window,
// freshness, and state is not a valid usage value.
type Usage struct {
	Quantity       int64      `json:"quantity"`
	Unit           string     `json:"unit"`
	Currency       string     `json:"currency,omitempty"`
	WindowStart    string     `json:"window_start,omitempty"`
	WindowEnd      string     `json:"window_end,omitempty"`
	LastUpdated    string     `json:"last_updated,omitempty"`
	Reconciled     bool       `json:"reconciled"`
	SourceQuantity int64      `json:"source_quantity,omitempty"`
	SourceUnit     string     `json:"source_unit,omitempty"`
	DisplayRate    string     `json:"display_rate,omitempty"`
	State          MeterState `json:"state,omitempty"`
	Reason         string     `json:"reason,omitempty"`
}

type UsageMeter struct {
	MetricType        string     `json:"metric_type"`
	Unit              string     `json:"unit"`
	Quantity          int64      `json:"quantity"`
	RateMicrosPerUnit string     `json:"rate_micros_per_unit,omitempty"`
	AmountMicros      int64      `json:"amount_micros"`
	Currency          string     `json:"currency,omitempty"`
	State             MeterState `json:"state"`
	Reason            string     `json:"reason,omitempty"`
	CostState         MeterState `json:"cost_state"`
	CostReason        string     `json:"cost_reason,omitempty"`
	LastUpdated       string     `json:"last_updated,omitempty"`
}

type UsageAmount struct {
	AmountMicros int64      `json:"amount_micros"`
	Currency     string     `json:"currency"`
	State        MeterState `json:"state"`
	Reason       string     `json:"reason,omitempty"`
	Rate         string     `json:"rate,omitempty"`
	RateSource   string     `json:"rate_source,omitempty"`
	RateAsOf     string     `json:"rate_as_of,omitempty"`
}

type UsageConversion struct {
	SourceCurrency        string     `json:"source_currency"`
	TargetCurrency        string     `json:"target_currency"`
	SourceAmountMicros    int64      `json:"source_amount_micros"`
	Rate                  string     `json:"rate,omitempty"`
	ConvertedAmountMicros int64      `json:"converted_amount_micros"`
	RateSource            string     `json:"rate_source,omitempty"`
	RateAsOf              string     `json:"rate_as_of,omitempty"`
	RateVersion           string     `json:"rate_version,omitempty"`
	State                 MeterState `json:"state"`
	Reason                string     `json:"reason,omitempty"`
}

type UsageModelTotal struct {
	InputTokens    int64            `json:"input_tokens"`
	OutputTokens   int64            `json:"output_tokens"`
	CostByCurrency map[string]int64 `json:"cost_by_currency"`
	PricingState   MeterState       `json:"pricing_state"`
}

// UsageTotal is the canonical usage response shared by organization, user,
// session, project, and analytics surfaces.
type UsageTotal struct {
	Range                string                     `json:"range,omitempty"`
	WindowStart          string                     `json:"window_start,omitempty"`
	WindowEnd            string                     `json:"window_end,omitempty"`
	LastUpdated          string                     `json:"last_updated,omitempty"`
	ByUnit               map[string]Usage           `json:"by_unit"`
	DisplayCurrency      string                     `json:"display_currency,omitempty"`
	DisplayTotal         UsageAmount                `json:"display_total"`
	CostByCurrency       map[string]UsageAmount     `json:"cost_by_currency"`
	Conversions          []UsageConversion          `json:"conversions"`
	Meters               []UsageMeter               `json:"meters"`
	Metrics              []UsageMeter               `json:"metrics"`
	ByMetric             map[string]UsageMeter      `json:"by_metric"`
	ByModel              map[string]Usage           `json:"by_model"`
	ByUser               map[string]Usage           `json:"by_user"`
	BySession            map[string]Usage           `json:"by_session"`
	ModelTotals          map[string]UsageModelTotal `json:"model_totals"`
	InputTokens          int64                      `json:"input_tokens"`
	OutputTokens         int64                      `json:"output_tokens"`
	TotalTokens          int64                      `json:"total_tokens"`
	TotalCostMicros      *int64                     `json:"total_cost_micros"`
	CostMicros           *int64                     `json:"cost_micros"`
	Currency             string                     `json:"currency,omitempty"`
	RecordCount          int                        `json:"record_count"`
	SessionCount         int                        `json:"session_count,omitempty"`
	Reconciled           bool                       `json:"reconciled"`
	ReconciliationErrors []string                   `json:"reconciliation_errors,omitempty"`
	Drilldown            []UsageLedgerRow           `json:"drilldown"`
	LedgerHasMore        bool                       `json:"ledger_has_more"`
	LedgerNextCursor     string                     `json:"ledger_next_cursor,omitempty"`
}

type UsageLedgerRow struct {
	ID                string `json:"id"`
	OccurredAt        string `json:"occurred_at"`
	Bucket            string `json:"bucket"`
	Unit              string `json:"unit"`
	Quantity          int64  `json:"quantity"`
	RateMicrosPerUnit string `json:"rate_micros_per_unit,omitempty"`
	AmountMicros      int64  `json:"amount_micros"`
	Currency          string `json:"currency,omitempty"`
	PricingState      string `json:"pricing_state,omitempty"`
	Note              string `json:"note,omitempty"`
	RefType           string `json:"ref_type,omitempty"`
	RefID             string `json:"ref_id,omitempty"`
	UserID            string `json:"user_id,omitempty"`
	HarnessID         string `json:"harness_id,omitempty"`
	SessionID         string `json:"session_id,omitempty"`
	ModelPackageID    string `json:"model_package_id,omitempty"`
	EndpointID        string `json:"endpoint_id,omitempty"`
}
