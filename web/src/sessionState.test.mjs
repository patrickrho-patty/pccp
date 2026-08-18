import test from 'node:test'
import assert from 'node:assert/strict'

import { isLiveSession, isInProgressSession, streamFreshness, relativeAge } from './sessionState.ts'
import { formatTenantTime } from './utils/format.ts'

test('only active sessions are live while idle and paused remain in progress', () => {
  assert.equal(isLiveSession({ status: 'active' }), true)
  for (const status of ['idle', 'paused', 'pending', 'closed', 'terminated']) {
    assert.equal(isLiveSession({ status }), false, status)
  }
  assert.equal(isInProgressSession({ status: 'idle' }), true)
  assert.equal(isInProgressSession({ status: 'paused' }), true)
  assert.equal(isInProgressSession({ status: 'pending' }), true)
})

test('stream freshness agrees with the fifteen-second heartbeat contract', () => {
  assert.equal(streamFreshness(null, false).ok, false)
  assert.equal(streamFreshness(null, true).ok, true)
  assert.equal(streamFreshness(34_999, true).ok, true)
  assert.equal(streamFreshness(35_000, true).ok, false)
})

test('session timestamps honor tenant timezone and expose clock skew', () => {
  const instant = '2026-01-01T00:00:00Z'
  assert.notEqual(formatTenantTime(instant, 'Asia/Seoul'), formatTenantTime(instant, 'America/New_York'))
  assert.match(formatTenantTime(instant, 'America/New_York'), /\(America\/New_York\)$/)
  assert.match(relativeAge('2026-01-01T00:02:00Z', Date.parse(instant)), /서버 시간보다 2분 이후/)
  assert.equal(relativeAge('not-a-date'), '-')
})
