package scheduler

// kvlookup.go implements the versioned cache-overlap seam (PAT-1445 B2):
// the router asks one question; the composite owns the directory-vs-legacy
// fallback policy in one place (previously duplicated at both scoring
// call sites). Either adapter may be absent — the seam degrades
// independently (WS1: components are independently degradable).

// KVLookup answers cache-overlap queries against the WS1 directory when
// the request carries cache identity, else the legacy namespace index.
type KVLookup struct {
	legacy *KVIndex
	dir    *KVDirectory
}

// NewKVLookup builds the composite; either adapter may be nil.
func NewKVLookup(legacy *KVIndex, dir *KVDirectory) *KVLookup {
	return &KVLookup{legacy: legacy, dir: dir}
}

// SetLegacy installs/replaces the legacy index adapter.
func (l *KVLookup) SetLegacy(kv *KVIndex) { l.legacy = kv }

// SetDirectory installs/replaces the WS1 directory adapter.
func (l *KVLookup) SetDirectory(d *KVDirectory) { l.dir = d }

// OverlapTokens returns the reusable prefix size on one worker: exact
// identity via the directory when identity is present and a directory is
// installed, else the legacy namespace-scoped index.
func (l *KVLookup) OverlapTokens(workerID, namespace, hash string, id CacheIdentity) int {
	if l.dir != nil && id != (CacheIdentity{}) {
		return l.dir.OverlapTokens(workerID, namespace, hash, id)
	}
	if l.legacy != nil {
		return l.legacy.OverlapTokens(workerID, namespace, hash)
	}
	return 0
}

// WorkersWithMedia returns workers holding the block with the given media
// variant, following the same directory-first policy.
func (l *KVLookup) WorkersWithMedia(namespace, hash, media string, id CacheIdentity) []string {
	if l.dir != nil && id != (CacheIdentity{}) {
		return l.dir.WorkersWithMedia(namespace, hash, media, id)
	}
	if l.legacy != nil {
		return l.legacy.WorkersWithMedia(namespace, hash, media)
	}
	return nil
}
