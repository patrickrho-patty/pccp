import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  sessionActionView, changeSetView, findingView, decisionView, replayEventView,
  sessionEvidenceRoute,
  auditCategoryOf, auditCategory, auditActorLabel, auditActorRoute,
  auditResourceLabel, auditResourceRoute, auditEventView, auditResultLabel,
  groupAuditBursts,
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

test('audit taxonomy classifies event types into canonical categories', () => {
  assert.equal(auditCategoryOf('cp.compliance.assessed'), 'compliance')
  assert.equal(auditCategoryOf('cp.session.opened'), 'session')
  assert.equal(auditCategoryOf('cp.fleet.quarantined'), 'harness')
  assert.equal(auditCategoryOf('cp.model.published'), 'model')
  assert.equal(auditCategoryOf('cp.user.created'), 'user')
  assert.equal(auditCategoryOf('enterprise.feature.violation'), 'security')
  assert.equal(auditCategoryOf('cp.tool.request'), 'tool')
  assert.equal(auditCategoryOf('unknown.thing'), 'system')
  assert.equal(auditCategoryOf(undefined), 'system')
  assert.equal(auditCategory('compliance').labelKo, '컴플라이언스')
  assert.equal(auditCategory('nope').labelKo, '시스템')
})

test('actor labels and routes', () => {
  assert.equal(auditActorLabel('admin'), '관리자')
  assert.equal(auditActorLabel('harness'), '하네스')
  assert.equal(auditActorLabel(undefined), '시스템')
  assert.equal(auditActorRoute('harness', 'hrn_1'), '/fleet?harness_id=hrn_1')
  assert.equal(auditActorRoute('user', 'usr_x'), '/users/usr_x')
  assert.equal(auditActorRoute('system', 'svc'), undefined)
})

test('resource labels and exact routes', () => {
  assert.equal(auditResourceLabel('session'), '세션')
  assert.equal(auditResourceRoute('session', 'ses_1'), '/sessions/ses_1')
  assert.equal(auditResourceRoute('user', 'usr_x'), '/users/usr_x')
  assert.equal(auditResourceRoute('finding', 'fn_1'), '/findings/fn_1')
  assert.equal(auditResourceRoute('repository', 'repo_1'), '/repositories/repo_1')
  assert.equal(auditResourceRoute('policy_rule', 'pr_1'), undefined)
  assert.equal(auditResourceRoute(undefined, 'x'), undefined)
})

test('auditEventView renders Korean summary without raw primary keys', () => {
  const v = auditEventView({
    event_type: 'cp.compliance.assessed', actor_type: 'admin', actor_id: 'usr_adm1',
    action: 'assessed', resource_type: 'repository', resource_id: 'repo_1', result: 'success',
  })
  assert.equal(v.categoryLabelKo, '컴플라이언스')
  assert.equal(v.actorLabel, '관리자')
  assert.equal(v.actorRoute, '/users/usr_adm1')
  assert.equal(v.resourceLabel, '저장소')
  assert.equal(v.resourceRoute, '/repositories/repo_1')
  assert.match(v.title, /관리자/)
  assert.match(v.title, /저장소/)
  assert.match(v.title, /성공/)
  assert.equal(v.outcome, '성공')
})

test('result labels + outcome severity', () => {
  assert.equal(auditResultLabel('denied'), '거부')
  assert.equal(auditResultLabel(''), '미기록')
  const denied = auditEventView({ event_type: 'cp.tool.request', result: 'denied' })
  assert.equal(denied.color, 'bg-red-50 text-red-700 border-red-200')
  const success = auditEventView({ event_type: 'cp.tool.request', result: 'success' })
  assert.equal(success.icon, '🟢')
})

test('burst grouping collapses repeats but preserves every record', () => {
  const base = { actor_id: 'sys1', event_type: 'cp.compliance.assessed' }
  const events = [
    { ...base, id: 'a1', occurred_at: '2026-08-18T10:00:00Z' },
    { ...base, id: 'a2', occurred_at: '2026-08-18T10:00:00Z' },
    { ...base, id: 'a3', occurred_at: '2026-08-18T10:00:00Z' },
    { ...base, id: 'a4', occurred_at: '2026-08-18T10:02:00Z' }, // different minute → new group
  ]
  const { rows } = groupAuditBursts(events)
  assert.equal(rows.length, 2)
  assert.equal(rows[0].count, 3)
  assert.equal(rows[1].count, 1)
  // every underlying record survives in the grouped row's items array
  const total = rows.reduce((n, r) => n + r.items.length, 0)
  assert.equal(total, 4)
})
