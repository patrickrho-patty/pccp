package scheduler

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
)

// rewrite.go implements S2 model rewriting (spec §14 rows 1, 17): aliases,
// version remap/migration, weighted traffic split (A/B, canary), and
// fallback chains. Resolution is deterministic per correlation ID so a
// split decision is stable across retries of the same request.

// RewriteStrategy selects how a multi-target rule picks its target.
type RewriteStrategy string

const (
	StrategySplit    RewriteStrategy = "split"    // weighted by traffic share
	StrategyFallback RewriteStrategy = "fallback" // first available target wins
)

// RewriteRule is one alias's resolution policy.
type RewriteRule struct {
	Targets     []string        // ordered candidate targets
	Weights     map[string]int  // relative weights for StrategySplit
	Unavailable map[string]bool // targets administratively withdrawn
	Strategy    RewriteStrategy
}

// ModelRewriter resolves external model names to serving catalog IDs and
// enforces per-tenant model access (spec §14 row 17: per-tenant access).
// All methods are safe for concurrent use (the live path resolves on
// every request).
type ModelRewriter struct {
	mu      sync.RWMutex
	aliases map[string]string
	rules   map[string]RewriteRule
	tenants map[string]map[string]bool // tenant → allowed catalog IDs
}

// NewModelRewriter builds an empty rewriter. Rules may be seeded and then
// mutated at runtime by admin config.
func NewModelRewriter(seed map[string]RewriteRule) *ModelRewriter {
	rw := &ModelRewriter{
		aliases: make(map[string]string),
		rules:   make(map[string]RewriteRule),
		tenants: make(map[string]map[string]bool),
	}
	for name, rule := range seed {
		rw.rules[name] = rule
	}
	return rw
}

// SetAlias registers a 1:1 name → catalog ID mapping.
func (rw *ModelRewriter) SetAlias(name, target string) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	rw.aliases[name] = target
}

// SetRemap registers a version migration (old ID → new ID, 100% traffic).
func (rw *ModelRewriter) SetRemap(from, to string) {
	rw.SetAlias(from, to)
}

// SetSplit registers a weighted traffic split for name.
func (rw *ModelRewriter) SetSplit(name string, weights map[string]int) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	targets := make([]string, 0, len(weights))
	for t := range weights {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	rw.rules[name] = RewriteRule{
		Targets:  targets,
		Weights:  weights,
		Strategy: StrategySplit,
	}
}

// SetRule registers an arbitrary rewrite rule (fallback chains, canary
// with withdrawal, etc.).
func (rw *ModelRewriter) SetRule(name string, rule RewriteRule) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	rw.rules[name] = rule
}

// Resolve maps an external model name to the serving catalog ID for the
// request identified by correlationID. Unknown names are rejected — the
// raw/fake model-ID gate holds even before catalog validation (§10A.11).
func (rw *ModelRewriter) Resolve(name string, correlationID int64) (string, error) {
	rw.mu.RLock()
	defer rw.mu.RUnlock()

	if target, ok := rw.aliases[name]; ok {
		return target, nil
	}
	rule, ok := rw.rules[name]
	if !ok {
		return "", fmt.Errorf("scheduler: unknown model %q", name)
	}

	var candidates []string
	for _, t := range rule.Targets {
		if !rule.Unavailable[t] {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("scheduler: model %q has no available targets", name)
	}

	switch rule.Strategy {
	case StrategyFallback:
		return candidates[0], nil
	case StrategySplit, "":
		if len(candidates) == 1 {
			return candidates[0], nil
		}
		total := 0
		for _, t := range candidates {
			total += rule.Weights[t]
		}
		if total <= 0 {
			return "", fmt.Errorf("scheduler: model %q split has no positive weights", name)
		}
		// Deterministic per correlation ID: hash the ID into [0,total).
		h := fnv.New64a()
		fmt.Fprintf(h, "%d\x00%s", correlationID, name)
		pick := int(h.Sum64() % uint64(total))
		acc := 0
		for _, t := range candidates {
			acc += rule.Weights[t]
			if pick < acc {
				return t, nil
			}
		}
		return candidates[len(candidates)-1], nil
	default:
		return "", fmt.Errorf("scheduler: unknown rewrite strategy %q for %q", rule.Strategy, name)
	}
}

// SetTenantModels replaces a tenant's allowed catalog-ID set. An empty
// list removes the tenant entirely (deny all — fail closed).
func (rw *ModelRewriter) SetTenantModels(tenant string, allowed []string) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if len(allowed) == 0 {
		delete(rw.tenants, tenant)
		return
	}
	set := make(map[string]bool, len(allowed))
	for _, m := range allowed {
		set[m] = true
	}
	rw.tenants[tenant] = set
}

// CheckTenantAccess reports whether the tenant may use the resolved
// catalog model. Unconfigured tenants are denied (fail closed).
func (rw *ModelRewriter) CheckTenantAccess(tenant, catalogID string) error {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	set, ok := rw.tenants[tenant]
	if !ok || !set[catalogID] {
		return fmt.Errorf("scheduler: tenant %q has no access to model %q", tenant, catalogID)
	}
	return nil
}
