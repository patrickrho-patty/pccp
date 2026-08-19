import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  sessionActionView, changeSetView, findingView, decisionView, replayEventView,
  sessionEvidenceRoute,
} from './evidenceView.ts'

test('allowed inference action renders Korean summary + success meta', () => {
  const v = sessionActionView({ action_type: 'ai_inference', verdict_result: 'allowed', user_id: 'usr_abc123', repository_id: 'repo_x' })
  assert.ok(v.title.includes('AI 추론을 수행'))
  assert.match(v.icon, /🟢/)
  assert.ok(!v.title.toUpperCase().includes('AI_INFERENCE'))
})

test('denied action is danger outcome', () => {
  const v = sessionActionView({ action_type: 'file_write', verdict_result: 'denied', user_id: 'u' })
  assert.match(v.icon, /🔴/)
})

test('change set surfaces files/diff and attribution', () => {
  const v = changeSetView({ summary: 'refund logic', files_changed: '["a.go","b.go"]', lines_added: 5, lines_removed: 2, attribution_state: 'AI_GENERATED' })
  assert.ok(v.title.includes('refund logic'))
  assert.ok(v.outcome.includes('+5'))
})

test('finding resolves to exact detail route', () => {
  const v = findingView({ id: 'fn_1', title_ko: '주입 공격', severity: 'high', status: 'open' })
  assert.equal(v.route, '/findings/fn_1')
  assert.match(v.icon, /🔴/)
})

test('allowed decision resolves to success + korean', () => {
  const v = decisionView({ verdict: 'allowed', model_package_id: 'pmp_m1', policy_epoch_id: 'ep_1' })
  assert.ok(v.title.includes('허용'))
  assert.equal(v.icon, '🟢')
})

test('raw enum never leaks into primary title (replay)', () => {
  const v = replayEventView({ kind: 'change_set', payload: { id: 'cs_verylongid_12345678' } })
  assert.ok(!v.title.includes('change_set'))
})

test('provenance route is exact', () => {
  assert.equal(sessionEvidenceRoute('sess_1', 'provenance'), '/sessions/sess_1/provenance')
})
