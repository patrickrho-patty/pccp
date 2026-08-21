package scheduler

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// router.go implements the S3 cost-model router (spec §12.3.1 locked
// decision): lowest-cost placement with KV-overlap credits, media-hash
// routing, breakable session affinity, overload filters, and topology
// awareness from capability cards.

// CostRouterVersion is the frozen baseline policy identity (PAT-1445
// baseline freeze): receipts and shadow comparisons key on it.
const CostRouterVersion = "cost-router/v1"

// Router is the versioned placement interface (PAT-1445 maintainability:
// a stable router interface with versioned candidate implementations).
// The baseline cost router is v1; candidates implement the same seam for
// shadow evaluation and, later, canary rollout.
type Router interface {
	Route(RouteRequest) (RouteDecision, error)
	Version() string
}

// RouterConfig tunes the cost model.
type RouterConfig struct {
	PrefillScale     float64 // cost per prefill token (uncached)
	DecodeKVScale    float64 // cost per projected decode KV slot
	RequestScale     float64 // cost per active request (w in the spec)
	AffinityDiscount float64 // cost discount for the affinity worker
	MediaDiscount    float64 // cost discount for warm media state
	// MaxLoadShare caps a worker's acceptable load before the overload
	// filter drops it (spec: sacrificing a cache hit beats overloading).
	MaxKVUtilization  float64
	MaxActiveRequests int
}

// DefaultRouterConfig returns reference weights for the locked cost
// model (spec §12.3.1).
func DefaultRouterConfig() RouterConfig {
	return RouterConfig{
		PrefillScale:      1.0,
		DecodeKVScale:     0.5,
		RequestScale:      0.1,
		AffinityDiscount:  0.2,
		MediaDiscount:     0.3,
		MaxKVUtilization:  0.95,
		MaxActiveRequests: 0, // 0 = unbounded; per-card seqs cap applies
	}
}

// RouterWorkerState is the live load picture for one worker.
type RouterWorkerState struct {
	PrefillActive  int // active prefill tokens (from heartbeats/metrics)
	DecodeKV       int // decode KV slots in use
	ActiveRequests int
	Load           WorkerLoad
}

// RouteRequest is the placement input set (spec §12.1: routing inputs).
type RouteRequest struct {
	Model                string
	Namespace            string
	PrefixHash           string
	MediaHash            string
	InputTokens          int
	CachedTokens         int
	ExpectedOutputTokens int
	AffinityWorker       string
	RequestClass         string        // agentic / interactive / batch (SLO scoping)
	Pool                 string        // model pool scope (spec §14 row 17)
	LoRAAdapter          string        // requested adapter (affinity, §14 row 18)
	Region               string        // residency constraint: only workers in this signed card region (empty = any)
	Cache                CacheIdentity // cache compatibility identity (WS1; zero = legacy index path)
}

// RoutePath is the hierarchical placement path a receipt explains
// (PAT-1445: region → pool → worker).
type RoutePath struct {
	Region string `json:"region,omitempty"`
	Pool   string `json:"pool,omitempty"`
	Worker string `json:"worker"`
}

// RouteDecision is one placement outcome.
type RouteDecision struct {
	WorkerID      string
	Cost          float64
	OverlapTokens int
	Reason        string
}

// CostRouter implements the locked cost model. Safe for concurrent use.
type CostRouter struct {
	mu         sync.RWMutex
	cfg        RouterConfig
	fleet      *WorkerFleet
	lookup     *KVLookup
	receipts   *ReceiptStore
	gang       *GangRegistry
	predictor  *LatencyPredictor
	slo        *SLOResolver
	workerCfg  map[string]string // workerID → predictor config ID
	maxSLORisk float64           // placements above this P(SLO violation) are rejected
	lora       *LoRaLifecycle
	pools      *ModelPoolManager
	regions    *RegionRegistry // global stage: region health + preauthorized failover
	shadow     Router          // candidate evaluated alongside (PAT-1445 shadow mode)
}

// NewCostRouter builds a router with the given config.
func NewCostRouter(cfg RouterConfig) *CostRouter {
	return &CostRouter{
		cfg:        cfg,
		fleet:      NewWorkerFleet(),
		lookup:     NewKVLookup(nil, nil),
		workerCfg:  make(map[string]string),
		maxSLORisk: 0.5,
	}
}

// Version returns the frozen baseline policy identity.
func (r *CostRouter) Version() string { return CostRouterVersion }

// SetFleet installs the shared worker-state module (PAT-1445 B1).
func (r *CostRouter) SetFleet(f *WorkerFleet) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fleet = f
}

// SetRegions installs the region registry (PAT-1445 global stage).
// Nil = single-region deployments: no failover, no behavior change.
func (r *CostRouter) SetRegions(reg *RegionRegistry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.regions = reg
}

// SetKV installs the fleet KV index as the lookup seam's legacy adapter
// (S3 §13.11).
func (r *CostRouter) SetKV(kv *KVIndex) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookup.SetLegacy(kv)
}

// SetKVDirectory installs the WS1 cache-extent directory as the lookup
// seam's directory adapter: requests carrying cache identity get
// identity-exact overlap lookups; all others keep the legacy path.
func (r *CostRouter) SetKVDirectory(d *KVDirectory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookup.SetDirectory(d)
}

// SetLoRA installs the adapter-residency tracker: requests for an
// adapter prefer workers with it loaded (LoRA affinity, spec §14 row 18).
func (r *CostRouter) SetLoRA(l *LoRaLifecycle) {
	r.mu.Lock()
	r.lora = l
	r.mu.Unlock()
}

// SetPools installs the model-pool manager (spec §14 row 17).
func (r *CostRouter) SetPools(p *ModelPoolManager) {
	r.mu.Lock()
	r.pools = p
	r.mu.Unlock()
}

// SetPredictor installs the S4 latency predictor: placements whose
// predicted TTFT violates the SLO with high probability are rejected
// (spec §13.3 risk-aware routing).
func (r *CostRouter) SetPredictor(p *LatencyPredictor) {
	r.mu.Lock()
	r.predictor = p
	r.mu.Unlock()
}

// SetSLOResolver installs the SLO objective table (S5).
func (r *CostRouter) SetSLOResolver(sl *SLOResolver) {
	r.mu.Lock()
	r.slo = sl
	r.mu.Unlock()
}

// SetConfigForWorker maps a worker to its predictor config ID (one
// model per serving config, spec §13.12).
func (r *CostRouter) SetConfigForWorker(workerID, configID string) {
	r.mu.Lock()
	r.workerCfg[workerID] = configID
	r.mu.Unlock()
}

// SetGang installs the gang registry: workers whose parallel group is
// incomplete are ineligible (spec §14 row 16).
func (r *CostRouter) SetGang(g *GangRegistry) {
	r.mu.Lock()
	r.gang = g
	r.mu.Unlock()
}

// SetReceipts installs the placement-receipt store (§13.6).
func (r *CostRouter) SetReceipts(rs *ReceiptStore) {
	r.mu.Lock()
	r.receipts = rs
	r.mu.Unlock()
}

// Receipts exposes recent placement receipts.
func (r *CostRouter) Receipts() *ReceiptStore { return r.receipts }

// UpsertWorker installs or refreshes a worker's card + load state
// (delegates to the shared fleet).
func (r *CostRouter) UpsertWorker(e WorkerEntry, s RouterWorkerState) {
	r.fleet.Upsert(e, s)
}

// RemoveWorker drops a worker.
func (r *CostRouter) RemoveWorker(workerID string) {
	r.fleet.Remove(workerID)
}

// Route runs the two decision phases (PAT-1445 Phase 1): the eligibility
// filter ladder first (governance, capability, risk, capacity — see
// eligibility.go), then the locked cost model over the survivors:
//
//	cost = prefillScale × max(activePrefill + newPrompt − KVOverlapCredits, 0)
//	     + projectedDecodeKV + w × activeRequests
//
// with discounts for session affinity and warm media state. Affinity is
// a preference, never a pin: overloaded workers lose it (spec §12.3.2).
func (r *CostRouter) Route(req RouteRequest) (RouteDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Global stage (PAT-1445): region health and preauthorized failover
	// resolve before any cluster/worker scoring. Failure is a clear
	// availability error, never a silent cross-region route.
	if r.regions != nil && req.Region != "" {
		resolved, ok := r.regions.SelectRegion(req.Region)
		if !ok {
			return RouteDecision{}, &RouteError{
				Model:       req.Model,
				Permanent:   true,
				Eligibility: &EligibilityReport{Region: req.Region},
			}
		}
		req.Region = resolved
	}

	best := RouteDecision{Cost: 1e18}
	found := false
	elig := &EligibilityReport{Region: req.Region}

	for _, w := range r.fleet.List() {
		id := w.Entry.Card.WorkerID
		e := w.Entry
		st := w.State
		if reason := r.ineligible(req, id, e, st); reason != "" {
			if elig.Filtered == nil {
				elig.Filtered = make(map[IneligibilityReason]int)
			}
			elig.Filtered[reason]++
			continue
		}
		elig.Eligible++

		// KV overlap credits: cached prefix tokens cancel prefill work.
		// The lookup seam owns the directory-vs-legacy policy (identity-
		// exact when the request carries cache identity, legacy otherwise).
		overlap := 0
		if req.Namespace != "" && req.PrefixHash != "" {
			overlap = r.lookup.OverlapTokens(id, req.Namespace, req.PrefixHash, req.Cache)
		}

		newPrompt := req.InputTokens - overlap
		if newPrompt < 0 {
			newPrompt = 0
		}
		prefillCost := r.cfg.PrefillScale * float64(st.PrefillActive+newPrompt)
		decodeCost := r.cfg.DecodeKVScale * float64(st.DecodeKV+req.ExpectedOutputTokens)
		reqCost := r.cfg.RequestScale * float64(st.ActiveRequests)
		cost := prefillCost + decodeCost + reqCost

		// Affinity preference: discount while healthy, break when not.
		if id == req.AffinityWorker {
			cost *= 1 - r.cfg.AffinityDiscount
		}

		// LoRA affinity (spec §14 row 18): a resident adapter discounts
		// its host; cold workers must load it first.
		if req.LoRAAdapter != "" && r.lora != nil && r.lora.Loaded(id, req.LoRAAdapter) {
			cost *= 1 - r.cfg.AffinityDiscount
		}

		// Media-hash routing (spec §12.3.6): warm encoder state discounts
		// the worker holding the same media hash for this context.
		if req.MediaHash != "" && req.PrefixHash != "" {
			if containsStr(r.lookup.WorkersWithMedia(req.Namespace, req.PrefixHash, req.MediaHash, req.Cache), id) {
				cost *= 1 - r.cfg.MediaDiscount
			}
		}

		if cost < best.Cost {
			best = RouteDecision{WorkerID: id, Cost: cost, OverlapTokens: overlap, Reason: "lowest-cost"}
			found = true
		}
	}
	if !found {
		return RouteDecision{}, &RouteError{Model: req.Model, Permanent: elig.Permanent(), Eligibility: elig}
	}
	if r.receipts != nil {
		st := RouterWorkerState{}
		if w, ok := r.fleet.Get(best.WorkerID); ok {
			st = w.State
		}
		rec := RoutingReceipt{
			Decision:      best,
			Model:         req.Model,
			Namespace:     req.Namespace,
			InputTokens:   req.InputTokens,
			AtUnixMs:      timeNowUnixMs(),
			PolicyVersion: r.Version(),
			Eligibility:   elig,
			Path: &RoutePath{
				Region: req.Region,
				Pool:   req.Pool,
				Worker: best.WorkerID,
			},
			Signals: &DecisionSignals{
				PrefillActive:  st.PrefillActive,
				DecodeKV:       st.DecodeKV,
				ActiveRequests: st.ActiveRequests,
			},
		}
		if r.predictor != nil {
			rec.PredictorVersion = PredictorVersion
		}
		if r.shadow != nil {
			rec.Shadow = r.runShadow(req, best)
		}
		r.receipts.Add(rec)
	}
	return best, nil
}

// RouteError reports a no-placement outcome with the eligibility
// evidence (WS3 §bounded early rejection: honest, retryable reasons).
// Permanent means no eligible path exists at any load level — the caller
// rejects early instead of requeueing to TTL.
type RouteError struct {
	Model       string
	Permanent   bool
	Eligibility *EligibilityReport
}

// Error renders the reason with its permanence class.
func (e *RouteError) Error() string {
	if e.Permanent {
		return fmt.Sprintf("scheduler: no eligible worker for model %q (permanent: no serving path)", e.Model)
	}
	return fmt.Sprintf("scheduler: no eligible worker for model %q (transient: capacity may free)", e.Model)
}

// timeNowUnixMs is a tiny clock indirection for deterministic tests.
var timeNowUnixMs = func() int64 { return time.Now().UnixMilli() }

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// RoutingReceipt is a signed, queryable record of one placement decision
// (spec §13.6): worker, overlap tokens, affinity decision, class. PAT-1445
// adds the policy/predictor versions that produced the decision and the
// shadow-candidate comparison. The scheduler signs with its evidence key;
// the CP API queries them (S10).
type RoutingReceipt struct {
	Decision         RouteDecision      `json:"decision"`
	Model            string             `json:"model"`
	Namespace        string             `json:"namespace"`
	InputTokens      int                `json:"input_tokens"`
	AtUnixMs         int64              `json:"at_unix_ms"`
	PolicyVersion    string             `json:"policy_version,omitempty"`
	PredictorVersion string             `json:"predictor_version,omitempty"`
	Eligibility      *EligibilityReport `json:"eligibility,omitempty"`
	Path             *RoutePath         `json:"path,omitempty"`
	Signals          *DecisionSignals   `json:"signals,omitempty"`
	Shadow           *ShadowRecord      `json:"shadow,omitempty"`
	SignatureHex     string             `json:"signature_hex,omitempty"`
}

// Sign binds the receipt with the given key (canonical body = the JSON
// fields excluding the signature).
func (r *RoutingReceipt) Sign(priv ed25519.PrivateKey) error {
	body := RoutingReceipt{
		Decision:         r.Decision,
		Model:            r.Model,
		Namespace:        r.Namespace,
		InputTokens:      r.InputTokens,
		AtUnixMs:         r.AtUnixMs,
		PolicyVersion:    r.PolicyVersion,
		PredictorVersion: r.PredictorVersion,
		Eligibility:      r.Eligibility,
		Path:             r.Path,
		Signals:          r.Signals,
		Shadow:           r.Shadow,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	r.SignatureHex = hex.EncodeToString(ed25519.Sign(priv, raw))
	return nil
}

// Verify checks the receipt signature.
func (r *RoutingReceipt) Verify(pub ed25519.PublicKey) bool {
	if r.SignatureHex == "" {
		return false
	}
	sig, err := hex.DecodeString(r.SignatureHex)
	if err != nil {
		return false
	}
	body := RoutingReceipt{
		Decision:         r.Decision,
		Model:            r.Model,
		Namespace:        r.Namespace,
		InputTokens:      r.InputTokens,
		AtUnixMs:         r.AtUnixMs,
		PolicyVersion:    r.PolicyVersion,
		PredictorVersion: r.PredictorVersion,
		Eligibility:      r.Eligibility,
		Path:             r.Path,
		Signals:          r.Signals,
		Shadow:           r.Shadow,
	}
	raw, _ := json.Marshal(body)
	return ed25519.Verify(pub, raw, sig)
}

// ReceiptStore holds placement receipts (bounded ring; drained by
// observability). The Router emits one receipt per Route decision.
type ReceiptStore struct {
	mu  sync.Mutex
	log []RoutingReceipt
	max int
	key ed25519.PrivateKey // signing key (nil = unsigned)
}

// NewReceiptStore builds a bounded receipt store.
func NewReceiptStore(max int) *ReceiptStore {
	if max <= 0 {
		max = 1024
	}
	return &ReceiptStore{max: max}
}

// SetSigningKey installs the receipt signing key (evidence key).
func (rs *ReceiptStore) SetSigningKey(priv ed25519.PrivateKey) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.key = priv
}

// PublicKey exposes the verification key (nil when unsigned).
func (rs *ReceiptStore) PublicKey() ed25519.PublicKey {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.key == nil {
		return nil
	}
	return rs.key.Public().(ed25519.PublicKey)
}

// Add appends a receipt (signing when a key is installed).
func (rs *ReceiptStore) Add(r RoutingReceipt) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.key != nil {
		_ = r.Sign(rs.key)
	}
	rs.log = append(rs.log, r)
	if len(rs.log) > rs.max {
		rs.log = rs.log[len(rs.log)-rs.max:]
	}
}

// Recent returns receipts in insertion order.
func (rs *ReceiptStore) Recent() []RoutingReceipt {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make([]RoutingReceipt, len(rs.log))
	copy(out, rs.log)
	return out
}
