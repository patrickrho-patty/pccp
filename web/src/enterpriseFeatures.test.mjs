import test from 'node:test'
import assert from 'node:assert/strict'

import {
  FEATURE_CATALOG,
  defaultScope,
  versionAtLeast,
  isHarnessOnline,
} from './enterpriseCatalog.ts'
import {
  parseGovernance,
  evaluateHarnesses,
  headEpochOf,
  scopeHarnessIds,
} from './governanceTrace.ts'
import {
  validateChange,
  buildPreview,
  applyChange,
  buildRollback,
} from './enterpriseChangeset.ts'

const NOW = Date.parse('2026-08-19T09:00:00Z')
const NOW_ISO = '2026-08-19T09:00:00Z'

function harness(id, overrides = {}) {
  return {
    harness_id: id,
    name: id,
    binary_version: '1.4.0',
    status: 'active',
    last_heartbeat: '2026-08-19T08:59:00Z',
    ...overrides,
  }
}

function feature(key, overrides = {}) {
  return { feature_key: key, enabled: false, enforced: false, status: 'active', config: '', ...overrides }
}

const ALL_KEYS = Object.keys(FEATURE_CATALOG)
function allFeatures(overrides = {}) {
  return ALL_KEYS.map(key => feature(key, { enabled: true, ...(overrides[key] || {}) }))
}

test('catalog covers every seeded feature key', () => {
  assert.equal(ALL_KEYS.length, 20)
  for (const entry of Object.values(FEATURE_CATALOG)) {
    assert.ok(entry.purposeKo && entry.rationaleKo, `${entry.key} needs Korean purpose and rationale`)
    for (const dep of entry.dependencies) assert.ok(FEATURE_CATALOG[dep], `${entry.key} depends on unknown ${dep}`)
  }
})

test('parseGovernance tolerates empty, invalid, and legacy config payloads', () => {
  assert.deepEqual(parseGovernance(''), { scope: defaultScope(), rollouts: [] })
  assert.deepEqual(parseGovernance(undefined), { scope: defaultScope(), rollouts: [] })
  assert.deepEqual(parseGovernance('not json'), { scope: defaultScope(), rollouts: [] })
  assert.deepEqual(parseGovernance('[1,2]'), { scope: defaultScope(), rollouts: [] })
  const gov = parseGovernance(JSON.stringify({ scope: { type: 'selected', harness_ids: ['h1', 7], exceptions: ['h2'] }, rollouts: [{ epoch: 3 }, { nope: true }] }))
  assert.deepEqual(gov.scope, { type: 'selected', harness_ids: ['h1'], exceptions: ['h2'] })
  assert.equal(gov.rollouts.length, 1)
})

test('scope resolution honors selected harnesses and exception scope', () => {
  const ids = ['h1', 'h2', 'h3']
  assert.deepEqual(scopeHarnessIds(defaultScope(), ids), ids)
  assert.deepEqual(scopeHarnessIds({ type: 'org', harness_ids: [], exceptions: ['h2'] }, ids), ['h1', 'h3'])
  assert.deepEqual(scopeHarnessIds({ type: 'selected', harness_ids: ['h1', 'h2'], exceptions: ['h2'] }, ids), ['h1'])
})

test('version compare handles segment ordering and rejects garbage', () => {
  assert.equal(versionAtLeast('1.10.0', '1.2.0'), true)
  assert.equal(versionAtLeast('1.2.0', '1.2.0'), true)
  assert.equal(versionAtLeast('1.1.9', '1.2.0'), false)
  assert.equal(versionAtLeast(undefined, '1.0.0'), false)
  assert.equal(versionAtLeast('dev-build', '1.0.0'), false)
})

test('offline harness: non-active status or stale heartbeat is offline', () => {
  assert.equal(isHarnessOnline(harness('h1'), NOW), true)
  assert.equal(isHarnessOnline(harness('h1', { status: 'quarantined' }), NOW), false)
  assert.equal(isHarnessOnline(harness('h1', { last_heartbeat: '2026-08-19T08:40:00Z' }), NOW), false)
  // Aligned with harnessHealth: a missing heartbeat is NOT online.
  assert.equal(isHarnessOnline(harness('h1', { last_heartbeat: '' }), NOW), false)
})

test('mandatory feature cannot be weakened by a tenant admin but can by patty ops', () => {
  const f = feature('network_egress', { enabled: true, enforced: true })
  const base = { feature: f, features: allFeatures(), harnesses: [harness('h1')], scope: defaultScope(), now: NOW }
  const tenant = validateChange({ ...base, target: { enabled: true, enforced: false }, role: 'admin' })
  assert.ok(tenant.blockers.some(b => b.includes('패티 필수')))
  const patty = validateChange({ ...base, target: { enabled: true, enforced: false }, role: 'super_admin' })
  assert.deepEqual(patty.blockers, [])
})

test('non-admin roles are rejected outright', () => {
  const f = feature('change_freeze')
  const v = validateChange({ feature: f, features: allFeatures(), harnesses: [], target: { enabled: true, enforced: false }, scope: defaultScope(), role: 'viewer', now: NOW })
  assert.ok(v.blockers.some(b => b.includes('권한')))
})

test('dependency disabled blocks activation', () => {
  const features = allFeatures({ network_egress: { enabled: false } })
  const f = feature('sandbox_execution', { status: 'active' })
  const v = validateChange({ feature: f, features, harnesses: [harness('h1', { binary_version: '2.0.0' })], target: { enabled: true, enforced: false }, scope: defaultScope(), role: 'admin', now: NOW })
  assert.ok(v.blockers.some(b => b.includes("의존 기능 'network_egress'")))
})

test('planned feature cannot be activated before harness support ships', () => {
  const f = feature('mandatory_ack', { status: 'planned' })
  const v = validateChange({ feature: f, features: allFeatures(), harnesses: [harness('h1', { binary_version: '2.0.0' })], target: { enabled: true, enforced: false }, scope: defaultScope(), role: 'super_admin', now: NOW })
  assert.ok(v.blockers.some(b => b.includes('시행되지 않은')))
})

test('incompatible harness version blocks enforcement but only warns on enable', () => {
  const f = feature('change_freeze')
  const old = harness('h1', { binary_version: '0.9.0' })
  const base = { feature: f, features: allFeatures(), harnesses: [old], scope: defaultScope(), role: 'admin', now: NOW }
  const enable = validateChange({ ...base, target: { enabled: true, enforced: false } })
  assert.deepEqual(enable.blockers, [])
  assert.ok(enable.warnings.some(w => w.includes('미충족')))
  const enforce = validateChange({ ...base, target: { enabled: true, enforced: true } })
  assert.ok(enforce.blockers.some(b => b.includes('미충족')))
})

test('offline harness warns instead of blocking; empty selected scope blocks', () => {
  const f = feature('change_freeze')
  const off = harness('h1', { status: 'revoked' })
  const v = validateChange({ feature: f, features: allFeatures(), harnesses: [off], target: { enabled: true, enforced: true }, scope: defaultScope(), role: 'admin', now: NOW })
  assert.deepEqual(v.blockers, [])
  assert.ok(v.warnings.some(w => w.includes('오프라인')))
  const empty = validateChange({ feature: f, features: allFeatures(), harnesses: [harness('h1')], target: { enabled: true, enforced: false }, scope: { type: 'selected', harness_ids: [], exceptions: [] }, role: 'admin', now: NOW })
  assert.ok(empty.blockers.some(b => b.includes('대상 하네스')))
})

test('no-op change and enforcing a disabled feature are blocked', () => {
  const f = feature('change_freeze', { enabled: true })
  const noop = validateChange({ feature: f, features: allFeatures(), harnesses: [], target: { enabled: true, enforced: false }, scope: defaultScope(), role: 'admin', now: NOW })
  assert.ok(noop.blockers.some(b => b.includes('변경 사항이 없습니다')))
  const bad = validateChange({ feature: feature('change_freeze'), features: allFeatures(), harnesses: [], target: { enabled: false, enforced: true }, scope: defaultScope(), role: 'admin', now: NOW })
  assert.ok(bad.blockers.some(b => b.includes('비활성화된 기능')))
})

test('preview shows the diff, scope, and per-result harness counts', () => {
  const f = feature('change_freeze')
  const evals = evaluateHarnesses(FEATURE_CATALOG.change_freeze, [harness('h1'), harness('h2', { status: 'revoked' }), harness('h3', { binary_version: '0.1.0' })], defaultScope(), NOW)
  const lines = buildPreview(f, { enabled: true, enforced: false }, defaultScope(), evals)
  assert.ok(lines.some(l => l.includes('활성화: 꺼짐 → 켜짐')))
  assert.ok(lines.some(l => l.includes('조직 전체')))
  assert.ok(lines.some(l => l.includes('적용 가능 1대') && l.includes('버전 미충족 1대') && l.includes('오프라인 1대')))
})

test('applyChange appends a versioned record and detects concurrent changes', () => {
  const f = feature('change_freeze')
  const evals = evaluateHarnesses(FEATURE_CATALOG.change_freeze, [harness('h1')], defaultScope(), NOW)
  const first = applyChange({ feature: f, target: { enabled: true, enforced: false }, scope: defaultScope(), reason: '감사 기간 동결', actor: 'admin@acme.kr', now: NOW_ISO, evals, expectedEpoch: 0 })
  assert.equal(first.error, undefined)
  assert.equal(first.record.epoch, 1)
  assert.equal(headEpochOf(first.config), 1)
  assert.equal(headEpochOf(''), 0)
  assert.equal(headEpochOf('not json'), 0)

  const f2 = feature('change_freeze', { enabled: true, config: first.config })
  const stale = applyChange({ feature: f2, target: { enabled: false, enforced: false }, scope: defaultScope(), reason: '해제', actor: 'admin@acme.kr', now: NOW_ISO, evals, expectedEpoch: 0 })
  assert.ok(stale.error.includes('동시 변경'))
  const second = applyChange({ feature: f2, target: { enabled: false, enforced: false }, scope: defaultScope(), reason: '해제', actor: 'admin@acme.kr', now: NOW_ISO, evals, expectedEpoch: 1 })
  assert.equal(second.record.epoch, 2)

  const missing = applyChange({ feature: f, target: { enabled: true, enforced: false }, scope: defaultScope(), reason: '  ', actor: 'a', now: NOW_ISO, evals, expectedEpoch: 0 })
  assert.ok(missing.error.includes('사유'))
})

test('rollback restores the prior state; rollback-of-rollback and missing epoch fail', () => {
  const f = feature('change_freeze')
  const evals = evaluateHarnesses(FEATURE_CATALOG.change_freeze, [harness('h1')], defaultScope(), NOW)
  const change = applyChange({ feature: f, target: { enabled: true, enforced: true }, scope: defaultScope(), reason: '강제 시작', actor: 'admin@acme.kr', now: NOW_ISO, evals, expectedEpoch: 0 })
  const f2 = feature('change_freeze', { enabled: true, enforced: true, config: change.config })

  const rb = buildRollback({ feature: f2, epoch: 1, reason: '하네스 장애로 원복', actor: 'admin@acme.kr', now: NOW_ISO, evals, expectedEpoch: 1 })
  assert.equal(rb.error, undefined)
  assert.deepEqual(rb.record.to, { enabled: false, enforced: false })
  assert.equal(rb.record.rollback_of, 1)

  // failed rollback: state already matches the target's from-state
  const f3 = feature('change_freeze', { config: rb.config })
  const again = buildRollback({ feature: f3, epoch: 1, reason: '재시도', actor: 'admin@acme.kr', now: NOW_ISO, evals, expectedEpoch: 2 })
  assert.ok(again.error.includes('동일'))
  // rollback record itself cannot be rolled back
  const rbRb = buildRollback({ feature: f3, epoch: 2, reason: '재시도', actor: 'admin@acme.kr', now: NOW_ISO, evals, expectedEpoch: 2 })
  assert.ok(rbRb.error.includes('롤백 기록'))
  // unknown epoch
  const missing = buildRollback({ feature: f2, epoch: 99, reason: '없는 기록', actor: 'admin@acme.kr', now: NOW_ISO, evals, expectedEpoch: 1 })
  assert.ok(missing.error.includes('실패'))
})
