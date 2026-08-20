package models

import "time"

// GPU queue / inference QoS operations analytics (PAT-1443). Durable,
// anonymized request-lifecycle timeline + bounded hourly rollups + EWMA
// wait forecasts. Metric labels are bounded, allow-listed dimensions only —
// never user/session/request/repository/prompt identifiers. This is an
// internal SRE/engineering surface, not customer-facing.

// Particle names an allow-listed, low-cardinality aggregation dimension.
type QoSDimension string

const (
	QoSDimModel      QoSDimension = "model"
	QoSDimRegion     QoSDimension = "region"
	QoSDimWorkerPool QoSDimension = "worker_pool"
	QoSDimTraffic    QoSDimension = "traffic_class"
	QoSDimPlan       QoSDimension = "plan"
)

// QoSRequestEvent is one anonymized lifecycle boundary on the service side.
// No user/session/request/repo/prompt identity is stored; correlation uses an
// opaque, short-lived, content-free trace bucket keyed by an internal
// monotonic sequence (never a request ID label).
type QoSRequestEvent struct {
	Base
	// Deploy/tenant opaque scope for isolation (never a user identity).
	Deployment string `gorm:"type:varchar(32);index;not null" json:"deployment"` // public|enterprise|sovereign
	ScopeKey   string `gorm:"type:varchar(32)" json:"scope_key,omitempty"`       // opaque tenant for drill-down only
	// Monotonic lifecycle boundary (schema v1 — each transition recorded once).
	Lifecycle string `gorm:"type:varchar(24);index;not null" json:"lifecycle"` // ingress|admission|queued|left|reserved|exec_started|first_token|completed|canceled|expired|shed|rejected|retry|failed
	// Dimensions (allow-listed, bounded).
	Model        string `gorm:"type:varchar(64);index" json:"model,omitempty"`
	Region       string `gorm:"type:varchar(32);index" json:"region,omitempty"`
	WorkerPool   string `gorm:"type:varchar(64);index" json:"worker_pool,omitempty"`
	TrafficClass string `gorm:"type:varchar(32);index" json:"traffic_class,omitempty"` // interactive-paid|interactive-normal|background-agent|batch
	Plan         string `gorm:"type:varchar(32)" json:"plan,omitempty"`
	// Workload size bucket (bounded token/media buckets; no raw tokens per label).
	SizeBucket string `gorm:"type:varchar(8)" json:"size_bucket,omitempty"` // s|m|l|xl|image
	// Wall-clock at boundary (for correlation). Monotonic duration derives from
	// per-request sequence deltas within the same StratumKey; never negative.
	OccurredAt time.Time `gorm:"index;not null" json:"occurred_at"`
	// TraceKey correlates stages WITHOUT leaking high-cardinality IDs into
	// metric labels; it is a short random handle + epoch, rotated hourly.
	TraceKey string `gorm:"type:varchar(40);index" json:"-"`
	// StratumKey groups request for wait/service-rate estimation without naming
	// the request: model|region|pool|traffic|sizeBucket.
	StratumKey string `gorm:"type:varchar(255);index" json:"-"`
	// Derived (set at terminal boundary to avoid recomputation): queue wait ms.
	QueueWaitMs int64 `json:"queue_wait_ms,omitempty"`
	// Terminal true only on completed/canceled/expired/failed to count once.
	Terminal bool `gorm:"default:false" json:"-"`
}

func (QoSRequestEvent) TableName() string { return "qos_request_events" }

// QoSRollup is a bounded hourly aggregate for capacity trending (90d default).
type QoSRollup struct {
	Base
	Deployment   string `gorm:"type:varchar(32);index;not null" json:"deployment"`
	Bucket       string `gorm:"type:varchar(19);index;not null" json:"bucket"` // "2006-01-02T15" hourly
	Model        string `gorm:"type:varchar(64)" json:"model,omitempty"`
	TrafficClass string `gorm:"type:varchar(32)" json:"traffic_class,omitempty"`
	// Counts / rates.
	Arrivals    int64 `json:"arrivals"`
	Dispatches  int64 `json:"dispatches"`
	Completions int64 `json:"completions"`
	Canceled    int64 `json:"canceled"`
	Expired     int64 `json:"expired"`
	Shed        int64 `json:"shed"`
	Rejected    int64 `json:"rejected"`
	Retried     int64 `json:"retried"`
	// Active concurrency snapshots (sample-based).
	MaxConcurrency int `json:"max_concurrency"`
	PeakActive     int `json:"peak_active"`
	// Sorted percentile aggregates (correct distribution).
	QueueWaitP50 float64 `json:"queue_wait_p50_ms"`
	QueueWaitP90 float64 `json:"queue_wait_p90_ms"`
	QueueWaitP95 float64 `json:"queue_wait_p95_ms"`
	QueueWaitP99 float64 `json:"queue_wait_p99_ms"`
	QueueWaitMax float64 `json:"queue_wait_max_ms"`
	WaitSampleN  int     `json:"wait_sample_n"`
	// Service rate EWMA (requests/hr) and confidence.
	ServiceRateEwma float64 `json:"service_rate_ewma"`
	EstimateHealth  string  `gorm:"type:varchar(16)" json:"estimate_health"` // healthy|degraded|insufficient
}

func (QoSRollup) TableName() string { return "qos_rollups" }

// QoSForecast is the persisted wait-time estimate for a stratum.
type QoSForecast struct {
	Base
	Deployment string `gorm:"type:varchar(32);index;not null" json:"deployment"`
	StratumKey string `gorm:"type:varchar(255);index;not null" json:"stratum_key"`
	// Estimate produced by estimator v1 (EWMA + robust percentile) — versioned.
	EstimatorVersion string    `json:"estimator_version"`
	P50WaitMs        float64   `json:"p50_wait_ms"`
	P90WaitMs        float64   `json:"p90_wait_ms"` // conservative range high
	SampleN          int       `json:"sample_n"`
	WindowHours      int       `json:"window_hours"`
	ProducedAt       time.Time `json:"produced_at"`
	// Calibration.
	MAEP50Ms         float64 `json:"mae_p50_ms"`
	MAEP90Ms         float64 `json:"mae_p90_ms"`
	UnderpredictRate float64 `json:"underpredict_rate"`
	Health           string  `gorm:"type:varchar(16)" json:"health"`
}

func (QoSForecast) TableName() string { return "qos_forecasts" }
