import { test } from 'node:test'
import assert from 'node:assert/strict'

import { HV_STATE_KO, isValidVersion, deadlineTone } from './releaseCampaignView.ts'

test('every harness version state has a Korean label', () => {
  for (const s of ['supported', 'update_available', 'update_required_grace', 'restricted', 'revoked',
    'updating', 'verifying', 'rollback_required', 'repair_required', 'unknown_or_tampered']) {
    assert.ok(HV_STATE_KO[s], s)
  }
})

test('isValidVersion mirrors the canonical parser acceptance', () => {
  for (const ok of ['1.2.3', 'v1.2.3', '1.2.3-beta.1', '2.0.0-rc.1']) assert.equal(isValidVersion(ok), true)
  for (const bad of ['1.2', '1.2.x', 'dev', '']) assert.equal(isValidVersion(bad), false)
})

test('deadlineTone flags past and imminent deadlines', () => {
  const now = new Date('2026-08-19T12:00:00Z')
  assert.equal(deadlineTone(undefined, now), 'none')
  assert.equal(deadlineTone('2026-08-19T13:00:00Z', now), 'soon')
  assert.equal(deadlineTone('2026-08-18T00:00:00Z', now), 'past')
  assert.equal(deadlineTone('2026-09-01T00:00:00Z', now), 'none')
})
