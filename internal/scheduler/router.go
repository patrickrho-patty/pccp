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
	RequestClass         string // agentic / interactive / batch (SLO scoping)
	Pool                 string // model pool scope (spec §14 row 17)
	LoRAAdapter          string // requested adapter (affinity, §14 row 18)
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
	workers    map[string]WorkerEntry
	state      map[string]RouterWorkerState
	kv         *KVIndex
	receipts   *ReceiptStore
	gang       *GangRegistry
	predictor  *LatencyPredictor
	slo        *SLOResolver
	workerCfg  map[string]string // workerID → predictor config ID
	maxSLORisk float64           // placements above this P(SLO violation) are rejected
	lora       *LoRaLifecycle
	pools      *ModelPoolManager
	shadow     Router            // candidate evaluated alongside (PAT-1445 shadow mode)
}

// NewCostRouter builds a router with the given config.
func NewCostRouter(cfg RouterConfig) *CostRouter {
	return &CostRouter{
		cfg:        cfg,
		workers:    make(map[string]WorkerEntry),
		state:      make(map[string]RouterWorkerState),
		workerCfg:  make(map[string]string),
		maxSLORisk: 0.5,
	}
}

// Version returns the frozen baseline policy identity.
func (r *CostRouter) Version() string { return CostRouterVersion }

// SetKV installs the fleet KV index (S3 §13.11).
func (r *CostRouter) SetKV(kv *KVIndex) { r.kv = kv }

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

// UpsertWorker installs or refreshes a worker's card + load state.
func (r *CostRouter) UpsertWorker(e WorkerEntry, s RouterWorkerState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workers[e.Card.WorkerID] = e
	r.state[e.Card.WorkerID] = s
}

// RemoveWorker drops a worker.
func (r *CostRouter) RemoveWorker(workerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.workers, workerID)
	delete(r.state, workerID)
}

// Route picks the lowest-cost eligible worker per the locked cost model:
//
//	cost = prefillScale × max(activePrefill + newPrompt − KVOverlapCredits, 0)
//	     + projectedDecodeKV + w × activeRequests
//
// with discounts for session affinity and warm media state. Affinity is
// a preference, never a pin: overloaded workers lose it (spec §12.3.2).
func (r *CostRouter) Route(req RouteRequest) (RouteDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	best := RouteDecision{Cost: 1e18}
	found := false

	for id, e := range r.workers {
		if e.Card.ModelName != req.Model {
			continue
		}
		if r.pools != nil && !r.pools.Contains(req.Pool, id) {
			continue
		}
		if e.Quarantined || e.Lapsed || !e.Card.Servable() {
			continue
		}
		// Gang readiness: an incomplete parallel group serves nothing
		// (spec §14 row 16).
		if r.gang != nil && r.gang.MemberBlocked(id) {
			continue
		}
		st := r.state[id]

		// SLO gate (S5): reject placements whose predicted TTFT
		// violates the objective with high probability.
		if r.predictor != nil && r.slo != nil {
			cfgID := r.workerCfg[id]
			if cfgID == "" {
				cfgID = id
			}
			target := r.slo.ForRequest(req.Model, req.RequestClass)
			if target.TTFTMs > 0 {
				f := PredictorFeatures{
					InputTokens:          req.InputTokens,
					CachedTokens:         req.CachedTokens,
					ExpectedOutputTokens: req.ExpectedOutputTokens,
					ActivePrefill:        st.PrefillActive,
					ActiveDecodeKV:       st.DecodeKV,
					ActiveRequests:       st.ActiveRequests,
				}
				if risk := r.predictor.PSLOViolation(cfgID, f, float64(target.TTFTMs)); risk > r.maxSLORisk {
					continue
				}
			}
		}

		// Overload filter (spec §12.3.1): a saturated worker is
		// ineligible regardless of how cheap its cache would be.
		if st.Load.MaxConcurrent > 0 && st.Load.Active >= st.Load.MaxConcurrent {
			continue
		}
		if r.cfg.MaxActiveRequests > 0 && st.ActiveRequests >= r.cfg.MaxActiveRequests {
			continue
		}

		// KV overlap credits: cached prefix tokens cancel prefill work.
		overlap := 0
		if r.kv != nil && req.Namespace != "" && req.PrefixHash != "" {
			overlap = r.kv.OverlapTokens(id, req.Namespace, req.PrefixHash)
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
		if req.MediaHash != "" && r.kv != nil && req.PrefixHash != "" {
			if ws := r.kv.WorkersWithMedia(req.Namespace, req.PrefixHash, req.MediaHash); containsStr(ws, id) {
				cost *= 1 - r.cfg.MediaDiscount
			}
		}

		if cost < best.Cost {
			best = RouteDecision{WorkerID: id, Cost: cost, OverlapTokens: overlap, Reason: "lowest-cost"}
			found = true
		}
	}
	if !found {
		return RouteDecision{}, fmt.Errorf("scheduler: no eligible worker for model %q", req.Model)
	}
	if r.receipts != nil {
		rec := RoutingReceipt{
			Decision:      best,
			Model:         req.Model,
			Namespace:     req.Namespace,
			InputTokens:   req.InputTokens,
			AtUnixMs:      timeNowUnixMs(),
			PolicyVersion: r.Version(),
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
	Decision         RouteDecision `json:"decision"`
	Model            string        `json:"model"`
	Namespace        string        `json:"namespace"`
	InputTokens      int           `json:"input_tokens"`
	AtUnixMs         int64         `json:"at_unix_ms"`
	PolicyVersion    string        `json:"policy_version,omitempty"`
	PredictorVersion string        `json:"predictor_version,omitempty"`
	Shadow           *ShadowRecord `json:"shadow,omitempty"`
	SignatureHex     string        `json:"signature_hex,omitempty"`
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
