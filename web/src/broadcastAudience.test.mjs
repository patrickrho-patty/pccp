import test from 'node:test'
import assert from 'node:assert/strict'

import {
  LARGE_AUDIENCE_THRESHOLD,
  audienceSizeOf,
  broadcastSendBlockers,
  exclusionReasonKo,
  mergeReachability,
  renderBroadcastText,
  resolveAudiencePreview,
} from './broadcastAudience.ts'

const USERS = [
  { id: 'u1', name: 'Ana', name_ko: '안나', email: 'ana@corp.kr', status: 'active' },
  { id: 'u2', name: 'Ben', name_ko: '벤', email: 'ben@corp.kr', status: 'suspended' },
  { id: 'u3', name: 'Cho', name_ko: '조', email: 'cho@corp.kr', status: 'offboarded' },
  { id: 'u1', name: 'Ana dup', email: 'ana@corp.kr', status: 'active' }, // duplicate id
]

test('org scope splits eligible vs suspended/offboarded and dedupes', () => {
  const p = resolveAudiencePreview(USERS, { type: 'org' })
  assert.deepEqual(p.eligible.map(u => u.id), ['u1'])
  assert.deepEqual(p.excluded.map(e => [e.user.id, e.reason]), [['u2', 'suspended'], ['u3', 'offboarded']])
})

test('user scope picks exactly the target; unknown target yields zero audience', () => {
  assert.deepEqual(resolveAudiencePreview(USERS, { type: 'user', targetId: 'u1' }).eligible.map(u => u.id), ['u1'])
  assert.equal(resolveAudiencePreview(USERS, { type: 'user', targetId: 'missing' }).eligible.length, 0)
  assert.equal(resolveAudiencePreview(USERS, { type: 'user', targetId: '' }).eligible.length, 0)
})

test('project scope follows the roster; empty roster yields zero audience', () => {
  const p = resolveAudiencePreview(USERS, { type: 'project', targetId: 'p1' }, new Set(['u1', 'u2']))
  assert.deepEqual(p.eligible.map(u => u.id), ['u1'])
  assert.deepEqual(p.excluded.map(e => e.user.id), ['u2'])
  assert.equal(resolveAudiencePreview(USERS, { type: 'project', targetId: 'p2' }, new Set()).eligible.length, 0)
})

test('no scope selected yields an empty preview (send gate blocks separately)', () => {
  const p = resolveAudiencePreview(USERS, { type: '' })
  assert.equal(p.eligible.length, 0)
  assert.equal(p.excluded.length, 0)
})

test('audience re-resolution reflects recipient changes before send', () => {
  const before = resolveAudiencePreview(USERS, { type: 'org' }).eligible.length
  const after = resolveAudiencePreview([...USERS, { id: 'u9', name: 'New', status: 'active' }], { type: 'org' }).eligible.length
  assert.equal(before, 1)
  assert.equal(after, 2)
})

test('reachability marks missing/offline presence as unreachable', () => {
  const r = mergeReachability(
    [{ id: 'u1' }, { id: 'u2' }, { id: 'u3' }],
    [{ user_id: 'u1', status: 'online' }, { user_id: 'u2', status: 'offline' }],
  )
  assert.deepEqual(r.rows.map(x => [x.id, x.reachable]), [['u1', true], ['u2', false], ['u3', false]])
  assert.equal(r.online, 1)
  assert.equal(r.offline, 2)
})

test('locale fallback renders the available side', () => {
  const bc = { title: 'Maintenance', title_ko: '', body: 'Restart at 9', body_ko: '9시 재시작' }
  assert.deepEqual(renderBroadcastText(bc, 'ko-KR'), { title: 'Maintenance', body: '9시 재시작' })
  assert.deepEqual(renderBroadcastText(bc, 'en-US'), { title: 'Maintenance', body: 'Restart at 9' })
  const koOnly = { title: '', title_ko: '점검', body: '', body_ko: '본문' }
  assert.deepEqual(renderBroadcastText(koOnly, 'en-US'), { title: '점검', body: '본문' })
})

test('audienceSizeOf reads the frozen snapshot; null when absent or malformed', () => {
  assert.equal(audienceSizeOf({ audience: '{"eligible_ids":["u1","u2"],"excluded":[],"resolved_at":"t"}' }), 2)
  assert.equal(audienceSizeOf({ audience: '{"eligible_ids":[]}' }), 0)
  assert.equal(audienceSizeOf({}), null)
  assert.equal(audienceSizeOf({ audience: '' }), null)
  assert.equal(audienceSizeOf({ audience: 'not json' }), null)
  assert.equal(audienceSizeOf({ audience: '{"foo":1}' }), null)
})

test('exclusionReasonKo delegates to the canonical STATUS_KO labels', () => {
  assert.equal(exclusionReasonKo('suspended'), '정지')
  assert.equal(exclusionReasonKo('offboarded'), '퇴사') // canonical STATUS_KO
})

const GATE_BASE = {
  title: 't',
  scope: { type: 'org' },
  eligibleCount: 3,
  severity: 'info',
  confirmReason: '',
  allowEmpty: false,
  confirmLarge: false,
}

test('send gate: scope, title and target are mandatory', () => {
  assert.ok(broadcastSendBlockers({ ...GATE_BASE, title: ' ' }).some(m => m.includes('제목')))
  assert.ok(broadcastSendBlockers({ ...GATE_BASE, scope: { type: '' } }).some(m => m.includes('수신 대상 범위')))
  assert.ok(broadcastSendBlockers({ ...GATE_BASE, scope: { type: 'user', targetId: '' } }).some(m => m.includes('대상을 선택')))
  assert.deepEqual(broadcastSendBlockers(GATE_BASE), [])
})

test('send gate: zero audience blocked unless explicitly confirmed', () => {
  assert.equal(broadcastSendBlockers({ ...GATE_BASE, eligibleCount: 0 }).length, 1)
  assert.deepEqual(broadcastSendBlockers({ ...GATE_BASE, eligibleCount: 0, allowEmpty: true }), [])
})

test('send gate: large audience needs explicit confirmation', () => {
  const big = { ...GATE_BASE, eligibleCount: LARGE_AUDIENCE_THRESHOLD + 1 }
  assert.ok(broadcastSendBlockers(big).some(m => m.includes('대규모')))
  assert.deepEqual(broadcastSendBlockers({ ...big, confirmLarge: true }), [])
  assert.deepEqual(broadcastSendBlockers({ ...GATE_BASE, eligibleCount: LARGE_AUDIENCE_THRESHOLD }), [])
})

test('send gate: critical/emergency require a confirmation reason', () => {
  assert.ok(broadcastSendBlockers({ ...GATE_BASE, severity: 'critical' }).some(m => m.includes('사유')))
  assert.ok(broadcastSendBlockers({ ...GATE_BASE, severity: 'emergency' }).some(m => m.includes('사유')))
  assert.deepEqual(broadcastSendBlockers({ ...GATE_BASE, severity: 'emergency', confirmReason: '긴급 패치' }), [])
  assert.deepEqual(broadcastSendBlockers({ ...GATE_BASE, severity: 'warning' }), [])
})
