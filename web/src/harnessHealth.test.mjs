import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  deriveHarnessHealth, HEARTBEAT_STALE_MS, ATTESTATION_STALE_MS,
  statusLabelKo, riskLabelKo,
} from './harnessHealth.ts'

const AT = 1787000000000 // fixed evaluation time (ms)
const hbNow = () => new Date(AT).toISOString()

test('healthy harness: all dimensions green, overall healthy', () => {
  const h = deriveHarnessHealth({
    status: 'active', risk_state: 'normal',
    last_heartbeat: hbNow(), last_attestation: hbNow(),
    binary_version: '1.2.0', version_blocked: false, at: AT,
  })
  assert.equal(h.overall, 'healthy')
  const labels = h.dimensions.map(d => d.state)
  assert.ok(labels.every(l => l === 'healthy' || l === 'unknown'))
})

test('expired heartbeat is warning and explained — green must not co-occur', () => {
  const staleAt = new Date(AT - HEARTBEAT_STALE_MS - 60_000).toISOString()
  const h = deriveHarnessHealth({
    status: 'active', risk_state: 'normal',
    last_heartbeat: staleAt, last_attestation: hbNow(),
    binary_version: '1.0.0', version_blocked: false, at: AT,
  })
  const hb = h.dimensions.find(d => d.key === 'heartbeat')
  assert.equal(hb.state, 'warning')
  assert.ok(hb.reason.includes('만료'))
  assert.ok(hb.observed)
  // overall is not healthy — the expired signal must surface.
  assert.equal(h.overall, 'warning')
  // no healthy heartbeat claim anywhere.
  assert.ok(!h.summary.includes('🟢'))
})

test('never-seen harness (no heartbeat) on live status is attention', () => {
  const h = deriveHarnessHealth({ status: 'active', risk_state: 'normal', last_heartbeat: '', at: AT })
  const hb = h.dimensions.find(d => d.key === 'heartbeat')
  assert.equal(hb.state, 'attention')
})

test('high risk is critical overall and wins over healthy heartbeat', () => {
  const h = deriveHarnessHealth({
    status: 'active', risk_state: 'high', last_heartbeat: hbNow(),
    last_attestation: hbNow(), binary_version: '1.0.0', version_blocked: false, at: AT,
  })
  assert.equal(h.overall, 'critical')
  const risk = h.dimensions.find(d => d.key === 'risk')
  assert.equal(risk.state, 'critical')
})

test('revoked harness is critical regardless of heartbeat/risk', () => {
  const h = deriveHarnessHealth({
    status: 'revoked', risk_state: 'normal', last_heartbeat: hbNow(), at: AT,
  })
  assert.equal(h.overall, 'critical')
  // heartbeat must not claim healthy for a revoked harness
  const hb = h.dimensions.find(d => d.key === 'heartbeat')
  assert.equal(hb.state, 'unknown')
  assert.ok(hb.reason.includes('폐기'))
})

test('quarantined harness is warning lifecycle', () => {
  const h = deriveHarnessHealth({ status: 'quarantined', risk_state: 'normal', last_heartbeat: hbNow(), at: AT })
  assert.equal(h.overall, 'warning')
})

test('stale attestation beyond 24h is attention', () => {
  const oldAtt = new Date(AT - ATTESTATION_STALE_MS - 3600_000).toISOString()
  const h = deriveHarnessHealth({ status: 'active', risk_state: 'normal', last_heartbeat: hbNow(), last_attestation: oldAtt, at: AT })
  const att = h.dimensions.find(d => d.key === 'attestation')
  assert.equal(att.state, 'attention')
})

test('version floor breach is critical', () => {
  const h = deriveHarnessHealth({ status: 'active', risk_state: 'normal', last_heartbeat: hbNow(), binary_version: '0.9.0', version_blocked: true, at: AT })
  assert.equal(h.overall, 'critical')
  assert.ok(h.dimensions.find(d => d.key === 'version') .reason.includes('하한'))
})

test('raw enum values are replaced by governed Korean labels', () => {
  assert.equal(statusLabelKo('enrolled'), '등록됨')
  assert.equal(statusLabelKo('quarantined'), '격리')
  assert.equal(riskLabelKo('elevated'), '주의')
  assert.equal(riskLabelKo('high'), '높음')
})

test('clock-skew tolerance: exact threshold boundary is still healthy', () => {
  const hbJustInside = new Date(AT - HEARTBEAT_STALE_MS + 1).toISOString()
  const h = deriveHarnessHealth({ status: 'active', risk_state: 'normal', last_heartbeat: hbJustInside, last_attestation: hbNow(), at: AT })
  assert.equal(h.dimensions.find(d => d.key === 'heartbeat').state, 'healthy')
})
