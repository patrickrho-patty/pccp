import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const live = readFileSync(new URL('./pages/LiveView.tsx', import.meta.url), 'utf8')
const sessions = readFileSync(new URL('./pages/Sessions.tsx', import.meta.url), 'utf8')
const fleet = readFileSync(new URL('./pages/Fleet.tsx', import.meta.url), 'utf8')

test('live EventSource uses a one-time ticket and manually reconnects with a cursor', () => {
  assert.match(live, /api\.liveStreamTicket\(\)/)
  assert.doesNotMatch(live, /pccp_token/)
  assert.doesNotMatch(live, /sse\?token=/)
  assert.match(live, /last_event_id=/)
  assert.match(live, /source\?\.close\(\)/)
})

test('live filters remain URL-authoritative across browser navigation', () => {
  assert.match(live, /const filters = useMemo/)
  assert.match(live, /setLiveFilter/)
  assert.doesNotMatch(live, /const \[filters, setFilters\]/)
})

test('session filters restore exact URL state after browser navigation', () => {
  assert.match(sessions, /const sessionFilterKeys/)
  assert.match(sessions, /new URLSearchParams\(sessionFilterKey\)/)
  assert.match(sessions, /table\.setFilter\(key, value\)/)
})

test('session and fleet bulk actions expose reachable row selection and reasons', () => {
  for (const source of [sessions, fleet]) {
    assert.match(source, /type="checkbox"/)
    assert.match(source, /toggleSelect\(/)
    assert.match(source, /사유 \(필수\)/)
  }
})

test('fleet bulk controls submit canonical backend action names', () => {
  assert.match(fleet, /value="pause_agent_execution"/)
  assert.match(fleet, /value="require_client_upgrade"/)
  assert.doesNotMatch(fleet, /value="pause_execution"/)
  assert.doesNotMatch(fleet, /value="require_upgrade"/)
})
