// newIdempotencyKey returns a stable per-request identifier suitable for
// Idempotency-Key headers (broadcast client_token, fleet bulk actions, ...).
//
// `crypto.randomUUID()` throws in non-secure contexts (plain-http deployments,
// older browsers); we fall back to a timestamp+random token so the caller
// still gets a unique key and the server's partial-unique index can serialize
// retries. Keep one source for both paths — fixing the fallback in one
// workflow (e.g. broadcasts) does not silently miss another (e.g. fleet).
export function newIdempotencyKey(): string {
	if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
		try {
			return crypto.randomUUID()
		} catch {
			// fall through
		}
	}
	return `idemp-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}