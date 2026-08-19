import { test } from 'node:test'
import assert from 'node:assert/strict'

import { newIdempotencyKey } from './utils/id.ts'

test('newIdempotencyKey returns a non-empty string', () => {
	const key = newIdempotencyKey()
	assert.equal(typeof key, 'string')
	assert.ok(key.length > 0, 'key must not be empty')
})

test('newIdempotencyKey falls back when crypto.randomUUID is missing or throws', () => {
	const savedCrypto = globalThis.crypto
	try {
		// crypto.randomUUID missing entirely (simulate very old browsers /
		// plain-http contexts where the API is undefined).
		Object.defineProperty(globalThis, 'crypto', { value: {}, configurable: true })
		const k = newIdempotencyKey()
		assert.equal(typeof k, 'string')
		assert.ok(k.startsWith('idemp-'), 'fallback key uses the idemp- prefix')

		// crypto.randomUUID present but throws (e.g. secure-context failure).
		Object.defineProperty(globalThis, 'crypto', {
			value: { randomUUID: () => { throw new Error('secure context required') } },
			configurable: true,
		})
		const k2 = newIdempotencyKey()
		assert.ok(k2.startsWith('idemp-'), 'throwing randomUUID falls back to idemp- prefix')
	} finally {
		Object.defineProperty(globalThis, 'crypto', { value: savedCrypto, configurable: true })
	}
})

test('newIdempotencyKey prefers crypto.randomUUID when available', () => {
	const savedCrypto = globalThis.crypto
	try {
		Object.defineProperty(globalThis, 'crypto', {
			value: { randomUUID: () => 'fixed-uuid-for-test' },
			configurable: true,
		})
		assert.equal(newIdempotencyKey(), 'fixed-uuid-for-test')
	} finally {
		Object.defineProperty(globalThis, 'crypto', { value: savedCrypto, configurable: true })
	}
})