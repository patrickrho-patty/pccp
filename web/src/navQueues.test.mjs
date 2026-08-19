import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  NAV_QUEUES, navQueueFor, navCountsFromDashboard, navSeverityTint,
} from './navQueues.ts'

test('nav queues only expose exact-scoped destinations', () => {
  const security = navQueueFor('/security')
  assert.ok(security)
  assert.equal(security.metric, 'open_critical_findings')
  assert.match(security.href, /severity=critical,high/)
  const tools = navQueueFor('/tools')
  assert.equal(tools.href, '/tools?tab=approvals')
  assert.equal(navQueueFor('/users'), undefined) // not a count queue
  assert.equal(navQueueFor('/dashboard'), undefined)
})

test('every queue has a canonical metric and a scoped destination', () => {
  for (const q of NAV_QUEUES) {
    assert.ok(q.metric.length > 0, `${q.path} metric`)
    assert.ok(q.href.length > 0 && (q.href.includes('?') || q.href === '/fleet'), `${q.path} href scoped`)
  }
})

test('counts derive from the dashboard, unknown metrics → 0', () => {
  const counts = navCountsFromDashboard({ open_critical_findings: 2, open_remediations: 3, pending_approvals: 0, quarantined_harnesses: 1 })
  assert.equal(counts['/security'], 2)
  assert.equal(counts['/compliance'], 3)
  assert.equal(counts['/tools'], 0)      // zero stays zero
  assert.equal(counts['/harnesses'], 1)
  const empty = navCountsFromDashboard({})
  assert.deepEqual(empty, { '/security': 0, '/compliance': 0, '/tools': 0, '/harnesses': 0 })
})

test('severity tint is deterministic and color never sole signal', () => {
  assert.equal(navSeverityTint('critical'), 'bg-red-500')
  assert.equal(navSeverityTint('warning'), 'bg-amber-500')
  assert.equal(navSeverityTint('neutral'), 'bg-gray-600')
  assert.equal(navSeverityTint(undefined), 'bg-gray-600')
})
