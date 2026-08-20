package scheduler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// kvdirectory.go implements the PAT-1445 WS1 distributed KV state plane:
// a versioned directory of cache extents with full compatibility identity,
// tiered locations, verification/expiry lifecycle, and tenant-keyed prefix
// identity. It extends — never replaces — the S3 KVIndex (which stays the
// legacy journal-ingestion view): the router prefers the directory when a
// request carries cache identity and falls back to the index otherwise.
//
// The directory stores metadata and bounded identifiers only: never prompt
// text, raw KV values, or content-derived fields.

// CacheIdentity is the compatibility namespace of one cache extent: reuse
// is allowed only when EVERY dimension matches (WS1 directory & identity).
// A model/tokenizer/template/policy change produces a different identity,
// which is what makes invalidation on such changes exact.
type CacheIdentity struct {
	ModelPackage string `json:"model_package"` // model name + version
	TokenizerID  string `json:"tokenizer_id"`
	TemplateID   string `json:"template_id"`
	AdapterID    string `json:"adapter_id,omitempty"`
	PolicyEpoch  string `json:"policy_epoch"`
}

// dirKey is the directory key: tenant namespace + tenant-keyed prefix hash
// + media variant + full compatibility identity.
type dirKey struct {
	namespace string
	hash      string
	media     string
	id        CacheIdentity
}

// ExtentLocation is one worker's residence of an extent.
type ExtentLocation struct {
	WorkerID    string `json:"worker_id"`
	Tier        KVTier `json:"tier"`
	Tokens      int    `json:"tokens"`
	Verified    bool   `json:"verified"` // residency confirmed with the source
	LastUseUnix int64  `json:"last_use_unix"`
}

// CacheExtent is the directory record for one prefix extent.
type CacheExtent struct {
	Namespace  string                     `json:"namespace"`
	Hash       string                     `json:"hash"`
	MediaHash  string                     `json:"media_hash,omitempty"`
	Identity   CacheIdentity              `json:"identity"`
	Locations  map[string]*ExtentLocation `json:"locations"` // workerID → residence
	Hits       int64                      `json:"hits"`
	ExpiryUnix int64                      `json:"expiry_unix,omitempty"` // 0 = no TTL
}

// HotPrefix is a replication candidate: an extent whose hit count exceeds
// the threshold while its replica count stays low (WS1 hot-prefix
// replication candidacy; execution is the migration coordinator's job).
type HotPrefix struct {
	Namespace string        `json:"namespace"`
	Hash      string        `json:"hash"`
	Identity  CacheIdentity `json:"identity"`
	Hits      int64         `json:"hits"`
	Replicas  int           `json:"replicas"`
	Tokens    int           `json:"tokens"` // largest single residence
}

// KVDirectory is the fleet cache-extent directory. Safe for concurrent use.
type KVDirectory struct {
	mu      sync.RWMutex
	extents map[dirKey]*CacheExtent
	water   map[string]uint64 // worker → highest applied journal seq (§13.11)
	now     func() int64
}

// NewKVDirectory builds an empty directory.
func NewKVDirectory() *KVDirectory {
	return &KVDirectory{
		extents: make(map[dirKey]*CacheExtent),
		water:   make(map[string]uint64),
		now:     func() int64 { return time.Now().Unix() },
	}
}

// SetNow injects a clock (deterministic tests).
func (d *KVDirectory) SetNow(fn func() int64) { d.now = fn }

// KeyPrefixHash derives the tenant-scoped prefix identity (HMAC-SHA256
// over the tenant key): identical prefix bytes under different tenant
// keys never produce the same directory identity, so a prefix hash cannot
// become a cross-tenant content oracle (WS1 safety boundary). Callers
// without a key must keep using the legacy plain hash — the directory
// keeps both safely separated by tenant namespace regardless.
func KeyPrefixHash(tenantKey, prefix []byte) string {
	m := hmac.New(sha256.New, tenantKey)
	m.Write(prefix)
	return hex.EncodeToString(m.Sum(nil))
}

// Add records (or refreshes) one worker's residence of an extent.
// verified marks source-confirmed residency; unverified locations earn no
// router credit until VerifyLocation confirms them (stale-directory
// fallback: recompute, never misroute).
func (d *KVDirectory) Add(workerID string, tier KVTier, b KVBlock, id CacheIdentity, verified bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.addLocked(workerID, tier, b, id, verified)
}

func (d *KVDirectory) addLocked(workerID string, tier KVTier, b KVBlock, id CacheIdentity, verified bool) {
	key := dirKey{namespace: b.Namespace, hash: b.Hash, media: b.MediaHash, id: id}
	ext, ok := d.extents[key]
	if !ok {
		ext = &CacheExtent{
			Namespace: b.Namespace,
			Hash:      b.Hash,
			MediaHash: b.MediaHash,
			Identity:  id,
			Locations: make(map[string]*ExtentLocation),
		}
		d.extents[key] = ext
	}
	ext.Locations[workerID] = &ExtentLocation{
		WorkerID:    workerID,
		Tier:        tier,
		Tokens:      b.Tokens,
		Verified:    verified,
		LastUseUnix: d.now(),
	}
}

// ApplyJournal applies one worker's journal batch with (worker, seq)
// dedup (spec §13.11 restart replay). Journaled blocks share the worker's
// current cache identity (one engine serves one model package) and are
// HBM-resident (L1).
func (d *KVDirectory) ApplyJournal(workerID string, seq uint64, blocks []KVBlock, id CacheIdentity) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, ok := d.water[workerID]; ok && seq <= prev {
		return false
	}
	d.water[workerID] = seq
	for _, b := range blocks {
		d.addLocked(workerID, L1GPU, b, id, true)
	}
	return true
}

// Watermark returns the highest applied journal seq for a worker.
func (d *KVDirectory) Watermark(workerID string) uint64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.water[workerID]
}

// usable reports whether a location earns reuse credit right now:
// source-verified and inside its extent's TTL. Anything else falls back
// to recompute — the deterministic conservative answer for stale or
// partitioned directory state.
func (d *KVDirectory) usable(ext *CacheExtent, loc *ExtentLocation) bool {
	if !loc.Verified {
		return false
	}
	if ext.ExpiryUnix > 0 && d.now() >= ext.ExpiryUnix {
		return false
	}
	return true
}

// OverlapTokens returns the reusable prefix size on one worker for the
// exact (namespace, hash, identity) — zero for any identity mismatch,
// unverified residence, or expired extent. Identity-less lookups match
// nothing: reuse without a full identity check is never granted.
func (d *KVDirectory) OverlapTokens(workerID, namespace, hash string, id CacheIdentity) int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	best := 0
	for key, ext := range d.extents {
		if key.namespace != namespace || key.hash != hash || key.id != id {
			continue
		}
		if loc, ok := ext.Locations[workerID]; ok && d.usable(ext, loc) {
			if loc.Tokens > best {
				best = loc.Tokens
			}
		}
	}
	return best
}

// WorkersWithMedia returns workers holding a usable residence of the
// extent with the given media variant and exact identity.
func (d *KVDirectory) WorkersWithMedia(namespace, hash, media string, id CacheIdentity) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ext, ok := d.extents[dirKey{namespace: namespace, hash: hash, media: media, id: id}]
	if !ok {
		return nil
	}
	var out []string
	for workerID, loc := range ext.Locations {
		if d.usable(ext, loc) {
			out = append(out, workerID)
		}
	}
	sort.Strings(out)
	return out
}

// Hit records one reuse of an extent (hot-prefix signal, last-use stamp).
func (d *KVDirectory) Hit(namespace, hash string, id CacheIdentity) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := dirKey{namespace: namespace, hash: hash, id: id}
	if ext, ok := d.extents[key]; ok {
		ext.Hits++
		now := d.now()
		for _, loc := range ext.Locations {
			loc.LastUseUnix = now
		}
	}
}

// VerifyLocation marks a residence source-confirmed (ownership/residency
// verified before binding, WS1 stale/false-location mitigation).
func (d *KVDirectory) VerifyLocation(workerID, namespace, hash string, id CacheIdentity) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, ext := range d.extents {
		if key.namespace != namespace || key.hash != hash || key.id != id {
			continue
		}
		if loc, ok := ext.Locations[workerID]; ok {
			loc.Verified = true
			return true
		}
	}
	return false
}

// Promote moves a residence to a hotter tier; Demote to a colder one.
func (d *KVDirectory) Promote(workerID, namespace, hash string, id CacheIdentity, to KVTier) {
	d.moveTier(workerID, namespace, hash, id, to, true)
}

// Demote moves a residence to a colder tier (e.g. HBM pressure relief).
func (d *KVDirectory) Demote(workerID, namespace, hash string, id CacheIdentity, to KVTier) {
	d.moveTier(workerID, namespace, hash, id, to, false)
}

func (d *KVDirectory) moveTier(workerID, namespace, hash string, id CacheIdentity, to KVTier, hotter bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, ext := range d.extents {
		if key.namespace != namespace || key.hash != hash || key.id != id {
			continue
		}
		if loc, ok := ext.Locations[workerID]; ok {
			if hotter && to < loc.Tier || !hotter && to > loc.Tier {
				loc.Tier = to
			}
		}
	}
}

// SetTTL bounds an extent's retention (policy-aware TTL).
func (d *KVDirectory) SetTTL(namespace, hash string, id CacheIdentity, ttl time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := dirKey{namespace: namespace, hash: hash, id: id}
	if ext, ok := d.extents[key]; ok {
		ext.ExpiryUnix = d.now() + int64(ttl.Seconds())
	}
}

// Sweep evicts locations idle longer than retentionSeconds and drops
// empty extents (policy-aware eviction).
func (d *KVDirectory) Sweep(retentionSeconds int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	for key, ext := range d.extents {
		for workerID, loc := range ext.Locations {
			if now-loc.LastUseUnix > retentionSeconds {
				delete(ext.Locations, workerID)
			}
		}
		if len(ext.Locations) == 0 {
			delete(d.extents, key)
		}
	}
}

// InvalidateIf drops every extent whose identity matches — the exact
// invalidation path for model/tokenizer/template/adapter/policy-epoch
// changes (WS1: namespace change invalidates incompatible cache state).
func (d *KVDirectory) InvalidateIf(match func(CacheIdentity) bool) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for key := range d.extents {
		if match(key.id) {
			delete(d.extents, key)
			n++
		}
	}
	return n
}

// HotPrefixes returns replication candidates: extents with at least
// minHits reuses and at most maxReplicas verified residences.
func (d *KVDirectory) HotPrefixes(minHits int64, maxReplicas int) []HotPrefix {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []HotPrefix
	for _, ext := range d.extents {
		if ext.Hits < minHits {
			continue
		}
		replicas := 0
		maxTokens := 0
		for _, loc := range ext.Locations {
			if d.usable(ext, loc) {
				replicas++
				if loc.Tokens > maxTokens {
					maxTokens = loc.Tokens
				}
			}
		}
		if replicas == 0 || replicas > maxReplicas {
			continue
		}
		out = append(out, HotPrefix{
			Namespace: ext.Namespace,
			Hash:      ext.Hash,
			Identity:  ext.Identity,
			Hits:      ext.Hits,
			Replicas:  replicas,
			Tokens:    maxTokens,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hits > out[j].Hits })
	return out
}

// EvictWorker drops every residence a worker holds (eviction/restart).
func (d *KVDirectory) EvictWorker(workerID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, ext := range d.extents {
		if _, ok := ext.Locations[workerID]; ok {
			delete(ext.Locations, workerID)
			if len(ext.Locations) == 0 {
				delete(d.extents, key)
			}
		}
	}
	delete(d.water, workerID)
}

// Locations returns an extent's verified, unexpired residences (transfer
// planning / locality discovery before placement).
func (d *KVDirectory) Locations(namespace, hash string, id CacheIdentity) []ExtentLocation {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []ExtentLocation
	for key, ext := range d.extents {
		if key.namespace != namespace || key.hash != hash || key.id != id {
			continue
		}
		for _, loc := range ext.Locations {
			if d.usable(ext, loc) {
				out = append(out, *loc)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return out[i].Tier < out[j].Tier
		}
		return out[i].WorkerID < out[j].WorkerID
	})
	return out
}
